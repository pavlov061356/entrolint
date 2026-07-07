# Example diagnostic: entrolint on entrolint

This is a filled example of the
[diagnostic report template](../diagnostic-report-template.md), using
`entrolint` against its own repository.

The purpose is to demonstrate the report shape, not to claim that these findings
are urgent refactoring work. In this run, the hottest files are mostly tests, so
the interpretation is intentionally conservative.

## Report metadata

- Repository: `github.com/pavlov061356/entrolint`
- Commit / ref: `v0.6.0` code snapshot; documentation-only changes were present
  during this sample run and do not affect Go scan results
- Date: 2026-07-07
- entrolint version: `dev` build from the current checkout
- Config path: default config (no `.entrolint.yaml` override)
- Scope: full Go tree scanned from the repository root
- Reviewer: entrolint self-diagnostic example

## Executive summary

- The self-scan analyzed 78 Go files and reported total entropy `S = 84.55`.
- The hottest file was `internal/engine/pipeline/check_test.go`
  (`S = 2.62`, `T = 5.16`, dominant `cyclomatic`).
- The most common dominant microstate was `cyclomatic`: it dominated 35 of 78
  analyzed files.
- The hottest production file was
  `internal/scaling/detectors/statemultiplier/state_multiplier.go`
  (`S = 2.44`, `T = 4.14`, dominant `cyclomatic`).
- The main caveat: test files dominate the top of the heat map, so the first
  interpretation should be "complex verification surface", not "bad code".

## Commands run

```bash
go build -o /tmp/entrolint-proof ./cmd/entrolint
/tmp/entrolint-proof scan --top 10
/tmp/entrolint-proof scan --format json
/tmp/entrolint-proof scan --html /tmp/entrolint-self-heatmap
```

The generated heat map was written to:

```text
/tmp/entrolint-self-heatmap/index.html
```

The HTML artifact is not committed; it is a local proof output that can be
regenerated from the commands above.

## Repository-level snapshot

- Total analyzed Go files: 78
- Total entropy `S`: 84.55
- Hottest package area: `internal/engine/pipeline`
- Hottest file: `internal/engine/pipeline/check_test.go`
- Dominant repository-level theme: branch-heavy tests and detector logic
- Notable skipped paths: none observed in this scan output

Dominant microstate counts:

| Dominant | Files |
| -------- | ----: |
| cyclomatic | 35 |
| nesting | 18 |
| coupling | 15 |
| cross_duplication | 3 |
| duplication | 1 |
| none / zero score | 6 |

Interpretation:

The repository's heat is concentrated in code that exercises many behavioral
branches: pipeline tests, scaling detector tests, and detector implementations.
That is consistent with a static-analysis tool whose risk is mostly in edge
cases, git-diff semantics, AST walking, and type-aware heuristics.

## Heat map observations

### Pattern 1: large and hot

- File/package: `internal/engine/pipeline/check_test.go`
- Why it stands out: highest temperature in the scan (`T = 5.16`)
- Dominant microstate: `cyclomatic`
- Team context to verify: the file likely carries many scenario tests for the
  PR gate pipeline; density may be acceptable if tests remain readable.
- Possible next step: inspect whether repeated fake-runner setup or fixture
  assembly can be factored without hiding important cases.

### Pattern 2: production detector hotspot

- File/package:
  `internal/scaling/detectors/statemultiplier/state_multiplier.go`
- Why it stands out: hottest non-test file (`S = 2.44`, `T = 4.14`)
- Dominant microstate: `cyclomatic`
- Team context to verify: state-multiplier logic handles AST diffs,
  public-signature detection, type loading, and external call-site counting.
- Possible next step: split the detector into smaller phases only if tests can
  keep the existing edge-case coverage explicit.

### Pattern 3: central pipeline code

- File/package: `internal/engine/pipeline/check.go`
- Why it stands out: high temperature for production orchestration code
  (`S = 1.51`, `T = 3.44`)
- Dominant microstate: `cyclomatic`
- Team context to verify: this file coordinates git refs, blob fetching,
  scoring, cross-file context, scaling analysis, and verdict data.
- Possible next step: keep future changes narrow; use the existing tests as a
  safety rail before extracting more orchestration helpers.

## Top hotspots

