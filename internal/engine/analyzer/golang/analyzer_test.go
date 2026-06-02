package golang

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeTree creates files inside dir from a {relPath: content} map.
// It mkdir-p's each parent as needed.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func paths(files []relFileView) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.path)
	}
	sort.Strings(out)
	return out
}

type relFileView struct {
	path string
	src  string
}

func collect(t *testing.T, root string) []relFileView {
	t.Helper()
	files, err := (Analyzer{}).Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	out := make([]relFileView, 0, len(files))
	for _, f := range files {
		out = append(out, relFileView{path: f.Path, src: string(f.Src)})
	}
	return out
}

func TestAnalyze_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := collect(t, dir)
	if len(got) != 0 {
		t.Errorf("got %d files, want 0", len(got))
	}
}

func TestAnalyze_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})
	got := collect(t, dir)
	if want := []string{"main.go"}; !sliceEqual(paths(got), want) {
		t.Errorf("paths = %v, want %v", paths(got), want)
	}
	if got[0].src != "package main\nfunc main() {}\n" {
		t.Errorf("src mismatch: %q", got[0].src)
	}
}

func TestAnalyze_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":               "package main\n",
		"pkg/util/util.go":      "package util\n",
		"pkg/util/util_test.go": "package util\n",
	})
	want := []string{
		filepath.Join("pkg", "util", "util.go"),
		filepath.Join("pkg", "util", "util_test.go"),
		"main.go",
	}
	sort.Strings(want)
	if got := paths(collect(t, dir)); !sliceEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestAnalyze_ExcludesVendor(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":                 "package main\n",
		"vendor/foo/bar.go":       "package foo\n",
		"sub/vendor/other/baz.go": "package other\n",
	})
	want := []string{"main.go"}
	if got := paths(collect(t, dir)); !sliceEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestAnalyze_ExcludesHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":             "package main\n",
		".git/objects/foo.go": "package foo\n",
		".idea/sub/bar.go":    "package bar\n",
	})
	want := []string{"main.go"}
	if got := paths(collect(t, dir)); !sliceEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestAnalyze_IgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go":   "package main\n",
		"README.md": "# title\n",
		"data.json": `{"x":1}`,
		"x.gox":     "not go", // .gox is not .go
	})
	want := []string{"main.go"}
	if got := paths(collect(t, dir)); !sliceEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestAnalyze_SkipsUnparseable(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"good.go": "package p\n",
		"bad.go":  "this is not valid Go\n",
	})
	want := []string{"good.go"}
	if got := paths(collect(t, dir)); !sliceEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestAnalyze_SingleFileRoot(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"main.go": "package main\n",
	})
	files, err := (Analyzer{}).Analyze(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
}

func TestAnalyze_NonExistentRoot(t *testing.T) {
	_, err := (Analyzer{}).Analyze("/nonexistent/path/entrolint-test")
	if err == nil {
		t.Error("expected error for non-existent root, got nil")
	}
}

func TestAnalyze_PathsRelativeToRoot(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"pkg/sub/a.go": "package sub\n",
	})
	got := collect(t, dir)
	if len(got) != 1 {
		t.Fatalf("got %d files, want 1", len(got))
	}
	wantRel := filepath.Join("pkg", "sub", "a.go")
	if got[0].path != wantRel {
		t.Errorf("path = %q, want %q", got[0].path, wantRel)
	}
}

// recordingRunner captures git invocations and returns canned output
// per file path (the last arg after `--`).
type recordingRunner struct {
	calls    [][]string
	outByArg map[string][]byte
}

func (r *recordingRunner) Run(args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	for i, a := range args {
		if a == "--" && i+1 < len(args) {
			path := args[i+1]
			if out, ok := r.outByArg[path]; ok {
				return out, nil
			}
		}
	}
	return nil, nil
}

func TestAnalyze_ChurnRunnerNilLeavesZero(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package p\n"})
	files, err := (Analyzer{}).Analyze(dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(files) != 1 || files[0].ChurnCount != 0 {
		t.Errorf("ChurnCount with nil runner = %d, want 0", files[0].ChurnCount)
	}
}

func TestAnalyze_ChurnRunnerPopulatesCount(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": "package p\n",
		"b.go": "package p\n",
	})
	r := &recordingRunner{
		outByArg: map[string][]byte{
			"a.go": []byte("hash1\nhash2\nhash3"),
			"b.go": []byte("hashX"),
		},
	}
	files, err := (Analyzer{ChurnRunner: r}).Analyze(dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	byPath := map[string]int{}
	for _, f := range files {
		byPath[f.Path] = f.ChurnCount
	}
	if byPath["a.go"] != 3 {
		t.Errorf("a.go ChurnCount = %d, want 3", byPath["a.go"])
	}
	if byPath["b.go"] != 1 {
		t.Errorf("b.go ChurnCount = %d, want 1", byPath["b.go"])
	}
	// Verify the since-days flag carries the default 90 when ChurnSinceDays is 0.
	found := false
	for _, call := range r.calls {
		for _, a := range call {
			if strings.HasPrefix(a, "--since=90.days.ago") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected default --since=90.days.ago in at least one call")
	}
}

func TestAnalyze_ChurnSinceDaysOverride(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package p\n"})
	r := &recordingRunner{outByArg: map[string][]byte{"a.go": []byte("h")}}
	_, err := (Analyzer{ChurnRunner: r, ChurnSinceDays: 14}).Analyze(dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := "--since=14.days.ago"
	found := false
	for _, call := range r.calls {
		for _, a := range call {
			if a == want {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected %q in calls, got %v", want, r.calls)
	}
}

func TestParseGoBytes_Valid(t *testing.T) {
	src := []byte("package p\nfunc f() {}\n")
	f, ok := ParseGoBytes("a.go", src)
	if !ok {
		t.Fatal("ParseGoBytes returned ok=false on valid source")
	}
	if f.Path != "a.go" {
		t.Errorf("Path = %q, want a.go", f.Path)
	}
	if string(f.Src) != string(src) {
		t.Errorf("Src not preserved verbatim")
	}
	if f.AST == nil || f.Fset == nil {
		t.Error("AST and Fset must be populated")
	}
	if f.ChurnCount != 0 {
		t.Errorf("ChurnCount = %d, want 0 (ParseGoBytes never touches churn)", f.ChurnCount)
	}
}

func TestParseGoBytes_Invalid(t *testing.T) {
	_, ok := ParseGoBytes("bad.go", []byte("this is not valid Go"))
	if ok {
		t.Error("ParseGoBytes returned ok=true on invalid source")
	}
}

func TestIsAnalyzablePath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain go file", "main.go", true},
		{"nested go file", "pkg/sub/a.go", true},
		{"test file is analyzable", "pkg/x_test.go", true},
		{"non-go", "README.md", false},
		{"go-ish suffix", "x.gox", false},
		{"vendored go", "vendor/foo/bar.go", false},
		{"vendored nested", "internal/vendor/x.go", false},
		{"dotdir go", ".git/objects/foo.go", false},
		{"dotdir nested go", "pkg/.gen/x.go", false},
		{"dotfile at root is still analyzable as long as no dir is dot-prefixed", ".bashrc.go", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsAnalyzablePath(c.path); got != c.want {
				t.Errorf("IsAnalyzablePath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestParseGoBytes_Empty(t *testing.T) {
	_, ok := ParseGoBytes("empty.go", nil)
	if ok {
		t.Error("ParseGoBytes returned ok=true on empty source (no package clause)")
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
