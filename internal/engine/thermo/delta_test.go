package thermo

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

const eps = 1e-9

func TestMakeFileDelta_Modified(t *testing.T) {
	fd := MakeFileDelta("a.go", DeltaModified, 1.5, 2.5)
	if fd.Delta != 1.0 || fd.SBase != 1.5 || fd.SHead != 2.5 {
		t.Errorf("got %+v, want Delta=1.0", fd)
	}
}

func TestMakeFileDelta_ModifiedRefactor(t *testing.T) {
	// Refactor: head is cleaner than base → negative Delta (improvement).
	fd := MakeFileDelta("a.go", DeltaModified, 3.0, 1.0)
	if fd.Delta != -2.0 {
		t.Errorf("got Delta=%v, want -2.0", fd.Delta)
	}
}

func TestMakeFileDelta_Added(t *testing.T) {
	// SBase must be forced to 0 regardless of caller input.
	fd := MakeFileDelta("a.go", DeltaAdded, 99, 2.5)
	if fd.Delta != 2.5 || fd.SBase != 0 || fd.SHead != 2.5 {
		t.Errorf("got %+v, want Delta=2.5 SBase=0", fd)
	}
}

func TestMakeFileDelta_Removed(t *testing.T) {
	// SHead must be forced to 0 regardless of caller input.
	fd := MakeFileDelta("a.go", DeltaRemoved, 2.0, 99)
	if fd.Delta != -2.0 || fd.SBase != 2.0 || fd.SHead != 0 {
		t.Errorf("got %+v, want Delta=-2.0 SHead=0", fd)
	}
}

func TestComputeDelta_Empty(t *testing.T) {
	d := ComputeDelta(nil, 0)
	if d.Total != 0 || d.Density != 0 || d.LinesChanged != 0 {
		t.Errorf("got %+v, want zero values", d)
	}
	if d.Files != nil {
		t.Errorf("Files = %v, want nil", d.Files)
	}
}

func TestComputeDelta_SumsContributions(t *testing.T) {
	files := []FileDelta{
		MakeFileDelta("a.go", DeltaModified, 1.0, 1.5), // +0.5
		MakeFileDelta("b.go", DeltaAdded, 0, 2.0),      // +2.0
		MakeFileDelta("c.go", DeltaRemoved, 0.5, 0),    // -0.5
		MakeFileDelta("d.go", DeltaModified, 2.0, 1.0), // -1.0
	}
	d := ComputeDelta(files, 100)
	want := 0.5 + 2.0 - 0.5 - 1.0
	if math.Abs(d.Total-want) > eps {
		t.Errorf("Total = %v, want %v", d.Total, want)
	}
	if math.Abs(d.Density-want/100) > eps {
		t.Errorf("Density = %v, want %v", d.Density, want/100)
	}
	if d.LinesChanged != 100 {
		t.Errorf("LinesChanged = %d, want 100", d.LinesChanged)
	}
	if len(d.Files) != 4 {
		t.Errorf("Files len = %d, want 4", len(d.Files))
	}
}

func TestComputeDelta_DensityClampedOnZeroLines(t *testing.T) {
	files := []FileDelta{MakeFileDelta("a.go", DeltaAdded, 0, 5.0)}
	d := ComputeDelta(files, 0)
	// With clamp, Density = 5.0 / 1 = 5.0; without clamp it'd be +Inf.
	if math.IsInf(d.Density, 0) || math.IsNaN(d.Density) {
		t.Errorf("Density = %v, expected finite", d.Density)
	}
	if d.Density != 5.0 {
		t.Errorf("Density = %v, want 5.0", d.Density)
	}
}

func TestComputeDelta_DensityClampedOnNegativeLines(t *testing.T) {
	// Defensive against caller bugs — negative is treated as 1.
	files := []FileDelta{MakeFileDelta("a.go", DeltaAdded, 0, 3.0)}
	d := ComputeDelta(files, -10)
	if d.Density != 3.0 {
		t.Errorf("Density = %v, want 3.0 (clamped)", d.Density)
	}
}

func TestDelta_Fails_StrictlyAbove(t *testing.T) {
	d := Delta{Density: 0.10}
	if !d.Fails(0.05) {
		t.Error("0.10 > 0.05 should fail")
	}
}

func TestDelta_Fails_AtThreshold(t *testing.T) {
	// Formula uses strict `>`; equal is a pass.
	d := Delta{Density: 0.05}
	if d.Fails(0.05) {
		t.Error("0.05 == 0.05 should NOT fail")
	}
}

func TestDelta_Fails_Below(t *testing.T) {
	d := Delta{Density: 0.01}
	if d.Fails(0.05) {
		t.Error("0.01 < 0.05 should NOT fail")
	}
}

