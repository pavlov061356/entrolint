package scaling

import (
	"encoding/json"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
)

func TestClass_Ordering(t *testing.T) {
	cases := []Class{ClassO1, ClassOk, ClassON, ClassONM, ClassO2N}
	for i := 0; i < len(cases)-1; i++ {
		if !cases[i].Less(cases[i+1]) {
			t.Errorf("%v should be < %v", cases[i], cases[i+1])
		}
	}
	if ClassO2N.Less(ClassO1) {
		t.Error("O(2^n) cannot be less than O(1)")
	}
}

func TestClass_String(t *testing.T) {
	want := map[Class]string{
		ClassO1:  "O(1)",
		ClassOk:  "O(k)",
		ClassON:  "O(n)",
		ClassONM: "O(n·m)",
		ClassO2N: "O(2ⁿ)",
	}
	for c, w := range want {
		if got := c.String(); got != w {
			t.Errorf("%d.String() = %q, want %q", c, got, w)
		}
	}
}

func TestMax(t *testing.T) {
	if Max(ClassO1, ClassOk) != ClassOk {
		t.Error("Max(O(1), O(k)) should be O(k)")
	}
	if Max(ClassONM, ClassOk) != ClassONM {
		t.Error("Max(O(n*m), O(k)) should be O(n*m)")
	}
	if Max(ClassO1, ClassO1) != ClassO1 {
		t.Error("Max(O(1), O(1)) should be O(1)")
	}
}

func TestClass_JSONRoundTrip(t *testing.T) {
	for _, c := range []Class{ClassO1, ClassOk, ClassON, ClassONM, ClassO2N} {
		blob, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", c, err)
		}
		var back Class
		if err := json.Unmarshal(blob, &back); err != nil {
			t.Fatalf("Unmarshal(%s): %v", blob, err)
		}
		if back != c {
			t.Errorf("round-trip: %v -> %s -> %v", c, blob, back)
		}
	}
}

func TestClass_UnmarshalUnknownFallsToO1(t *testing.T) {
	var c Class = ClassOk
	if err := json.Unmarshal([]byte(`"O(n log n)"`), &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if c != ClassO1 {
		t.Errorf("unknown class should fall to O(1), got %v", c)
	}
}

func TestParseClass_AcceptsBothNotations(t *testing.T) {
	for _, in := range []string{"O(n*m)", "O(n·m)"} {
		c, err := ParseClass(in)
		if err != nil || c != ClassONM {
			t.Errorf("ParseClass(%q) = %v, %v; want O(n*m), nil", in, c, err)
		}
	}
	for _, in := range []string{"O(2^n)", "O(2ⁿ)"} {
		c, err := ParseClass(in)
		if err != nil || c != ClassO2N {
			t.Errorf("ParseClass(%q) = %v, %v; want O(2^n), nil", in, c, err)
		}
	}
}

func TestAnalyze_EmptyRegistryYieldsO1(t *testing.T) {
	got := Analyze([]gitx.Change{
		{Kind: gitx.ChangeModified, Path: "a.go"},
		{Kind: gitx.ChangeAdded, Path: "b.go"},
	})
	if got.Class != ClassO1 {
		t.Errorf("Class = %v, want O(1)", got.Class)
	}
	if len(got.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(got.Files))
	}
	for _, f := range got.Files {
		if f.Class != ClassO1 || len(f.Hits) != 0 {
			t.Errorf("file %s: class=%v hits=%v, want O(1) with no hits", f.Path, f.Class, f.Hits)
		}
	}
	if got.DowngradeBonus != 0 {
		t.Errorf("DowngradeBonus = %v, want 0", got.DowngradeBonus)
	}
}

type fakeDetector struct {
	name string
	hits []Hit
}

func (f fakeDetector) Name() string                { return f.name }
func (f fakeDetector) Analyze([]gitx.Change) []Hit { return f.hits }

func TestAnalyzeWith_AggregatesHitsByMax(t *testing.T) {
	detectors := []Detector{
		fakeDetector{name: "low", hits: []Hit{{Detector: "low", Class: ClassOk, Path: "a.go"}}},
		fakeDetector{name: "high", hits: []Hit{{Detector: "high", Class: ClassON, Path: "a.go"}}},
		fakeDetector{name: "elsewhere", hits: []Hit{{Detector: "elsewhere", Class: ClassOk, Path: "b.go"}}},
	}
	got := AnalyzeWith(detectors, []gitx.Change{
		{Kind: gitx.ChangeModified, Path: "a.go"},
		{Kind: gitx.ChangeModified, Path: "b.go"},
	})

	if got.Class != ClassON {
		t.Errorf("PR Class = %v, want O(n) (max of all per-file classes)", got.Class)
	}

	byPath := map[string]FileResult{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	if byPath["a.go"].Class != ClassON {
		t.Errorf("a.go Class = %v, want O(n) (max of O(k), O(n))", byPath["a.go"].Class)
	}
	if byPath["b.go"].Class != ClassOk {
		t.Errorf("b.go Class = %v, want O(k)", byPath["b.go"].Class)
	}
	if got.Class != Max(byPath["a.go"].Class, byPath["b.go"].Class) {
		t.Error("PR Class must equal max over file classes")
	}
}

func TestAnalyzeWith_HitOnUntrackedPathStillSurfaces(t *testing.T) {
	// A detector that fires on a path the diff didn't include (e.g. a
	// dependency file pulled in by transitive analysis) must still
	// appear in the breakdown — otherwise downstream tooling loses
	// the evidence.
	detectors := []Detector{
		fakeDetector{name: "x", hits: []Hit{{Detector: "x", Class: ClassOk, Path: "untouched.go"}}},
	}
	got := AnalyzeWith(detectors, nil)
	if len(got.Files) != 1 || got.Files[0].Path != "untouched.go" {
		t.Fatalf("expected detector-only path to surface, got %+v", got.Files)
	}
	if got.Class != ClassOk {
		t.Errorf("Class = %v, want O(k)", got.Class)
	}
}
