package typesx

import (
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func ch(path string) gitx.Change {
	return gitx.Change{Kind: gitx.ChangeModified, Path: path}
}

func TestChangedFileSet_NormalizesRelativeAndAbsolute(t *testing.T) {
	root := "/tmp/repo"
	set := ChangedFileSet(scaling.Input{
		Root: root,
		Changes: []gitx.Change{
			ch("foo.go"),
			ch("./bar.go"),
			ch("/tmp/repo/baz.go"),
			ch("docs.md"), // non-Go filtered
		},
	})
	for _, want := range []string{"/tmp/repo/foo.go", "/tmp/repo/bar.go", "/tmp/repo/baz.go"} {
		if !set[want] {
			t.Errorf("set missing %q\nfull: %#v", want, set)
		}
	}
	if set[filepath.Join(root, "docs.md")] {
		t.Errorf("non-Go path leaked into the set")
	}
}

func TestRelativize_FallsBackOnError(t *testing.T) {
	// Empty root → identity.
	if got := Relativize("", "/abs/x.go"); got != "/abs/x.go" {
		t.Errorf("empty root: got %q, want identity", got)
	}
	// Normal case.
	if got := Relativize("/repo", "/repo/pkg/x.go"); got != filepath.Join("pkg", "x.go") {
		t.Errorf("relative: got %q", got)
	}
}

func TestDefault_ReturnsSingleton(t *testing.T) {
	a := Default()
	b := Default()
	if a != b {
		t.Error("Default() must return the same Loader instance across calls")
	}
}
