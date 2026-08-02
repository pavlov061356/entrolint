package calibration

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

const (
	// DefaultHistorySubjectPattern is the bounded validation's transparent
	// heuristic for likely corrective commits.
	DefaultHistorySubjectPattern = `(?i)\b(fix|fixes|fixed|bug|bugfix|revert|reverted)\b`

	defaultHistorySearchLimit     = 500
	defaultHistorySamplesPerRepo  = 10
	defaultHistoryMaxChangedFiles = 10

	historySkipNoMatchedLabels = "no_matched_labels"
	historySkipNoNegativeFiles = "no_negative_files"
	historySkipUnmatchedLabels = "unmatched_labels"
)

// HistoryValidationOptions configures pre-fix file-ranking validation.
type HistoryValidationOptions struct {
	Roots           []string
	Candidates      []Candidate
	Ref             string
	SearchLimit     int
	SamplesPerRepo  int
	MaxChangedFiles int
	SubjectPattern  string
}

// HistoryProtocol records the bounded sample-selection rules.
type HistoryProtocol struct {
	Ref             string `json:"ref"`
	SearchLimit     int    `json:"search_limit"`
	SamplesPerRepo  int    `json:"samples_per_repo"`
	MaxChangedFiles int    `json:"max_changed_files"`
	SubjectPattern  string `json:"subject_pattern"`
}

// HistoryValidationReport compares candidate rankings against corrective
// commit labels.
type HistoryValidationReport struct {
	Protocol   HistoryProtocol          `json:"protocol"`
	Candidates []HistoryCandidateReport `json:"candidates"`
}

// HistoryCandidateReport is one candidate's validation result.
type HistoryCandidateReport struct {
	Name      string              `json:"name"`
	Weights   map[string]float64  `json:"weights"`
	Repos     []HistoryRepoReport `json:"repos"`
	Aggregate HistorySummary      `json:"aggregate"`
}

// HistoryRepoReport contains the auditable commit samples for one repository.
type HistoryRepoReport struct {
	Root            string                `json:"root"`
	FrameSHA        string                `json:"frame_sha"`
	SelectedCommits int                   `json:"selected_commits"`
	Summary         HistorySummary        `json:"summary"`
	Commits         []HistoryCommitReport `json:"commits"`
}

// HistorySummary aggregates pre-fix ranking quality and the exact random
// baselines implied by each commit's ceiling-rounded top-k cutoff.
type HistorySummary struct {
	Commits                  int     `json:"commits"`
	LabeledFiles             int     `json:"labeled_files"`
	MeanAUC                  float64 `json:"mean_auc"`
	Top10Recall              float64 `json:"top_10_recall"`
	Top20Recall              float64 `json:"top_20_recall"`
	Top10RandomBaseline      float64 `json:"top_10_random_baseline"`
	Top20RandomBaseline      float64 `json:"top_20_random_baseline"`
	MedianPositivePercentile float64 `json:"median_positive_percentile"`
}

// HistoryCommitReport is one pre-fix ranking evaluation.
type HistoryCommitReport struct {
	SHA                      string   `json:"sha"`
	ShortSHA                 string   `json:"short_sha"`
	ParentSHA                string   `json:"parent_sha"`
	CommitTime               string   `json:"commit_time"`
	Subject                  string   `json:"subject"`
	Files                    int      `json:"files"`
	Labels                   []string `json:"labels"`
	MatchedLabels            []string `json:"matched_labels"`
	UnmatchedLabels          []string `json:"unmatched_labels"`
	Scored                   bool     `json:"scored"`
	SkipReason               string   `json:"skip_reason,omitempty"`
	AUC                      float64  `json:"auc"`
	Top10Recall              float64  `json:"top_10_recall"`
	Top20Recall              float64  `json:"top_20_recall"`
	Top10RandomBaseline      float64  `json:"top_10_random_baseline"`
	Top20RandomBaseline      float64  `json:"top_20_random_baseline"`
	MedianPositivePercentile float64  `json:"median_positive_percentile"`

	positivePercentiles []float64
}

type historySample struct {
	commit gitx.Commit
	parent string
	labels []string
}

type historyRepoSamples struct {
	root     string
	frameSHA string
	samples  []historySample
}

