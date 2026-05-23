package cli

import (
	"fmt"

	"github.com/pavlov061356/entrolint/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build version info",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("entrolint %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		return nil
	},
}
