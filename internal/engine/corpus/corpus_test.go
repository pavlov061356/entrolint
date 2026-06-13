package corpus

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
)

// cloneBody is a function whose body holds a ≥dupMinNodes structural
// block; reused verbatim across files so the block forms a clone class.
func cloneBody(name string) string {
	return fmt.Sprintf(`package p

func %s(x int) int {
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	return 0
}
`, name)
}

// fakeRepo is a Runner backed by an in-memory tree+blobs at one ref. It
// implements only Run, so BlobsAtRef uses its per-file fallback.
type fakeRepo struct {
	tree    []string
	blobs   map[string]string // repo-relative path -> content
	fetched map[string]bool   // paths the corpus actually asked for
}

func (f *fakeRepo) Run(args ...string) ([]byte, error) {
	switch args[0] {
	case "ls-tree":
		var b strings.Builder
		for _, p := range f.tree {
			b.WriteString(p)
			b.WriteByte(0)
		}
		return []byte(b.String()), nil
	case "cat-file":
		path := strings.SplitN(args[2], ":", 2)[1]
		if f.fetched == nil {
			f.fetched = map[string]bool{}
		}
		f.fetched[path] = true
		if c, ok := f.blobs[path]; ok {
			return []byte(c), nil
		}
		return nil, fmt.Errorf("fatal: path '%s' does not exist in 'ref'", path)
	}
	return nil, fmt.Errorf("unhandled fake call: %v", args)
}

func TestBuildFromFiles(t *testing.T) {
	files := []microstate.File{
		mustParse(t, "a.go", cloneBody("A")),
		mustParse(t, "b.go", cloneBody("B")),
	}
	ctx := BuildFromFiles(files)
	if ctx.CrossDupMass("a.go") != 0 {
		t.Errorf("lowest path is the free original, got %v", ctx.CrossDupMass("a.go"))
	}
	if ctx.CrossDupMass("b.go") <= 0 {
		t.Errorf("non-original file must carry mass, got %v", ctx.CrossDupMass("b.go"))
	}
}

func TestBuild_ReconstructsTreeAndExcludesVendor(t *testing.T) {
	repo := &fakeRepo{
		tree: []string{"a.go", "b.go", "vendor/v.go", "README.md"},
		blobs: map[string]string{
			"a.go":        cloneBody("A"),
			"b.go":        cloneBody("B"),
			"vendor/v.go": cloneBody("V"),
		},
	}
	ctx, err := Build(repo, "HEAD")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ctx.CrossDupMass("b.go") <= 0 {
		t.Errorf("b.go should carry cross-file mass, got %v", ctx.CrossDupMass("b.go"))
	}
	if repo.fetched["vendor/v.go"] {
		t.Error("vendor path must be filtered before the blob fetch")
	}
	if repo.fetched["README.md"] {
		t.Error("non-.go path must be filtered before the blob fetch")
	}
}

func TestContext_NilSafe(t *testing.T) {
	var c *Context
	if c.CrossDupMass("anything") != 0 {
		t.Error("nil *Context must return 0")
	}
}

func mustParse(t *testing.T, path, src string) microstate.File {
	t.Helper()
	f, ok := golang.ParseGoBytes(path, []byte(src))
	if !ok {
		t.Fatalf("parse %s failed", path)
	}
	return f
}
