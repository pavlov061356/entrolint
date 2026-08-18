package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/calibration"
)

func TestRun(t *testing.T) {
	t.Run("renders table output", func(t *testing.T) {
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
	})

	t.Run("rejects malformed candidate", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--candidate", "bad", "."}, &out); err == nil {
			t.Fatal("run must reject malformed candidate")
		}
	})

	t.Run("rejects missing candidate file", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--candidate", "missing=/does/not/exist.yaml", "."}, &out); err == nil {
			t.Fatal("run must reject a missing candidate file")
		}
	})

	t.Run("history requires root", func(t *testing.T) {
		var out bytes.Buffer
		err := run([]string{"history"}, &out)
		if err == nil || !strings.Contains(err.Error(), "entrolint-calibrate history") {
			t.Fatalf("run history error = %v, want usage error", err)
		}
	})

	t.Run("history rejects invalid subject regexp before git", func(t *testing.T) {
		var out bytes.Buffer
		err := run([]string{"history", "--subject-regexp", "[", "."}, &out)
		if err == nil || !strings.Contains(err.Error(), "subject regexp") {
			t.Fatalf("run history error = %v, want regexp error", err)
		}
	})

	t.Run("includes default with candidates", func(t *testing.T) {
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
	})

	t.Run("requires a candidate when default is excluded", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"a.go": "package p\nfunc A() {}\n",
		})

		for _, args := range [][]string{
			{"--include-default=false", dir},
			{"history", "--include-default=false", dir},
		} {
			var out bytes.Buffer
			err := run(args, &out)
			if err == nil || !strings.Contains(err.Error(), "at least one candidate") {
				t.Fatalf("run %v error = %v, want at least one candidate error", args, err)
			}
			if out.Len() != 0 {
				t.Fatalf("run %v wrote output before candidate validation: %q", args, out.String())
			}
		}
	})

	t.Run("renders auditable history JSON", func(t *testing.T) {
		dir := t.TempDir()
		runCalibrationGit(t, dir, "init", "-q")
		runCalibrationGit(t, dir, "config", "user.email", "test@example.com")
		runCalibrationGit(t, dir, "config", "user.name", "Entrolint Test")
		runCalibrationGit(t, dir, "config", "commit.gpgsign", "false")
		writeTree(t, dir, map[string]string{
			"simple.go": "package p\nfunc Simple() {}\n",
			"complex.go": `package p
func Complex(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 { total += x }
	}
	return total
}
`,
			"medium.go": "package p\nfunc Medium(x int) int { if x > 0 { return x }; return 0 }\n",
		})
		runCalibrationGit(t, dir, "add", ".")
		runCalibrationGit(t, dir, "commit", "-q", "-m", "initial")
		writeTree(t, dir, map[string]string{
			"complex.go": "package p\nfunc Complex(xs []int) int { return len(xs) }\n",
		})
		runCalibrationGit(t, dir, "add", "complex.go")
		runCalibrationGit(t, dir, "commit", "-q", "-m", "fix: simplify complex path")
		frameSHA := strings.TrimSpace(runCalibrationGit(t, dir, "rev-parse", "HEAD"))
		candidatePath := filepath.Join(t.TempDir(), "partial.yaml")
		if err := os.WriteFile(candidatePath, []byte("weights:\n  length: 0.25\n"), 0o644); err != nil {
			t.Fatalf("write candidate: %v", err)
		}

		var out bytes.Buffer
		if err := run([]string{
			"history",
			"--format", "json",
			"--include-default=false",
			"--candidate", "partial=" + candidatePath,
			"--search-limit", "10",
			dir,
		}, &out); err != nil {
			t.Fatalf("run history JSON: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
			t.Fatalf("decode raw history JSON: %v\n%s", err, out.String())
		}
		rawCandidates, ok := raw["candidates"].([]any)
		if !ok || len(rawCandidates) != 1 {
			t.Fatalf("raw candidates = %#v", raw["candidates"])
		}
		rawCandidate, ok := rawCandidates[0].(map[string]any)
		if !ok {
			t.Fatalf("raw candidate = %#v", rawCandidates[0])
		}
		if _, ok := rawCandidate["weights"].(map[string]any); !ok {
			t.Fatalf("raw candidate weights = %#v", rawCandidate["weights"])
		}
		rawRepos, ok := rawCandidate["repos"].([]any)
		if !ok || len(rawRepos) != 1 {
			t.Fatalf("raw repos = %#v", rawCandidate["repos"])
		}
		rawRepo, ok := rawRepos[0].(map[string]any)
		if !ok || rawRepo["frame_sha"] != frameSHA {
			t.Fatalf("raw repo = %#v, want frame_sha %q", rawRepos[0], frameSHA)
		}
		rawCommits, ok := rawRepo["commits"].([]any)
		if !ok || len(rawCommits) != 1 {
			t.Fatalf("raw commits = %#v", rawRepo["commits"])
		}
		rawCommit, ok := rawCommits[0].(map[string]any)
		if !ok {
			t.Fatalf("raw commit = %#v", rawCommits[0])
		}
		for _, key := range []string{"labels", "matched_labels", "unmatched_labels", "scored"} {
			if _, ok := rawCommit[key]; !ok {
				t.Fatalf("raw commit missing %q: %#v", key, rawCommit)
			}
		}

		var report calibration.HistoryValidationReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("decode history JSON: %v\n%s", err, out.String())
		}
		candidate := report.Candidates[0]
		wantWeights := map[string]float64{
			"cyclomatic":        1,
			"nesting":           0.8,
			"coupling":          0.6,
			"length":            0.25,
			"duplication":       0.7,
			"cross_duplication": 0.7,
		}
		if candidate.Name != "partial" || len(candidate.Weights) != len(wantWeights) {
			t.Fatalf("effective weights = %v", candidate.Weights)
		}
		for name, want := range wantWeights {
			if candidate.Weights[name] != want {
				t.Fatalf("effective weight %s = %v, want %v; all weights = %v", name, candidate.Weights[name], want, candidate.Weights)
			}
		}
		repo := candidate.Repos[0]
		if repo.FrameSHA != frameSHA || len(repo.Commits) != 1 {
			t.Fatalf("repo audit = %+v, want frame %s and one commit", repo, frameSHA)
		}
		commit := repo.Commits[0]
		if !commit.Scored || len(commit.Labels) != 1 || commit.Labels[0] != "complex.go" ||
			len(commit.MatchedLabels) != 1 || len(commit.UnmatchedLabels) != 0 {
			t.Fatalf("commit audit = %+v", commit)
		}
	})
}

