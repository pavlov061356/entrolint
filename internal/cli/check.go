package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/scaling"
	"github.com/spf13/cobra"
)

var (
	checkBase        string
	checkHead        string
	checkJSON        bool
	checkConfigPath  string
	checkRecalibrate bool
	checkRoot        string
)

// errGateFailed is returned (and wrapped) when the check pipeline's
// Verdict trips on any axis (ΔS_density or scaling_class). It exists so
// callers (and tests) can detect a gate failure without parsing the
// error message — the typical CLI exit code is still 1.
var errGateFailed = errors.New("entrolint: gate failed")

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Compute ΔS and scaling class between base and head and gate the PR",
	Long: `check resolves the diff between --base and --head (triple-dot,
matching what GitHub shows on a PR), scores each Go file at both refs
under the calibrated entropy engine, computes the PR's scaling class,
and fails when ΔS_density exceeds delta_s_max OR scaling_class exceeds
scaling_class_max in .entrolint.yaml.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return runCheck(cmd.OutOrStdout(), checkRoot)
	},
}

func runCheck(out io.Writer, root string) error {
	cfgPath := checkConfigPath
	if cfgPath == "" {
		cfgPath = filepath.Join(root, ".entrolint.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	res, err := pipeline.Check(pipeline.CheckOptions{
		Root: root,
		Base: checkBase,
		Head: checkHead,
		ScanOptions: pipeline.ScanOptions{
			Config:      cfg,
			CachePath:   filepath.Join(root, cache.DefaultPath),
			Recalibrate: checkRecalibrate,
		},
	})
	if err != nil {
		return err
	}

	verdict := res.Verdict(cfg)

	if checkJSON {
		if err := writeCheckJSON(out, res, cfg, verdict); err != nil {
			return err
		}
	} else if err := writeCheckTable(out, res, cfg, verdict); err != nil {
		return err
	}

	if verdict.Failed {
		return fmt.Errorf("%w: %s", errGateFailed, strings.Join(verdict.Reasons, "; "))
	}
	return nil
}

// checkJSONReport is the JSON envelope `entrolint check --json` emits.
// Verdict + thresholds live here (not on pipeline.CheckResult) so the
// engine layer stays gate-policy-free — thresholds are a CLI/config
// concern. Downstream tooling can read Verdict + Reasons without
// recomputing them.
type checkJSONReport struct {
	Verdict         string               `json:"verdict"`
	Reasons         []string             `json:"reasons,omitempty"`
	Threshold       float64              `json:"threshold"`
	ScalingClassMax scaling.Class        `json:"scaling_class_max"`
	Result          pipeline.CheckResult `json:"result"`
}

func writeCheckJSON(out io.Writer, res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) error {
	verdict := "pass"
	if v.Failed {
		verdict = "fail"
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(checkJSONReport{
		Verdict:         verdict,
		Reasons:         v.Reasons,
		Threshold:       cfg.DeltaSMax,
		ScalingClassMax: cfg.ScalingClassMax,
		Result:          res,
	})
}

func writeCheckTable(out io.Writer, res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) error {
	label := "PASS"
	if v.Failed {
		label = "FAIL"
	}
	if _, err := fmt.Fprintf(out,
		"%s  ΔS_total=%.4f  ΔS_density=%.4f  threshold=%.4f  scaling_class=%s  lines_changed=%d  files=%d\n",
		label, res.Delta.Total, res.Delta.Density, cfg.DeltaSMax,
		res.Scaling.Class, res.Delta.LinesChanged, len(res.Delta.Files),
	); err != nil {
		return err
	}
	for _, reason := range v.Reasons {
		if _, err := fmt.Fprintf(out, "  reason: %s\n", reason); err != nil {
			return err
		}
	}
	if err := writeScalingHits(out, res.Scaling); err != nil {
		return err
	}
	if len(res.Delta.Files) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "KIND\tPATH\tS_BASE\tS_HEAD\tΔ"); err != nil {
		return err
	}
	for _, f := range res.Delta.Files {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%.3f\t%.3f\t%+.3f\n",
			f.Kind, f.Path, f.SBase, f.SHead, f.Delta,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// writeScalingHits prints one line per non-O(1) detector hit, so the
// reader sees *why* the class is elevated. Skipped for empty results
// (the skeleton-phase case in v0.2.0).
func writeScalingHits(out io.Writer, r scaling.Result) error {
	for _, f := range r.Files {
		for _, h := range f.Hits {
			if h.Class == scaling.ClassO1 {
				continue
			}
			line := fmt.Sprintf("  scaling: %s %s in %s", h.Detector, h.Class, h.Path)
			if h.Size > 0 {
				line += fmt.Sprintf(" (size=%d)", h.Size)
			}
			if h.Evidence != "" {
				line += " — " + h.Evidence
			}
			if _, err := fmt.Fprintln(out, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func init() {
	checkCmd.Flags().StringVar(&checkBase, "base", "dev", "base git ref (branch, tag, SHA)")
	checkCmd.Flags().StringVar(&checkHead, "head", "HEAD", "head git ref")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "machine-readable JSON output")
	checkCmd.Flags().StringVar(&checkConfigPath, "config", "", "path to .entrolint.yaml (default: <root>/.entrolint.yaml)")
	checkCmd.Flags().BoolVar(&checkRecalibrate, "recalibrate", false, "ignore cache and refit calibration")
	checkCmd.Flags().StringVar(&checkRoot, "root", ".", "working tree root used for calibration")
}
