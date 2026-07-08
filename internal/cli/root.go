// Package cli wires the entrolint command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "entrolint",
	Short: "Measure code entropy and gate PRs that raise it",
	Long: `entrolint measures the maintenance complexity of a Go codebase as
"entropy" S, ranks hotspots by temperature T, and gates pull requests by
ΔS density. See https://github.com/pavlov061356/entrolint for the model.`,
}

// Execute runs the root command. Called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(scanCmd, checkCmd, historyCmd, versionCmd)
}
