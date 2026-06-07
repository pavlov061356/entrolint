package cli

import (
	"fmt"
	"path/filepath"

	"github.com/pavlov061356/entrolint/internal/engine/cache"
	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/report"
	"github.com/pavlov061356/entrolint/internal/version"
	"github.com/spf13/cobra"
)

// scanFormats are the output formats `scan` accepts. Markdown is a
// check-only format (PR-comment shape) — it is intentionally absent
// here.
var scanFormats = []string{"table", "json", "sarif"}

var (
	scanTop         int
	scanFormat      string
	scanJSON        bool
	scanRecalibrate bool
	scanConfigPath  string
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan a codebase and rank files by temperature T",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := "."
		if len(args) == 1 {
			root = args[0]
		}
		return runScan(cmd, root)
	},
}

func runScan(cmd *cobra.Command, root string) error {
	out := cmd.OutOrStdout()
	format, err := resolveOutputFormat(cmd, scanFormat, scanJSON, scanFormats)
	if err != nil {
		return err
	}

	cfgPath := scanConfigPath
	if cfgPath == "" {
		cfgPath = filepath.Join(root, ".entrolint.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	result, err := pipeline.Scan(pipeline.ScanOptions{
		Root:        root,
		Config:      cfg,
		CachePath:   filepath.Join(root, cache.DefaultPath),
		Recalibrate: scanRecalibrate,
	})
	if err != nil {
		return err
	}

	files := result.Files
	if scanTop > 0 && scanTop < len(files) {
		files = files[:scanTop]
	}

	payload, err := renderScan(format, files)
	if err != nil {
		return err
	}
	_, err = out.Write(payload)
	return err
}

// renderScan formats scan scores for the chosen output format. All
// rendering lives in internal/report; this is just the dispatch.
func renderScan(format string, files []pipeline.FileScore) ([]byte, error) {
	switch format {
	case "json":
		return report.ScanJSON(files)
	case "sarif":
		return report.ScanSARIF(files, report.DefaultSARIFOptions(version.Version))
	default:
		return []byte(report.ScanTable(files)), nil
	}
}

func init() {
	scanCmd.Flags().IntVar(&scanTop, "top", 0, "show only the N hottest files (0 = all)")
	scanCmd.Flags().StringVar(&scanFormat, "format", "table", "output format: table|json|sarif")
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "machine-readable JSON output (deprecated: use --format=json)")
	scanCmd.Flags().BoolVar(&scanRecalibrate, "recalibrate", false, "ignore cache and refit calibration")
	scanCmd.Flags().StringVar(&scanConfigPath, "config", "", "path to .entrolint.yaml (default: <root>/.entrolint.yaml)")
}
