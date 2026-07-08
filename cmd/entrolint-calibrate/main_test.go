package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_TableOutput(t *testing.T) {
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

	var out bytes.Buffer
	if err := run([]string{dir}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"# entrolint calibration report",
		"## Candidate: default",
		"| scope | files | total S | median S | p90 S | dominant |",
		"| aggregate | 2 |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRun_RejectsMalformedCandidate(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--candidate", "bad", "."}, &out); err == nil {
		t.Fatal("run must reject malformed candidate")
	}
}

func TestRun_RejectsMissingCandidateFile(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--candidate", "missing=/does/not/exist.yaml", "."}, &out); err == nil {
		t.Fatal("run must reject a missing candidate file")
	}
}

func TestRun_IncludesDefaultWithCandidates(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package p
func A() {}
`,
	})
	cfgPath := filepath.Join(dir, "candidate.yaml")
	if err := os.WriteFile(cfgPath, []byte("weights:\n  length: 0\n"), 0o644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"--candidate", "no-length=" + cfgPath, dir}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "## Candidate: default") {
		t.Fatalf("output missing default candidate:\n%s", got)
	}
	if !strings.Contains(got, "## Candidate: no-length") {
		t.Fatalf("output missing custom candidate:\n%s", got)
	}
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
