// Package pipeline orchestrates the entrolint analysis end-to-end:
// walks the source tree, computes microstates, calibrates the thermo
// engine (or loads cached calibration), and produces ranked file
// scores. The CLI layer translates the result into table or JSON
// output.
package pipeline

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/corpus"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

// ScanOptions configures one scan run.
type ScanOptions struct {
	// Root is the directory or file to analyze.
	Root string

	// Config carries weights, thresholds, and the churn window.
	Config config.Config

	// CachePath, if non-empty, is consulted for calibration; on miss
	// or when Recalibrate is set, fresh calibration is written here.
	CachePath string

	// Recalibrate forces a fresh fit even when a valid cache exists.
	Recalibrate bool
}

// FileScore is one file's calibrated entropy, temperature, and
// per-microstate breakdown.
type FileScore struct {
	Path          string             `json:"path"`
	S             float64            `json:"s"`
	T             float64            `json:"t"`
	Dominant      string             `json:"dominant"`
	Contributions map[string]float64 `json:"contributions"`
}

// ScanResult is the output of a successful scan. Files are pre-sorted
// by T descending — the CLI just takes the top N.
type ScanResult struct {
	Files []FileScore `json:"files"`
}

// structuralMicrostates lists the contributors to S (churn lives only in T).
// v0.1: cyclomatic, nesting, length. v0.3 adds coupling (per-file import count
// as efferent-coupling proxy) and duplication (intra-file AST-subtree clones).
// v0.5 adds cross_duplication (clones spanning more than one file), fed by the
// corpus pre-pass attached to each File before scoring (see attachCorpus). Full
// cross-file coupling (the Ca/Ce graph) stays deferred — it needs go/types, not
// just ASTs; see docs/crossfile.md.
func structuralMicrostates() []microstate.Microstate {
	return []microstate.Microstate{
		microstate.Cyclomatic{},
		microstate.Nesting{},
		microstate.Length{},
		microstate.Coupling{},
		microstate.Duplication{},
		microstate.CrossDuplication{},
	}
}

// formulaVersion identifies the public S/T/ΔS contract. Bump it when the
// calibration math changes but the cache schema itself can still be parsed.
const formulaVersion = 1

// Scan runs the full pipeline for `opts` and returns ranked scores.
func Scan(opts ScanOptions) (ScanResult, error) {
	ms := structuralMicrostates()

	files, err := analyzeTree(opts)
	if err != nil {
		return ScanResult{}, err
	}

	engine := resolveEngine(opts, ms, files)
	return ScanResult{Files: rank(engine, files)}, nil
}

func analyzeTree(opts ScanOptions) ([]microstate.File, error) {
	rootAbs, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	a := golang.Analyzer{
		ChurnRunner:    gitx.LocalRunner{Dir: rootAbs},
		ChurnSinceDays: opts.Config.ChurnSinceDays,
	}
	files, err := a.Analyze(opts.Root)
	if err != nil {
		return nil, err
	}
	if crossDupEnabled(opts.Config.Weights) {
		attachCorpus(files)
	}
	return files, nil
}

// attachCorpus runs the cross-file pre-pass over the freshly analyzed
// tree and attaches the resulting context to every File, in place. This
// is load-bearing for both Scan and calibrateForCheck: the cross_duplication
// microstate's lognormal is fit during calibration, so its per-file mass
// must be visible on the corpus BEFORE the slice reaches thermo.Calibrate
// — otherwise it calibrates on all-zeros and silently contributes nothing.
// The scan path needs no git: the whole on-disk tree is already in `files`.
func attachCorpus(files []microstate.File) {
	ctx := corpus.BuildFromFiles(files)
	for i := range files {
		files[i].Corpus = ctx
	}
}

func resolveEngine(opts ScanOptions, ms []microstate.Microstate, files []microstate.File) *thermo.Engine {
	sig := cacheSignature(ms, opts.Config)
	if !opts.Recalibrate && opts.CachePath != "" {
		if state, err := cache.Load(opts.CachePath); err == nil && state.ValidFor(sig) {
			return thermo.NewEngine(ms, opts.Config.Weights, state.K, state.Alpha, state.Microstates)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// Malformed cache: fall through to fresh calibration.
			_ = err
		}
		// Stale cache (missing lognormal for a new microstate): also
		// fall through — fresh calibration will overwrite below.
	}
	engine := thermo.Calibrate(ms, opts.Config.Weights, files)
	if opts.CachePath != "" {
		_ = cache.Save(opts.CachePath, cache.StateFromEngine(engine, sig))
	}
	return engine
}

func cacheSignature(ms []microstate.Microstate, cfg config.Config) cache.Signature {
	names := microstateNames(ms)
	weights := make(map[string]float64, len(names))
	for _, name := range names {
		weights[name] = cfg.Weights[name]
	}
	return cache.Signature{
		FormulaVersion:     formulaVersion,
		Microstates:        names,
		Weights:            weights,
		NormalizationFloor: thermo.NormalizationFloor,
		Alpha:              thermo.DefaultAlpha,
	}
}

func microstateNames(ms []microstate.Microstate) []string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name()
	}
	return names
}

func rank(engine *thermo.Engine, files []microstate.File) []FileScore {
	scores := make([]FileScore, 0, len(files))
	for _, f := range files {
		sc := engine.Score(f)
		scores = append(scores, FileScore{
			Path:          f.Path,
			S:             sc.S,
			T:             sc.T,
			Dominant:      dominant(sc.Contributions),
			Contributions: sc.Contributions,
		})
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].T > scores[j].T
	})
	return scores
}

func dominant(contribs map[string]float64) string {
	var name string
	var best float64
	for n, v := range contribs {
		if v > best {
			best = v
			name = n
		}
	}
	return name
}
