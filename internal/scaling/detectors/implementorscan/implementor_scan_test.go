package implementorscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// fixture creates a temp Go module with the supplied file contents
// (relative paths → file body) and returns the module root. Every
// fixture has a go.mod so packages.Load can resolve `./...`.
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

// canonical 3-implementor corpus: an interface and three concrete
// types in separate files, so each test can pick which subset to
// claim as "touched".
func threeImplsFixture(t *testing.T) string {
	return fixture(t, map[string]string{
		"iface.go": `package probe

type Storage interface {
	Get(key string) string
	Set(key, val string)
}
`,
		"mem.go": `package probe

type Mem struct{ m map[string]string }

func (m *Mem) Get(key string) string  { return m.m[key] }
func (m *Mem) Set(key, val string)    { m.m[key] = val }
`,
		"disk.go": `package probe

type Disk struct{ path string }

func (d *Disk) Get(key string) string { return "" }
func (d *Disk) Set(key, val string)   {}
`,
		"net.go": `package probe

type Net struct{ addr string }

func (n *Net) Get(key string) string { return "" }
func (n *Net) Set(key, val string)   {}
`,
	})
}

func TestImplementorScan_AllImplementorsTouchedFires(t *testing.T) {
	root := threeImplsFixture(t)
	d := New()
	hits := d.Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem.go", "disk.go", "net.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Class != scaling.ClassOk {
		t.Errorf("class = %v, want O(k)", h.Class)
	}
	if h.Size != 3 {
		t.Errorf("size = %d, want 3 (implementors)", h.Size)
	}
	if h.Detector != name {
		t.Errorf("detector = %q, want %q", h.Detector, name)
	}
	if h.Path != "iface.go" {
		t.Errorf("hit Path = %q, want iface.go (interface decl site)", h.Path)
	}
	if !strings.Contains(h.Evidence, "Storage") {
		t.Errorf("evidence = %q, want it to mention the interface name", h.Evidence)
	}
}

func TestImplementorScan_HalfTouchedFires(t *testing.T) {
	root := threeImplsFixture(t)
	// 2 of 3 = 66% ≥ 50%
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem.go", "disk.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit at 2/3 touched, got %d", len(hits))
	}
}

func TestImplementorScan_BelowRatioStaysSilent(t *testing.T) {
	root := threeImplsFixture(t)
	// 1 of 3 ≈ 33% < 50%
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem.go"),
	})
	if len(hits) != 0 {
		t.Errorf("1 of 3 touched (33%%) must not fire, got %d hits", len(hits))
	}
}

func TestImplementorScan_NoTouchedNoFire(t *testing.T) {
	root := threeImplsFixture(t)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("README.md"),
	})
	if len(hits) != 0 {
		t.Errorf("no impl touched must not fire, got %d hits", len(hits))
	}
}

func TestImplementorScan_SingleImplementorBelowFloor(t *testing.T) {
	// One impl below DefaultMinImplementors=2 — even 100% touched
	// doesn't constitute "ripples through architecture".
	root := fixture(t, map[string]string{
		"iface.go": `package probe

type Marker interface{ Tag() string }
`,
		"only.go": `package probe

type Only struct{}

func (Only) Tag() string { return "x" }
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("only.go"),
	})
	if len(hits) != 0 {
		t.Errorf("1-impl interface must not fire, got %d hits", len(hits))
	}
}

func TestImplementorScan_EmptyRootSkipsSilently(t *testing.T) {
	hits := New().Analyze(scaling.Input{Changes: changes("a.go")})
	if len(hits) != 0 {
		t.Errorf("empty Root must soft-skip, got %d hits", len(hits))
	}
}

func TestImplementorScan_BrokenModuleSoftSkips(t *testing.T) {
	// A directory without go.mod and with broken syntax can't be
	// type-checked. The detector must return zero hits, not panic
	// or fail — other detectors keep running.
	root := fixture(t, map[string]string{
		"broken.go": "this is not valid Go either",
	})
	// Drop the go.mod fixture added by default to force load failure.
	_ = os.Remove(filepath.Join(root, "go.mod"))
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("broken.go"),
	})
	if len(hits) != 0 {
		t.Errorf("broken module must soft-skip, got %d hits", len(hits))
	}
}

func TestImplementorScan_NoInterfacesNoFire(t *testing.T) {
	root := fixture(t, map[string]string{
		"plain.go": `package probe

type Plain struct{}

func (p Plain) Do() {}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("plain.go"),
	})
	if len(hits) != 0 {
		t.Errorf("no interfaces means no hits, got %d", len(hits))
	}
}

