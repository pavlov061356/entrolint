package pipeline

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/corpus"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

// HistoryOptions configures an S(t) run over recent git history.
type HistoryOptions struct {
	// Root is the repository checkout used for calibration and as the
	// anchor directory for git commands.
	Root string

	// Ref is the branch, tag, or SHA whose history is sampled. Empty means HEAD.
	Ref string

	// Limit caps the number of most-recent commits. A non-positive value means
	// no explicit cap at the git layer; the CLI sets a positive default.
	Limit int

	// FirstParent follows only the mainline parent chain, useful for branch
	// history where merge commits would otherwise pull in side branches.
	FirstParent bool

	// ScanOptions carries Config + cache settings shared with Scan.
	// Root inside ScanOptions is ignored; this struct's Root wins.
	ScanOptions ScanOptions

	// GitRunner is the gitx.Runner used for log/ls-tree/cat-file.
	// If nil, a LocalRunner anchored at Root is used.
	GitRunner gitx.Runner
}

// HistoryPoint is one commit-level total entropy sample.
type HistoryPoint struct {
	SHA        string  `json:"sha"`
	ShortSHA   string  `json:"short_sha"`
	CommitTime string  `json:"commit_time"`
	Subject    string  `json:"subject"`
	S          float64 `json:"s"`
	FileCount  int     `json:"file_count"`
}

// HistoryResult is the phase-portrait data layer: total S over time.
type HistoryResult struct {
	Ref    string         `json:"ref"`
	Points []HistoryPoint `json:"points"`
}

// TreeScorerOptions configures a scorer for historical git trees.
type TreeScorerOptions struct {
	// Root anchors Git commands and supplies the on-disk calibration tree when
	// FrameRef is empty.
	Root string

	// FrameRef, when non-empty, calibrates from the resolved committed Git tree
	// instead of the on-disk Root. Empty preserves the production history path's
	// working-tree calibration behavior.
	FrameRef string

	// ScanOptions carries the candidate config and cache policy. Root inside
	// ScanOptions is ignored; this struct's Root wins.
	ScanOptions ScanOptions

	// GitRunner reads historical trees. If nil, a LocalRunner at Root is used.
	GitRunner gitx.Runner
}

// TreeScorer scores multiple historical refs in one calibration frame.
type TreeScorer struct {
	runner   gitx.Runner
	engine   *thermo.Engine
	weights  map[string]float64
	frameSHA string
}

// NewTreeScorer calibrates once on either FrameRef's committed Git tree or the
// current on-disk Root when FrameRef is empty.
func NewTreeScorer(opts TreeScorerOptions) (*TreeScorer, error) {
	rootAbs, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	runner := opts.GitRunner
	if runner == nil {
		runner = gitx.LocalRunner{Dir: rootAbs}
	}
	scanOpts := opts.ScanOptions
	scanOpts.Root = opts.Root
	var files []microstate.File
	var frameSHA string
	if opts.FrameRef == "" {
		files, err = analyzeTree(scanOpts)
		if err != nil {
			return nil, fmt.Errorf("calibration tree walk: %w", err)
		}
	} else {
		frameSHA, err = gitx.ResolveRef(runner, opts.FrameRef)
		if err != nil {
			return nil, fmt.Errorf("resolve calibration frame %q: %w", opts.FrameRef, err)
		}
		files, err = treeMicrostateFiles(runner, frameSHA, scanOpts.Config.Weights)
		if err != nil {
			return nil, fmt.Errorf("calibration Git tree %s: %w", frameSHA, err)
		}
		// The cache signature does not include a tree identity. A committed frame
		// must therefore always be fitted from the resolved Git tree.
		scanOpts.CachePath = ""
		scanOpts.Recalibrate = true
	}
	return &TreeScorer{
		runner:   runner,
		engine:   resolveEngine(scanOpts, structuralMicrostates(), files),
		weights:  scanOpts.Config.Weights,
		frameSHA: frameSHA,
	}, nil
}

// Score returns per-file structural scores for ref. Historical blobs do not
// carry working-tree churn, so T equals S and the result is an S ranking.
func (s *TreeScorer) Score(ref string) ([]FileScore, error) {
	return scoreTreeFiles(s.runner, s.engine, ref, s.weights)
}

func (s *TreeScorer) scoreTotal(ref string) (float64, int, error) {
	files, err := treeMicrostateFiles(s.runner, ref, s.weights)
	if err != nil {
		return 0, 0, err
	}
	var total float64
	for _, file := range files {
		total += s.engine.Score(file).S
	}
	return total, len(files), nil
}

// FrameSHA returns the full commit SHA used for Git-tree calibration. It is
// empty when the scorer was calibrated from the on-disk working tree.
func (s *TreeScorer) FrameSHA() string { return s.frameSHA }

// History computes total structural entropy at recent commits without
// checking them out. The engine is calibrated once on the current Root tree,
// then every historical tree is scored in that same frame so the points are
// comparable.
func History(opts HistoryOptions) (HistoryResult, error) {
	rootAbs, err := filepath.Abs(opts.Root)
	if err != nil {
		return HistoryResult{}, err
	}
	runner := opts.GitRunner
	if runner == nil {
		runner = gitx.LocalRunner{Dir: rootAbs}
	}
	ref := opts.Ref
	if ref == "" {
		ref = "HEAD"
	}

	commits, err := gitx.LogCommits(runner, ref, opts.Limit, opts.FirstParent)
	if err != nil {
		return HistoryResult{}, fmt.Errorf("history log %q: %w", ref, err)
	}
	scorer, err := NewTreeScorer(TreeScorerOptions{
		Root:        opts.Root,
		ScanOptions: opts.ScanOptions,
		GitRunner:   runner,
	})
	if err != nil {
		return HistoryResult{}, err
	}

	points := make([]HistoryPoint, 0, len(commits))
	for _, c := range commits {
		total, files, err := scorer.scoreTotal(c.SHA)
		if err != nil {
			return HistoryResult{}, fmt.Errorf("score tree %s: %w", c.SHA, err)
		}
		points = append(points, HistoryPoint{
			SHA:        c.SHA,
			ShortSHA:   c.ShortSHA,
			CommitTime: c.CommitTime.Format("2006-01-02T15:04:05Z07:00"),
			Subject:    c.Subject,
			S:          total,
			FileCount:  files,
		})
	}
	return HistoryResult{Ref: ref, Points: points}, nil
}

func scoreTreeFiles(runner gitx.Runner, engine *thermo.Engine, ref string, weights map[string]float64) ([]FileScore, error) {
	files, err := treeMicrostateFiles(runner, ref, weights)
	if err != nil {
		return nil, err
	}
	return rank(engine, files), nil
}

func treeMicrostateFiles(runner gitx.Runner, ref string, weights map[string]float64) ([]microstate.File, error) {
	paths, err := gitx.TreeFiles(runner, ref)
	if err != nil {
		return nil, err
	}
	paths = analyzableSorted(paths)
	blobs, err := gitx.BlobsAtRef(runner, ref, paths)
	if err != nil {
		return nil, err
	}

	var cx microstate.CrossFileSource
	if crossDupEnabled(weights) {
		cx = corpus.BuildFromBlobs(blobs, nil)
	}

	files := make([]microstate.File, 0, len(paths))
	for _, path := range paths {
		f, ok := golang.ParseGoBytes(path, blobs[path])
		if !ok {
			continue
		}
		f.Corpus = cx
		files = append(files, f)
	}
	return files, nil
}

func analyzableSorted(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if golang.IsAnalyzablePath(path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
