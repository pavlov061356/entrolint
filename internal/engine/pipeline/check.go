package pipeline

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// CheckOptions configures one `entrolint check` run.
type CheckOptions struct {
	// Root is the working tree to calibrate against — usually the
	// repository checkout where the head ref is currently materialized.
	Root string

	// Base, Head are git refs (branch, tag, SHA, HEAD~N). The diff is
	// computed with triple-dot semantics so the result matches what a
	// PR shows on GitHub.
	Base, Head string

	// ScanOptions carries Config + cache settings shared with Scan.
	// Root inside ScanOptions is ignored; this struct's Root wins.
	ScanOptions ScanOptions

	// GitRunner is the gitx.Runner used for diff/cat-file/rev-parse.
	// If nil, a LocalRunner anchored at Root is used. Tests inject a
	// fake here.
	GitRunner gitx.Runner
}

// CheckResult is the typed output of the check pipeline. The CLI layer
// emits this as JSON or formats a human-readable summary.
type CheckResult struct {
	Base    string             `json:"base"`
	Head    string             `json:"head"`
	Delta   thermo.Delta       `json:"delta"`
	Scaling scaling.Result     `json:"scaling"`
	Skipped []gitx.SkippedPath `json:"skipped,omitempty"`
}

// errSoftMiss marks scoreBlob failures the scorer should silently skip
// — file not present at ref (legitimate for the opposite side of Add /
// Remove) or syntactically broken Go (S is undefined for the ref). Any
// other error from scoreBlob is fatal: gitx.ErrUnavailable, ErrInvalidRef
// and the like mean the gate's verdict would be computed from a
// partial corpus, which is more dangerous than failing the run.
var errSoftMiss = errors.New("pipeline: soft miss")

// Check computes ΔS between base and head and aggregates it into a
// thermo.Delta the CLI can gate on. v0.1 scores Go files only —
// non-`.go` paths in the diff are silently excluded from both Total
// and lines_changed so the density ratio stays meaningful as ΔS per
// Go-line. Paths git classified as binary/submodule/symlink/mode-only
// surface verbatim on CheckResult.Skipped for the report.
func Check(opts CheckOptions) (CheckResult, error) {
	rootAbs, err := filepath.Abs(opts.Root)
	if err != nil {
		return CheckResult{}, err
	}
	runner := opts.GitRunner
	if runner == nil {
		runner = gitx.LocalRunner{Dir: rootAbs}
	}

	baseSHA, err := gitx.ResolveRef(runner, opts.Base)
	if err != nil {
		return CheckResult{}, fmt.Errorf("resolve base %q: %w", opts.Base, err)
	}
	headSHA, err := gitx.ResolveRef(runner, opts.Head)
	if err != nil {
		return CheckResult{}, fmt.Errorf("resolve head %q: %w", opts.Head, err)
	}

	engine, err := calibrateForCheck(opts)
	if err != nil {
		return CheckResult{}, err
	}

	diff, err := gitx.Diff(runner, baseSHA, headSHA)
	if err != nil {
		return CheckResult{}, fmt.Errorf("diff %s...%s: %w", baseSHA, headSHA, err)
	}

	fileDeltas := make([]thermo.FileDelta, 0, len(diff.Files))
	linesChanged := 0
	for _, c := range diff.Files {
		if !isGoPath(c) {
			continue
		}
		fd, ok, err := scoreChange(runner, engine, baseSHA, headSHA, c)
		if err != nil {
			return CheckResult{}, fmt.Errorf("score %s: %w", c.Path, err)
		}
		if !ok {
			continue
		}
		fileDeltas = append(fileDeltas, fd)
		linesChanged += c.LinesChanged()
	}

	patches, err := diff.Patches(runner)
	if err != nil {
		return CheckResult{}, fmt.Errorf("patches %s...%s: %w", baseSHA, headSHA, err)
	}
	scalingResult := scaling.Analyze(scaling.Input{Changes: diff.Files, Patches: patches})
	delta := thermo.ComputeDelta(fileDeltas, linesChanged)
	if scalingResult.DowngradeBonus != 0 {
		delta = applyScalingBonus(delta, scalingResult.DowngradeBonus)
	}

	return CheckResult{
		Base:    baseSHA,
		Head:    headSHA,
		Delta:   delta,
		Scaling: scalingResult,
		Skipped: diff.Skipped,
	}, nil
}

// applyScalingBonus folds the (always-negative) downgrade reward into
// ΔS_total per docs/scaling.md §"Downgrade reward". Density re-derives
// from the new total against d.LinesChanged — the same denominator
// ComputeDelta already used, so the two stay coherent.
func applyScalingBonus(d thermo.Delta, bonus float64) thermo.Delta {
	denom := d.LinesChanged
	if denom < 1 {
		denom = 1
	}
	d.Total += bonus
	d.Density = d.Total / float64(denom)
	return d
}

