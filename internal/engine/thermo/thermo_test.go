package thermo

import (
	"math"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/microstate"
)

// fakeMicrostate looks up its raw value per file by Path.
type fakeMicrostate struct {
	name   string
	byPath map[string]float64
}

func (f fakeMicrostate) Name() string { return f.name }
func (f fakeMicrostate) Measure(file microstate.File) float64 {
	return f.byPath[file.Path]
}

func mkFile(path string, churn int) microstate.File {
	return microstate.File{Path: path, ChurnCount: churn}
}

func TestMedian_OddCount(t *testing.T) {
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("median = %v, want 2", got)
	}
}

func TestMedian_EvenCount(t *testing.T) {
	if got := median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("median = %v, want 2.5", got)
	}
}

func TestMedian_Empty(t *testing.T) {
	if got := median(nil); got != 0 {
		t.Errorf("median = %v, want 0", got)
	}
}

func TestFitMicrostate_InsufficientData(t *testing.T) {
	m := fakeMicrostate{name: "x", byPath: map[string]float64{"a.go": 5}}
	p := fitMicrostate(m, []microstate.File{mkFile("a.go", 0)})
	if p.Valid {
		t.Errorf("expected invalid params with only one positive value, got %v", p)
	}
}

func TestFitMicrostate_ZeroVariance(t *testing.T) {
	m := fakeMicrostate{name: "x", byPath: map[string]float64{
		"a.go": 5, "b.go": 5, "c.go": 5,
	}}
	files := []microstate.File{mkFile("a.go", 0), mkFile("b.go", 0), mkFile("c.go", 0)}
	p := fitMicrostate(m, files)
	if p.Valid {
		t.Errorf("expected invalid params for zero variance, got %v", p)
	}
}

func TestFitMicrostate_TypicalDistribution(t *testing.T) {
	m := fakeMicrostate{name: "x", byPath: map[string]float64{
		"a.go": 1, "b.go": math.E, "c.go": math.E * math.E,
	}}
	files := []microstate.File{mkFile("a.go", 0), mkFile("b.go", 0), mkFile("c.go", 0)}
	p := fitMicrostate(m, files)
	if !p.Valid {
		t.Fatalf("expected valid params, got %v", p)
	}
	// ln values are 0, 1, 2; mean = 1, stddev = sqrt(2/3) ≈ 0.816
	if math.Abs(p.Mu-1) > 1e-9 {
		t.Errorf("Mu = %v, want 1", p.Mu)
	}
	wantSigma := math.Sqrt(2.0 / 3.0)
	if math.Abs(p.Sigma-wantSigma) > 1e-9 {
		t.Errorf("Sigma = %v, want %v", p.Sigma, wantSigma)
	}
}

func TestNormalize_BelowFloorIsZero(t *testing.T) {
	e := &Engine{lognormal: map[string]LogNormalParams{
		"x": {Mu: 0, Sigma: 1, Valid: true},
	}}
	// raw=0 always returns 0
	if got := e.normalize("x", 0); got != 0 {
		t.Errorf("normalize(0) = %v, want 0", got)
	}
	// raw very small → CDF near 0 → below floor → 0
	if got := e.normalize("x", 0.001); got != 0 {
		t.Errorf("normalize(0.001) = %v, want 0", got)
	}
}

func TestNormalize_HighValueClampsToOne(t *testing.T) {
	e := &Engine{lognormal: map[string]LogNormalParams{
		"x": {Mu: 0, Sigma: 1, Valid: true},
	}}
	if got := e.normalize("x", 1e6); got != 1 {
		t.Errorf("normalize(huge) = %v, want 1", got)
	}
}

func TestNormalize_InvalidParamsReturnZero(t *testing.T) {
	e := &Engine{lognormal: map[string]LogNormalParams{
		"x": {Valid: false},
	}}
	if got := e.normalize("x", 5); got != 0 {
		t.Errorf("normalize with invalid params = %v, want 0", got)
	}
}

func TestNormalize_UnknownMicrostateReturnsZero(t *testing.T) {
	e := &Engine{lognormal: map[string]LogNormalParams{}}
	if got := e.normalize("missing", 5); got != 0 {
		t.Errorf("normalize unknown = %v, want 0", got)
	}
}