func TestRenderHistoryTable(t *testing.T) {
	report := calibration.HistoryValidationReport{
		Protocol: calibration.HistoryProtocol{
			Ref:             "HEAD",
			SearchLimit:     500,
			SamplesPerRepo:  10,
			MaxChangedFiles: 10,
		},
		Candidates: []calibration.HistoryCandidateReport{{
			Name: "default",
			Repos: []calibration.HistoryRepoReport{{
				Root:            "/repo",
				SelectedCommits: 2,
				Summary: calibration.HistorySummary{
					Commits:                  2,
					LabeledFiles:             3,
					MeanAUC:                  0.75,
					Top10Recall:              1.0 / 3.0,
					Top20Recall:              2.0 / 3.0,
					Top10RandomBaseline:      0.12,
					Top20RandomBaseline:      0.22,
					MedianPositivePercentile: 0.8,
				},
			}},
			Aggregate: calibration.HistorySummary{
				Commits:                  2,
				LabeledFiles:             3,
				MeanAUC:                  0.75,
				Top10Recall:              1.0 / 3.0,
				Top20Recall:              2.0 / 3.0,
				Top10RandomBaseline:      0.12,
				Top20RandomBaseline:      0.22,
				MedianPositivePercentile: 0.8,
			},
		}},
	}

	var out bytes.Buffer
	renderHistoryTable(&out, report)
	got := out.String()
	for _, want := range []string{
		"# entrolint history validation report",
		"Random recall baselines use each commit's ceiling-rounded cutoff.",
		"## Candidate: default",
		"| aggregate | 2 | 2 | 3 | 0.750 | 33.3% | 12.0% | 66.7% | 22.0% | 80.0% |",
		"| /repo | 2 | 2 | 3 |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
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

func runCalibrationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
