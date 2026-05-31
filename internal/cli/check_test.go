package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
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
		Skipped: []gitx.SkippedPath{{Path: "vendor/foo.go", Reason: gitx.SkipBinary}},
	}
}

func TestWriteCheckTable_PASS(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckTable(&buf, sampleResult(), 0.05); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "PASS") {
		t.Errorf("expected verdict PASS at start, got: %q", out)
	}
	for _, want := range []string{"a.go", "b.go", "modified", "added", "ΔS_density=0.0250"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestWriteCheckTable_FAIL(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10 // exceeds the 0.05 threshold below
	var buf bytes.Buffer
	if err := writeCheckTable(&buf, res, 0.05); err != nil {
		t.Fatalf("writeCheckTable: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "FAIL") {
		t.Errorf("expected verdict FAIL at start, got: %q", buf.String())
	}
}

func TestWriteCheckJSON_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckJSON(&buf, sampleResult()); err != nil {
		t.Fatalf("writeCheckJSON: %v", err)
	}
	var got pipeline.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON emitted: %v\nbytes: %s", err, buf.String())
	}
	if got.Base != "abc123" || got.Head != "def456" {
		t.Errorf("round-tripped Base/Head wrong: %+v", got)
	}
	if got.Delta.LinesChanged != 50 || len(got.Delta.Files) != 2 {
		t.Errorf("round-tripped Delta wrong: %+v", got.Delta)
	}
	// DeltaKind and SkipReason must serialize as strings for downstream
	// tooling. SetIndent inserts a space after colons so the substring
	// includes it.
	for _, want := range []string{`"kind": "added"`, `"reason": "binary"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JSON missing %q\nfull:\n%s", want, buf.String())
		}
	}
}

func TestCheckCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"base", "head", "json", "config", "recalibrate", "root"} {
		if checkCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
