package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

func TestHistoryCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"limit", "first-parent", "format", "json", "config", "recalibrate", "root", "html"} {
		if historyCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestWriteHistoryHTMLReport_WritesIndexHTML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	var out bytes.Buffer
	res := pipeline.HistoryResult{
		Ref: "HEAD",
		Points: []pipeline.HistoryPoint{
			{ShortSHA: "aaaaaaa", CommitTime: "2026-07-01T10:00:00+03:00", Subject: "start", S: 1, FileCount: 1},
		},
	}
	if err := writeHistoryHTMLReport(&out, dir, res); err != nil {
		t.Fatalf("writeHistoryHTMLReport: %v", err)
	}
	path := filepath.Join(dir, "index.html")
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("index.html not written: %v", err)
	}
	if !strings.HasPrefix(string(data), "<!doctype html>") {
		t.Error("index.html is not an HTML document")
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("expected the written path on stdout, got %q", out.String())
	}
}
