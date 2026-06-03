package shotgun

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// patch builds a minimal unified-0 hunk body: a single hunk with one
// added line. Useful when a test only cares about the canonical form
// of a change, not its surrounding metadata.
func patch(addedLines ...string) []byte {
	var b strings.Builder
	b.WriteString("@@ -0,0 +1," + fmt.Sprintf("%d", len(addedLines)) + " @@\n")
	for _, l := range addedLines {
		b.WriteString("+")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func change(path string) gitx.Change {
	return gitx.Change{Kind: gitx.ChangeModified, Path: path}
}

func makeInput(perPath map[string][]byte) scaling.Input {
	changes := make([]gitx.Change, 0, len(perPath))
	for p := range perPath {
		changes = append(changes, change(p))
	}
	return scaling.Input{Changes: changes, Patches: perPath}
}

func TestShotgun_BelowThresholdNoHit(t *testing.T) {
	in := makeInput(map[string][]byte{
		"a.go": patch("dosomething()"),
		"b.go": patch("dosomething()"),
		"c.go": patch("dosomething()"),
		"d.go": patch("dosomething()"),
	})
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("4 identical patches must not fire (threshold=5), got %d hits", len(hits))
	}
}

func TestShotgun_FiresAtExactlyThreshold(t *testing.T) {
	files := map[string][]byte{}
	for _, p := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		files[p] = patch("registerHandler()")
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 5 {
		t.Fatalf("expected one O(n) hit per file at threshold, got %d hits", len(hits))
	}
	for _, h := range hits {
		if h.Class != scaling.ClassON {
			t.Errorf("hit class = %v, want O(n)", h.Class)
		}
		if h.Size != 5 {
			t.Errorf("hit size = %d, want 5", h.Size)
		}
		if h.Detector != name {
			t.Errorf("hit detector = %q, want %q", h.Detector, name)
		}
	}
}

func TestShotgun_DistinctPatternsDontGroup(t *testing.T) {
	files := map[string][]byte{}
	for i, p := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		files[p] = patch(fmt.Sprintf("uniqueCall_%d()", i))
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 0 {
		t.Errorf("5 distinct patches must not group, got %d hits", len(hits))
	}
}

func TestShotgun_WhitespaceOnlyDiffsAreIgnored(t *testing.T) {
	// gofumpt-only PRs: identical leading/trailing whitespace changes
	// across many files. Must not trip the heuristic.
	files := map[string][]byte{}
	for _, p := range []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"} {
		files[p] = []byte("@@ -1,1 +1,1 @@\n-   \n+   \n")
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 0 {
		t.Errorf("whitespace-only diffs must not fire, got %d hits", len(hits))
	}
}

func TestShotgun_TrimToleratesIndentationDrift(t *testing.T) {
	// Same line added at different indentation levels in each file —
	// canonicalization strips leading whitespace, so they group.
	files := map[string][]byte{
		"a.go": []byte("@@ -0,0 +1,1 @@\n+ register(handler)\n"),
		"b.go": []byte("@@ -0,0 +1,1 @@\n+\tregister(handler)\n"),
		"c.go": []byte("@@ -0,0 +1,1 @@\n+    register(handler)\n"),
		"d.go": []byte("@@ -0,0 +1,1 @@\n+\t\tregister(handler)\n"),
		"e.go": []byte("@@ -0,0 +1,1 @@\n+register(handler)\n"),
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 5 {
		t.Errorf("indentation-only variation must still group, got %d hits", len(hits))
	}
}

func TestShotgun_NonGoFilesIgnored(t *testing.T) {
	files := map[string][]byte{
		"a.go":      patch("samecall()"),
		"b.go":      patch("samecall()"),
		"c.go":      patch("samecall()"),
		"d.go":      patch("samecall()"),
		"README.md": patch("samecall()"),
		"x.yaml":    patch("samecall()"),
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 0 {
		// Only 4 Go files share the pattern; Markdown/YAML excluded.
		t.Errorf("non-Go files must not count toward threshold, got %d hits", len(hits))
	}
}

func TestShotgun_MissingPatchIsSkipped(t *testing.T) {
	in := scaling.Input{
		Changes: []gitx.Change{
			change("a.go"), change("b.go"), change("c.go"),
			change("d.go"), change("e.go"),
		},
		Patches: map[string][]byte{}, // empty — no patches available
	}
	hits := New().Analyze(in)
	if len(hits) != 0 {
		t.Errorf("files without patch payload must not fire, got %d hits", len(hits))
	}
}

func TestShotgun_PatchBoundaryLinesDontPolluteFingerprint(t *testing.T) {
	// `@@`, `+++`, `---`, `index ` lines must be ignored by
	// fingerprinting — otherwise two patches with identical body but
	// different SHA prefixes would group differently.
	withHeaders := func(body string) []byte {
		return []byte("--- a/x.go\n+++ b/x.go\nindex deadbeef..cafebabe 100644\n@@ -1,1 +1,1 @@\n" + body)
	}
	files := map[string][]byte{
		"a.go": withHeaders("+ samebody()\n"),
		"b.go": withHeaders("+ samebody()\n"),
		"c.go": withHeaders("+ samebody()\n"),
		"d.go": withHeaders("+ samebody()\n"),
		"e.go": withHeaders("+ samebody()\n"),
	}
	hits := New().Analyze(makeInput(files))
	if len(hits) != 5 {
		t.Errorf("patch headers must not affect fingerprint, got %d hits", len(hits))
	}
}

func TestShotgun_RegisteredInGlobalRegistry(t *testing.T) {
	// init() side-effect: the detector must be in the global Registry
	// after the package is loaded, so the cmd binary picks it up via
	// blank import.
	found := false
	for _, d := range scaling.Registry {
		if d.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("shotgun detector not registered in scaling.Registry; got %d entries", len(scaling.Registry))
	}
}

func TestShotgun_CustomMinFilesHonored(t *testing.T) {
	in := makeInput(map[string][]byte{
		"a.go": patch("same()"),
		"b.go": patch("same()"),
	})
	if hits := (&Detector{MinFiles: 2}).Analyze(in); len(hits) != 2 {
		t.Errorf("MinFiles=2 with 2 matching files should fire 2 hits, got %d", len(hits))
	}
	// MinFiles ≤ 0 must fall back to DefaultMinFiles, not fire below it.
	if hits := (&Detector{MinFiles: 0}).Analyze(in); len(hits) != 0 {
		t.Errorf("MinFiles=0 should fall back to default %d and not fire on 2 files, got %d hits",
			DefaultMinFiles, len(hits))
	}
}
