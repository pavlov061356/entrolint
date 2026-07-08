package pipeline

import (
	"fmt"
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
