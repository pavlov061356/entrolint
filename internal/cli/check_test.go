package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func sampleResult() pipeline.CheckResult {
	return pipeline.CheckResult{
		Base: "abc123",
		Head: "def456",
		Delta: thermo.Delta{
			Total:        1.25,
			Density:      0.025,
			LinesChanged: 50,
			Files: []thermo.FileDelta{
				thermo.MakeFileDelta("a.go", thermo.DeltaModified, 0.5, 1.0),
				thermo.MakeFileDelta("b.go", thermo.DeltaAdded, 0, 0.75),
			},
		},
		Scaling: scaling.Result{
			Class: scaling.ClassO1,
			Files: []scaling.FileResult{
				{Path: "a.go", Class: scaling.ClassO1},
				{Path: "b.go", Class: scaling.ClassO1},
			},
		},
		Skipped: []gitx.SkippedPath{{Path: "vendor/foo.go", Reason: gitx.SkipBinary}},
	}
}

func defaultCfg() config.Config { return config.Default() }

func TestWriteCheckTable_PASS(t *testing.T) {
	var buf bytes.Buffer
	res := sampleResult()
	cfg := defaultCfg()
	if err := writeCheckTable(&buf, res, cfg, res.Verdict(cfg)); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	out := buf.String()
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

func TestWriteCheckTable_FailLinesIncludeReasons(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10            // trips ΔS gate
	res.Scaling.Class = scaling.ClassON // trips scaling gate
	cfg := defaultCfg()
	var buf bytes.Buffer
	if err := writeCheckTable(&buf, res, cfg, res.Verdict(cfg)); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	out := buf.String()
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

func TestWriteCheckTable_RendersNonO1Hits(t *testing.T) {
	res := sampleResult()
	res.Scaling = scaling.Result{
		Class: scaling.ClassOk,
		Files: []scaling.FileResult{
			{
				Path:  "internal/foo/iface.go",
				Class: scaling.ClassOk,
				Hits: []scaling.Hit{
					{
						Detector: "implementor_scan",
						Class:    scaling.ClassOk,
						Size:     7,
						Path:     "internal/foo/iface.go",
						Evidence: "7 implementors touched",
					},
				},
			},
		},
	}
	cfg := defaultCfg()
	var buf bytes.Buffer
	if err := writeCheckTable(&buf, res, cfg, res.Verdict(cfg)); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"implementor_scan", "O(k)", "size=7", "7 implementors touched"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestWriteCheckJSON_RoundTripsPASS(t *testing.T) {
	res := sampleResult()
	cfg := defaultCfg()
	var buf bytes.Buffer
	if err := writeCheckJSON(&buf, res, cfg, res.Verdict(cfg)); err != nil {
		t.Fatalf("writeCheckJSON: %v", err)
	}
	var got checkJSONReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON emitted: %v\nbytes: %s", err, buf.String())
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
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JSON missing %q\nfull:\n%s", want, buf.String())
		}
	}
}

func TestWriteCheckJSON_FailReportsBothReasons(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10
	res.Scaling.Class = scaling.ClassON
	cfg := defaultCfg()
	var buf bytes.Buffer
	if err := writeCheckJSON(&buf, res, cfg, res.Verdict(cfg)); err != nil {
		t.Fatalf("writeCheckJSON: %v", err)
	}
	var got checkJSONReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON emitted: %v", err)
	}
	if got.Verdict != "fail" {
		t.Errorf("Verdict = %q, want fail", got.Verdict)
	}
	if len(got.Reasons) != 2 {
		t.Errorf("expected 2 reasons, got %v", got.Reasons)
	}
}

func TestCheckCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"base", "head", "format", "json", "config", "recalibrate", "root"} {
		if checkCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
