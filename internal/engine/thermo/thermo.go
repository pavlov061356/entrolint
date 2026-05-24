// Package thermo implements the entrolint v0.1 entropy formula:
// S = k · Σᵢ wᵢ · ln(1 + mᵢ_norm), with per-microstate self-
// calibrated lognormal-CDF normalization and k chosen so that the
// median file scores S = 1. Temperature T = S · ξ(churn) is computed
// separately — churn lives only in T, never in S.
//
// See docs/formula.md for the full specification.
package thermo

import (
	"math"
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/microstate"
)

// DefaultAlpha is the temperature sensitivity to churn: T = S · (1 + α·ln(1+churn)).
const DefaultAlpha = 0.5

// NormalizationFloor is the lower CDF cut: m_norm = max(0, Φ - floor) / (1 - floor).
// Floor = 0.3 means the 30th percentile of the corpus maps to 0,
// preventing the median file in a clean repo from looking half-bad.
const NormalizationFloor = 0.3

// LogNormalParams holds the fitted parameters of a single microstate's
// raw-value distribution. Exported for serialization to the cache.
type LogNormalParams struct {
	Mu    float64
	Sigma float64
	Valid bool // false if the fit failed (insufficient data or zero variance)
}

// Score is what the engine returns per file.
type Score struct {
	S             float64            // calibrated entropy
	T             float64            // S · ξ(churn)
	Contributions map[string]float64 // per-microstate contribution to S (k-scaled)
}

// Engine holds calibrated state and scores files against it.
type Engine struct {
	microstates []microstate.Microstate
	weights     map[string]float64
	lognormal   map[string]LogNormalParams
	k           float64
	alpha       float64
}

// Calibrate fits each microstate's lognormal parameters from `files`
// and computes k so the median file scores S = 1. The microstates
// passed here are the structural contributors to S (cyclomatic,
// nesting, length); churn is intentionally absent — it joins later
// via the temperature factor.
func Calibrate(ms []microstate.Microstate, weights map[string]float64, files []microstate.File) *Engine {
	e := &Engine{
		microstates: ms,
		weights:     weights,
		lognormal:   make(map[string]LogNormalParams, len(ms)),
		alpha:       DefaultAlpha,
		k:           1,
	}
	for _, m := range ms {
		e.lognormal[m.Name()] = fitMicrostate(m, files)
	}
	e.k = calibrateK(e, files)
	return e
}

// Score returns S, T and the per-microstate contributions for a file.
func (e *Engine) Score(f microstate.File) Score {
	contribs := make(map[string]float64, len(e.microstates))
	var sumUnscaled float64
	for _, m := range e.microstates {
		c := e.contribution(m, f)
		contribs[m.Name()] = e.k * c
		sumUnscaled += c
	}
	s := e.k * sumUnscaled
	t := s * (1 + e.alpha*math.Log(1+float64(f.ChurnCount)))
	return Score{S: s, T: t, Contributions: contribs}
}

// K returns the calibrated normalization constant.
func (e *Engine) K() float64 { return e.k }

// LogNormalByName returns a copy of the calibrated lognormal params.
func (e *Engine) LogNormalByName() map[string]LogNormalParams {
	out := make(map[string]LogNormalParams, len(e.lognormal))
	for k, v := range e.lognormal {
		out[k] = v
	}
	return out
}

// contribution returns w · ln(1 + m_norm) — the per-microstate term
// before k-scaling.
func (e *Engine) contribution(m microstate.Microstate, f microstate.File) float64 {
	raw := m.Measure(f)
	norm := e.normalize(m.Name(), raw)
	w := e.weights[m.Name()]
	return w * math.Log(1+norm)
}

// normalize applies the lognormal CDF with floor.
func (e *Engine) normalize(name string, raw float64) float64 {
	if raw <= 0 {
		return 0
	}
	p, ok := e.lognormal[name]
	if !ok || !p.Valid {
		return 0
	}
	z := (math.Log(raw) - p.Mu) / p.Sigma
	cdf := 0.5 * (1 + math.Erf(z/math.Sqrt2))
	out := (cdf - NormalizationFloor) / (1 - NormalizationFloor)
	switch {
	case out < 0:
		return 0
	case out > 1:
		return 1
	default:
		return out
	}
}

func fitMicrostate(m microstate.Microstate, files []microstate.File) LogNormalParams {
	logs := make([]float64, 0, len(files))
	for _, f := range files {
		v := m.Measure(f)
		if v > 0 {
			logs = append(logs, math.Log(v))
		}
	}
	if len(logs) < 2 {
		return LogNormalParams{}
	}
	var sum float64
	for _, l := range logs {
		sum += l
	}
	mu := sum / float64(len(logs))
	var ss float64
	for _, l := range logs {
		d := l - mu
		ss += d * d
	}
	sigma := math.Sqrt(ss / float64(len(logs)))
	if sigma <= 0 {
		return LogNormalParams{}
	}
	return LogNormalParams{Mu: mu, Sigma: sigma, Valid: true}
}

func calibrateK(e *Engine, files []microstate.File) float64 {
	if len(files) == 0 {
		return 1
	}
	unscaled := make([]float64, len(files))
	for i, f := range files {
		var sum float64
		for _, m := range e.microstates {
			sum += e.contribution(m, f)
		}
		unscaled[i] = sum
	}
	med := median(unscaled)
	if med <= 0 {
		return 1
	}
	return 1 / med
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
