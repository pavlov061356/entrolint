// Package config loads entrolint's per-repo configuration from
// `.entrolint.yaml`. Defaults are baked into the binary; the file is
// optional and only needs to specify fields the user wants to
// override. Missing files yield defaults silently; malformed files
// surface as errors.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/pavlov061356/entrolint/internal/scaling"
)

// Config is the resolved configuration for one entrolint run.
type Config struct {
	// Weights maps microstate name to its multiplier in S.
	Weights map[string]float64 `yaml:"weights"`

	// DeltaSMax is the gate threshold for `entrolint check` —
	// PRs whose ΔS_density exceeds this fail.
	DeltaSMax float64 `yaml:"delta_s_max"`

	// ChurnSinceDays is the window passed to the analyzer for churn.
	ChurnSinceDays int `yaml:"churn_since_days"`

	// ScalingClassMax is the inclusive ceiling on the scaling class
	// the check gate tolerates. PRs whose aggregated class exceeds
	// this fail. See docs/scaling.md#gate.
	ScalingClassMax scaling.Class `yaml:"-"`

	// ScalingBonusBeta is the global coefficient on the downgrade
	// reward formula `-β · log2(size_before / size_after)`. Lower β
	// makes refactors less rewarded.
	ScalingBonusBeta float64 `yaml:"scaling_bonus_beta"`
}

// Default returns the v0.3 baked-in configuration.
func Default() Config {
	return Config{
		Weights: map[string]float64{
			"cyclomatic":  1.0,
			"nesting":     0.8,
			"coupling":    0.6,
			"length":      0.5,
			"duplication": 0.7,
		},
		DeltaSMax:        0.05,
		ChurnSinceDays:   90,
		ScalingClassMax:  scaling.ClassOk,
		ScalingBonusBeta: 0.5,
	}
}

// Load reads `path` and overlays its contents on the defaults. A
// missing file is not an error — it just yields defaults. Per-field
// overrides merge with the default weights map (unset keys keep their
// default values). An invalid `scaling_class_max` string surfaces as
// a fatal error so a typo doesn't silently change gate behavior.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path) // #nosec G304 -- path is a config file the user explicitly passes.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	var raw struct {
		Weights          map[string]float64 `yaml:"weights"`
		DeltaSMax        *float64           `yaml:"delta_s_max"`
		ChurnSinceDays   *int               `yaml:"churn_since_days"`
		ScalingClassMax  *string            `yaml:"scaling_class_max"`
		ScalingBonusBeta *float64           `yaml:"scaling_bonus_beta"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, err
	}
	for k, v := range raw.Weights {
		cfg.Weights[k] = v
	}
	if raw.DeltaSMax != nil {
		cfg.DeltaSMax = *raw.DeltaSMax
	}
	if raw.ChurnSinceDays != nil {
		cfg.ChurnSinceDays = *raw.ChurnSinceDays
	}
	if raw.ScalingClassMax != nil {
		c, err := scaling.ParseClass(*raw.ScalingClassMax)
		if err != nil {
			return cfg, fmt.Errorf("config: %w", err)
		}
		cfg.ScalingClassMax = c
	}
	if raw.ScalingBonusBeta != nil {
		cfg.ScalingBonusBeta = *raw.ScalingBonusBeta
	}
	return cfg, nil
}