func TestImplementorScan_MethodEditCountsAsTouched(t *testing.T) {
	// The impl's type decl is in mem.go, but the method receiver is
	// in mem_methods.go. Editing the methods file alone still counts
	// as touching the implementor.
	root := fixture(t, map[string]string{
		"iface.go": `package probe

type Storage interface{ Get(string) string }
`,
		"mem.go": `package probe

type Mem struct{}
`,
		"mem_methods.go": `package probe

func (Mem) Get(string) string { return "" }
`,
		"disk.go": `package probe

type Disk struct{}

func (Disk) Get(string) string { return "" }
`,
	})
	// Touched: only the methods file of Mem + disk.go. By type-decl
	// count, that's 2 of 2 implementors touched (Mem via methods,
	// Disk via type+method file).
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem_methods.go", "disk.go"),
	})
	if len(hits) != 1 {
		t.Errorf("method-only edit must count toward 'touched'; got %d hits", len(hits))
	}
}

func TestImplementorScan_RegisteredInRegistry(t *testing.T) {
	found := false
	for _, d := range scaling.Registry {
		if d.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Error("implementor_scan not registered in scaling.Registry")
	}
}

func TestImplementorScan_CustomThresholdsHonored(t *testing.T) {
	root := threeImplsFixture(t)
	// Strict 80% requires all 3 → 2/3 must not fire.
	strict := &Detector{MinImplementors: 2, TouchedRatio: 0.8}
	if hits := strict.Analyze(scaling.Input{Root: root, Changes: changes("mem.go", "disk.go")}); len(hits) != 0 {
		t.Errorf("strict ratio 0.8 with 2/3 touched should not fire, got %d", len(hits))
	}

	// Lenient 30% — 1/3 fires.
	lenient := &Detector{MinImplementors: 2, TouchedRatio: 0.3}
	if hits := lenient.Analyze(scaling.Input{Root: root, Changes: changes("mem.go")}); len(hits) != 1 {
		t.Errorf("lenient ratio 0.3 with 1/3 touched should fire 1, got %d", len(hits))
	}
}

func TestImplementorScan_CrossPackageImplementorsFire(t *testing.T) {
	root := fixture(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"storage/storage.go": `package storage

type Storage interface{ Get(string) string }
`,
		"mem/mem.go": `package mem

type Mem struct{}

func (Mem) Get(string) string { return "" }
`,
		"disk/disk.go": `package disk

type Disk struct{}

func (Disk) Get(string) string { return "" }
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem/mem.go", "disk/disk.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit on cross-package impls, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != filepath.Join("storage", "storage.go") {
		t.Errorf("hit Path = %q, want storage/storage.go (canonical decl site)", hits[0].Path)
	}
}

func TestImplementorScan_EmbeddedPromotedMethodsCount(t *testing.T) {
	// Outer satisfies Storage only via embedded Base — direct
	// named.NumMethods would miss it. Editing base.go must mark Outer
	// as touched.
	root := fixture(t, map[string]string{
		"iface.go": `package probe

type Storage interface{ Get(string) string }
`,
		"base.go": `package probe

type Base struct{}

func (Base) Get(string) string { return "base" }
`,
		"outer.go": `package probe

type Outer struct{ Base }
type Direct struct{}

func (Direct) Get(string) string { return "direct" }
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("base.go", "outer.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit when promoted-method file is edited, got %d", len(hits))
	}
}