// ValidateHistory scores each corrective commit's parent tree, then tests
// whether the files changed by the commit rank above untouched files.
func ValidateHistory(opts HistoryValidationOptions) (HistoryValidationReport, error) {
	normalized, pattern, err := normalizeHistoryOptions(opts)
	if err != nil {
		return HistoryValidationReport{}, err
	}
	candidates, err := normalizedCandidates(normalized.Candidates)
	if err != nil {
		return HistoryValidationReport{}, err
	}

	repos := make([]historyRepoSamples, 0, len(normalized.Roots))
	var selected int
	for _, root := range normalized.Roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return HistoryValidationReport{}, err
		}
		runner := gitx.LocalRunner{Dir: rootAbs}
		frameSHA, err := gitx.ResolveRef(runner, "HEAD")
		if err != nil {
			return HistoryValidationReport{}, fmt.Errorf("history validation resolve frame for %q: %w", root, err)
		}
		samples, err := collectHistorySamples(
			runner,
			normalized.Ref,
			normalized.SearchLimit,
			normalized.SamplesPerRepo,
			normalized.MaxChangedFiles,
			pattern,
		)
		if err != nil {
			return HistoryValidationReport{}, fmt.Errorf("history validation %q: %w", root, err)
		}
		repos = append(repos, historyRepoSamples{
			root:     filepath.Clean(root),
			frameSHA: frameSHA,
			samples:  samples,
		})
		selected += len(samples)
	}
	if selected == 0 {
		return HistoryValidationReport{}, errors.New("history validation: no eligible commits found")
	}

	report := HistoryValidationReport{
		Protocol: HistoryProtocol{
			Ref:             normalized.Ref,
			SearchLimit:     normalized.SearchLimit,
			SamplesPerRepo:  normalized.SamplesPerRepo,
			MaxChangedFiles: normalized.MaxChangedFiles,
			SubjectPattern:  normalized.SubjectPattern,
		},
		Candidates: make([]HistoryCandidateReport, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		candidateReport, err := validateHistoryCandidate(candidate, repos)
		if err != nil {
			return HistoryValidationReport{}, err
		}
		report.Candidates = append(report.Candidates, candidateReport)
	}
	return report, nil
}

func normalizeHistoryOptions(opts HistoryValidationOptions) (HistoryValidationOptions, *regexp.Regexp, error) {
	if len(opts.Roots) == 0 {
		return opts, nil, errors.New("history validation: at least one root is required")
	}
	if opts.Ref == "" {
		opts.Ref = "HEAD"
	}
	if opts.SearchLimit == 0 {
		opts.SearchLimit = defaultHistorySearchLimit
	}
	if opts.SamplesPerRepo == 0 {
		opts.SamplesPerRepo = defaultHistorySamplesPerRepo
	}
	if opts.MaxChangedFiles == 0 {
		opts.MaxChangedFiles = defaultHistoryMaxChangedFiles
	}
	if opts.SearchLimit < 0 || opts.SamplesPerRepo < 0 || opts.MaxChangedFiles < 0 {
		return opts, nil, errors.New("history validation: numeric limits must be positive")
	}
	if opts.SubjectPattern == "" {
		opts.SubjectPattern = DefaultHistorySubjectPattern
	}
	pattern, err := regexp.Compile(opts.SubjectPattern)
	if err != nil {
		return opts, nil, fmt.Errorf("history validation subject regexp: %w", err)
	}
	return opts, pattern, nil
}

func collectHistorySamples(
	runner gitx.Runner,
	ref string,
	searchLimit int,
	samplesPerRepo int,
	maxChangedFiles int,
	pattern *regexp.Regexp,
) ([]historySample, error) {
	commits, err := gitx.LogCommits(runner, ref, searchLimit, false)
	if err != nil {
		return nil, err
	}
	samples := make([]historySample, 0, samplesPerRepo)
	for i := len(commits) - 1; i >= 0 && len(samples) < samplesPerRepo; i-- {
		commit := commits[i]
		if !pattern.MatchString(commit.Subject) {
			continue
		}
		parent, ok, err := gitx.SingleParent(runner, commit.SHA)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		diff, err := gitx.Diff(runner, parent, commit.SHA)
		if err != nil {
			return nil, err
		}
		labels := existingGoLabels(diff.Files)
		if len(labels) == 0 || len(labels) > maxChangedFiles {
			continue
		}
		samples = append(samples, historySample{
			commit: commit,
			parent: parent,
			labels: labels,
		})
	}
	for left, right := 0, len(samples)-1; left < right; left, right = left+1, right-1 {
		samples[left], samples[right] = samples[right], samples[left]
	}
	return samples, nil
}

