package identifierfanout

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/probe\n\ngo 1.21\n"
	}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

func changes(paths ...string) []gitx.Change {
	out := make([]gitx.Change, 0, len(paths))
	for _, p := range paths {
		out = append(out, gitx.Change{Kind: gitx.ChangeModified, Path: p})
	}
	return out
}

// callersFixture: an exported function `Format` with 10 call-sites
// spread across 10 caller files in the same package. Each test picks
// which subset to claim as "touched".
func callersFixture(t *testing.T, callerCount int) string {
	files := map[string]string{
		"format.go": `package probe

func Format(s string) string { return s + "!" }
`,
	}
	for i := 0; i < callerCount; i++ {
		files[fmt.Sprintf("caller%d.go", i)] = fmt.Sprintf(
			"package probe\n\nfunc caller%d() string { return Format(\"x\") }\n",
			i,
		)
	}
	return fixture(t, files)
}

// changedCallers returns []Change for the first n caller files plus
// the def file format.go, mimicking a PR that renamed the function.
func changedCallers(n int) []gitx.Change {
	out := []gitx.Change{{Kind: gitx.ChangeModified, Path: "format.go"}}
	for i := 0; i < n; i++ {
		out = append(out, gitx.Change{Kind: gitx.ChangeModified, Path: fmt.Sprintf("caller%d.go", i)})
	}
	return out
}

func TestFanout_AllCallersTouchedFires(t *testing.T) {
	root := callersFixture(t, 10)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changedCallers(10),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit when all callers touched, got %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Class != scaling.ClassOk {
		t.Errorf("class = %v, want O(k)", h.Class)
	}
	if h.Size != 10 {
		t.Errorf("size = %d, want 10 (call-sites)", h.Size)
	}
	if h.Path != "format.go" {
		t.Errorf("hit Path = %q, want format.go (decl site)", h.Path)
	}
	if h.Detector != name {
		t.Errorf("detector = %q, want %q", h.Detector, name)
	}
}

func TestFanout_EightyPercentFires(t *testing.T) {
	// 8 of 10 = 80% — exactly at threshold; strict >= so fire.
	root := callersFixture(t, 10)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changedCallers(8),
	})
	if len(hits) != 1 {
		t.Fatalf("8/10 touched (80%%) should fire, got %d", len(hits))
	}
}

func TestFanout_BelowRatioStaysSilent(t *testing.T) {
	// 5 of 10 = 50% — below 80%, must not fire.
	root := callersFixture(t, 10)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changedCallers(5),
	})
	if len(hits) != 0 {
		t.Errorf("5/10 touched (50%%) must not fire, got %d", len(hits))
	}
}

func TestFanout_BelowMinRefsNoFire(t *testing.T) {
	// 7 call-sites (< DefaultMinReferences=8) — even at 100% touched,
	// the architectural ripple is small enough not to flag.
	root := callersFixture(t, 7)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changedCallers(7),
	})
	if len(hits) != 0 {
		t.Errorf("below min-refs must not fire, got %d", len(hits))
	}
}

func TestFanout_DefNotTouchedNoFire(t *testing.T) {
	// PR touched many callers but NOT the def — v0.2 proxy for "PR
	// changed the symbol" requires the decl file in Changes. Without
	// it the detector silently skips.
	root := callersFixture(t, 10)
	// Touch only callers, not format.go.
	var paths []string
	for i := 0; i < 10; i++ {
		paths = append(paths, fmt.Sprintf("caller%d.go", i))
	}
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes(paths...),
	})
	if len(hits) != 0 {
		t.Errorf("def-not-touched must not fire, got %d", len(hits))
	}
}

func TestFanout_UnexportedSymbolIgnored(t *testing.T) {
	// Lowercase helper used 10 times in changed files. The spec is
	// about exported symbols (cross-package fan-out); internal helpers
	// are local refactors regardless of caller count.
	root := fixture(t, map[string]string{
		"helper.go": `package probe

func helper(s string) string { return s + "!" }
`,
	})
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("caller%d.go", i)
		body := fmt.Sprintf("package probe\n\nfunc caller%d() string { return helper(\"x\") }\n", i)
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changedCallers(10),
	})
	if len(hits) != 0 {
		t.Errorf("unexported symbol must not fire, got %d", len(hits))
	}
}

func TestFanout_CrossPackageReferencesCount(t *testing.T) {
	// Def in pkg api, callers spread across pkg use1, use2, use3,
	// use4, use5, use6, use7, use8. PR touches def + all callers.
	files := map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"api/api.go": `package api

func Format(s string) string { return s + "!" }
`,
	}
	var paths []string
	paths = append(paths, "api/api.go")
	for i := 0; i < 8; i++ {
		dir := fmt.Sprintf("use%d", i)
		path := fmt.Sprintf("%s/use.go", dir)
		body := fmt.Sprintf(
			"package use%d\n\nimport \"example.com/probe/api\"\n\nfunc Caller() string { return api.Format(\"x\") }\n",
			i,
		)
		files[path] = body
		paths = append(paths, path)
	}
	root := fixture(t, files)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes(paths...),
	})
	if len(hits) != 1 {
		t.Fatalf("cross-pkg refs all-touched must fire 1 hit, got %d", len(hits))
	}
	if hits[0].Path != filepath.Join("api", "api.go") {
		t.Errorf("Path = %q, want api/api.go", hits[0].Path)
	}
	if hits[0].Size != 8 {
		t.Errorf("Size = %d, want 8 (cross-pkg call sites)", hits[0].Size)
	}
}

