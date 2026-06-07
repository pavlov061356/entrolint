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
// sees why the class is elevated. Shares nonO1Hits with the table
// renderer (text.go) — only the per-hit formatting differs.
func writeScalingSignals(b *strings.Builder, r scaling.Result) {
	hits := nonO1Hits(r)
	if len(hits) == 0 {
		return
	}
	b.WriteString("\n**Scaling signals**\n")
	for _, h := range hits {
		fmt.Fprintf(b, "- `%s` **%s** in `%s`", h.Detector, h.Class, mdInline(h.Path))
		if h.Size > 0 {
			fmt.Fprintf(b, " (size %d)", h.Size)
		}
		if h.Evidence != "" {
			b.WriteString(" — " + h.Evidence)
		}
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
			f.Kind, mdCell(f.Path), f.SBase, f.SHead, f.Delta)
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
		fmt.Fprintf(b, "%d. `%s` — S %.3f\n", i+1, mdInline(f.Path), f.SHead)
	}
}

func writeSkipped(b *strings.Builder, skipped []gitx.SkippedPath) {
	if len(skipped) == 0 {
		return
	}
	parts := make([]string, 0, len(skipped))
	for _, s := range skipped {
		parts = append(parts, fmt.Sprintf("`%s` (%s)", mdInline(s.Path), s.Reason))
	}
	fmt.Fprintf(b, "\n_%d path(s) skipped: %s._\n", len(skipped), strings.Join(parts, ", "))
}

// mdInline makes s safe to drop inside a Markdown inline code span. The
// comment renders untrusted file paths (from any repo the Action runs
// on) inside backtick spans; a backtick in the path would close the
// span early and corrupt the comment. Backticks can't be backslash-
// escaped inside a span, so swap them for a look-alike modifier grave
// accent. Newlines (which a path realistically can't hold, but be
// defensive) collapse to spaces so a single line stays single.
func mdInline(s string) string {
	s = strings.ReplaceAll(s, "`", "ˋ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// mdCell is mdInline plus escaping the GFM table column delimiter: a
// literal '|' splits the cell even inside a backtick span.
func mdCell(s string) string {
	return strings.ReplaceAll(mdInline(s), "|", "\\|")
}
