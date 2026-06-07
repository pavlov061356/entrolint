package switchsymmetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// enumFixture: an enum `Kind` with 3 constants and 3 switch statements
// across 3 separate files. Tests pick which subset to claim touched.
func enumFixture(t *testing.T) string {
	return fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
	KindC
)
`,
		"sw1.go": `package probe

func S1(k Kind) string {
	switch k {
	case KindA:
		return "a"
	case KindB:
		return "b"
	}
	return ""
}
`,
		"sw2.go": `package probe

func S2(k Kind) int {
	switch k {
	case KindA:
		return 1
	case KindB:
		return 2
	}
	return 0
}
`,
		"sw3.go": `package probe

func S3(k Kind) bool {
	switch k {
	case KindA:
		return true
	case KindB:
		return false
	}
	return false
}
`,
	})
}

func TestSwitchSymmetry_AllSwitchesTouchedFires(t *testing.T) {
	root := enumFixture(t)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go", "sw3.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Class != scaling.ClassOk {
		t.Errorf("class = %v, want O(k)", h.Class)
	}
	if h.Size != 3 {
		t.Errorf("size = %d, want 3 (switches)", h.Size)
	}
	if h.Path != "kind.go" {
		t.Errorf("hit Path = %q, want kind.go (enum decl site)", h.Path)
	}
	if h.Detector != name {
		t.Errorf("detector = %q, want %q", h.Detector, name)
	}
}

func TestSwitchSymmetry_HalfFires(t *testing.T) {
	root := enumFixture(t)
	// 2 of 3 switches = 66% ≥ 50%
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit at 2/3 switches, got %d", len(hits))
	}
}

func TestSwitchSymmetry_BelowRatioStaysSilent(t *testing.T) {
	root := enumFixture(t)
	// 1 of 3 ≈ 33%
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go"),
	})
	if len(hits) != 0 {
		t.Errorf("1 of 3 (33%%) must not fire, got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_BelowMinSwitchesNoFire(t *testing.T) {
	// One switch over an enum: there's no "symmetry" to detect.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
		"sw.go": `package probe

func S(k Kind) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{Root: root, Changes: changes("sw.go")})
	if len(hits) != 0 {
		t.Errorf("single-switch enum must not fire, got %d", len(hits))
	}
}

