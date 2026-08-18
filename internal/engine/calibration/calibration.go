// Package calibration contains the pre-1.0 weight-calibration harness.
//
// The harness intentionally reuses pipeline.Scan instead of reimplementing the
// entropy formula. Its first job is auditability: compare candidate weight sets
// on the same local corpus and expose contribution shares before any default
// weights are changed.
package calibration

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

var contributionOrder = []string{
	"cyclomatic",
	"nesting",
	"coupling",
	"length",
	"duplication",
	"cross_duplication",
}

// Candidate is one weight/config set to evaluate against every corpus root.
type Candidate struct {
	Name   string
	Config config.Config
}

// Options configures a calibration run.
type Options struct {
	Roots      []string
	Candidates []Candidate
}

// Report is the full calibration-audit output.
type Report struct {
	Candidates []CandidateReport `json:"candidates"`
}

// CandidateReport is the result for one candidate weight/config set.
type CandidateReport struct {
	Name      string       `json:"name"`
	Repos     []RepoReport `json:"repos"`
	Aggregate Summary      `json:"aggregate"`
}

// RepoReport is the result for one repository root.
type RepoReport struct {
	Root    string  `json:"root"`
	Summary Summary `json:"summary"`
}

// Summary contains contribution-balance statistics over a set of file scores.
type Summary struct {
	Files         int            `json:"files"`
	TotalS        float64        `json:"total_s"`
	MedianS       float64        `json:"median_s"`
	P90S          float64        `json:"p90_s"`
	Contributions []Contribution `json:"contributions"`
	Dominants     []Dominant     `json:"dominants"`
}

// Contribution is a total and relative share for one microstate.
type Contribution struct {
	Name  string  `json:"name"`
	Total float64 `json:"total"`
	Share float64 `json:"share"`
}

// Dominant is how often a microstate was the largest S contributor.
type Dominant struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Share float64 `json:"share"`
}

// ContributionOrder returns the canonical display order for known microstates.
func ContributionOrder() []string {
	return append([]string(nil), contributionOrder...)
}

// Run evaluates every candidate against every root.
func Run(opts Options) (Report, error) {
	if len(opts.Roots) == 0 {
		return Report{}, errors.New("calibration: at least one root is required")
	}
	candidates, err := normalizedCandidates(opts.Candidates)
	if err != nil {
		return Report{}, err
	}

	out := Report{Candidates: make([]CandidateReport, 0, len(candidates))}
	for _, candidate := range candidates {
		repos := make([]RepoReport, 0, len(opts.Roots))
		var all []pipeline.FileScore
		for _, root := range opts.Roots {
			result, err := pipeline.Scan(pipeline.ScanOptions{
				Root:        root,
				Config:      candidate.Config,
				Recalibrate: true,
			})
			if err != nil {
				return Report{}, fmt.Errorf("calibration: scan %q for candidate %q: %w", root, candidate.Name, err)
			}
			cleanRoot := filepath.Clean(root)
			repos = append(repos, RepoReport{
				Root:    cleanRoot,
				Summary: Summarize(result.Files),
			})
			all = append(all, result.Files...)
		}
		out.Candidates = append(out.Candidates, CandidateReport{
			Name:      candidate.Name,
			Repos:     repos,
			Aggregate: Summarize(all),
		})
	}
	return out, nil
}

func normalizedCandidates(candidates []Candidate) ([]Candidate, error) {
	if len(candidates) == 0 {
		candidates = []Candidate{{Name: "default", Config: config.Default()}}
	}
	out := make([]Candidate, len(candidates))
	for i, candidate := range candidates {
		if candidate.Name == "" {
			return nil, errors.New("calibration: candidate name is required")
		}
		if candidate.Config.Weights == nil {
			candidate.Config = config.Default()
		}
		out[i] = candidate
	}
	return out, nil
}

// Summarize aggregates entropy scores into calibration-audit statistics.
func Summarize(files []pipeline.FileScore) Summary {
	summary := Summary{Files: len(files)}
	if len(files) == 0 {
		summary.Contributions = orderedContributions(nil, 0)
		return summary
	}

	values := make([]float64, 0, len(files))
	contribTotals := make(map[string]float64)
	dominantCounts := make(map[string]int)
	for _, f := range files {
		values = append(values, f.S)
		summary.TotalS += f.S
		if f.Dominant != "" {
			dominantCounts[f.Dominant]++
		}
		for name, value := range f.Contributions {
			contribTotals[name] += value
		}
	}
	sort.Float64s(values)
	summary.MedianS = percentileSorted(values, 0.50)
	summary.P90S = percentileSorted(values, 0.90)
	summary.Contributions = orderedContributions(contribTotals, summary.TotalS)
	summary.Dominants = orderedDominants(dominantCounts, len(files))
	return summary
}

func orderedContributions(totals map[string]float64, totalS float64) []Contribution {
	seen := make(map[string]bool, len(totals)+len(contributionOrder))
	out := make([]Contribution, 0, len(totals)+len(contributionOrder))
	for _, name := range contributionOrder {
		out = append(out, contribution(name, totals[name], totalS))
		seen[name] = true
	}

	var extra []string
	for name := range totals {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, contribution(name, totals[name], totalS))
	}
	return out
}

func contribution(name string, total, totalS float64) Contribution {
	var share float64
	if totalS > 0 {
		share = total / totalS
	}
	return Contribution{Name: name, Total: total, Share: share}
}

func orderedDominants(counts map[string]int, files int) []Dominant {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] == counts[names[j]] {
			return names[i] < names[j]
		}
		return counts[names[i]] > counts[names[j]]
	})

	out := make([]Dominant, 0, len(names))
	for _, name := range names {
		var share float64
		if files > 0 {
			share = float64(counts[name]) / float64(files)
		}
		out = append(out, Dominant{Name: name, Count: counts[name], Share: share})
	}
	return out
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
