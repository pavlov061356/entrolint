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

// TestBuildReusing_ExcludeHoldsPathOut verifies the held-out path is
// neither fetched nor indexed, so the clone class it would have formed
// disappears and its partner carries no cross-file mass. This is the seam
// `check` uses to keep clone membership symmetric across refs (issue #68).
func TestBuildReusing_ExcludeHoldsPathOut(t *testing.T) {
	repo := &fakeRepo{
		tree: []string{"a.go", "b.go"},
		blobs: map[string]string{
			"a.go": cloneBody("A"),
			"b.go": cloneBody("B"),
		},
	}
	ctx, err := BuildReusing(repo, "HEAD", nil, map[string]bool{"a.go": true})
	if err != nil {
		t.Fatalf("BuildReusing: %v", err)
	}
	if repo.fetched["a.go"] {
		t.Error("excluded path must not be fetched")
	}
	if m := ctx.CrossDupMass("b.go"); m != 0 {
		t.Errorf("with its only clone partner excluded, b.go must carry no cross-file mass, got %v", m)
	}
	if m := ctx.CrossDupMass("a.go"); m != 0 {
		t.Errorf("excluded path carries no mass, got %v", m)
	}
}

// TestBuildReusing_ReusesProvidedBlobs verifies a blob supplied in `have`
// is taken as-is and NOT re-fetched, while the rest of the tree still is —
// the seam `check` uses so each blob is read from git exactly once.
func TestBuildReusing_ReusesProvidedBlobs(t *testing.T) {
	repo := &fakeRepo{
		tree: []string{"a.go", "b.go"},
		blobs: map[string]string{
			// a.go is NOT in repo.blobs: if BuildReusing tried to fetch it,
			// the fake would error it absent and it would drop from the corpus.
			"b.go": cloneBody("B"),
		},
	}
	have := map[string][]byte{"a.go": []byte(cloneBody("A"))}

	ctx, err := BuildReusing(repo, "HEAD", have, nil)
	if err != nil {
		t.Fatalf("BuildReusing: %v", err)
	}
	if repo.fetched["a.go"] {
		t.Error("a blob supplied in `have` must be reused, not re-fetched")
	}
	if !repo.fetched["b.go"] {
		t.Error("a tree blob absent from `have` must still be fetched")
	}
	// a.go (reused) and b.go (fetched) form a clone class: the lower path
	// a.go is the free original, b.go carries the class size.
	if m := ctx.CrossDupMass("b.go"); m <= 0 {
		t.Errorf("reused + fetched blobs must form a clone class, got b.go mass %v", m)
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