func TestDelta_Fails_NegativeDensityRefactor(t *testing.T) {
	// A PR that lowers entropy never fails the gate.
	d := Delta{Density: -0.5}
	if d.Fails(0.05) {
		t.Error("negative density (refactor) should NOT fail")
	}
}

func TestComputeDelta_AllRemoved(t *testing.T) {
	// Pure deletions yield negative ΔS — a "clean-up" PR.
	files := []FileDelta{
		MakeFileDelta("a.go", DeltaRemoved, 2.0, 0),
		MakeFileDelta("b.go", DeltaRemoved, 1.5, 0),
	}
	d := ComputeDelta(files, 50)
	if d.Total != -3.5 {
		t.Errorf("Total = %v, want -3.5", d.Total)
	}
	if d.Density >= 0 {
		t.Errorf("Density = %v, want negative", d.Density)
	}
	if d.Fails(0.05) {
		t.Error("clean-up PR should not fail gate")
	}
}

func TestComputeDelta_FilesAreDefensivelyCopied(t *testing.T) {
	// Mutating the input slice after ComputeDelta must NOT change the
	// returned Delta — verifies the defensive copy.
	files := []FileDelta{MakeFileDelta("a.go", DeltaAdded, 0, 1.0)}
	d := ComputeDelta(files, 10)
	if len(d.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(d.Files))
	}
	files[0].Path = "MUTATED"
	if d.Files[0].Path != "a.go" {
		t.Errorf("Delta.Files[0].Path = %q, expected the copy to survive caller mutation", d.Files[0].Path)
	}
}

func TestComputeDelta_RecomputesDeltaFromKind(t *testing.T) {
	// A hand-constructed FileDelta with a desynced Delta field must
	// NOT poison the aggregate — ComputeDelta recomputes from Kind +
	// SBase + SHead.
	files := []FileDelta{
		{Path: "a.go", Kind: DeltaAdded, SBase: 0, SHead: 2.0, Delta: 99}, // bogus 99
	}
	d := ComputeDelta(files, 10)
	if d.Total != 2.0 {
		t.Errorf("Total = %v, want 2.0 (recomputed, not 99)", d.Total)
	}
}

func TestDelta_Fails_NaNDensityFailsClosed(t *testing.T) {
	// A degenerate upstream Score that produced NaN must not silently
	// pass the gate.
	d := Delta{Density: math.NaN()}
	if !d.Fails(0.05) {
		t.Error("NaN density should fail the gate")
	}
}

func TestDelta_Fails_PosInfDensityFailsClosed(t *testing.T) {
	d := Delta{Density: math.Inf(1)}
	if !d.Fails(0.05) {
		t.Error("+Inf density should fail the gate")
	}
}

func TestDelta_Fails_NegInfDensityPasses(t *testing.T) {
	// −Inf would only arise from S_base = +Inf which is itself a
	// corruption signal, but the gate is for "growth"; a wildly
	// negative density still represents a refactor by sign and we
	// keep the spec's `> threshold` semantics.
	d := Delta{Density: math.Inf(-1)}
	if d.Fails(0.05) {
		t.Error("-Inf density should not fail the gate")
	}
}

func TestComputeDelta_MixedInfFailsClosed(t *testing.T) {
	// +Inf and -Inf in the same aggregate cancel to NaN per IEEE 754.
	// Combined with the NaN guard in Fails, this must fail closed.
	files := []FileDelta{
		{Path: "a.go", Kind: DeltaModified, SBase: 0, SHead: math.Inf(1)},
		{Path: "b.go", Kind: DeltaModified, SBase: math.Inf(1), SHead: 0},
	}
	d := ComputeDelta(files, 10)
	if !math.IsNaN(d.Density) {
		t.Errorf("mixed ±Inf should yield NaN, got Density=%v", d.Density)
	}
	if !d.Fails(0.05) {
		t.Error("mixed-Inf density (NaN) should fail the gate")
	}
}

func TestDeltaKind_String(t *testing.T) {
	tests := map[DeltaKind]string{
		DeltaModified: "modified",
		DeltaAdded:    "added",
		DeltaRemoved:  "removed",
	}
	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("DeltaKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestDeltaKind_MarshalJSON(t *testing.T) {
	files := []FileDelta{
		MakeFileDelta("a.go", DeltaAdded, 0, 1.0),
		MakeFileDelta("b.go", DeltaModified, 1, 2),
		MakeFileDelta("c.go", DeltaRemoved, 1, 0),
	}
	d := ComputeDelta(files, 10)
	blob, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(blob)
	for _, want := range []string{`"kind":"added"`, `"kind":"modified"`, `"kind":"removed"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON output missing %q\nfull: %s", want, s)
		}
	}
}
