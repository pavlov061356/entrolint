package pipeline

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
)

type historyRunner struct {
	trees map[string][]string
	blobs map[string]map[string]string
}

func (h *historyRunner) Run(args ...string) ([]byte, error) {
	switch args[0] {
	case "log":
		return []byte(
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x1faaaaaaa\x1f2026-07-01T10:00:00+03:00\x1fstart\x1e" +
				"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x1fbbbbbbb\x1f2026-07-02T10:00:00+03:00\x1fgrow\x1e",
		), nil
	case "ls-tree":
		ref := args[len(args)-1]
		var b strings.Builder
		for _, path := range h.trees[ref] {
			b.WriteString(path)
			b.WriteByte(0)
		}
		return []byte(b.String()), nil
	case "cat-file":
		spec := args[2]
		parts := strings.SplitN(spec, ":", 2)
		ref, path := parts[0], parts[1]
		if content, ok := h.blobs[ref][path]; ok {
			return []byte(content), nil
		}
		return nil, fmt.Errorf("fatal: path '%s' does not exist in '%s'", path, ref)
	default:
		return nil, fmt.Errorf("unhandled git call: %v", args)
	}
}

func TestHistory_ScoresRecentCommitsInOneCalibrationFrame(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"simple.go":  simpleSource(),
		"complex.go": complexSource(),
	})
	r := &historyRunner{
		trees: map[string][]string{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {"simple.go"},
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": {"simple.go", "complex.go", "README.md"},
		},
		blobs: map[string]map[string]string{
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {
				"simple.go": simpleSource(),
			},
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": {
				"simple.go":  simpleSource(),
				"complex.go": complexSource(),
			},
		},
	}

	got, err := History(HistoryOptions{
		Root:        dir,
		Ref:         "HEAD",
		Limit:       2,
		FirstParent: true,
		ScanOptions: ScanOptions{Config: config.Default()},
		GitRunner:   r,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got.Points) != 2 {
		t.Fatalf("got %d points, want 2", len(got.Points))
	}
	if got.Points[0].ShortSHA != "aaaaaaa" || got.Points[1].ShortSHA != "bbbbbbb" {
		t.Errorf("points out of order: %+v", got.Points)
	}
	if got.Points[0].FileCount != 1 || got.Points[1].FileCount != 2 {
		t.Errorf("file counts = %d/%d, want 1/2", got.Points[0].FileCount, got.Points[1].FileCount)
	}
	if got.Points[1].S <= got.Points[0].S {
		t.Errorf("expected S to grow after adding complex.go, got %.4f -> %.4f", got.Points[0].S, got.Points[1].S)
	}
}

