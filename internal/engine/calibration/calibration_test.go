package calibration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
)

func TestRun_DefaultCandidate(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A(x int) int {
	if x > 0 { return x }
	return -x
}
`,
		"b.go": `package p
func B(xs []int) int {
	var total int
	for _, x := range xs {
		if x%2 == 0 { total += x }
	}
	return total
}
`,
	})

	report, err := Run(Options{Roots: []string{dir}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(report.Candidates))
	}
	got := report.Candidates[0]
	if got.Name != "default" {
		t.Fatalf("candidate name = %q, want default", got.Name)
	}
	if got.Aggregate.Files != 2 {
		t.Fatalf("aggregate files = %d, want 2", got.Aggregate.Files)
	}
	if got.Aggregate.TotalS <= 0 {
		t.Fatalf("aggregate TotalS = %v, want positive", got.Aggregate.TotalS)
	}
	if share(got.Aggregate, "cyclomatic") <= 0 {
		t.Fatal("cyclomatic share must be positive for branching files")
	}
}

func TestRun_CandidateWeightsAffectContributionShares(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"short.go": `package p
func Short() {}
`,
		"long.go": `package p
func Long() {
	_ = 1
	_ = 2
	_ = 3
	_ = 4
	_ = 5
}
`,
	})

	noLength := config.Default()
	noLength.Weights["length"] = 0
	report, err := Run(Options{
		Roots: []string{dir},
		Candidates: []Candidate{
			{Name: "default", Config: config.Default()},
			{Name: "no-length", Config: noLength},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(report.Candidates))
	}
	if got := share(report.Candidates[1].Aggregate, "length"); got != 0 {
		t.Fatalf("no-length length share = %v, want 0", got)
	}
}

func TestRun_CandidateWithoutConfigUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A(x int) int {
	if x > 0 { return x }
	return -x
}
`,
		"b.go": `package p
func B() {}
`,
	})

	report, err := Run(Options{
		Roots:      []string{dir},
		Candidates: []Candidate{{Name: "implicit-default"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := report.Candidates[0].Aggregate.TotalS; got <= 0 {
		t.Fatalf("TotalS = %v, want positive default scoring", got)
	}
}

func TestRun_RequiresRoot(t *testing.T) {
	if _, err := Run(Options{}); err == nil {
		t.Fatal("Run without roots must fail")
	}
}

func share(s Summary, name string) float64 {
	for _, c := range s.Contributions {
		if c.Name == name {
			return c.Share
		}
	}
	return 0
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}
