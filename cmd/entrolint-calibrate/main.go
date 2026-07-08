package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/calibration"
	"github.com/pavlov061356/entrolint/internal/engine/config"
)

type candidateFlags []string

func (f *candidateFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *candidateFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("candidate must not be empty")
	}
	*f = append(*f, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	var candidates candidateFlags
	var format string
	var includeDefault bool

	fs := flag.NewFlagSet("entrolint-calibrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&candidates, "candidate", "candidate config as name=path; may be repeated")
	fs.StringVar(&format, "format", "table", "output format: table or json")
	fs.BoolVar(&includeDefault, "include-default", true, "include the compiled default weights as a baseline candidate")
	if err := fs.Parse(args); err != nil {
		return err
	}

	roots := fs.Args()
	if len(roots) == 0 {
		return errors.New("usage: entrolint-calibrate [--candidate name=config.yaml] [--format table|json] ROOT")
	}
	loaded, err := loadCandidates(candidates, includeDefault)
	if err != nil {
		return err
	}
	report, err := calibration.Run(calibration.Options{
		Roots:      roots,
		Candidates: loaded,
	})
	if err != nil {
		return err
	}

	switch format {
	case "table":
		renderTable(stdout, report)
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
	return nil
}

func loadCandidates(specs []string, includeDefault bool) ([]calibration.Candidate, error) {
	out := make([]calibration.Candidate, 0, len(specs)+1)
	seen := make(map[string]bool, len(specs)+1)
	if includeDefault {
		out = append(out, calibration.Candidate{Name: "default", Config: config.Default()})
		seen["default"] = true
	}
	for _, spec := range specs {
		name, path, ok := strings.Cut(spec, "=")
		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("candidate must be name=path, got %q", spec)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate candidate name %q", name)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("candidate %q config %q: %w", name, path, err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			return nil, fmt.Errorf("load candidate %q from %q: %w", name, path, err)
		}
		out = append(out, calibration.Candidate{Name: name, Config: cfg})
		seen[name] = true
	}
	return out, nil
}

func renderTable(w io.Writer, report calibration.Report) {
	order := calibration.ContributionOrder()
	fmt.Fprintln(w, "# entrolint calibration report")
	fmt.Fprintln(w)
	for _, candidate := range report.Candidates {
		fmt.Fprintf(w, "## Candidate: %s\n\n", candidate.Name)
		fmt.Fprintln(w, "| scope | files | total S | median S | p90 S | dominant | "+strings.Join(order, " | ")+" |")
		fmt.Fprintln(w, "| --- | ---: | ---: | ---: | ---: | --- | "+strings.Repeat("---: | ", len(order)))
		renderSummaryRow(w, "aggregate", candidate.Aggregate, order)
		for _, repo := range candidate.Repos {
			renderSummaryRow(w, repo.Root, repo.Summary, order)
		}
		fmt.Fprintln(w)
	}
}

func renderSummaryRow(w io.Writer, label string, summary calibration.Summary, order []string) {
	fmt.Fprintf(
		w,
		"| %s | %d | %.3f | %.3f | %.3f | %s |",
		label,
		summary.Files,
		summary.TotalS,
		summary.MedianS,
		summary.P90S,
		renderDominant(summary),
	)
	for _, name := range order {
		fmt.Fprintf(w, " %.1f%% |", contributionShare(summary, name)*100)
	}
	fmt.Fprintln(w)
}

func contributionShare(summary calibration.Summary, name string) float64 {
	for _, c := range summary.Contributions {
		if c.Name == name {
			return c.Share
		}
	}
	return 0
}

func renderDominant(summary calibration.Summary) string {
	if len(summary.Dominants) == 0 {
		return "n/a"
	}
	top := summary.Dominants[0]
	return fmt.Sprintf("%s %.1f%%", top.Name, top.Share*100)
}
