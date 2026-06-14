package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

func TestScanCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"top", "format", "json", "config", "recalibrate", "html"} {
		if scanCmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestWriteHTMLReport_WritesIndexHTML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "report") // nested: must be created
	var out bytes.Buffer
	files := []pipeline.FileScore{
		{Path: "a.go", S: 1, T: 1, Dominant: "length", Contributions: map[string]float64{"length": 1}},
	}
	if err := writeHTMLReport(&out, dir, files); err != nil {
		t.Fatalf("writeHTMLReport: %v", err)
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
