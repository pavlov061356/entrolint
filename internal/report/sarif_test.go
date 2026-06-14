package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

func sampleScores() []pipeline.FileScore {
	// Pre-sorted by T desc, as pipeline.Scan emits.
	return []pipeline.FileScore{
		{Path: "internal/hot/burn.go", S: 3.20, T: 3.40, Dominant: "cyclomatic"},
		{Path: "internal/warm/heat.go", S: 1.80, T: 1.90, Dominant: "nesting"},
		{Path: "internal/mild/note.go", S: 1.05, T: 1.10, Dominant: "length"},
		{Path: "internal/cold/calm.go", S: 0.40, T: 0.45, Dominant: "length"},
	}
}

func TestScanSARIF_ValidJSONAndBands(t *testing.T) {
	out, err := ScanSARIF(sampleScores(), DefaultSARIFOptions("0.4.0"))
	if err != nil {
		t.Fatalf("ScanSARIF: %v", err)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Error("SARIF output must end with a trailing newline")
	}

	var log sarifLog
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatalf("emitted SARIF is not valid JSON: %v\n%s", err, out)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "entrolint" || run.Tool.Driver.Version != "0.4.0" {
		t.Errorf("driver = %+v, want name=entrolint version=0.4.0", run.Tool.Driver)
	}
	if len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != highEntropyID {
		t.Errorf("rules = %+v, want one rule %q", run.Tool.Driver.Rules, highEntropyID)
	}

	// The cold file (T=0.45 < NoteFloor 1.0) is dropped; the other three remain.
	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3 (cold file below floor)", len(run.Results))
	}
	wantLevels := map[string]string{
		"internal/hot/burn.go":  levelWarning, // T 3.40: error is capped to warning pre-v1.0
		"internal/warm/heat.go": levelWarning, // T 1.90 ≥ 1.5
		"internal/mild/note.go": levelNote,    // T 1.10 ≥ 1.0
	}
	for _, r := range run.Results {
		uri := r.Locations[0].PhysicalLocation.ArtifactLocation.URI
		want, ok := wantLevels[uri]
		if !ok {
			t.Errorf("unexpected result for %q", uri)
			continue
		}
		if r.Level != want {
			t.Errorf("%s level = %q, want %q", uri, r.Level, want)
		}
		if r.RuleID != highEntropyID {
			t.Errorf("%s ruleId = %q, want %q", uri, r.RuleID, highEntropyID)
		}
		if r.Locations[0].PhysicalLocation.Region.StartLine != 1 {
			t.Errorf("%s startLine = %d, want 1", uri, r.Locations[0].PhysicalLocation.Region.StartLine)
		}
		if r.PartialFingerprints["entrolintPath"] != uri {
			t.Errorf("%s fingerprint = %q, want %q", uri, r.PartialFingerprints["entrolintPath"], uri)
		}
	}
}

func TestScanSARIF_DefaultNeverEmitsError(t *testing.T) {
	// Pre-v1.0 the default caps at warning so GitHub Code Scanning never reds
	// an advisory run. Even a very hot file must not reach `error`.
	out, err := ScanSARIF([]pipeline.FileScore{{Path: "blaze.go", S: 9, T: 99, Dominant: "cyclomatic"}}, DefaultSARIFOptions("0.6.0"))
	if err != nil {
		t.Fatalf("ScanSARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatal(err)
	}
	if got := log.Runs[0].Results[0].Level; got != levelWarning {
		t.Errorf("default level for T=99 = %q, want %q (error disabled pre-v1.0)", got, levelWarning)
	}
}

func TestScanSARIF_ExplicitErrorBandStillEscalates(t *testing.T) {
	// The error band is opt-in: a consumer who wants a hard band sets ErrorAt.
	opts := DefaultSARIFOptions("0.6.0")
	opts.ErrorAt = 3.0
	out, err := ScanSARIF(sampleScores(), opts)
	if err != nil {
		t.Fatalf("ScanSARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatal(err)
	}
	var burn string
	for _, r := range log.Runs[0].Results {
		if r.Locations[0].PhysicalLocation.ArtifactLocation.URI == "internal/hot/burn.go" {
			burn = r.Level
		}
	}
	if burn != levelError {
		t.Errorf("with explicit ErrorAt=3.0, burn.go (T=3.40) level = %q, want %q", burn, levelError)
	}
}

func TestScanSARIF_EmptyInputStillValid(t *testing.T) {
	out, err := ScanSARIF(nil, DefaultSARIFOptions("dev"))
	if err != nil {
		t.Fatalf("ScanSARIF(nil): %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatalf("invalid SARIF for empty input: %v", err)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 0 {
		t.Errorf("empty input must yield a run with zero results, got %+v", log.Runs)
	}
}

func TestScanSARIF_ZeroOptionsUsesDefaults(t *testing.T) {
	// A zero-value options struct (only the version set) must behave like
	// DefaultSARIFOptions, not flag every file at floor 0.
	out, err := ScanSARIF(sampleScores(), SARIFOptions{ToolVersion: "x"})
	if err != nil {
		t.Fatalf("ScanSARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatal(err)
	}
	if len(log.Runs[0].Results) != 3 {
		t.Errorf("zero-options results = %d, want 3 (defaults applied)", len(log.Runs[0].Results))
	}
}