func TestSwitchSymmetry_NotAnEnumNoFire(t *testing.T) {
	// A named type with only ONE constant of its type doesn't qualify
	// as an enum, even with switches over it.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const KindA Kind = 1
`,
		"sw1.go": `package probe

func S1(k Kind) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
		"sw2.go": `package probe

func S2(k Kind) int {
	switch k {
	case KindA:
		return 2
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go"),
	})
	if len(hits) != 0 {
		t.Errorf("type with 1 const isn't an enum; got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_TypeSwitchIsSkipped(t *testing.T) {
	// `switch x.(type) {}` is a different language construct from
	// `switch x` over an enum. Must not be counted.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)

type Iface interface{ K() Kind }
`,
		"sw1.go": `package probe

func S1(i Iface) string {
	switch i.(type) {
	case nil:
		return "nil"
	}
	return ""
}
`,
		"sw2.go": `package probe

func S2(i Iface) string {
	switch i.(type) {
	case nil:
		return "nil"
	}
	return ""
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go"),
	})
	if len(hits) != 0 {
		t.Errorf("type switches must not count; got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_BareSwitchNoTagIgnored(t *testing.T) {
	// `switch { case x == 1: ... }` has nil Tag — no static type to
	// associate with an enum. Must be skipped.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
		"sw1.go": `package probe

func S1(k Kind) int {
	switch {
	case k == KindA:
		return 1
	}
	return 0
}
`,
		"sw2.go": `package probe

func S2(k Kind) int {
	switch {
	case k == KindB:
		return 2
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go"),
	})
	if len(hits) != 0 {
		t.Errorf("tag-less switches must not count; got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_CrossPackageSwitchesFire(t *testing.T) {
	// Enum declared in pkg kind, switches in sibling pkg use.
	root := fixture(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"kind/kind.go": `package kind

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
		"use/a.go": `package use

import "example.com/probe/kind"

func A(k kind.Kind) int {
	switch k {
	case kind.KindA:
		return 1
	}
	return 0
}
`,
		"use/b.go": `package use

import "example.com/probe/kind"

func B(k kind.Kind) int {
	switch k {
	case kind.KindB:
		return 2
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("use/a.go", "use/b.go"),
	})
	if len(hits) != 1 {
		t.Fatalf("cross-pkg switches must group under their enum; got %d hits", len(hits))
	}
	if hits[0].Path != filepath.Join("kind", "kind.go") {
		t.Errorf("hit Path = %q, want kind/kind.go", hits[0].Path)
	}
}

func TestSwitchSymmetry_EmptyRootSkipsSilently(t *testing.T) {
	if hits := New().Analyze(scaling.Input{Changes: changes("a.go")}); len(hits) != 0 {
		t.Errorf("empty Root must soft-skip, got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_NoSwitchesNoFire(t *testing.T) {
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
	})
	hits := New().Analyze(scaling.Input{Root: root, Changes: changes("kind.go")})
	if len(hits) != 0 {
		t.Errorf("enum with no switches must not fire; got %d", len(hits))
	}
}

func TestSwitchSymmetry_NonGoChangesIgnored(t *testing.T) {
	root := enumFixture(t)
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go", "sw3.go", "README.md"),
	})
	if len(hits) != 1 {
		t.Errorf("non-Go changes must not perturb; got %d", len(hits))
	}
}

func TestSwitchSymmetry_AbsolutePathsInChangesNormalized(t *testing.T) {
	root := enumFixture(t)
	hits := New().Analyze(scaling.Input{
		Root: root,
		Changes: changes(
			filepath.Join(root, "sw1.go"),
			filepath.Join(root, "sw2.go"),
		),
	})
	if len(hits) != 1 {
		t.Errorf("absolute paths must produce the same hit; got %d", len(hits))
	}
}

func TestSwitchSymmetry_BogusRatioFallsBackToDefault(t *testing.T) {
	root := enumFixture(t)
	in := scaling.Input{Root: root, Changes: changes("sw1.go", "sw2.go")} // 2/3 = 66%
	for _, bogus := range []float64{-0.1, 0, 1.5} {
		t.Run(fmt.Sprintf("ratio=%v", bogus), func(t *testing.T) {
			d := &Detector{MinSwitches: 2, TouchedRatio: bogus}
			if hits := d.Analyze(in); len(hits) != 1 {
				t.Errorf("bogus %v must fall back to default; got %d", bogus, len(hits))
			}
		})
	}
}

func TestSwitchSymmetry_RegisteredInRegistry(t *testing.T) {
	found := false
	for _, d := range scaling.Registry {
		if d.Name() == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("switch_case_symmetry not registered; got %d entries", len(scaling.Registry))
	}
}

func TestSwitchSymmetry_MultipleEnumsEachFire(t *testing.T) {
	// Two unrelated enums each with 2 switches each, all touched.
	// Detector must emit one hit per enum — a regression that
	// collapsed groups into a single accumulator would surface as 1
	// hit instead of 2.
	root := fixture(t, map[string]string{
		"enums.go": `package probe

type Kind int
type Color int

const (
	KindA Kind = iota
	KindB
)

const (
	Red Color = iota
	Blue
)
`,
		"kind_sw.go": `package probe

func K(k Kind) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
		"kind_sw2.go": `package probe

func K2(k Kind) int {
	switch k {
	case KindB:
		return 2
	}
	return 0
}
`,
		"color_sw.go": `package probe

func C(c Color) string {
	switch c {
	case Red:
		return "r"
	}
	return ""
}
`,
		"color_sw2.go": `package probe

func C2(c Color) string {
	switch c {
	case Blue:
		return "b"
	}
	return ""
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("kind_sw.go", "kind_sw2.go", "color_sw.go", "color_sw2.go"),
	})
	if len(hits) != 2 {
		t.Fatalf("two enums each fully touched must emit 2 hits, got %d: %+v", len(hits), hits)
	}
	gotEvidence := hits[0].Evidence + "|" + hits[1].Evidence
	for _, want := range []string{"Kind", "Color"} {
		if !strings.Contains(gotEvidence, want) {
			t.Errorf("expected evidence mentioning %q, got %q", want, gotEvidence)
		}
	}
}

func TestSwitchSymmetry_AliasEnumStillFires(t *testing.T) {
	// Under Go 1.23+ default `gotypesalias=1`, `type AliasKind = Kind`
	// surfaces as *types.Alias from TypesInfo.TypeOf, not *types.Named.
	// Without types.Unalias the detector silently misses the alias and
	// records zero switches. Regression pin for that bug.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
		"alias.go": `package probe

type AliasKind = Kind
`,
		"sw1.go": `package probe

func S1(k AliasKind) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
		"sw2.go": `package probe

func S2(k AliasKind) int {
	switch k {
	case KindB:
		return 2
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2.go"),
	})
	if len(hits) != 1 {
		t.Errorf("alias-typed enum tag must fold into the canonical Kind via types.Unalias; got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_TestFileSwitchesNotCounted(t *testing.T) {
	// Spec says "switches over this type in the package" — production code,
	// not _test.go. Tests=false in the loader is the load-time
	// defense; this test pins the contract from the outside.
	root := fixture(t, map[string]string{
		"kind.go": `package probe

type Kind int

const (
	KindA Kind = iota
	KindB
)
`,
		"sw1.go": `package probe

func S1(k Kind) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
		"sw2_test.go": `package probe

import "testing"

func TestSw(t *testing.T) {
	var k Kind
	switch k {
	case KindA:
		t.Log("a")
	}
}
`,
		"sw3_test.go": `package probe

import "testing"

func TestOther(t *testing.T) {
	var k Kind
	switch k {
	case KindB:
		t.Log("b")
	}
}
`,
	})
	// Only 1 production switch exists → below DefaultMinSwitches=2
	// after test files are excluded. If test switches leaked in,
	// minSwitches=3 would be met and the detector would fire.
	hits := New().Analyze(scaling.Input{
		Root:    root,
		Changes: changes("sw1.go", "sw2_test.go", "sw3_test.go"),
	})
	if len(hits) != 0 {
		t.Errorf("test-file switches must not enter the denominator; got %d hits", len(hits))
	}
}

func TestSwitchSymmetry_GenericEnumSkipped(t *testing.T) {
	// type Kind[T any] int doesn't appear as an enum candidate: a
	// generic named type is skipped per the same policy as
	// implementor_scan.
	root := fixture(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.21\n",
		"kind.go": `package probe

type Kind[T any] int

const (
	KindA Kind[int] = 1
	KindB Kind[int] = 2
)
`,
		"sw1.go": `package probe

func S1(k Kind[int]) int {
	switch k {
	case KindA:
		return 1
	}
	return 0
}
`,
		"sw2.go": `package probe

func S2(k Kind[int]) int {
	switch k {
	case KindB:
		return 2
	}
	return 0
}
`,
	})
	hits := New().Analyze(scaling.Input{Root: root, Changes: changes("sw1.go", "sw2.go")})
	if len(hits) != 0 {
		t.Errorf("generic enum must skip; got %d", len(hits))
	}
}
