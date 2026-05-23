package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan a codebase and rank files by temperature T",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("scan: not implemented")
		return nil
	},
}