// calibrateForCheck fits (or loads from cache) the engine against the
// working tree at Root. Calibration uses the same logic as Scan so a
// single `.entrolint-cache.json` is shared between commands.
func calibrateForCheck(opts CheckOptions) (*thermo.Engine, error) {
	scanOpts := opts.ScanOptions
	scanOpts.Root = opts.Root
	files, err := analyzeTree(scanOpts)
	if err != nil {
		return nil, fmt.Errorf("calibration tree walk: %w", err)
	}
	return resolveEngine(scanOpts, structuralMicrostates(), files), nil
}

// scoreChange turns a single diff entry into a thermo.FileDelta.
//
// Return shape: (delta, ok, err). err != nil means a fatal upstream
// failure (git binary unavailable, ref lost mid-run) — the caller must
// propagate it. ok=false with nil err means the change should be
// silently excluded (one side parsed-failed, file not present at the
// canonical ref, etc.). For Modified/Renamed the rule is symmetric:
// EITHER side soft-missing drops the change rather than emit a
// half-measured Δ that defeats the gate (a parseable base + broken
// head would otherwise read as a beneficial refactor of -sBase).
func scoreChange(runner gitx.Runner, e *thermo.Engine, baseSHA, headSHA string, c gitx.Change) (thermo.FileDelta, bool, error) {
	switch c.Kind {
	case gitx.ChangeAdded:
		sHead, err := scoreBlob(runner, e, headSHA, c.Path)
		if err != nil {
			return thermo.FileDelta{}, false, softOrFatal(err)
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaAdded, 0, sHead), true, nil
	case gitx.ChangeRemoved:
		sBase, err := scoreBlob(runner, e, baseSHA, c.Path)
		if err != nil {
			return thermo.FileDelta{}, false, softOrFatal(err)
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaRemoved, sBase, 0), true, nil
	case gitx.ChangeRenamed:
		return finalizeModified(c.Path, runner, e, baseSHA, c.OldPath, headSHA, c.Path)
	default: // ChangeModified (and any unrecognized kind)
		return finalizeModified(c.Path, runner, e, baseSHA, c.Path, headSHA, c.Path)
	}
}

// finalizeModified scores both sides of a Modified/Renamed entry. A
// fatal error on either side propagates; a soft miss on either side
// drops the entry — matching the symmetry the Add/Remove branches have
// for free.
func finalizeModified(reportPath string, runner gitx.Runner, e *thermo.Engine, baseSHA, basePath, headSHA, headPath string) (thermo.FileDelta, bool, error) {
	sBase, errB := scoreBlob(runner, e, baseSHA, basePath)
	sHead, errH := scoreBlob(runner, e, headSHA, headPath)
	if err := fatalOf(errB, errH); err != nil {
		return thermo.FileDelta{}, false, err
	}
	if errB != nil || errH != nil {
		return thermo.FileDelta{}, false, nil
	}
	return thermo.MakeFileDelta(reportPath, thermo.DeltaModified, sBase, sHead), true, nil
}

// softOrFatal returns nil for a soft miss (drop the change) and the
// original error otherwise (propagate as fatal). Used by Add/Remove
// branches that only score one side.
func softOrFatal(err error) error {
	if errors.Is(err, errSoftMiss) {
		return nil
	}
	return err
}

// fatalOf returns the first non-soft error among the two, or nil if
// both are nil or both are soft misses.
func fatalOf(errs ...error) error {
	for _, err := range errs {
		if err != nil && !errors.Is(err, errSoftMiss) {
			return err
		}
	}
	return nil
}

// scoreBlob fetches `ref:path` and returns the structural entropy S of
// that file under the calibrated engine.
//
// Error contract: nil → use the returned S. errSoftMiss → the change
// should be silently excluded (file legitimately absent at ref, or
// blob is syntactically broken Go and has no defined S). Anything else
// is fatal (git binary went away mid-loop, shallow clone lost a
// resolved SHA, ...) — the caller must propagate it to the user so the
// gate does not ship a verdict computed from a partial corpus.
func scoreBlob(runner gitx.Runner, e *thermo.Engine, ref, path string) (float64, error) {
	blob, err := gitx.FileAtRef(runner, ref, path)
	if err != nil {
		if errors.Is(err, gitx.ErrNotAtRef) {
			return 0, errSoftMiss
		}
		return 0, err
	}
	f, ok := golang.ParseGoBytes(path, blob)
	if !ok {
		return 0, errSoftMiss
	}
	return e.Score(f).S, nil
}

// isGoPath gates which diff entries enter ΔS — only `.go` paths under
// directories the analyzer would walk during calibration. Without the
// shared exclusion, a PR touching `vendor/foo.go` would be scored
// against a lognormal CDF fit on a corpus that explicitly skipped
// vendor, inflating ΔS by a frame the user did not opt into.
func isGoPath(c gitx.Change) bool {
	return golang.IsAnalyzablePath(c.Path)
}
