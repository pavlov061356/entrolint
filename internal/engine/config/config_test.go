package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pavlov061356/entrolint/internal/scaling"
)

func TestDefault_ContainsExpectedFields(t *testing.T) {
	d := Default()
	wantWeights := map[string]float64{"cyclomatic": 1.0, "nesting": 0.8, "coupling": 0.6, "length": 0.5}
	for k, v := range wantWeights {
		if got := d.Weights[k]; got != v {
			t.Errorf("Default Weights[%q] = %v, want %v", k, got, v)
		}
	}
	if d.DeltaSMax != 0.05 {
		t.Errorf("Default DeltaSMax = %v, want 0.05", d.DeltaSMax)
	}
	if d.ChurnSinceDays != 90 {
		t.Errorf("Default ChurnSinceDays = %d, want 90", d.ChurnSinceDays)
	}
	if d.ScalingClassMax != scaling.ClassOk {
		t.Errorf("Default ScalingClassMax = %v, want O(k)", d.ScalingClassMax)
	}
	if d.ScalingBonusBeta != 0.5 {
		t.Errorf("Default ScalingBonusBeta = %v, want 0.5", d.ScalingBonusBeta)
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	got, err := Load("/nonexistent/path/.entrolint.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Default()
	if got.DeltaSMax != want.DeltaSMax {
		t.Errorf("DeltaSMax = %v, want %v", got.DeltaSMax, want.DeltaSMax)
	}
	if got.ChurnSinceDays != want.ChurnSinceDays {
		t.Errorf("ChurnSinceDays = %d, want %d", got.ChurnSinceDays, want.ChurnSinceDays)
	}
	if len(got.Weights) != len(want.Weights) {
		t.Errorf("Weights size = %d, want %d", len(got.Weights), len(want.Weights))
	}
}

func TestLoad_FullOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	yaml := `weights:
  cyclomatic: 2.0
  nesting: 1.5
  length: 0.25
delta_s_max: 0.1
churn_since_days: 30
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Weights["cyclomatic"] != 2.0 || got.Weights["nesting"] != 1.5 || got.Weights["length"] != 0.25 {
		t.Errorf("Weights = %v, want full override", got.Weights)
	}
	if got.Weights["coupling"] != 0.6 {
		t.Errorf("Weights[coupling] = %v, want preserved default 0.6 (YAML did not override)", got.Weights["coupling"])
	}
	if got.DeltaSMax != 0.1 {
		t.Errorf("DeltaSMax = %v, want 0.1", got.DeltaSMax)
	}
	if got.ChurnSinceDays != 30 {
		t.Errorf("ChurnSinceDays = %d, want 30", got.ChurnSinceDays)
	}
}

func TestLoad_PartialOverrideKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	yaml := `weights:
  cyclomatic: 3.0
delta_s_max: 0.2
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Weights["cyclomatic"] != 3.0 {
		t.Errorf("Weights[cyclomatic] = %v, want 3.0", got.Weights["cyclomatic"])
	}
	if got.Weights["nesting"] != 0.8 {
		t.Errorf("Weights[nesting] = %v, want default 0.8", got.Weights["nesting"])
	}
	if got.Weights["length"] != 0.5 {
		t.Errorf("Weights[length] = %v, want default 0.5", got.Weights["length"])
	}
	if got.Weights["coupling"] != 0.6 {
		t.Errorf("Weights[coupling] = %v, want default 0.6", got.Weights["coupling"])
	}
	if got.DeltaSMax != 0.2 {
		t.Errorf("DeltaSMax = %v, want 0.2", got.DeltaSMax)
	}
	if got.ChurnSinceDays != 90 {
		t.Errorf("ChurnSinceDays = %d, want default 90", got.ChurnSinceDays)
	}
}

func TestLoad_EmptyFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Default()
	if got.DeltaSMax != want.DeltaSMax || got.ChurnSinceDays != want.ChurnSinceDays {
		t.Errorf("empty file produced %+v, want defaults", got)
	}
}

func TestLoad_MalformedYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	if err := os.WriteFile(path, []byte("not: : valid: yaml: ::"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

func TestLoad_ScalingClassMaxOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	yaml := `scaling_class_max: O(n)
scaling_bonus_beta: 0.25
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ScalingClassMax != scaling.ClassON {
		t.Errorf("ScalingClassMax = %v, want O(n)", got.ScalingClassMax)
	}
	if got.ScalingBonusBeta != 0.25 {
		t.Errorf("ScalingBonusBeta = %v, want 0.25", got.ScalingBonusBeta)
	}
}

func TestLoad_InvalidScalingClassMaxIsFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	if err := os.WriteFile(path, []byte("scaling_class_max: O(garbage)\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid scaling_class_max, got nil")
	}
}

func TestLoad_PartialKeepsScalingDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	if err := os.WriteFile(path, []byte("delta_s_max: 0.2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ScalingClassMax != scaling.ClassOk {
		t.Errorf("ScalingClassMax = %v, want default O(k)", got.ScalingClassMax)
	}
	if got.ScalingBonusBeta != 0.5 {
		t.Errorf("ScalingBonusBeta = %v, want default 0.5", got.ScalingBonusBeta)
	}
}

func TestLoad_NewMicrostateInWeightsAddedToMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".entrolint.yaml")
	yaml := `weights:
  coupling: 1.2
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Weights["coupling"] != 1.2 {
		t.Errorf("Weights[coupling] = %v, want 1.2", got.Weights["coupling"])
	}
	if got.Weights["cyclomatic"] != 1.0 {
		t.Errorf("Weights[cyclomatic] = %v, want preserved default 1.0", got.Weights["cyclomatic"])
	}
}
