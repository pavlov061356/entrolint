# Example diagnostic: Chi

This is a filled diagnostic example for
[`github.com/go-chi/chi`](https://github.com/go-chi/chi), generated from an
`entrolint scan` run.

The purpose is to demonstrate interpretation on a mature public Go repository.
It is not a quality ranking.

## Report metadata

- Repository: `github.com/go-chi/chi`
- Commit / ref: `8b258c7`
- Commit date: 2026-07-05
- Date of diagnostic run: 2026-07-07
- entrolint version: `dev` build from this checkout
- Config path: default config
- Scope: full Go tree scanned from the repository root

## Executive summary

- The scan analyzed 78 Go files and reported total entropy `S = 98.40`.
- The hottest file was `mux_test.go` (`S = 3.74`, `T = 5.03`, dominant
  `cyclomatic`).
- The hottest production file was `tree.go` (`S = 3.56`, `T = 4.79`, dominant
  `cyclomatic`).
- The highest-entropy area was `middleware` (`S = 66.77` across 49 files),
  followed by the repository root (`S = 18.89` across 10 files).
- Interpretation: Chi's profile is middleware-heavy, with a distinct router tree
  hotspot in the core package.

## Commands run

```bash
git clone --depth 1 https://github.com/go-chi/chi.git /tmp/entrolint-proof-repos/chi
cd /tmp/entrolint-proof-repos/chi
/tmp/entrolint-proof scan --top 10
/tmp/entrolint-proof scan --format json
/tmp/entrolint-proof scan --html /tmp/entrolint-proof-reports/chi/heatmap
```

The generated heat map was written to:

```text
/tmp/entrolint-proof-reports/chi/heatmap/index.html
```

## Repository-level snapshot

- Total analyzed Go files: 78
- Total entropy `S`: 98.40
- Hottest package area: `middleware`
- Hottest file: `mux_test.go`
- Hottest production file: `tree.go`
- Dominant repository-level theme: middleware breadth plus router tree behavior

Dominant microstate counts:

| Dominant | Files |
| -------- | ----: |
| cyclomatic | 26 |
| nesting | 19 |
| coupling | 14 |
| cross_duplication | 13 |
| duplication | 3 |
| none / zero score | 3 |

Top package areas by total `S`:

| Area | Files | Total S |
| ---- | ----: | ------: |
| `middleware` | 49 | 66.77 |
| `.` | 10 | 18.89 |
| `_examples/rest` | 1 | 3.25 |
| `_examples/versions` | 1 | 2.45 |
| `_examples/limits` | 1 | 2.06 |

## Top hotspots

| Rank | Path | S | T | Dominant | Interpretation | Suggested next step |
| ---- | ---- | ---: | ---: | -------- | -------------- | ------------------- |
| 1 | `mux_test.go` | 3.74 | 5.03 | cyclomatic | Router behavior test matrix | Preserve scenario clarity |
| 2 | `tree.go` | 3.56 | 4.79 | cyclomatic | Router tree implementation | Core production hotspot |
| 3 | `middleware/strip_test.go` | 3.31 | 4.46 | cyclomatic | Middleware edge-case tests | Check repeated setup only |
| 4 | `_examples/rest/main.go` | 3.25 | 4.38 | cyclomatic | Example application | Treat as docs/example code |
| 5 | `tree_test.go` | 3.16 | 4.25 | cyclomatic | Router tree tests | Important guardrail |
| 6 | `middleware/compress_test.go` | 3.03 | 4.08 | cyclomatic | Compression middleware tests | Preserve behavior matrix |
| 7 | `middleware/throttle_test.go` | 2.85 | 3.84 | cyclomatic | Throttle middleware tests | Preserve behavior matrix |
| 8 | `mux.go` | 2.84 | 3.82 | cyclomatic | Core mux orchestration | Keep changes narrow |
| 9 | `middleware/client_ip_test.go` | 2.82 | 3.79 | cyclomatic | Client IP parsing tests | Edge-case-heavy by nature |
| 10 | `middleware/wrap_writer.go` | 2.63 | 3.54 | cyclomatic | Response writer wrapping | Production middleware hotspot |

## Findings

### Finding 1: middleware dominates repository entropy

- Evidence: `middleware` contributes `S = 66.77` across 49 files.
- Why it matters: middleware is the broadest area and likely where many users
  interact with optional behavior.
- Suggested action: inspect middleware changes by behavior family rather than as
  one large package.
- Caveat: breadth is expected in a middleware library.

### Finding 2: router tree code is the key core hotspot

- Evidence: `tree.go` is the hottest production file and `tree_test.go` is also
  high in the table.
- Why it matters: routing tree behavior is central to request dispatch and path
  matching.
- Suggested action: changes to `tree.go` should be paired with targeted route
  matching tests.
- Caveat: branch-heavy trie/tree matching code can be legitimate.

### Finding 3: examples appear in the heat map

- Evidence: `_examples/rest/main.go` appears in the top 10.
- Why it matters: examples can be hot because they demonstrate many features in
  one file, not because they are production debt.
- Suggested action: interpret examples separately from library internals.
- Caveat: this is a reminder that diagnostics need human context.

## Recommended next steps

1. For a walkthrough, inspect `tree.go`, `mux.go`, and `middleware/wrap_writer.go`
   as production hotspots.
2. Interpret `_examples/*` files as documentation artifacts before proposing
   refactoring.
3. Use middleware heat to select focused case studies, such as compression,
   throttle, or client IP handling, instead of treating the package as one blob.
