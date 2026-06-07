package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// resolveOutputFormat reconciles the new --format flag with the
// deprecated --json bool and validates the result against the command's
// allowed set. --json is a hard alias for --format=json: setting both
// is an error (ambiguous intent), and using --json alone emits a
// one-line deprecation notice to stderr so machine-readable stdout
// stays clean.
func resolveOutputFormat(cmd *cobra.Command, format string, jsonFlag bool, allowed []string) (string, error) {
	if jsonFlag {
		if cmd.Flags().Changed("format") {
			return "", fmt.Errorf("--json and --format conflict; use --format=json")
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "entrolint: --json is deprecated; use --format=json")
		format = "json"
	}
	// Validate uniformly — the --json alias is no exception, so a command
	// whose allowed set omits "json" would still reject it (honoring the
	// contract this function documents).
	for _, a := range allowed {
		if format == a {
			return format, nil
		}
	}
	return "", fmt.Errorf("invalid --format %q for %s (allowed: %s)",
		format, cmd.Name(), strings.Join(allowed, ", "))
}
