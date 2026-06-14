package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/microstate"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

func sampleState() State {
	return State{
		Version: SchemaVersion,
		K:       1.5,
		Alpha:   0.5,
		Microstates: map[string]thermo.LogNormalParams{
			"cyclomatic": {Mu: 1.2, Sigma: 0.7, Valid: true},
			"nesting":    {Mu: 0.8, Sigma: 0.3, Valid: true},
			"length":     {Mu: 4.2, Sigma: 1.1, Valid: true},
		},
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	want := sampleState()
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %d, want %d", got.Version, want.Version)
	}
	if got.K != want.K {
		t.Errorf("K = %v, want %v", got.K, want.K)
	}
	if got.Alpha != want.Alpha {
		t.Errorf("Alpha = %v, want %v", got.Alpha, want.Alpha)
	}
	for name, wantP := range want.Microstates {
		gotP := got.Microstates[name]
		if gotP != wantP {
			t.Errorf("Microstates[%q] = %+v, want %+v", name, gotP, wantP)
		}
	}
}

func TestLoad_MissingFileReturnsErrNotExist(t *testing.T) {
	_, err := Load("/nonexistent/cache/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestLoad_RejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte(`{"version": 999, "k": 1, "alpha": 0.5}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
}

func TestHasAll(t *testing.T) {
	s := sampleState()
	tests := []struct {
		name  string
		names []string
		want  bool
	}{
		{"all present", []string{"cyclomatic", "nesting", "length"}, true},
		{"subset", []string{"cyclomatic"}, true},
		{"empty set", []string{}, true},
		{"missing one (new microstate added)", []string{"cyclomatic", "nesting", "length", "coupling"}, false},
		{"missing all", []string{"coupling"}, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := s.HasAll(tc.names); got != tc.want {
				t.Errorf("HasAll(%v) = %v, want %v", tc.names, got, tc.want)
			}
		})
	}
}

func TestHasAll_PresentButDegenerateCountsAsCached(t *testing.T) {
	// A present-but-degenerate (Valid:false) microstate is a legitimately
	// cached result for a microstate with no signal in its corpus (e.g.
	// cross_duplication on a clone-free repo). It must NOT force a
	// recalibration every run — only a missing key (a stale cache that
	// predates the microstate) does. thermo.normalize returns 0 for the
	// degenerate params, so it still scores correctly as 0.
	s := State{
		Microstates: map[string]thermo.LogNormalParams{
			"cyclomatic": {Mu: 1, Sigma: 1, Valid: true},
			"coupling":   {Valid: false}, // present, fit on a corpus with no signal
		},
	}
	if !s.HasAll([]string{"cyclomatic", "coupling"}) {
		t.Error("present-but-degenerate microstate must count as cached, not missing")
	}
	// A key absent from the map IS a miss (cache predates the microstate).
	if s.HasAll([]string{"cyclomatic", "coupling", "length"}) {
		t.Error("a microstate absent from the cache must force recalibration")
	}
}

func TestSave_FillsMissingVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	s := sampleState()
	s.Version = 0
	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != SchemaVersion {
		t.Errorf("Version after save with 0 = %d, want %d", got.Version, SchemaVersion)
	}
}

// fakeMicrostate makes engine reconstruction testable without parsing Go code.
type fakeMicrostate struct{ name string }

func (f fakeMicrostate) Name() string                      { return f.name }
func (f fakeMicrostate) Measure(_ microstate.File) float64 { return 0 }

func TestStateFromEngine_ReflectsCalibration(t *testing.T) {
	ms := []microstate.Microstate{fakeMicrostate{name: "cyclomatic"}}
	weights := map[string]float64{"cyclomatic": 1.0}
	lognormal := map[string]thermo.LogNormalParams{
		"cyclomatic": {Mu: 2.5, Sigma: 0.9, Valid: true},
	}
	e := thermo.NewEngine(ms, weights, 0.42, 0.5, lognormal)

	s := StateFromEngine(e)
	if s.K != 0.42 {
		t.Errorf("K = %v, want 0.42", s.K)
	}
	if s.Alpha != 0.5 {
		t.Errorf("Alpha = %v, want 0.5", s.Alpha)
	}
	if s.Microstates["cyclomatic"] != lognormal["cyclomatic"] {
		t.Errorf("Microstates[cyclomatic] = %+v, want %+v", s.Microstates["cyclomatic"], lognormal["cyclomatic"])
	}
}

func TestSaveLoad_RoundtripPreservesEngineState(t *testing.T) {
	ms := []microstate.Microstate{fakeMicrostate{name: "cyclomatic"}}
	weights := map[string]float64{"cyclomatic": 1.0}
	lognormal := map[string]thermo.LogNormalParams{
		"cyclomatic": {Mu: 2.5, Sigma: 0.9, Valid: true},
	}
	original := thermo.NewEngine(ms, weights, 0.42, 0.5, lognormal)

	path := filepath.Join(t.TempDir(), "cache.json")
	if err := Save(path, StateFromEngine(original)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rebuilt := thermo.NewEngine(ms, weights, loaded.K, loaded.Alpha, loaded.Microstates)

	if rebuilt.K() != original.K() {
		t.Errorf("K mismatch after roundtrip: %v vs %v", rebuilt.K(), original.K())
	}
	if rebuilt.Alpha() != original.Alpha() {
		t.Errorf("Alpha mismatch: %v vs %v", rebuilt.Alpha(), original.Alpha())
	}
	if rebuilt.LogNormalByName()["cyclomatic"] != original.LogNormalByName()["cyclomatic"] {
		t.Errorf("LogNormalByName mismatch after roundtrip")
	}
}
