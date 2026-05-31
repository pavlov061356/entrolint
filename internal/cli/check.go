package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
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

// errGateFailed is returned (and wrapped) when ΔS_density exceeds the
// configured threshold. It exists so callers (and tests) can detect a
// gate failure without parsing the error message — the typical CLI
// exit code is still 1.
var errGateFailed = errors.New("entrolint: ΔS gate failed")

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Compute ΔS between base and head and gate the PR",
	Long: `check resolves the diff between --base and --head (triple-dot,
matching what GitHub shows on a PR), scores each Go file at both refs
under the calibrated entropy engine, and fails when the resulting
ΔS_density exceeds delta_s_max in .entrolint.yaml.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
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

	if checkJSON {
		if err := writeCheckJSON(out, res); err != nil {
			return err
		}
	} else if err := writeCheckTable(out, res, cfg.DeltaSMax); err != nil {
		return err
	}

	if res.Delta.Fails(cfg.DeltaSMax) {
		return fmt.Errorf("%w: ΔS_density=%.4f > %.4f", errGateFailed, res.Delta.Density, cfg.DeltaSMax)
	}
	return nil
}

func writeCheckJSON(out io.Writer, res pipeline.CheckResult) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func writeCheckTable(out io.Writer, res pipeline.CheckResult, threshold float64) error {
	verdict := "PASS"
	if res.Delta.Fails(threshold) {
		verdict = "FAIL"
	}
	if _, err := fmt.Fprintf(out,
		"%s  ΔS_total=%.4f  ΔS_density=%.4f  threshold=%.4f  lines_changed=%d  files=%d\n",
		verdict, res.Delta.Total, res.Delta.Density, threshold, res.Delta.LinesChanged, len(res.Delta.Files),
	); err != nil {
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

func init() {
	checkCmd.Flags().StringVar(&checkBase, "base", "dev", "base git ref (branch, tag, SHA)")
	checkCmd.Flags().StringVar(&checkHead, "head", "HEAD", "head git ref")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "machine-readable JSON output")
	checkCmd.Flags().StringVar(&checkConfigPath, "config", "", "path to .entrolint.yaml (default: <root>/.entrolint.yaml)")
	checkCmd.Flags().BoolVar(&checkRecalibrate, "recalibrate", false, "ignore cache and refit calibration")
	checkCmd.Flags().StringVar(&checkRoot, "root", ".", "working tree root used for calibration")
}
