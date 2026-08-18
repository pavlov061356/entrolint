package calibration

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

func TestValidateHistory(t *testing.T) {
	t.Run("ranks the pre-fix file", func(t *testing.T) {
		dir := initHistoryRepo(t)

		writeTree(t, dir, map[string]string{
			"simple.go": `package p
func Simple() {}
`,
			"complex.go": complexHistorySource(),
			"medium.go": `package p
func Medium(x int) int {
	if x > 0 { return x }
	return 0
}
`,
		})
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-q", "-m", "initial")
		parentSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))

		if err := os.WriteFile(
			filepath.Join(dir, "complex.go"),
			[]byte("package p\nfunc Complex() {}\n"),
			0o644,
		); err != nil {
			t.Fatalf("write fix: %v", err)
		}
		runGit(t, dir, "add", "complex.go")
		runGit(t, dir, "commit", "-q", "-m", "fix: handle complex edge case")
		frameSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))

		report, err := ValidateHistory(HistoryValidationOptions{
			Roots:           []string{dir},
			SearchLimit:     10,
			SamplesPerRepo:  1,
			MaxChangedFiles: 5,
		})
		if err != nil {
			t.Fatalf("ValidateHistory: %v", err)
		}
		if len(report.Candidates) != 1 {
			t.Fatalf("candidates = %d, want 1", len(report.Candidates))
		}
		got := report.Candidates[0]
		if len(got.Weights) != 6 || got.Weights["cyclomatic"] != 1 || got.Weights["nesting"] != 0.8 {
			t.Fatalf("effective weights = %v", got.Weights)
		}
		if got.Aggregate.Commits != 1 || got.Aggregate.LabeledFiles != 1 {
			t.Fatalf("aggregate = %+v, commits = %+v; want one commit and label", got.Aggregate, got.Repos[0].Commits)
		}
		if got.Aggregate.MeanAUC != 1 {
			t.Fatalf("mean AUC = %.3f, want 1", got.Aggregate.MeanAUC)
		}
		if got.Repos[0].FrameSHA != frameSHA || len(got.Repos[0].FrameSHA) != 40 {
			t.Fatalf("frame SHA = %q, want %q", got.Repos[0].FrameSHA, frameSHA)
		}
		commit := got.Repos[0].Commits[0]
		if !commit.Scored || fmt.Sprint(commit.Labels) != "[complex.go]" ||
			fmt.Sprint(commit.MatchedLabels) != "[complex.go]" {
			t.Fatalf("commit audit = %+v", commit)
		}
		if commit.Subject != "fix: handle complex edge case" {
			t.Fatalf("subject = %q", commit.Subject)
		}
		if commit.ParentSHA != parentSHA {
			t.Fatalf("parent SHA = %q, want %q", commit.ParentSHA, parentSHA)
		}
	})

	t.Run("keeps skipped commits auditable", func(t *testing.T) {
		dir := initHistoryRepo(t)
		writeTree(t, dir, map[string]string{
			"a.go":        "package p\nconst a = 1\n",
			"broken.go":   "package p\nfunc Broken(\n",
			"negative.go": "package p\nconst negative = 1\n",
		})
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-q", "-m", "initial")

		writeTree(t, dir, map[string]string{
			"a.go":      "package p\nconst a = 2\n",
			"broken.go": "package p\nfunc Broken() {}\n",
		})
		runGit(t, dir, "add", "a.go", "broken.go")
		runGit(t, dir, "commit", "-q", "-m", "fix: repair broken parser input")

		report, err := ValidateHistory(HistoryValidationOptions{
			Roots:           []string{dir},
			SearchLimit:     10,
			SamplesPerRepo:  1,
			MaxChangedFiles: 5,
		})
		if err != nil {
			t.Fatalf("ValidateHistory: %v", err)
		}
		repo := report.Candidates[0].Repos[0]
		if repo.SelectedCommits != 1 || repo.Summary.Commits != 0 {
			t.Fatalf("repo counts = selected %d, scored %d", repo.SelectedCommits, repo.Summary.Commits)
		}
		if repo.Summary != (HistorySummary{}) || report.Candidates[0].Aggregate != (HistorySummary{}) {
			t.Fatalf("skipped commit leaked into summaries: repo=%+v aggregate=%+v", repo.Summary, report.Candidates[0].Aggregate)
		}
		if len(repo.Commits) != 1 {
			t.Fatalf("audited commits = %d, want 1", len(repo.Commits))
		}
		commit := repo.Commits[0]
		if commit.Scored || commit.SkipReason != historySkipUnmatchedLabels {
			t.Fatalf("skipped commit = %+v", commit)
		}
		if fmt.Sprint(commit.Labels) != "[a.go broken.go]" ||
			fmt.Sprint(commit.MatchedLabels) != "[a.go]" ||
			fmt.Sprint(commit.UnmatchedLabels) != "[broken.go]" {
			t.Fatalf("skipped commit labels = %+v", commit)
		}
	})
}