func existingGoLabels(changes []gitx.Change) []string {
	seen := make(map[string]bool)
	for _, change := range changes {
		var path string
		switch change.Kind {
		case gitx.ChangeModified, gitx.ChangeRemoved:
			path = change.Path
		case gitx.ChangeRenamed:
			if change.LinesChanged() == 0 {
				continue
			}
			path = change.OldPath
		case gitx.ChangeAdded:
			continue
		}
		if path != "" && golang.IsAnalyzablePath(path) {
			seen[path] = true
		}
	}
	labels := make([]string, 0, len(seen))
	for path := range seen {
		labels = append(labels, path)
	}
	sort.Strings(labels)
	return labels
}

func validateHistoryCandidate(candidate Candidate, repos []historyRepoSamples) (HistoryCandidateReport, error) {
	report := HistoryCandidateReport{
		Name:    candidate.Name,
		Weights: effectiveHistoryWeights(candidate.Config.Weights),
		Repos:   make([]HistoryRepoReport, 0, len(repos)),
	}
	var all []HistoryCommitReport
	for _, repo := range repos {
		scorer, err := pipeline.NewTreeScorer(pipeline.TreeScorerOptions{
			Root:     repo.root,
			FrameRef: repo.frameSHA,
			ScanOptions: pipeline.ScanOptions{
				Config:      candidate.Config,
				Recalibrate: true,
			},
		})
		if err != nil {
			return HistoryCandidateReport{}, fmt.Errorf(
				"history validation calibrate %q for candidate %q: %w",
				repo.root,
				candidate.Name,
				err,
			)
		}

		commits := make([]HistoryCommitReport, 0, len(repo.samples))
		for _, sample := range repo.samples {
			scores, err := scorer.Score(sample.parent)
			if err != nil {
				return HistoryCandidateReport{}, fmt.Errorf(
					"history validation score %s in %q for candidate %q: %w",
					sample.parent,
					repo.root,
					candidate.Name,
					err,
				)
			}
			metrics, _ := evaluateHistoryCommit(scores, sample.labels)
			metrics.SHA = sample.commit.SHA
			metrics.ShortSHA = sample.commit.ShortSHA
			metrics.ParentSHA = sample.parent
			metrics.CommitTime = sample.commit.CommitTime.Format("2006-01-02T15:04:05Z07:00")
			metrics.Subject = sample.commit.Subject
			commits = append(commits, metrics)
		}
		report.Repos = append(report.Repos, HistoryRepoReport{
			Root:            repo.root,
			FrameSHA:        repo.frameSHA,
			SelectedCommits: len(repo.samples),
			Summary:         summarizeHistory(commits),
			Commits:         commits,
		})
		all = append(all, commits...)
	}
	report.Aggregate = summarizeHistory(all)
	return report, nil
}

func effectiveHistoryWeights(weights map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(contributionOrder))
	for _, name := range contributionOrder {
		out[name] = weights[name]
	}
	return out
}

