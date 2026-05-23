package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Compute ΔS between base and head and gate the PR",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("check: not implemented")
		return nil
	},
}