| Rank | Path | S | T | Dominant | Interpretation | Suggested next step |
| ---- | ---- | ---: | ---: | -------- | -------------- | ------------------- |
| 1 | `internal/engine/pipeline/check_test.go` | 2.62 | 5.16 | cyclomatic | Branch-heavy pipeline test surface | Look for repeated fixture setup, not fewer cases |
| 2 | `internal/scaling/detectors/statemultiplier/state_multiplier.go` | 2.44 | 4.14 | cyclomatic | Hottest production detector | Inspect phase boundaries inside the detector |
| 3 | `internal/scaling/detectors/switchsymmetry/switch_symmetry_test.go` | 2.53 | 3.92 | cyclomatic | Broad detector test matrix | Preserve coverage; consider fixture helpers |
| 4 | `internal/engine/pipeline/scan_test.go` | 2.12 | 3.82 | cyclomatic | Scan pipeline scenarios | Check for reusable calibration/test tree setup |
| 5 | `internal/engine/pipeline/cross_duplication_test.go` | 2.07 | 3.73 | cyclomatic | Cross-file clone edge cases | Keep explicit cases; refactor only repeated scaffolding |
| 6 | `internal/scaling/detectors/statemultiplier/state_multiplier_test.go` | 2.17 | 3.67 | cyclomatic | Detector behavior matrix | Group scenarios by failure mode |
| 7 | `internal/engine/pipeline/check.go` | 1.51 | 3.44 | cyclomatic | Central check orchestration | Keep future edits small and well-tested |
| 8 | `internal/scaling/detectors/implementorscan/implementor_scan_test.go` | 2.55 | 3.43 | cyclomatic | Type-aware detector tests | Consider fixture module reuse |
| 9 | `internal/engine/config/config_test.go` | 1.87 | 3.38 | cyclomatic | Config merge/error cases | Low priority; tests are likely acceptable |
| 10 | `internal/engine/analyzer/golang/analyzer_test.go` | 1.98 | 3.36 | cyclomatic | Analyzer path and churn scenarios | Low priority unless adding analyzer behavior |

## Microstate breakdown

### Cyclomatic complexity

- Where it appears: most top hotspots, especially pipeline tests and scaling
  detectors.
- Why it matters: branch-heavy files are harder to modify safely, even when they
  are tests.
- Example to inspect: `state_multiplier.go` for production logic, and
  `check_test.go` for test scaffolding.
- Recommendation: do not reduce scenarios for the metric. Prefer extracting
  helpers only when they make the test intent clearer.

### Nesting

- Where it appears: secondary theme in helper-heavy files and some report code.
- Why it matters: nested control flow makes local reasoning slower.
- Example to inspect: files where `nesting` dominates but total `T` is moderate.
- Recommendation: low priority for this pass.

### Coupling

- Where it appears: CLI/report/pipeline entry points with many imports.
- Why it matters: high import count can signal orchestration breadth.
- Example to inspect: `internal/engine/pipeline/scan.go` and `internal/cli/scan.go`.
- Recommendation: acceptable for orchestration layers; monitor during feature
  additions.

### Duplication and cross-file duplication

- Where it appears: mostly in tests and fixtures in this scan.
- Why it matters: duplicated test scaffolding can make broad behavior changes
  expensive.
- Example to inspect: pipeline and detector test files.
- Recommendation: extract shared fixtures only where duplication obscures the
  scenario being tested.

## PR gate view

This example is a scan-only repository diagnostic. No PR comparison was run.

- Base: n/a
- Head: n/a
- Verdict: n/a
- `Delta S_total`: n/a
- `Delta S_density`: n/a
- Threshold: n/a
- Scaling class: n/a
- Scaling class max: n/a
- Lines changed: n/a
- Files changed: n/a

## Findings

### Finding 1: tests dominate the heat map

- Evidence: 8 of the top 10 hotspots are test files.
- Why it matters: test complexity is still maintenance cost, but it has a
  different interpretation than production complexity.
- Confidence: high.
- Suggested action: inspect repeated fixture setup and fake-runner construction
  before changing production code.
- Expected benefit: easier test additions without weakening coverage.
- Risk / caveat: over-abstracted tests can become less readable than explicit
  scenario tests.

### Finding 2: `state_multiplier.go` is the clearest production hotspot

- Evidence: hottest non-test file, `S = 2.44`, `T = 4.14`, dominant
  `cyclomatic`.
- Why it matters: scaling detectors are central to entrolint's predictive layer.
- Confidence: medium; the score should be checked against actual recent edit
  pain.
- Suggested action: review the detector's internal phases before the next
  feature change in that area.
- Expected benefit: lower risk when extending public-signature or enum handling.
- Risk / caveat: premature extraction could make AST/type logic harder to follow.

### Finding 3: `check.go` should stay guarded

- Evidence: `internal/engine/pipeline/check.go` is a top production hotspot with
  high temperature.
- Why it matters: this file coordinates the PR gate path and can create broad
  behavioral regressions.
- Confidence: high.
- Suggested action: keep changes small, with tests around git refs, blob
  symmetry, skipped files, scaling, and cross-file context.
- Expected benefit: safer evolution of the core `check` workflow.
- Risk / caveat: orchestration files naturally collect branches; not every branch
  is a refactoring smell.

## Recommended next steps

1. Use this example as the first proof that the diagnostic template is fillable.
2. Repeat the same report on one public Go repository before writing a
   flagship-style article.
3. When touching `state_multiplier` or `check`, use the heat map as a reminder to
   keep the change scoped and heavily tested.

## Follow-up checks

- Re-run `entrolint scan --html` after any production refactor in the highlighted
  areas.
- Use `entrolint check --base <before> --head <after>` for actual refactoring
  branches.
- Confirm that a lower score did not come from hiding meaningful test scenarios.
- Keep the advisory framing: this is maintainability guidance, not a quality
  grade.