func TestHistory_SumsScoresInAnalyzablePathOrder(t *testing.T) {
	dir := t.TempDir()
	sources := make(map[string]string, 64)
	paths := make([]string, 0, 64)
	for i := range 64 {
		path := fmt.Sprintf("file_%02d.go", i)
		decisions := i
		if i%2 == 0 {
			decisions = 63 - i
		}
		var body strings.Builder
		body.WriteString("package p\nfunc F() int {\n\tn := 0\n")
		for range decisions {
			body.WriteString("\tif n%2 == 0 { n++ }\n")
		}
		body.WriteString("\treturn n\n}\n")
		sources[path] = body.String()
		paths = append(paths, path)
	}
	writeTree(t, dir, sources)

	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r := &historyRunner{
		trees: map[string][]string{
			sha: paths,
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": paths,
		},
		blobs: map[string]map[string]string{
			sha: sources,
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": sources,
		},
	}
	cfg := config.Default()

	got, err := History(HistoryOptions{
		Root:        dir,
		Ref:         "HEAD",
		Limit:       2,
		FirstParent: true,
		ScanOptions: ScanOptions{Config: cfg},
		GitRunner:   r,
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	scorer, err := NewTreeScorer(TreeScorerOptions{
		Root:        dir,
		ScanOptions: ScanOptions{Config: cfg},
		GitRunner:   r,
	})
	if err != nil {
		t.Fatalf("NewTreeScorer: %v", err)
	}
	files, err := treeMicrostateFiles(r, sha, cfg.Weights)
	if err != nil {
		t.Fatalf("treeMicrostateFiles: %v", err)
	}
	var want float64
	for _, file := range files {
		want += scorer.engine.Score(file).S
	}

	if got.Points[0].S != want {
		t.Fatalf("history total = %.17g, path-order total = %.17g", got.Points[0].S, want)
	}
}

func TestTreeScorer(t *testing.T) {
	t.Run("returns historical file scores", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, map[string]string{
			"simple.go":  simpleSource(),
			"complex.go": complexSource(),
		})
		r := &historyRunner{
			trees: map[string][]string{
				"historical": {"simple.go", "complex.go", "README.md"},
			},
			blobs: map[string]map[string]string{
				"historical": {
					"simple.go":  simpleSource(),
					"complex.go": complexSource(),
				},
			},
		}

		scorer, err := NewTreeScorer(TreeScorerOptions{
			Root:        dir,
			ScanOptions: ScanOptions{Config: config.Default()},
			GitRunner:   r,
		})
		if err != nil {
			t.Fatalf("NewTreeScorer: %v", err)
		}
		scores, err := scorer.Score("historical")
		if err != nil {
			t.Fatalf("Score: %v", err)
		}
		if len(scores) != 2 {
			t.Fatalf("scores = %d, want 2", len(scores))
		}
		byPath := make(map[string]FileScore, len(scores))
		for _, score := range scores {
			byPath[score.Path] = score
		}
		if byPath["complex.go"].S <= byPath["simple.go"].S {
			t.Fatalf("complex S %.3f <= simple S %.3f", byPath["complex.go"].S, byPath["simple.go"].S)
		}
	})

	t.Run("uses the committed calibration frame", func(t *testing.T) {
		dir := t.TempDir()
		runPipelineHistoryGit(t, dir, "init", "-q")
		runPipelineHistoryGit(t, dir, "config", "user.email", "test@example.com")
		runPipelineHistoryGit(t, dir, "config", "user.name", "Entrolint Test")
		runPipelineHistoryGit(t, dir, "config", "commit.gpgsign", "false")
		writeTree(t, dir, map[string]string{
			"simple.go":  simpleSource(),
			"complex.go": complexSource(),
		})
		runPipelineHistoryGit(t, dir, "add", ".")
		runPipelineHistoryGit(t, dir, "commit", "-q", "-m", "initial")
		head := strings.TrimSpace(runPipelineHistoryGit(t, dir, "rev-parse", "HEAD"))

		clean, err := NewTreeScorer(TreeScorerOptions{
			Root:        dir,
			FrameRef:    "HEAD",
			ScanOptions: ScanOptions{Config: config.Default()},
		})
		if err != nil {
			t.Fatalf("NewTreeScorer clean: %v", err)
		}
		cleanScores, err := clean.Score("HEAD")
		if err != nil {
			t.Fatalf("Score clean: %v", err)
		}

		writeTree(t, dir, map[string]string{
			"simple.go":    "package p\nfunc Simple(\n",
			"complex.go":   "package p\nfunc Complex(\n",
			"untracked.go": complexSource(),
		})
		dirty, err := NewTreeScorer(TreeScorerOptions{
			Root:        dir,
			FrameRef:    "HEAD",
			ScanOptions: ScanOptions{Config: config.Default()},
		})
		if err != nil {
			t.Fatalf("NewTreeScorer dirty: %v", err)
		}
		dirtyScores, err := dirty.Score("HEAD")
		if err != nil {
			t.Fatalf("Score dirty: %v", err)
		}

		if clean.FrameSHA() != head || dirty.FrameSHA() != head {
			t.Fatalf("frame SHAs = %q/%q, want %q", clean.FrameSHA(), dirty.FrameSHA(), head)
		}
		if !reflect.DeepEqual(cleanScores, dirtyScores) {
			t.Fatalf("dirty worktree changed committed-frame scores:\nclean=%+v\ndirty=%+v", cleanScores, dirtyScores)
		}
	})
}

func runPipelineHistoryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func simpleSource() string {
	return "package p\nfunc Simple() {}\n"
}

func complexSource() string {
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