func evaluateHistoryCommit(scores []pipeline.FileScore, labels []string) (HistoryCommitReport, bool) {
	scoreByPath := make(map[string]float64, len(scores))
	allScores := make([]float64, 0, len(scores))
	for _, score := range scores {
		scoreByPath[score.Path] = score.S
		allScores = append(allScores, score.S)
	}

	selectedLabels := append([]string(nil), labels...)
	sort.Strings(selectedLabels)
	matchedLabels := make([]string, 0, len(selectedLabels))
	unmatchedLabels := make([]string, 0)
	positiveScores := make([]float64, 0, len(labels))
	positivePaths := make(map[string]bool, len(labels))
	for _, label := range selectedLabels {
		value, ok := scoreByPath[label]
		if !ok {
			unmatchedLabels = append(unmatchedLabels, label)
			continue
		}
		matchedLabels = append(matchedLabels, label)
		positiveScores = append(positiveScores, value)
		positivePaths[label] = true
	}
	report := HistoryCommitReport{
		Files:           len(scores),
		Labels:          selectedLabels,
		MatchedLabels:   matchedLabels,
		UnmatchedLabels: unmatchedLabels,
	}
	if len(unmatchedLabels) > 0 {
		report.SkipReason = historySkipUnmatchedLabels
		return report, false
	}
	if len(positiveScores) == 0 {
		report.SkipReason = historySkipNoMatchedLabels
		return report, false
	}
	if len(positiveScores) == len(scores) {
		report.SkipReason = historySkipNoNegativeFiles
		return report, false
	}

	negativeScores := make([]float64, 0, len(scores)-len(positiveScores))
	for _, score := range scores {
		if !positivePaths[score.Path] {
			negativeScores = append(negativeScores, score.S)
		}
	}

	percentiles := make([]float64, 0, len(positiveScores))
	var top10 float64
	var top20 float64
	for _, positive := range positiveScores {
		percentiles = append(percentiles, scorePercentile(positive, allScores))
		top10 += topKCredit(positive, allScores, 0.10)
		top20 += topKCredit(positive, allScores, 0.20)
	}
	top10 /= float64(len(positiveScores))
	top20 /= float64(len(positiveScores))
	sort.Float64s(percentiles)

	report.Scored = true
	report.AUC = historyAUC(positiveScores, negativeScores)
	report.Top10Recall = top10
	report.Top20Recall = top20
	report.Top10RandomBaseline = topKRandomBaseline(len(allScores), 0.10)
	report.Top20RandomBaseline = topKRandomBaseline(len(allScores), 0.20)
	report.MedianPositivePercentile = medianSorted(percentiles)
	report.positivePercentiles = percentiles
	return report, true
}

func historyAUC(positiveScores, negativeScores []float64) float64 {
	var wins float64
	for _, positive := range positiveScores {
		for _, negative := range negativeScores {
			switch {
			case positive > negative:
				wins++
			case positive == negative:
				wins += 0.5
			}
		}
	}
	return wins / float64(len(positiveScores)*len(negativeScores))
}

func medianSorted(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func scorePercentile(value float64, scores []float64) float64 {
	if len(scores) <= 1 {
		return 1
	}
	var higher int
	var equal int
	for _, score := range scores {
		switch {
		case score > value:
			higher++
		case score == value:
			equal++
		}
	}
	averageRank := float64(higher) + float64(equal-1)/2
	return 1 - averageRank/float64(len(scores)-1)
}

func topKCredit(value float64, scores []float64, fraction float64) float64 {
	k := int(math.Ceil(fraction * float64(len(scores))))
	if k < 1 {
		k = 1
	}
	var higher int
	var equal int
	for _, score := range scores {
		switch {
		case score > value:
			higher++
		case score == value:
			equal++
		}
	}
	remaining := k - higher
	switch {
	case remaining <= 0:
		return 0
	case remaining >= equal:
		return 1
	default:
		return float64(remaining) / float64(equal)
	}
}

func topKRandomBaseline(files int, fraction float64) float64 {
	if files == 0 {
		return 0
	}
	k := int(math.Ceil(fraction * float64(files)))
	if k < 1 {
		k = 1
	}
	return float64(k) / float64(files)
}

func summarizeHistory(commits []HistoryCommitReport) HistorySummary {
	summary := HistorySummary{}

	var top10Hits float64
	var top20Hits float64
	var top10RandomHits float64
	var top20RandomHits float64
	var percentiles []float64
	for _, commit := range commits {
		if !commit.Scored {
			continue
		}
		summary.Commits++
		labels := len(commit.MatchedLabels)
		summary.LabeledFiles += labels
		summary.MeanAUC += commit.AUC
		top10Hits += commit.Top10Recall * float64(labels)
		top20Hits += commit.Top20Recall * float64(labels)
		top10RandomHits += commit.Top10RandomBaseline * float64(labels)
		top20RandomHits += commit.Top20RandomBaseline * float64(labels)
		percentiles = append(percentiles, commit.positivePercentiles...)
	}
	if summary.Commits == 0 {
		return summary
	}
	summary.MeanAUC /= float64(summary.Commits)
	if summary.LabeledFiles > 0 {
		summary.Top10Recall = top10Hits / float64(summary.LabeledFiles)
		summary.Top20Recall = top20Hits / float64(summary.LabeledFiles)
		summary.Top10RandomBaseline = top10RandomHits / float64(summary.LabeledFiles)
		summary.Top20RandomBaseline = top20RandomHits / float64(summary.LabeledFiles)
	}
	sort.Float64s(percentiles)
	summary.MedianPositivePercentile = medianSorted(percentiles)
	return summary
}
