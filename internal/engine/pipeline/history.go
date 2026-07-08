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
	engine, err := calibrateForHistory(opts)
	if err != nil {
		return HistoryResult{}, err
	}

	points := make([]HistoryPoint, 0, len(commits))
	for _, c := range commits {
		total, files, err := scoreTreeTotal(runner, engine, c.SHA, opts.ScanOptions.Config.Weights)
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

func calibrateForHistory(opts HistoryOptions) (*thermo.Engine, error) {
	scanOpts := opts.ScanOptions
	scanOpts.Root = opts.Root
	files, err := analyzeTree(scanOpts)
	if err != nil {
		return nil, fmt.Errorf("calibration tree walk: %w", err)
	}
	return resolveEngine(scanOpts, structuralMicrostates(), files), nil
}

func scoreTreeTotal(runner gitx.Runner, engine *thermo.Engine, ref string, weights map[string]float64) (float64, int, error) {
	paths, err := gitx.TreeFiles(runner, ref)
	if err != nil {
		return 0, 0, err
	}
	paths = analyzableSorted(paths)
	blobs, err := gitx.BlobsAtRef(runner, ref, paths)
	if err != nil {
		return 0, 0, err
	}

	var cx microstate.CrossFileSource
	if crossDupEnabled(weights) {
		cx = corpus.BuildFromBlobs(blobs, nil)
	}

	var total float64
	var count int
	for _, path := range paths {
		f, ok := golang.ParseGoBytes(path, blobs[path])
		if !ok {
			continue
		}
		f.Corpus = cx
		total += engine.Score(f).S
		count++
	}
	return total, count, nil
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
