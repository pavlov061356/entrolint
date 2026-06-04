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

	baseBlobs, headBlobs, err := fetchAllBlobs(runner, diff.Files, baseSHA, headSHA)
	if err != nil {
		return CheckResult{}, fmt.Errorf("fetch blobs %s...%s: %w", baseSHA, headSHA, err)
	}

	fileDeltas := make([]thermo.FileDelta, 0, len(diff.Files))
	linesChanged := 0
	for _, c := range diff.Files {
		if !isGoPath(c) {
			continue
		}
		fd, ok := scoreFromCache(engine, baseBlobs, headBlobs, c)
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
	scalingResult := scaling.Analyze(scaling.Input{
		Changes:   diff.Files,
		Patches:   patches,
		Root:      rootAbs,
		BaseBlobs: baseBlobs,
		HeadBlobs: headBlobs,
	})
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

// fetchAllBlobs pulls every `.go` change's base and head content in a
// single pass and returns the two per-path maps that thermo scoring
// and scaling detectors both consume. Fatal errors (ErrUnavailable,
// ErrInvalidRef, etc.) propagate — silently dropping the blob would
// hand thermo and scaling a partial corpus and produce an unsafe
// verdict. The only swallowed case is ErrNotAtRef on a side that
// might legitimately be absent (a rename's OldPath after the tree
// pruned, or a mid-rebase missing blob); we treat that as soft and
// the consumer drops the file from its result.
func fetchAllBlobs(runner gitx.Runner, files []gitx.Change, baseSHA, headSHA string) (map[string][]byte, map[string][]byte, error) {
	baseBlobs := make(map[string][]byte)
	headBlobs := make(map[string][]byte)
	for _, c := range files {
		if !isGoPath(c) {
			continue
		}
		if err := fetchSide(runner, c.Kind != gitx.ChangeAdded, baseSHA, basePath(c), c.Path, baseBlobs); err != nil {
			return nil, nil, err
		}
		if err := fetchSide(runner, c.Kind != gitx.ChangeRemoved, headSHA, c.Path, c.Path, headBlobs); err != nil {
			return nil, nil, err
		}
	}
	return baseBlobs, headBlobs, nil
}

func basePath(c gitx.Change) string {
	if c.Kind == gitx.ChangeRenamed && c.OldPath != "" {
		return c.OldPath
	}
	return c.Path
}

// fetchSide pulls one blob and stores it in `into` keyed by `key`.
// `want=false` skips the fetch entirely (used when the side does not
// exist for this change Kind). ErrNotAtRef is soft (blob simply
// absent); anything else is fatal.
func fetchSide(runner gitx.Runner, want bool, ref, path, key string, into map[string][]byte) error {
	if !want {
		return nil
	}
	blob, err := gitx.FileAtRef(runner, ref, path)
	if err != nil {
		if errors.Is(err, gitx.ErrNotAtRef) {
			return nil
		}
		return err
	}
	into[key] = blob
	return nil
}

// scoreFromCache turns a single diff entry into a thermo.FileDelta
// using the pre-fetched blob maps. Returns ok=false when the change
// should be silently excluded (parse failure on either side of a
// Modified/Renamed entry — the asymmetry would otherwise read as a
// fake refactor).
func scoreFromCache(e *thermo.Engine, baseBlobs, headBlobs map[string][]byte, c gitx.Change) (thermo.FileDelta, bool) {
	switch c.Kind {
	case gitx.ChangeAdded:
		s, ok := scoreBlob(e, headBlobs[c.Path], c.Path)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaAdded, 0, s), true
	case gitx.ChangeRemoved:
		s, ok := scoreBlob(e, baseBlobs[c.Path], c.Path)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaRemoved, s, 0), true
	default: // ChangeModified or ChangeRenamed
		sBase, okB := scoreBlob(e, baseBlobs[c.Path], c.Path)
		sHead, okH := scoreBlob(e, headBlobs[c.Path], c.Path)
		if !okB || !okH {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaModified, sBase, sHead), true
	}
}

// scoreBlob parses + scores a single blob. ok=false means the blob is
// missing or unparseable — both treated identically by ComputeDelta
// callers per the symmetric soft-miss rule.
func scoreBlob(e *thermo.Engine, blob []byte, path string) (float64, bool) {
	if len(blob) == 0 {
		return 0, false
	}
	f, ok := golang.ParseGoBytes(path, blob)
	if !ok {
		return 0, false
	}
	return e.Score(f).S, true
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

// isGoPath gates which diff entries enter ΔS — only `.go` paths under
// directories the analyzer would walk during calibration. Without the
// shared exclusion, a PR touching `vendor/foo.go` would be scored
// against a lognormal CDF fit on a corpus that explicitly skipped
// vendor, inflating ΔS by a frame the user did not opt into.
func isGoPath(c gitx.Change) bool {
	return golang.IsAnalyzablePath(c.Path)
}
