package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newFmtCmd(name string) *cobra.Command {
	c := &cobra.Command{Use: name}
	c.Flags().String("format", "table", "")
	c.Flags().Bool("json", false, "")
	return c
}

func TestResolveOutputFormat_Default(t *testing.T) {
	got, err := resolveOutputFormat(newFmtCmd("scan"), "table", false, scanFormats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "table" {
		t.Errorf("got %q, want table", got)
	}
}

func TestResolveOutputFormat_JSONAliasIsDeprecated(t *testing.T) {
	c := newFmtCmd("check")
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	got, err := resolveOutputFormat(c, "table", true, checkFormats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "json" {
		t.Errorf("got %q, want json", got)
	}
	if !strings.Contains(errBuf.String(), "deprecated") {
		t.Errorf("expected a deprecation notice on stderr, got %q", errBuf.String())
	}
}

func TestResolveOutputFormat_JSONAndFormatConflict(t *testing.T) {
	c := newFmtCmd("check")
	if err := c.Flags().Set("format", "markdown"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveOutputFormat(c, "markdown", true, checkFormats); err == nil {
		t.Error("expected an error when both --json and --format are set")
	}
}

func TestResolveOutputFormat_RejectsForeignFormat(t *testing.T) {
	// sarif is scan-only — invalid for check.
	_, err := resolveOutputFormat(newFmtCmd("check"), "sarif", false, checkFormats)
	if err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Errorf("expected an 'allowed' error for sarif on check, got %v", err)
	}
	// markdown is check-only — invalid for scan.
	if _, err := resolveOutputFormat(newFmtCmd("scan"), "markdown", false, scanFormats); err == nil {
		t.Error("expected an error for markdown on scan")
	}
}
