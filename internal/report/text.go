package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// CheckReport is the JSON envelope `entrolint check --format json` emits.
// Verdict + thresholds live here (not on pipeline.CheckResult) so the
// engine layer stays gate-policy-free — thresholds are a CLI/config
// concern. Downstream tooling can read Verdict + Reasons without
// recomputing them.
type CheckReport struct {
	Verdict         string               `json:"verdict"`
	Reasons         []string             `json:"reasons,omitempty"`
	Threshold       float64              `json:"threshold"`
	ScalingClassMax scaling.Class        `json:"scaling_class_max"`
	Result          pipeline.CheckResult `json:"result"`
}

// CheckJSON renders the check result as the indented JSON envelope, with
// a trailing newline (matching the historical streaming-encoder output).
func CheckJSON(res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) ([]byte, error) {
	verdict := "pass"
	if v.Failed {
		verdict = "fail"
	}
	b, err := json.MarshalIndent(CheckReport{
		Verdict:         verdict,
		Reasons:         v.Reasons,
		Threshold:       cfg.DeltaSMax,
		ScalingClassMax: cfg.ScalingClassMax,
		Result:          res,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// CheckTable renders the human-readable check summary: a verdict line,
// the failure reasons, the non-O(1) scaling hits, and the per-file ΔS
// table. Built into a strings.Builder (which never errors), so the
// renderer stays branch-free at the I/O layer.
func CheckTable(res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) string {
	var b strings.Builder
	label := "PASS"
	if v.Failed {
		label = "FAIL"
	}
	fmt.Fprintf(&b,
		"%s  ΔS_total=%.4f  ΔS_density=%.4f  threshold=%.4f  scaling_class=%s  lines_changed=%d  files=%d\n",
		label, res.Delta.Total, res.Delta.Density, cfg.DeltaSMax,
		res.Scaling.Class, res.Delta.LinesChanged, len(res.Delta.Files),
	)
	for _, reason := range v.Reasons {
		fmt.Fprintf(&b, "  reason: %s\n", reason)
	}
	for _, h := range nonO1Hits(res.Scaling) {
		fmt.Fprintf(&b, "  scaling: %s %s in %s", h.Detector, h.Class, h.Path)
		if h.Size > 0 {
			fmt.Fprintf(&b, " (size=%d)", h.Size)
		}
		if h.Evidence != "" {
			b.WriteString(" — " + h.Evidence)
		}
		b.WriteByte('\n')
	}
	if len(res.Delta.Files) > 0 {
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "KIND\tPATH\tS_BASE\tS_HEAD\tΔ")
		for _, f := range res.Delta.Files {
			fmt.Fprintf(tw, "%s\t%s\t%.3f\t%.3f\t%+.3f\n", f.Kind, f.Path, f.SBase, f.SHead, f.Delta)
		}
		_ = tw.Flush() // Flush to a strings.Builder cannot fail.
	}
	return b.String()
}

// ScanTable renders scan scores as the PATH/S/T/DOMINANT table.
func ScanTable(files []pipeline.FileScore) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tS\tT\tDOMINANT")
	for _, f := range files {
		fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%s\n", f.Path, f.S, f.T, f.Dominant)
	}
	_ = tw.Flush()
	return b.String()
}

// ScanJSON renders scan scores as indented JSON with a trailing newline.
func ScanJSON(files []pipeline.FileScore) ([]byte, error) {
	b, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// HistoryTable renders total entropy over recent commits as an S(t) table.
func HistoryTable(res pipeline.HistoryResult) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SHA\tDATE\tFILES\tS\tSUBJECT")
	for _, p := range res.Points {
		date := p.CommitTime
		if len(date) >= len("2006-01-02") {
			date = date[:len("2006-01-02")]
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%.2f\t%s\n", p.ShortSHA, date, p.FileCount, p.S, p.Subject)
	}
	_ = tw.Flush()
	return b.String()
}

// HistoryJSON renders the S(t) result as indented JSON with a trailing newline.
func HistoryJSON(res pipeline.HistoryResult) ([]byte, error) {
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// nonO1Hits flattens the non-O(1) detector hits across a scaling result,
// preserving file/hit order. Shared by the table and Markdown renderers
// so the "why is the class elevated" iteration lives in exactly one
// place.
func nonO1Hits(r scaling.Result) []scaling.Hit {
	var hits []scaling.Hit
	for _, f := range r.Files {
		for _, h := range f.Hits {
			if h.Class != scaling.ClassO1 {
				hits = append(hits, h)
			}
		}
	}
	return hits
}
