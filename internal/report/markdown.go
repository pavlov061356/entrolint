// Package report renders the typed engine results into integration
// formats — a Markdown PR comment and a SARIF code-scanning log — that
// the GitHub Action consumes. It only formats; it never computes
// entropy, so the hard engine/integration split holds.
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// CommentMarker is the hidden HTML comment the Action greps for to find
// and update its own sticky PR comment in place (instead of posting a
// new one on every push). It is the first line of CheckMarkdown output.
const CommentMarker = "<!-- entrolint-report -->"

// maxHotspots caps the "Top hotspots" list so the comment stays compact
// even on a large PR. The ROADMAP names top-3.
const maxHotspots = 3

// CheckMarkdown renders a `check` result as a GitHub-flavored Markdown
// PR comment: verdict header, summary line, failure reasons, scaling
// signals, the changed-file ΔS table, and the hottest changed files.
// The output is deterministic (stable ordering) so a re-render with the
// same input updates the sticky comment to byte-identical text.
func CheckMarkdown(res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) string {
	var b strings.Builder
	b.WriteString(CommentMarker)
	b.WriteByte('\n')

	if v.Failed {
		b.WriteString("## ❌ entrolint — FAIL\n\n")
	} else {
		b.WriteString("## ✅ entrolint — PASS\n\n")
	}

	fmt.Fprintf(&b,
		"**ΔS_total** `%+.4f` · **ΔS_density** `%.4f` (threshold `%.4f`) · **scaling class** `%s` · %d lines, %d files\n",
		res.Delta.Total, res.Delta.Density, cfg.DeltaSMax,
		res.Scaling.Class, res.Delta.LinesChanged, len(res.Delta.Files),
	)

	if v.Failed {
		b.WriteString("\n> ❌ Gate failed:\n")
		for _, r := range v.Reasons {
			fmt.Fprintf(&b, "> - %s\n", r)
		}
	}

	writeScalingSignals(&b, res.Scaling)
	writeChangedFiles(&b, res.Delta.Files)
	writeHotspots(&b, res.Delta.Files)
	writeSkipped(&b, res.Skipped)

	return b.String()
}

// writeScalingSignals lists every non-O(1) detector hit so the reader
// sees why the class is elevated. Mirrors writeScalingHits in the CLI
// table renderer.
func writeScalingSignals(b *strings.Builder, r scaling.Result) {
	var lines []string
	for _, f := range r.Files {
		for _, h := range f.Hits {
			if h.Class == scaling.ClassO1 {
				continue
			}
			line := fmt.Sprintf("- `%s` **%s** in `%s`", h.Detector, h.Class, h.Path)
			if h.Size > 0 {
				line += fmt.Sprintf(" (size %d)", h.Size)
			}
			if h.Evidence != "" {
				line += " — " + h.Evidence
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n**Scaling signals**\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
}

func writeChangedFiles(b *strings.Builder, files []thermo.FileDelta) {
	if len(files) == 0 {
		b.WriteString("\n_No analyzable Go files changed._\n")
		return
	}
	b.WriteString("\n### Changed files\n\n")
	b.WriteString("| Kind | Path | S_base | S_head | Δ |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: |\n")
	for _, f := range files {
		fmt.Fprintf(b, "| %s | `%s` | %.3f | %.3f | %+.3f |\n",
			f.Kind, f.Path, f.SBase, f.SHead, f.Delta)
	}
}

// writeHotspots ranks the changed files by head-side entropy and lists
// the hottest few — the first refactoring candidates the PR introduces
// or grows. Files with zero head entropy (deletions) are excluded.
func writeHotspots(b *strings.Builder, files []thermo.FileDelta) {
	ranked := make([]thermo.FileDelta, 0, len(files))
	for _, f := range files {
		if f.SHead > 0 {
			ranked = append(ranked, f)
		}
	}
	if len(ranked) == 0 {
		return
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].SHead > ranked[j].SHead })
	if len(ranked) > maxHotspots {
		ranked = ranked[:maxHotspots]
	}
	b.WriteString("\n### Top hotspots (changed files)\n\n")
	for i, f := range ranked {
		fmt.Fprintf(b, "%d. `%s` — S %.3f\n", i+1, f.Path, f.SHead)
	}
}

func writeSkipped(b *strings.Builder, skipped []gitx.SkippedPath) {
	if len(skipped) == 0 {
		return
	}
	parts := make([]string, 0, len(skipped))
	for _, s := range skipped {
		parts = append(parts, fmt.Sprintf("`%s` (%s)", s.Path, s.Reason))
	}
	fmt.Fprintf(b, "\n_%d path(s) skipped: %s._\n", len(skipped), strings.Join(parts, ", "))
}