func TestCollectHistorySamples_SelectsLatestEligibleCommit(t *testing.T) {
	dir := initHistoryRepo(t)
	writeTree(t, dir, map[string]string{
		"a.go": "package p\nconst a = 1\n",
		"b.go": "package p\nconst b = 1\n",
	})
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	writeTree(t, dir, map[string]string{"a.go": "package p\nconst a = 2\n"})
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "commit", "-q", "-m", "fix: first eligible change")

	writeTree(t, dir, map[string]string{"b.go": "package p\nconst b = 2\n"})
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "commit", "-q", "-m", "fix: latest eligible change")

	writeTree(t, dir, map[string]string{"new.go": "package p\nconst added = 1\n"})
	runGit(t, dir, "add", "new.go")
	runGit(t, dir, "commit", "-q", "-m", "fix: added-only change")

	samples, err := collectHistorySamples(
		gitx.LocalRunner{Dir: dir},
		"HEAD",
		10,
		1,
		10,
		regexp.MustCompile(DefaultHistorySubjectPattern),
	)
	if err != nil {
		t.Fatalf("collectHistorySamples: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(samples))
	}
	if got := samples[0].commit.Subject; got != "fix: latest eligible change" {
		t.Fatalf("selected subject = %q, want latest eligible change", got)
	}
	if got := fmt.Sprint(samples[0].labels); got != "[b.go]" {
		t.Fatalf("selected labels = %s, want [b.go]", got)
	}
}

