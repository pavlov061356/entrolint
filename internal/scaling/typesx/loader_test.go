package typesx

import (
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

// fixtureDir materializes files under a fresh temp dir and returns the
// root. A go.mod is supplied unless the caller provides one.
func fixtureDir(t *testing.T, files map[string]string) string {
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

// probeFixture: a `probe` package carrying a real enum (Color, ≥2 consts),
// a single-const named type (Single — not an enum), a non-named const
// (Plain), a sibling `sub` package, and a deliberately ill-typed `broken`
// package that filterUsable must drop.
func probeFixture(t *testing.T) string {
	t.Helper()
	return fixtureDir(t, map[string]string{
		"enum.go": `package probe

type Color int

const (
	Red Color = iota
	Green
	Blue
)

type Single int

const Only Single = 0

const Plain = 5
`,
		"sub/sub.go": `package sub

func Helper() int { return 1 }
`,
		"broken/broken.go": `package broken

var X = undefinedSymbol
`,
	})
}

// findProbe returns the loaded `probe` package by import path.
func findProbe(t *testing.T, pkgs []*packages.Package) *packages.Package {
	t.Helper()
	for _, p := range pkgs {
		if p.PkgPath == "example.com/probe" {
			return p
		}
	}
	t.Fatalf("probe package not found among %d loaded packages", len(pkgs))
	return nil
}

func TestLoad_FiltersIllTypedAndCaches(t *testing.T) {
	dir := probeFixture(t)
	var l Loader

	pkgs, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("Load returned no usable packages")
	}
	for _, p := range pkgs {
		if p.PkgPath == "example.com/probe/broken" {
			t.Errorf("ill-typed package leaked past filterUsable: %s", p.PkgPath)
		}
		if p.Types == nil || p.IllTyped || len(p.Errors) > 0 {
			t.Errorf("unusable package survived filterUsable: %s", p.PkgPath)
		}
	}

	// Second Load on the same root must hit the cache: same backing slice.
	again, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load (cached): %v", err)
	}
	if len(again) != len(pkgs) || &again[0] != &pkgs[0] {
		t.Error("second Load did not return the cached slice")
	}
}

func TestFindPackageByFile_AbsoluteRelativeAndMiss(t *testing.T) {
	dir := probeFixture(t)
	var l Loader
	pkgs, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	probe := findProbe(t, pkgs)

	// Absolute path matches a GoFiles entry directly.
	absEnum := filepath.Join(dir, "enum.go")
	if got := FindPackageByFile(pkgs, absEnum); got != probe {
		t.Errorf("absolute lookup: got %v, want probe", got)
	}

	// Repo-relative path matches by suffix.
	if got := FindPackageByFile(pkgs, "enum.go"); got != probe {
		t.Errorf("relative-suffix lookup: got %v, want probe", got)
	}

	// Unknown file → nil.
	if got := FindPackageByFile(pkgs, "nope.go"); got != nil {
		t.Errorf("missing file: got %v, want nil", got)
	}
}

func TestFindOwningPackage_IdentityAndNilCases(t *testing.T) {
	dir := probeFixture(t)
	var l Loader
	pkgs, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	probe := findProbe(t, pkgs)

	red := probe.Types.Scope().Lookup("Red")
	if red == nil {
		t.Fatal("Red not found in probe scope")
	}
	if got := FindOwningPackage(pkgs, red); got != probe {
		t.Errorf("owning package: got %v, want probe", got)
	}

	// nil object.
	if got := FindOwningPackage(pkgs, nil); got != nil {
		t.Errorf("nil object: got %v, want nil", got)
	}

	// Object with no package (a universe type) → nil.
	if got := FindOwningPackage(pkgs, types.Universe.Lookup("error")); got != nil {
		t.Errorf("universe object: got %v, want nil", got)
	}
}

func TestPosInChanged(t *testing.T) {
	dir := probeFixture(t)
	var l Loader
	pkgs, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	probe := findProbe(t, pkgs)
	red := probe.Types.Scope().Lookup("Red")

	changed := ChangedFileSet(scaling.Input{
		Root:    dir,
		Changes: []gitx.Change{{Kind: gitx.ChangeModified, Path: "enum.go"}},
	})
	if !PosInChanged(probe, red.Pos(), changed) {
		t.Error("Red lives in the changed enum.go but PosInChanged returned false")
	}

	empty := map[string]bool{}
	if PosInChanged(probe, red.Pos(), empty) {
		t.Error("empty changed set must not match any pos")
	}
}

func TestCollectEnums(t *testing.T) {
	dir := probeFixture(t)
	var l Loader
	pkgs, err := l.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	probe := findProbe(t, pkgs)

	enums := CollectEnums(pkgs)

	color, ok := probe.Types.Scope().Lookup("Color").(*types.TypeName)
	if !ok {
		t.Fatal("Color is not a TypeName")
	}
	colorNamed, ok := types.Unalias(color.Type()).(*types.Named)
	if !ok {
		t.Fatal("Color type is not *types.Named")
	}
	if !enums[colorNamed] {
		t.Error("Color (3 consts) should be detected as an enum")
	}

	// Single has only one const → not an enum.
	single := probe.Types.Scope().Lookup("Single").(*types.TypeName)
	singleNamed := types.Unalias(single.Type()).(*types.Named)
	if enums[singleNamed] {
		t.Error("Single (1 const) must not be classified as an enum")
	}
}

func TestRelativize_FallbackOnRelError(t *testing.T) {
	// A relative base with an absolute target makes filepath.Rel fail;
	// Relativize must fall back to the cleaned absolute path.
	if got := Relativize("rel/base", "/abs/x.go"); got != "/abs/x.go" {
		t.Errorf("fallback: got %q, want %q", got, "/abs/x.go")
	}
}
