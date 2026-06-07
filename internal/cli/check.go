package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/report"
	"github.com/spf13/cobra"
)

// checkFormats are the output formats `check` accepts. SARIF is a
// scan-only format (full-tree code-scanning log) — it is intentionally
// absent here.
var checkFormats = []string{"table", "json", "markdown"}

var (
	checkBase        string
	checkHead        string
	checkFormat      string
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
		return runCheck(cmd, checkRoot)
	},
}

func runCheck(cmd *cobra.Command, root string) error {
	out := cmd.OutOrStdout()
	format, err := resolveOutputFormat(cmd, checkFormat, checkJSON, checkFormats)
	if err != nil {
		return err
	}

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

	payload, err := renderCheck(format, res, cfg, verdict)
	if err != nil {
		return err
	}
	if _, err := out.Write(payload); err != nil {
		return err
	}

	if verdict.Failed {
		return fmt.Errorf("%w: %s", errGateFailed, strings.Join(verdict.Reasons, "; "))
	}
	return nil
}

// renderCheck formats the check result for the chosen output format.
// All rendering lives in internal/report; this is just the dispatch.
func renderCheck(format string, res pipeline.CheckResult, cfg config.Config, v pipeline.Verdict) ([]byte, error) {
	switch format {
	case "json":
		return report.CheckJSON(res, cfg, v)
	case "markdown":
		return []byte(report.CheckMarkdown(res, cfg, v)), nil
	default:
		return []byte(report.CheckTable(res, cfg, v)), nil
	}
}

func init() {
	checkCmd.Flags().StringVar(&checkBase, "base", "dev", "base git ref (branch, tag, SHA)")
	checkCmd.Flags().StringVar(&checkHead, "head", "HEAD", "head git ref")
	checkCmd.Flags().StringVar(&checkFormat, "format", "table", "output format: table|json|markdown")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "machine-readable JSON output (deprecated: use --format=json)")
	checkCmd.Flags().StringVar(&checkConfigPath, "config", "", "path to .entrolint.yaml (default: <root>/.entrolint.yaml)")
	checkCmd.Flags().BoolVar(&checkRecalibrate, "recalibrate", false, "ignore cache and refit calibration")
	checkCmd.Flags().StringVar(&checkRoot, "root", ".", "working tree root used for calibration")
}
