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
	"github.com/spf13/cobra"
)

var historyFormats = []string{"table", "json"}

var (
	historyLimit       int
	historyFirstParent bool
	historyFormat      string
	historyJSON        bool
	historyConfigPath  string
	historyRecalibrate bool
	historyRoot        string
	historyHTMLDir     string
)

var historyCmd = &cobra.Command{
	Use:   "history [ref]",
	Short: "Compute total entropy S over recent git history",
	Long: `history samples recent commits without checking them out, scores each
tree in one calibration frame, and emits the S(t) data behind the phase portrait.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref := "HEAD"
		if len(args) == 1 {
			ref = args[0]
		}
		return runHistory(cmd, historyRoot, ref)
	},
}

func runHistory(cmd *cobra.Command, root, ref string) error {
	if historyLimit <= 0 {
		return fmt.Errorf("--limit must be positive")
	}
	out := cmd.OutOrStdout()
	format, err := resolveOutputFormat(cmd, historyFormat, historyJSON, historyFormats)
	if err != nil {
		return err
	}

	cfgPath := historyConfigPath
	if cfgPath == "" {
		cfgPath = filepath.Join(root, ".entrolint.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	res, err := pipeline.History(pipeline.HistoryOptions{
		Root:        root,
		Ref:         ref,
		Limit:       historyLimit,
		FirstParent: historyFirstParent,
		ScanOptions: pipeline.ScanOptions{
			Config:      cfg,
			CachePath:   filepath.Join(root, cache.DefaultPath),
			Recalibrate: historyRecalibrate,
		},
	})
	if err != nil {
		return err
	}

	if historyHTMLDir != "" {
		return writeHistoryHTMLReport(out, historyHTMLDir, res)
	}

	payload, err := renderHistory(format, res)
	if err != nil {
		return err
	}
	_, err = out.Write(payload)
	return err
}

func renderHistory(format string, res pipeline.HistoryResult) ([]byte, error) {
	switch format {
	case "json":
		return report.HistoryJSON(res)
	default:
		return []byte(report.HistoryTable(res)), nil
	}
}

func writeHistoryHTMLReport(out io.Writer, dir string, res pipeline.HistoryResult) error {
	html, err := report.HistoryHTML(res)
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
	fmt.Fprintf(out, "wrote phase portrait to %s\n", path)
	return nil
}

func init() {
	historyCmd.Flags().IntVar(&historyLimit, "limit", 30, "number of recent commits to sample")
	historyCmd.Flags().BoolVar(&historyFirstParent, "first-parent", true, "follow only the first-parent history")
	historyCmd.Flags().StringVar(&historyFormat, "format", "table", "output format: table|json")
	historyCmd.Flags().BoolVar(&historyJSON, "json", false, "machine-readable JSON output (deprecated: use --format=json)")
	historyCmd.Flags().StringVar(&historyConfigPath, "config", "", "path to .entrolint.yaml (default: <root>/.entrolint.yaml)")
	historyCmd.Flags().BoolVar(&historyRecalibrate, "recalibrate", false, "ignore cache and refit calibration")
	historyCmd.Flags().StringVar(&historyRoot, "root", ".", "working tree root used for calibration")
	historyCmd.Flags().StringVar(&historyHTMLDir, "html", "", "write a self-contained HTML phase portrait to this directory (as index.html)")
}