func TestEvaluateHistoryCommit(t *testing.T) {
	t.Run("ties match random baseline", func(t *testing.T) {
		scores := make([]pipeline.FileScore, 10)
		for i := range scores {
			scores[i] = pipeline.FileScore{
				Path: fmt.Sprintf("f%d.go", i),
				S:    1,
			}
		}
		got, ok := evaluateHistoryCommit(scores, []string{"f0.go"})
		if !ok {
			t.Fatal("evaluateHistoryCommit rejected a valid sample")
		}
		assertClose(t, "AUC", got.AUC, 0.5)
		assertClose(t, "top 10 recall", got.Top10Recall, 0.1)
		assertClose(t, "top 20 recall", got.Top20Recall, 0.2)
		assertClose(t, "median percentile", got.MedianPositivePercentile, 0.5)
	})

	t.Run("reports ceiling adjusted random baselines", func(t *testing.T) {
		scores := []pipeline.FileScore{
			{Path: "top.go", S: 3},
			{Path: "middle.go", S: 2},
			{Path: "bottom.go", S: 1},
		}
		got, ok := evaluateHistoryCommit(scores, []string{"top.go"})
		if !ok {
			t.Fatal("evaluateHistoryCommit rejected a valid sample")
		}
		assertClose(t, "top 10 random baseline", got.Top10RandomBaseline, 1.0/3.0)
		assertClose(t, "top 20 random baseline", got.Top20RandomBaseline, 1.0/3.0)

		summary := summarizeHistory([]HistoryCommitReport{got})
		assertClose(t, "summary top 10 random baseline", summary.Top10RandomBaseline, 1.0/3.0)
		assertClose(t, "summary top 20 random baseline", summary.Top20RandomBaseline, 1.0/3.0)

		scores = make([]pipeline.FileScore, 11)
		for i := range scores {
			scores[i] = pipeline.FileScore{Path: fmt.Sprintf("f%d.go", i), S: float64(len(scores) - i)}
		}
		got, ok = evaluateHistoryCommit(scores, []string{"f0.go"})
		if !ok {
			t.Fatal("evaluateHistoryCommit rejected an eleven-file sample")
		}
		assertClose(t, "eleven-file top 10 random baseline", got.Top10RandomBaseline, 2.0/11.0)
		assertClose(t, "eleven-file top 20 random baseline", got.Top20RandomBaseline, 3.0/11.0)
	})

	t.Run("requires positive and negative files", func(t *testing.T) {
		scores := []pipeline.FileScore{{Path: "only.go", S: 1}}
		missing, ok := evaluateHistoryCommit(scores, []string{"missing.go"})
		if ok {
			t.Fatal("missing label must not produce a sample")
		}
		if missing.Scored || missing.SkipReason != historySkipUnmatchedLabels {
			t.Fatalf("missing label report = %+v", missing)
		}

		only, ok := evaluateHistoryCommit(scores, []string{"only.go"})
		if ok {
			t.Fatal("sample without negative files must be rejected")
		}
		if only.Scored || only.SkipReason != historySkipNoNegativeFiles {
			t.Fatalf("no-negative report = %+v", only)
		}
	})

	t.Run("rejects partial labels without hiding them", func(t *testing.T) {
		scores := []pipeline.FileScore{
			{Path: "a.go", S: 2},
			{Path: "b.go", S: 1},
		}
		got, ok := evaluateHistoryCommit(scores, []string{"a.go", "broken.go"})
		if ok {
			t.Fatal("partially matched labels must not produce a scored sample")
		}
		if got.Scored || got.SkipReason != historySkipUnmatchedLabels {
			t.Fatalf("partial-label report = %+v", got)
		}
		if fmt.Sprint(got.Labels) != "[a.go broken.go]" {
			t.Fatalf("labels = %v", got.Labels)
		}
		if fmt.Sprint(got.MatchedLabels) != "[a.go]" {
			t.Fatalf("matched labels = %v", got.MatchedLabels)
		}
		if fmt.Sprint(got.UnmatchedLabels) != "[broken.go]" {
			t.Fatalf("unmatched labels = %v", got.UnmatchedLabels)
		}
	})

	t.Run("averages the middle positive percentiles", func(t *testing.T) {
		scores := []pipeline.FileScore{
			{Path: "top.go", S: 3},
			{Path: "middle.go", S: 2},
			{Path: "bottom.go", S: 1},
		}
		got, ok := evaluateHistoryCommit(scores, []string{"top.go", "bottom.go"})
		if !ok {
			t.Fatal("evaluateHistoryCommit rejected a valid sample")
		}
		assertClose(t, "commit median percentile", got.MedianPositivePercentile, 0.5)

		summary := summarizeHistory([]HistoryCommitReport{got})
		assertClose(t, "summary median percentile", summary.MedianPositivePercentile, 0.5)
	})
}

func TestExistingGoLabels_UsesOnlyPreExistingAnalyzablePaths(t *testing.T) {
	changes := []gitx.Change{
		{Kind: gitx.ChangeModified, Path: "modified.go"},
		{Kind: gitx.ChangeAdded, Path: "added.go"},
		{Kind: gitx.ChangeRemoved, Path: "removed.go"},
		{Kind: gitx.ChangeRenamed, OldPath: "pure.go", Path: "pure_new.go"},
		{
			Kind:         gitx.ChangeRenamed,
			OldPath:      "edited.go",
			Path:         "edited_new.go",
			LinesAdded:   1,
			LinesRemoved: 1,
		},
		{Kind: gitx.ChangeModified, Path: "vendor/ignored.go"},
		{Kind: gitx.ChangeModified, Path: "README.md"},
	}
	got := existingGoLabels(changes)
	want := []string{"edited.go", "modified.go", "removed.go"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

func TestNormalizeHistoryOptions_RejectsInvalidPattern(t *testing.T) {
	_, _, err := normalizeHistoryOptions(HistoryValidationOptions{
		Roots:          []string{"."},
		SubjectPattern: "[",
	})
	if err == nil {
		t.Fatal("invalid subject regexp must fail")
	}
}

func complexHistorySource() string {
	return `package p
func Complex(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			for i := 0; i < x; i++ {
			if i%2 == 0 {
					total += i
				}
			}
		}
	}
	return total
}
`
}

func initHistoryRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Entrolint Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %.6f, want %.6f", name, got, want)
	}
}
