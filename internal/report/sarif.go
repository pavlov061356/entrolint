package report

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"

	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
)

// SARIF constants — the single rule entrolint emits and the URIs GitHub
// Code Scanning surfaces in its UI.
const (
	sarifSchema   = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion  = "2.1.0"
	highEntropyID = "entrolint/high-entropy"
	repoURI       = "https://github.com/pavlov061356/entrolint"
	formulaDocURI = "https://github.com/pavlov061356/entrolint/blob/master/docs/formula.md"
	levelNote     = "note"
	levelWarning  = "warning"
	levelError    = "error"
)

// SARIFOptions tunes the temperature bands that map a file's T to a
// Code Scanning severity. A file is reported only when its T reaches
// NoteFloor; warning/error escalate at WarnAt/ErrorAt. The default
// floor/warn bands (1.0 / 1.5) keep the floor near the calibrated median so
// the log reads as a hotspot backlog, not every file; ErrorAt is disabled by
// default until v1.0 (see DefaultSARIFOptions). Revisit once real Code
// Scanning noise is observed.
type SARIFOptions struct {
	ToolVersion string
	NoteFloor   float64
	WarnAt      float64
	ErrorAt     float64
}

// DefaultSARIFOptions returns the documented default bands for the given
// tool version. Until the formula stabilises at v1.0 the severity is capped
// at `warning`: an admittedly-unstable metric must not raise `error`, which
// GitHub Code Scanning treats as a BLOCKING check failure — that would
// contradict entrolint's advisory-until-v1.0 stance (the dogfood Action runs
// with fail-on-gate:false yet a single error-level SARIF result still reds the
// check). So error is disabled by default (ErrorAt = +Inf, never reached); a
// consumer who wants a hard error band sets ErrorAt explicitly, and v1.0 will
// restore a finite default.
func DefaultSARIFOptions(toolVersion string) SARIFOptions {
	return SARIFOptions{ToolVersion: toolVersion, NoteFloor: 1.0, WarnAt: 1.5, ErrorAt: math.Inf(1)}
}

// normalize fills in default bands when the caller left them zero (the
// zero-value struct case). A non-positive band is treated as unset.
func (o SARIFOptions) normalize() SARIFOptions {
	d := DefaultSARIFOptions(o.ToolVersion)
	if o.NoteFloor > 0 {
		d.NoteFloor = o.NoteFloor
	}
	if o.WarnAt > 0 {
		d.WarnAt = o.WarnAt
	}
	if o.ErrorAt > 0 {
		d.ErrorAt = o.ErrorAt
	}
	return d
}

// level maps a temperature to a SARIF severity, or "" when T is below
// the reporting floor (no result emitted).
func (o SARIFOptions) level(t float64) string {
	switch {
	case t >= o.ErrorAt:
		return levelError
	case t >= o.WarnAt:
		return levelWarning
	case t >= o.NoteFloor:
		return levelNote
	default:
		return ""
	}
}

// ScanSARIF renders scan results as a SARIF 2.1.0 log for GitHub Code
// Scanning. One result per file whose temperature clears the note
// floor, located at the file (line 1 — entrolint scores per file, not
// per line). Input order is preserved (scan pre-sorts by T desc) for
// deterministic output. The result ends with a trailing newline.
func ScanSARIF(files []pipeline.FileScore, opts SARIFOptions) ([]byte, error) {
	opts = opts.normalize()

	results := make([]sarifResult, 0, len(files))
	for _, f := range files {
		lvl := opts.level(f.T)
		if lvl == "" {
			continue
		}
		uri := filepath.ToSlash(f.Path)
		results = append(results, sarifResult{
			RuleID: highEntropyID,
			Level:  lvl,
			Message: sarifText{Text: fmt.Sprintf(
				"Code entropy S=%.2f, temperature T=%.2f (dominant microstate: %s).",
				f.S, f.T, f.Dominant,
			)},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: uri},
					Region:           sarifRegion{StartLine: 1},
				},
			}},
			PartialFingerprints: map[string]string{"entrolintPath": uri},
		})
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "entrolint",
				InformationURI: repoURI,
				Version:        opts.ToolVersion,
				Rules: []sarifRule{{
					ID:               highEntropyID,
					Name:             "HighEntropy",
					ShortDescription: sarifText{Text: "High code entropy"},
					FullDescription: sarifText{Text: "The file's entropy score S (and temperature T) " +
						"is high relative to the calibrated corpus — a maintenance hotspot."},
					HelpURI:       formulaDocURI,
					DefaultConfig: sarifRuleConfig{Level: levelNote},
				}},
			}},
			Results: results,
		}},
	}

	out, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sarif: %w", err)
	}
	return append(out, '\n'), nil
}

// SARIF 2.1.0 wire types — a minimal hand-rolled subset (no external
// dependency). Field order and json tags follow the spec so GitHub's
// upload-sarif validator accepts the log.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	ShortDescription sarifText       `json:"shortDescription"`
	FullDescription  sarifText       `json:"fullDescription"`
	HelpURI          string          `json:"helpUri"`
	DefaultConfig    sarifRuleConfig `json:"defaultConfiguration"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}
