package report

import (
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/pipeline"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
	"github.com/pavlov061356/entrolint/internal/scaling"
)

func sampleResult() pipeline.CheckResult {
	return pipeline.CheckResult{
		Base: "abc123",
		Head: "def456",
		Delta: thermo.Delta{
			Total:        1.25,
			Density:      0.025,
			LinesChanged: 50,
			Files: []thermo.FileDelta{
				thermo.MakeFileDelta("a.go", thermo.DeltaModified, 0.5, 1.0),
				thermo.MakeFileDelta("b.go", thermo.DeltaAdded, 0, 0.75),
			},
		},
		Scaling: scaling.Result{
			Class: scaling.ClassO1,
			Files: []scaling.FileResult{
				{Path: "a.go", Class: scaling.ClassO1},
				{Path: "b.go", Class: scaling.ClassO1},
			},
		},
		Skipped: []gitx.SkippedPath{{Path: "vendor/foo.go", Reason: gitx.SkipBinary}},
	}
}

func TestCheckMarkdown_PASS(t *testing.T) {
	res := sampleResult()
	cfg := config.Default()
	out := CheckMarkdown(res, cfg, res.Verdict(cfg))

	if !strings.HasPrefix(out, CommentMarker+"\n") {
		t.Errorf("output must start with the sticky-comment marker, got:\n%s", out)
	}
	for _, want := range []string{
		"## ✅ entrolint — PASS",
		"**ΔS_total** `+1.2500`",
		"**ΔS_density** `0.0250` (threshold `0.0500`)",
		"**scaling class** `O(1)`",
		"50 lines, 2 files",
		"### Changed files",
		"| modified | `a.go` | 0.500 | 1.000 | +0.500 |",
		"| added | `b.go` | 0.000 | 0.750 | +0.750 |",
		"### Top hotspots (changed files)",
		"1. `a.go` — S 1.000", // a.go (S_head 1.0) ranks above b.go (0.75)
		"2. `b.go` — S 0.750",
		"1 path(s) skipped: `vendor/foo.go` (binary).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\nfull:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Gate failed") {
		t.Errorf("PASS output must not contain a failure block:\n%s", out)
	}
}

func TestCheckMarkdown_FailListsReasons(t *testing.T) {
	res := sampleResult()
	res.Delta.Density = 0.10            // trips ΔS gate
	res.Scaling.Class = scaling.ClassON // trips scaling gate
	cfg := config.Default()
	out := CheckMarkdown(res, cfg, res.Verdict(cfg))

	if !strings.Contains(out, "## ❌ entrolint — FAIL") {
		t.Errorf("expected FAIL header:\n%s", out)
	}
	for _, want := range []string{"> ❌ Gate failed:", "> - ΔS_density", "> - scaling_class"} {
		if !strings.Contains(out, want) {
			t.Errorf("fail block missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestCheckMarkdown_RendersNonO1ScalingSignals(t *testing.T) {
	res := sampleResult()
	res.Scaling = scaling.Result{
		Class: scaling.ClassOk,
		Files: []scaling.FileResult{{
			Path:  "internal/foo/iface.go",
			Class: scaling.ClassOk,
			Hits: []scaling.Hit{{
				Detector: "implementor_scan",
				Class:    scaling.ClassOk,
				Size:     7,
				Path:     "internal/foo/iface.go",
				Evidence: "7 implementors touched",
			}},
		}},
	}
	cfg := config.Default()
	out := CheckMarkdown(res, cfg, res.Verdict(cfg))
	for _, want := range []string{
		"**Scaling signals**",
		"`implementor_scan` **O(k)** in `internal/foo/iface.go` (size 7) — 7 implementors touched",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scaling signals missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestCheckMarkdown_EmptyDiff(t *testing.T) {
	res := pipeline.CheckResult{Scaling: scaling.Result{Class: scaling.ClassO1}}
	cfg := config.Default()
	out := CheckMarkdown(res, cfg, res.Verdict(cfg))
	if !strings.Contains(out, "_No analyzable Go files changed._") {
		t.Errorf("empty diff must say so:\n%s", out)
	}
	if strings.Contains(out, "### Changed files") || strings.Contains(out, "Top hotspots") {
		t.Errorf("empty diff must omit table/hotspots:\n%s", out)
	}
}

func TestCheckMarkdown_Deterministic(t *testing.T) {
	res := sampleResult()
	cfg := config.Default()
	first := CheckMarkdown(res, cfg, res.Verdict(cfg))
	second := CheckMarkdown(res, cfg, res.Verdict(cfg))
	if first != second {
		t.Error("CheckMarkdown is not deterministic for identical input")
	}
}
