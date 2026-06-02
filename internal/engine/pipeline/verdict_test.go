package pipeline

import (
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func cfgWith(deltaMax float64, classMax scaling.Class) config.Config {
	c := config.Default()
	c.DeltaSMax = deltaMax
	c.ScalingClassMax = classMax
	return c
}

func TestVerdict_PassWhenBothAxesUnderThreshold(t *testing.T) {
	r := CheckResult{
		Delta:   thermo.Delta{Density: 0.01},
		Scaling: scaling.Result{Class: scaling.ClassO1},
	}
	v := r.Verdict(cfgWith(0.05, scaling.ClassOk))
	if v.Failed || len(v.Reasons) != 0 {
		t.Errorf("expected PASS with no reasons, got %+v", v)
	}
}

func TestVerdict_FailOnDeltaOnly(t *testing.T) {
	r := CheckResult{
		Delta:   thermo.Delta{Density: 0.10},
		Scaling: scaling.Result{Class: scaling.ClassO1},
	}
	v := r.Verdict(cfgWith(0.05, scaling.ClassOk))
	if !v.Failed {
		t.Fatal("expected FAIL")
	}
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %v", v.Reasons)
	}
	if !strings.Contains(v.Reasons[0], "ΔS_density") {
		t.Errorf("reason = %q, want ΔS_density mention", v.Reasons[0])
	}
}

func TestVerdict_FailOnScalingOnly(t *testing.T) {
	r := CheckResult{
		Delta:   thermo.Delta{Density: 0.01},
		Scaling: scaling.Result{Class: scaling.ClassON},
	}
	v := r.Verdict(cfgWith(0.05, scaling.ClassOk))
	if !v.Failed {
		t.Fatal("expected FAIL")
	}
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %v", v.Reasons)
	}
	if !strings.Contains(v.Reasons[0], "scaling_class") {
		t.Errorf("reason = %q, want scaling_class mention", v.Reasons[0])
	}
}

func TestVerdict_FailOnBothAxes(t *testing.T) {
	r := CheckResult{
		Delta:   thermo.Delta{Density: 0.10},
		Scaling: scaling.Result{Class: scaling.ClassONM},
	}
	v := r.Verdict(cfgWith(0.05, scaling.ClassOk))
	if !v.Failed || len(v.Reasons) != 2 {
		t.Fatalf("expected FAIL with 2 reasons, got %+v", v)
	}
	// Ordering contract: ΔS first, scaling second — stable for rendering.
	if !strings.Contains(v.Reasons[0], "ΔS_density") {
		t.Errorf("reasons[0] = %q, want ΔS first", v.Reasons[0])
	}
	if !strings.Contains(v.Reasons[1], "scaling_class") {
		t.Errorf("reasons[1] = %q, want scaling second", v.Reasons[1])
	}
}

func TestVerdict_ScalingCeilingIsInclusive(t *testing.T) {
	// PR class exactly equals the configured max — must pass.
	r := CheckResult{
		Delta:   thermo.Delta{Density: 0.01},
		Scaling: scaling.Result{Class: scaling.ClassOk},
	}
	v := r.Verdict(cfgWith(0.05, scaling.ClassOk))
	if v.Failed {
		t.Errorf("class == max must pass, got %+v", v)
	}
}
