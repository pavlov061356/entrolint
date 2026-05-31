package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
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

	fileDeltas := make([]thermo.FileDelta, 0, len(diff.Files))
	linesChanged := 0
	for _, c := range diff.Files {
		if !isGoPath(c) {
			continue
		}
		fd, ok := scoreChange(runner, engine, baseSHA, headSHA, c)
		if !ok {
			continue
		}
		fileDeltas = append(fileDeltas, fd)
		linesChanged += c.LinesChanged()
	}

	return CheckResult{
		Base:    baseSHA,
		Head:    headSHA,
		Delta:   thermo.ComputeDelta(fileDeltas, linesChanged),
		Skipped: diff.Skipped,
	}, nil
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
// Returns (zero, false) when neither side can be parsed — the change
// is silently excluded from the aggregate (it cannot meaningfully
// contribute to ΔS without a numeric S on at least one side).
func scoreChange(runner gitx.Runner, e *thermo.Engine, baseSHA, headSHA string, c gitx.Change) (thermo.FileDelta, bool) {
	switch c.Kind {
	case gitx.ChangeAdded:
		sHead, ok := scoreBlob(runner, e, headSHA, c.Path)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaAdded, 0, sHead), true
	case gitx.ChangeRemoved:
		sBase, ok := scoreBlob(runner, e, baseSHA, c.Path)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaRemoved, sBase, 0), true
	case gitx.ChangeRenamed:
		sBase, okB := scoreBlob(runner, e, baseSHA, c.OldPath)
		sHead, okH := scoreBlob(runner, e, headSHA, c.Path)
		if !okB && !okH {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaModified, sBase, sHead), true
	default: // ChangeModified (and any unrecognized kind)
		sBase, okB := scoreBlob(runner, e, baseSHA, c.Path)
		sHead, okH := scoreBlob(runner, e, headSHA, c.Path)
		if !okB && !okH {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaModified, sBase, sHead), true
	}
}

// scoreBlob fetches `ref:path` and returns the structural entropy S of
// that file under the calibrated engine. Returns (0, false) on either
// fetch error (e.g. file not present at ref) or parse failure (the file
// at that ref was syntactically broken Go).
func scoreBlob(runner gitx.Runner, e *thermo.Engine, ref, path string) (float64, bool) {
	blob, err := gitx.FileAtRef(runner, ref, path)
	if err != nil {
		return 0, false
	}
	f, ok := golang.ParseGoBytes(path, blob)
	if !ok {
		return 0, false
	}
	return e.Score(f).S, true
}

func isGoPath(c gitx.Change) bool {
	// For renames the head-side Path is what survives; for deletions
	// Path holds the base-side path. In both cases checking the head
	// path is the relevant filter — a `.txt → .go` rename should be
	// scored, a `.go → .md` rename treated as a Go removal at base,
	// not scored at head. v0.1 keeps the simpler rule: include only if
	// the head-visible name ends in `.go`. The rare cross-type rename
	// will round-trip as a skip; we'll revisit when it actually shows
	// up in dogfooding.
	return strings.HasSuffix(c.Path, ".go")
}
