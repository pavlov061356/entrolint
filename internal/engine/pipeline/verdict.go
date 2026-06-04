package pipeline

import (
	"fmt"

	"github.com/pavlov061356/entrolint/internal/engine/config"
)

// Verdict is the gate's decision and the human-readable reasons behind
// it. Two axes contribute independently — ΔS_density and scaling class
// — and the gate fails if either trips. The reasons slice is ordered
// for stable rendering: ΔS first, scaling second.
type Verdict struct {
	Failed  bool
	Reasons []string
}

// Verdict computes the gate decision from cfg's thresholds. Pure: no
// side effects, safe to call repeatedly.
func (r CheckResult) Verdict(cfg config.Config) Verdict {
	var reasons []string
	if r.Delta.Fails(cfg.DeltaSMax) {
		reasons = append(reasons, fmt.Sprintf("ΔS_density %.4f > %.4f", r.Delta.Density, cfg.DeltaSMax))
	}
	// `Max.Less(Current)` == `Current > Max`. Inclusive ceiling: a PR
	// whose class equals ScalingClassMax passes.
	if cfg.ScalingClassMax.Less(r.Scaling.Class) {
		reasons = append(reasons,
			fmt.Sprintf("scaling_class %s > %s", r.Scaling.Class, cfg.ScalingClassMax))
	}
	return Verdict{Failed: len(reasons) > 0, Reasons: reasons}
}