func TestImplementorScan_TypeAliasDoesNotDoubleCount(t *testing.T) {
	root := fixture(t, map[string]string{
		"iface.go": `package probe

type Storage interface{ Get(string) string }
`,
		"mem.go": `package probe

type Mem struct{}

func (Mem) Get(string) string { return "" }
`,
		"alias.go": `package probe

type Renamed = Mem
`,
		"disk.go": `package probe

type Disk struct{}

func (Disk) Get(string) string { return "" }
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem.go", "disk.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	if hits[0].Size != 2 {
		t.Errorf("Size = %d, want 2 (Mem + Disk; alias must dedupe to Mem)", hits[0].Size)
	}
}

func TestImplementorScan_GenericInterfaceSkipped(t *testing.T) {
	root := fixture(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"iface.go": `package probe

type Comparer[T any] interface{ Less(T) bool }
`,
		"int_cmp.go": `package probe

type IntCmp struct{}

func (IntCmp) Less(int) bool { return false }
`,
		"str_cmp.go": `package probe

type StrCmp struct{}

func (StrCmp) Less(string) bool { return false }
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("int_cmp.go", "str_cmp.go"),
	})
	if len(hits) != 0 {
		t.Errorf("generic interface must skip silently, got %d hits", len(hits))
	}
}

func TestImplementorScan_MixedNonGoChangesIgnored(t *testing.T) {
	root := threeImplsFixture(t)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("mem.go", "disk.go", "net.go", "README.md", "CHANGELOG.txt"),
	})
	if len(hits) != 1 {
		t.Errorf("non-Go changes must not perturb the result; got %d hits", len(hits))
	}
}

func TestImplementorScan_AbsolutePathsInChangesNormalized(t *testing.T) {
	root := threeImplsFixture(t)
	hits := New().Analyze(scaling.Input{
		Root: root,
		Changes: changes(
			filepath.Join(root, "mem.go"),
			filepath.Join(root, "disk.go"),
			filepath.Join(root, "net.go"),
		),
	})
	if len(hits) != 1 {
		t.Errorf("absolute paths must yield the same hit; got %d", len(hits))
	}
}

func TestImplementorScan_BogusRatioFallsBackToDefault(t *testing.T) {
	root := threeImplsFixture(t)
	in := scaling.Input{Root: root, Changes: changes("mem.go", "disk.go")} // 2/3 = 66%
	for _, bogus := range []float64{-0.1, 0, 1.5} {
		t.Run(fmt.Sprintf("ratio=%v", bogus), func(t *testing.T) {
			d := &Detector{MinImplementors: 2, TouchedRatio: bogus}
			if hits := d.Analyze(in); len(hits) != 1 {
				t.Errorf("bogus ratio %v must fall back to default 0.5 and fire on 2/3, got %d hits", bogus, len(hits))
			}
		})
	}
}

func TestImplementorScan_LoaderCacheBucketsByRoot(t *testing.T) {
	// Singleton-Detector trap: two distinct roots must NOT share the
	// cached load. The reviewer flagged the original sync.Once design
	// would silently pin rootA's pkgs forever and leak into rootB.
	rootA := threeImplsFixture(t)
	rootB := fixture(t, map[string]string{
		"plain.go": `package probe

type Plain struct{}

func (p Plain) Do() {}
`,
	})
	d := New()
	if hits := d.Analyze(scaling.Input{Root: rootA, Changes: changes("mem.go", "disk.go", "net.go")}); len(hits) != 1 {
		t.Errorf("rootA expected 1 hit, got %d", len(hits))
	}
	if hits := d.Analyze(scaling.Input{Root: rootB, Changes: changes("plain.go")}); len(hits) != 0 {
		t.Errorf("rootB has no interfaces; cache must not leak rootA's pkgs. Got %d hits", len(hits))
	}
}