func TestCalibrate_MedianFileScoresOne(t *testing.T) {
	values := map[string]float64{"a.go": 1, "b.go": 4, "c.go": 16, "d.go": 64, "e.go": 256}
	m := fakeMicrostate{name: "x", byPath: values}
	files := []microstate.File{
		mkFile("a.go", 0), mkFile("b.go", 0), mkFile("c.go", 0), mkFile("d.go", 0), mkFile("e.go", 0),
	}
	e := Calibrate([]microstate.Microstate{m}, map[string]float64{"x": 1}, files)

	scores := make([]float64, len(files))
	for i, f := range files {
		scores[i] = e.Score(f).S
	}
	med := median(scores)
	if math.Abs(med-1) > 1e-9 {
		t.Errorf("median S after calibration = %v, want 1", med)
	}
}

func TestCalibrate_EmptyCorpusYieldsUnitK(t *testing.T) {
	e := Calibrate(nil, nil, nil)
	if e.K() != 1 {
		t.Errorf("k on empty corpus = %v, want 1", e.K())
	}
	score := e.Score(mkFile("anywhere.go", 0))
	if score.S != 0 || score.T != 0 {
		t.Errorf("empty engine Score = %+v, want zero", score)
	}
}

func TestScore_ChurnAddsTemperature(t *testing.T) {
	values := map[string]float64{"a.go": 1, "b.go": 4, "c.go": 16}
	files := []microstate.File{mkFile("a.go", 0), mkFile("b.go", 5), mkFile("c.go", 0)}
	m := fakeMicrostate{name: "x", byPath: values}
	e := Calibrate([]microstate.Microstate{m}, map[string]float64{"x": 1}, files)

	cold := e.Score(mkFile("b.go", 0))
	hot := e.Score(mkFile("b.go", 50))
	if cold.S != hot.S {
		t.Errorf("S should be churn-independent: cold.S=%v hot.S=%v", cold.S, hot.S)
	}
	if cold.T != cold.S {
		t.Errorf("zero churn: T (%v) should equal S (%v)", cold.T, cold.S)
	}
	if hot.T <= cold.T {
		t.Errorf("high churn should raise T: cold.T=%v hot.T=%v", cold.T, hot.T)
	}
	// ξ(50) = 1 + 0.5·ln(51)
	wantXi := 1 + 0.5*math.Log(51)
	if math.Abs(hot.T/hot.S-wantXi) > 1e-9 {
		t.Errorf("ξ(50) = %v, want %v", hot.T/hot.S, wantXi)
	}
}

func TestScore_ContributionsSumToS(t *testing.T) {
	values1 := map[string]float64{"a.go": 1, "b.go": 4, "c.go": 16}
	values2 := map[string]float64{"a.go": 2, "b.go": 8, "c.go": 32}
	files := []microstate.File{mkFile("a.go", 0), mkFile("b.go", 0), mkFile("c.go", 0)}
	m1 := fakeMicrostate{name: "m1", byPath: values1}
	m2 := fakeMicrostate{name: "m2", byPath: values2}
	e := Calibrate(
		[]microstate.Microstate{m1, m2},
		map[string]float64{"m1": 1, "m2": 0.5},
		files,
	)
	for _, f := range files {
		s := e.Score(f)
		var sum float64
		for _, c := range s.Contributions {
			sum += c
		}
		if math.Abs(sum-s.S) > 1e-9 {
			t.Errorf("file %s: Σ contributions (%v) != S (%v)", f.Path, sum, s.S)
		}
	}
}

func TestEngine_AccessorsReturnCalibratedState(t *testing.T) {
	values := map[string]float64{"a.go": 1, "b.go": 4, "c.go": 16}
	files := []microstate.File{mkFile("a.go", 0), mkFile("b.go", 0), mkFile("c.go", 0)}
	m := fakeMicrostate{name: "x", byPath: values}
	e := Calibrate([]microstate.Microstate{m}, map[string]float64{"x": 1}, files)

	if e.K() <= 0 {
		t.Errorf("K = %v, want positive", e.K())
	}
	params := e.LogNormalByName()
	p, ok := params["x"]
	if !ok {
		t.Fatalf("expected calibrated params for microstate x")
	}
	if !p.Valid {
		t.Errorf("expected Valid params, got %+v", p)
	}
	// Copy returned; mutating doesn't affect engine.
	params["x"] = LogNormalParams{}
	if !e.LogNormalByName()["x"].Valid {
		t.Errorf("LogNormalByName should return a copy; engine state was mutated")
	}
}
