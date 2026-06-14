package cli

import (
	"fmt"
	"io"
	"os"
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
	scanHTMLDir     string
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

	// The HTML heatmap is a whole-repo artifact written to a directory, so it
	// renders every file (not just --top, a terminal-table convenience) and
	// short-circuits the stdout formats.
	if scanHTMLDir != "" {
		return writeHTMLReport(out, scanHTMLDir, result.Files)
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

// writeHTMLReport renders the self-contained heatmap and writes it to
// <dir>/index.html, creating the directory if needed, then prints the path.
func writeHTMLReport(out io.Writer, dir string, files []pipeline.FileScore) error {
	html, err := report.ScanHTML(files)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create html output dir: %w", err)
	}
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, html, 0o644); err != nil { // #nosec G306 -- a report artifact, no secrets
		return fmt.Errorf("write html report: %w", err)
	}
	fmt.Fprintf(out, "wrote heatmap to %s\n", path)
	return nil
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
	scanCmd.Flags().StringVar(&scanHTMLDir, "html", "", "write a self-contained HTML heatmap to this directory (as index.html)")
}
