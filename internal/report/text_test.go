package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func TestCheckTable_PASS(t *testing.T) {
	res := sampleResult()
	cfg := config.Default()
	out := CheckTable(res, cfg, res.Verdict(cfg))
	if !strings.HasPrefix(out, "PASS") {
		t.Errorf("expected verdict PASS at start, got: %q", out)
	}
	for _, want := range []string{"a.go", "b.go", "modified", "added", "ΔS_density=0.0250", "scaling_class=O(1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
	if strings.Contains(out, "reason:") {
		t.Errorf("PASS output must not contain reason lines:\n%s", out)
	}
}

func TestCheckTable_FailLinesIncludeReasons(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10            // trips ΔS gate
	res.Scaling.Class = scaling.ClassON // trips scaling gate
	cfg := config.Default()
	out := CheckTable(res, cfg, res.Verdict(cfg))
	if !strings.HasPrefix(out, "FAIL") {
		t.Errorf("expected FAIL at start, got: %q", out)
	}
	if !strings.Contains(out, "reason: ΔS_density") {
		t.Errorf("missing ΔS_density reason line:\n%s", out)
	}
	if !strings.Contains(out, "reason: scaling_class") {
		t.Errorf("missing scaling_class reason line:\n%s", out)
	}
}

func TestCheckTable_RendersNonO1Hits(t *testing.T) {
	res := sampleResult()
	res.Scaling = scaling.Result{
		Class: scaling.ClassOk,
		Files: []scaling.FileResult{{
			Path:  "internal/foo/iface.go",
			Class: scaling.ClassOk,
			Hits: []scaling.Hit{{
				Detector: "implementor_scan",
				Class:    scaling.ClassOk,
				Size:     7,
				Path:     "internal/foo/iface.go",
				Evidence: "7 implementors touched",
			}},
		}},
	}
	cfg := config.Default()
	out := CheckTable(res, cfg, res.Verdict(cfg))
	for _, want := range []string{"implementor_scan", "O(k)", "size=7", "7 implementors touched"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestCheckJSON_RoundTripsPASS(t *testing.T) {
	res := sampleResult()
	cfg := config.Default()
	data, err := CheckJSON(res, cfg, res.Verdict(cfg))
	if err != nil {
		t.Fatalf("CheckJSON: %v", err)
	}
	var got CheckReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON emitted: %v\nbytes: %s", err, data)
	}
	if got.Verdict != "pass" {
		t.Errorf("Verdict = %q, want pass", got.Verdict)
	}
	if got.Threshold != cfg.DeltaSMax {
		t.Errorf("Threshold = %v, want %v", got.Threshold, cfg.DeltaSMax)
	}
	if got.ScalingClassMax != cfg.ScalingClassMax {
		t.Errorf("ScalingClassMax = %v, want %v", got.ScalingClassMax, cfg.ScalingClassMax)
	}
	if got.Result.Scaling.Class != scaling.ClassO1 {
		t.Errorf("Scaling.Class round-trip lost: got %v", got.Result.Scaling.Class)
	}
	if len(got.Reasons) != 0 {
		t.Errorf("PASS Reasons must be empty, got %v", got.Reasons)
	}
	for _, want := range []string{`"kind": "added"`, `"reason": "binary"`, `"class": "O(1)"`, `"scaling_class_max": "O(k)"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON missing %q\nfull:\n%s", want, data)
		}
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("CheckJSON must end with a trailing newline")
	}
}

func TestCheckJSON_FailReportsBothReasons(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10
	res.Scaling.Class = scaling.ClassON
	cfg := config.Default()
	data, err := CheckJSON(res, cfg, res.Verdict(cfg))
	if err != nil {
		t.Fatalf("CheckJSON: %v", err)
	}
	var got CheckReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON emitted: %v", err)
	}
	if got.Verdict != "fail" {
		t.Errorf("Verdict = %q, want fail", got.Verdict)
	}
	if len(got.Reasons) != 2 {
		t.Errorf("expected 2 reasons, got %v", got.Reasons)
	}
}

func TestScanTableAndJSON(t *testing.T) {
	files := []pipeline.FileScore{
		{Path: "a.go", S: 1.23, T: 1.50, Dominant: "cyclomatic"},
		{Path: "b.go", S: 0.40, T: 0.45, Dominant: "length"},
	}
	table := ScanTable(files)
	if !strings.HasPrefix(table, "PATH") {
		t.Errorf("table must start with the header, got:\n%s", table)
	}
	for _, want := range []string{"a.go", "1.23", "cyclomatic", "b.go"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q\n%s", want, table)
		}
	}

	data, err := ScanJSON(files)
	if err != nil {
		t.Fatalf("ScanJSON: %v", err)
	}
	var got []pipeline.FileScore
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if len(got) != 2 || got[0].Path != "a.go" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("ScanJSON must end with a trailing newline")
	}
}

func TestHistoryTableAndJSON(t *testing.T) {
	res := pipeline.HistoryResult{
		Ref: "HEAD",
		Points: []pipeline.HistoryPoint{
			{
				SHA:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ShortSHA:   "aaaaaaa",
				CommitTime: "2026-07-01T10:00:00+03:00",
				Subject:    "start",
				S:          1.25,
				FileCount:  3,
			},
		},
	}
	table := HistoryTable(res)
	for _, want := range []string{"SHA", "2026-07-01", "aaaaaaa", "1.25", "start"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q\n%s", want, table)
		}
	}

	data, err := HistoryJSON(res)
	if err != nil {
		t.Fatalf("HistoryJSON: %v", err)
	}
	var got pipeline.HistoryResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if got.Ref != "HEAD" || len(got.Points) != 1 || got.Points[0].ShortSHA != "aaaaaaa" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("HistoryJSON must end with a trailing newline")
	}
}

func TestNonO1Hits_FiltersAndFlattens(t *testing.T) {
	r := scaling.Result{Files: []scaling.FileResult{
		{Hits: []scaling.Hit{
			{Detector: "a", Class: scaling.ClassO1},
			{Detector: "b", Class: scaling.ClassOk},
		}},
		{Hits: []scaling.Hit{{Detector: "c", Class: scaling.ClassON}}},
	}}
	got := nonO1Hits(r)
	if len(got) != 2 || got[0].Detector != "b" || got[1].Detector != "c" {
		t.Errorf("nonO1Hits = %+v, want [b, c]", got)
	}
}