func TestFanout_MethodReferences(t *testing.T) {
	// Methods on exported types are themselves objects. Touching a
	// method along with most of its call-sites is the same fan-out
	// signal as for free functions.
	files := map[string]string{
		"client.go": `package probe

type Client struct{}

func (c *Client) Send(msg string) error { return nil }
`,
	}
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("caller%d.go", i)] = fmt.Sprintf(
			"package probe\n\nfunc caller%d(c *Client) error { return c.Send(\"x\") }\n",
			i,
		)
	}
	root := fixture(t, files)
	paths := []string{"client.go"}
	for i := 0; i < 10; i++ {
		paths = append(paths, fmt.Sprintf("caller%d.go", i))
	}
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes(paths...),
	})
	// Two hits: the type Client itself (referenced as *Client in every
	// caller signature) and the Send method (called in every caller).
	if len(hits) < 1 {
		t.Errorf("expected at least 1 method/type hit, got %d", len(hits))
	}
	foundSend := false
	for _, h := range hits {
		if h.Path == "client.go" && h.Size >= 10 {
			foundSend = true
		}
	}
	if !foundSend {
		t.Errorf("expected a hit on client.go with size ≥ 10, got %+v", hits)
	}
}

func TestFanout_EmptyRootSkipsSilently(t *testing.T) {
	if hits := New().Analyze(scaling.Input{Changes: changes("a.go")}); len(hits) != 0 {
		t.Errorf("empty Root must soft-skip, got %d hits", len(hits))
	}
}

func TestFanout_NoChangesNoFire(t *testing.T) {
	root := callersFixture(t, 10)
	hits := New().Analyze(scaling.Input{Root: root})
	if len(hits) != 0 {
		t.Errorf("no changes must not fire, got %d", len(hits))
	}
}

func TestFanout_NonGoChangesIgnored(t *testing.T) {
	root := callersFixture(t, 10)
	cs := changedCallers(10)
	cs = append(cs, gitx.Change{Kind: gitx.ChangeModified, Path: "README.md"})
	hits := New().Analyze(scaling.Input{Root: root, Changes: cs})
	if len(hits) != 1 {
		t.Errorf("non-Go changes must not perturb, got %d hits", len(hits))
	}
}

func TestFanout_AbsolutePathsInChangesNormalized(t *testing.T) {
	root := callersFixture(t, 10)
	cs := []gitx.Change{{Kind: gitx.ChangeModified, Path: filepath.Join(root, "format.go")}}
	for i := 0; i < 10; i++ {
		cs = append(cs, gitx.Change{
			Kind: gitx.ChangeModified,
			Path: filepath.Join(root, fmt.Sprintf("caller%d.go", i)),
		})
	}
	hits := New().Analyze(scaling.Input{Root: root, Changes: cs})
	if len(hits) != 1 {
		t.Errorf("abs paths must produce the same hit, got %d", len(hits))
	}
}

func TestFanout_BogusRatioFallsBackToDefault(t *testing.T) {
	root := callersFixture(t, 10)
	in := scaling.Input{Root: root, Changes: changedCallers(9)} // 90% > 80% default
	for _, bogus := range []float64{-0.1, 0, 1.5} {
		t.Run(fmt.Sprintf("ratio=%v", bogus), func(t *testing.T) {
			d := &Detector{MinReferences: 8, TouchedRatio: bogus}
			if hits := d.Analyze(in); len(hits) != 1 {
				t.Errorf("bogus %v must fall back to default 0.8 and fire on 9/10, got %d", bogus, len(hits))
			}
		})
	}
}

func TestFanout_CustomMinRefsHonored(t *testing.T) {
	root := callersFixture(t, 5)
	// 5 callers < default 8 → no fire with default. Lower MinReferences to 4 → fires.
	if hits := New().Analyze(scaling.Input{Root: root, Changes: changedCallers(5)}); len(hits) != 0 {
		t.Errorf("default MinRefs=8 with 5 callers must not fire, got %d", len(hits))
	}
	d := &Detector{MinReferences: 4, TouchedRatio: 0.8}
	if hits := d.Analyze(scaling.Input{Root: root, Changes: changedCallers(5)}); len(hits) != 1 {
		t.Errorf("MinRefs=4 with 5/5 touched must fire 1, got %d", len(hits))
	}
}

func TestFanout_RegisteredInRegistry(t *testing.T) {
	found := false
	for _, d := range scaling.Registry {
		if d.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("identifier_fanout not registered; got %d entries", len(scaling.Registry))
	}
}
