package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/corpus"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
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

	var baseCx, headCx microstate.CrossFileSource
	if crossDupEnabled(opts.ScanOptions.Config.Weights) {
		excludeBase, excludeHead := asymmetricParseExclusions(diff.Files, baseBlobs, headBlobs)
		baseCx, headCx = buildCorpora(runner, baseSHA, headSHA, diff.Files, baseBlobs, headBlobs, excludeBase, excludeHead)
	}

	fileDeltas := make([]thermo.FileDelta, 0, len(diff.Files))
	linesChanged := 0
	for _, c := range diff.Files {
		if !isGoPath(c) {
			continue
		}
		fd, ok := scoreFromCache(engine, baseBlobs, headBlobs, baseCx, headCx, c)
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

// fetchAllBlobs pulls every `.go` change's base and head content and
// returns the two per-path maps that thermo scoring and the scaling
// detectors both consume, each keyed by the change's HEAD path (c.Path)
// so a rename's two sides line up under one key. Both sides are fetched
// with a single batched cat-file per ref — the dominant performance lever
// over one fork per file. Fatal errors (ErrUnavailable, ErrInvalidRef)
// propagate: silently dropping a blob would hand thermo and scaling a
// partial corpus and an unsafe verdict. A blob simply absent at its ref
// (a rename's OldPath after the tree pruned, or a mid-rebase missing blob)
// is a soft miss — BlobsAtRef omits it and the consumer drops the file.
func fetchAllBlobs(runner gitx.Runner, files []gitx.Change, baseSHA, headSHA string) (map[string][]byte, map[string][]byte, error) {
	var basePaths, headPaths []string
	for _, c := range files {
		if !isGoPath(c) {
			continue
		}
		if c.Kind != gitx.ChangeAdded {
			basePaths = append(basePaths, basePath(c))
		}
		if c.Kind != gitx.ChangeRemoved {
			headPaths = append(headPaths, c.Path)
		}
	}
	baseAtPath, err := gitx.BlobsAtRef(runner, baseSHA, basePaths)
	if err != nil {
		return nil, nil, err
	}
	headBlobs, err := gitx.BlobsAtRef(runner, headSHA, headPaths)
	if err != nil {
		return nil, nil, err
	}
	// headBlobs is already keyed by c.Path (the head tree path). Re-key the
	// base side from its base-tree path to c.Path so a rename's base and
	// head share a key.
	baseBlobs := make(map[string][]byte, len(basePaths))
	for _, c := range files {
		if !isGoPath(c) || c.Kind == gitx.ChangeAdded {
			continue
		}
		if b, ok := baseAtPath[basePath(c)]; ok {
			baseBlobs[c.Path] = b
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

// scoreFromCache turns a single diff entry into a thermo.FileDelta
// using the pre-fetched blob maps and the per-ref cross-file corpora.
// Returns ok=false when the change should be silently excluded (parse
// failure on either side of a Modified/Renamed entry — the asymmetry
// would otherwise read as a fake refactor).
//
// The base side is keyed and parsed under basePath(c) (OldPath for a
// rename) so the base corpus — enumerated by base-side ls-tree paths —
// is queried with the name it knows; using the head-side c.Path would
// miss a renamed file's cross-file mass and manufacture spurious ΔS.
func scoreFromCache(e *thermo.Engine, baseBlobs, headBlobs map[string][]byte, baseCx, headCx microstate.CrossFileSource, c gitx.Change) (thermo.FileDelta, bool) {
	switch c.Kind {
	case gitx.ChangeAdded:
		s, ok := scoreBlob(e, headBlobs[c.Path], c.Path, headCx)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaAdded, 0, s), true
	case gitx.ChangeRemoved:
		s, ok := scoreBlob(e, baseBlobs[c.Path], basePath(c), baseCx)
		if !ok {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaRemoved, s, 0), true
	default: // ChangeModified or ChangeRenamed
		sBase, okB := scoreBlob(e, baseBlobs[c.Path], basePath(c), baseCx)
		sHead, okH := scoreBlob(e, headBlobs[c.Path], c.Path, headCx)
		if !okB || !okH {
			return thermo.FileDelta{}, false
		}
		return thermo.MakeFileDelta(c.Path, thermo.DeltaModified, sBase, sHead), true
	}
}

// scoreBlob parses + scores a single blob, attaching the ref's cross-file
// corpus (nil-safe) so the cross_duplication microstate sees its mass.
// ok=false means the blob is missing or unparseable — both treated
// identically by ComputeDelta callers per the symmetric soft-miss rule.
func scoreBlob(e *thermo.Engine, blob []byte, path string, cx microstate.CrossFileSource) (float64, bool) {
	if len(blob) == 0 {
		return 0, false
	}
	f, ok := golang.ParseGoBytes(path, blob)
	if !ok {
		return 0, false
	}
	f.Corpus = cx
	return e.Score(f).S, true
}

// crossDupEnabled reports whether the cross_duplication microstate carries
// any weight. When it is ≤ 0 the contribution is zeroed anyway, so check
// skips the whole-tree corpus pre-pass (and scan skips attachCorpus) — the
// expensive part — rather than build a context nothing will read.
func crossDupEnabled(weights map[string]float64) bool {
	return weights[microstate.CrossDuplication{}.Name()] > 0
}

// buildCorpora builds the whole-tree cross-file context at the base and
// head refs for the cross_duplication microstate, reusing the changed
// blobs the scoring path already fetched so each blob is read from git
// once. On any failure (git unavailable, a ref that won't enumerate) BOTH
// sides degrade to nil so cross_duplication contributes 0 symmetrically and
// never manufactures a one-sided ΔS — the same symmetric-soft-miss stance
// scoreBlob takes for an unparseable blob.
//
// excludeBase/excludeHead hold the diff files whose parseability differs
// between refs out of each ref's corpus, keeping clone-class membership
// symmetric across base and head (see asymmetricParseExclusions, issue #68).
func buildCorpora(runner gitx.Runner, baseSHA, headSHA string, files []gitx.Change, baseBlobs, headBlobs map[string][]byte, excludeBase, excludeHead map[string]bool) (base, head microstate.CrossFileSource) {
	baseCx, errBase := corpus.BuildReusing(runner, baseSHA, baseCorpusSeed(files, baseBlobs), excludeBase)
	headCx, errHead := corpus.BuildReusing(runner, headSHA, headBlobs, excludeHead)
	if errBase != nil || errHead != nil {
		return nil, nil
	}
	return baseCx, headCx
}

// baseCorpusSeed re-keys the scoring-side base blobs (keyed by the head
// path c.Path so a rename's two sides share a key) back to their base-tree
// path — OldPath for a rename — the keying corpus.BuildReusing needs to line
// reused blobs up against the base ref's ls-tree. For anything but a rename
// this is the identity. headBlobs needs no such step: c.Path IS the head
// tree path, so it is fed to BuildReusing as-is.
func baseCorpusSeed(files []gitx.Change, baseBlobs map[string][]byte) map[string][]byte {
	seed := make(map[string][]byte, len(baseBlobs))
	for _, c := range files {
		if !isGoPath(c) || c.Kind == gitx.ChangeAdded {
			continue
		}
		if b, ok := baseBlobs[c.Path]; ok {
			seed[basePath(c)] = b
		}
	}
	return seed
}

// asymmetricParseExclusions finds diff files present on BOTH refs that
// parse on exactly one side. Such a file sits in one ref's cross-file
// corpus and is absent from the other, shifting a clone class's lowest-path
// "original" between refs and perturbing a sibling's cross_duplication ΔS
// even though the sibling did not change (issue #68). Holding it out of
// BOTH corpora keeps clone-class membership symmetric — the same soft-miss
// stance scoreBlob takes for an unparseable changed file (which is itself
// dropped from ΔS, so excluding it costs no real signal).
//
// Only Modified/Renamed entries qualify: an Added or Removed file is
// LEGITIMATELY one-sided, and its appearance/disappearance is a real change
// in cross-file duplication the gate should see — excluding it would hide
// the very signal cross_duplication exists to catch. Both blob maps are
// keyed by c.Path (the head path); the base exclusion is recorded under the
// base-tree path basePath(c) so it matches the base corpus enumeration.
func asymmetricParseExclusions(files []gitx.Change, baseBlobs, headBlobs map[string][]byte) (base, head map[string]bool) {
	base = make(map[string]bool)
	head = make(map[string]bool)
	for _, c := range files {
		if !isGoPath(c) || (c.Kind != gitx.ChangeModified && c.Kind != gitx.ChangeRenamed) {
			continue
		}
		if blobParses(basePath(c), baseBlobs[c.Path]) != blobParses(c.Path, headBlobs[c.Path]) {
			base[basePath(c)] = true
			head[c.Path] = true
		}
	}
	return base, head
}

// blobParses reports whether a blob is non-empty Go that parses — the same
// "scorable" test scoreBlob applies, single-sourced so the corpus-exclusion
// decision and the ΔS soft-miss decision agree on what "parses" means.
func blobParses(path string, blob []byte) bool {
	if len(blob) == 0 {
		return false
	}
	_, ok := golang.ParseGoBytes(path, blob)
	return ok
}

// applyScalingBonus folds the (always-negative) downgrade reward into
// ΔS_total per docs/scaling.md#downgrade-reward--proportional. Density re-derives
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
