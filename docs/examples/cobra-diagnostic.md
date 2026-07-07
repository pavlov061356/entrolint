# Example diagnostic: Cobra

This is a filled diagnostic example for
[`github.com/spf13/cobra`](https://github.com/spf13/cobra), generated from an
`entrolint scan` run.

The purpose is to demonstrate interpretation on a mature public Go repository.
It is not a quality ranking.

## Report metadata

- Repository: `github.com/spf13/cobra`
- Commit / ref: `ad460ea`
- Commit date: 2026-04-25
- Date of diagnostic run: 2026-07-07
- entrolint version: `dev` build from this checkout
- Config path: default config
- Scope: full Go tree scanned from the repository root

## Executive summary

- The scan analyzed 36 Go files and reported total entropy `S = 36.87`.
- The hottest file was `completions_test.go` (`S = 2.87`, `T = 3.87`,
  dominant `cyclomatic`).
- The hottest production file was `command.go` (`S = 2.61`, `T = 3.51`,
  dominant `cyclomatic`).
- The highest-entropy area was the repository root (`S = 25.00` across 25
  files), followed by `doc` (`S = 11.87` across 11 files).
- Interpretation: Cobra's profile is compact and centered on command execution,
  shell completion behavior, and documentation generators.

## Commands run

```bash
git clone --depth 1 https://github.com/spf13/cobra.git /tmp/entrolint-proof-repos/cobra
cd /tmp/entrolint-proof-repos/cobra
/tmp/entrolint-proof scan --top 10
/tmp/entrolint-proof scan --format json
/tmp/entrolint-proof scan --html /tmp/entrolint-proof-reports/cobra/heatmap
```

The generated heat map was written to:

```text
/tmp/entrolint-proof-reports/cobra/heatmap/index.html
```

## Repository-level snapshot

- Total analyzed Go files: 36
- Total entropy `S`: 36.87
- Hottest package area: repository root
- Hottest file: `completions_test.go`
- Hottest production file: `command.go`
- Dominant repository-level theme: command behavior, completions, and generated
  documentation helpers

Dominant microstate counts:

| Dominant | Files |
| -------- | ----: |
| cyclomatic | 10 |
| nesting | 8 |
| cross_duplication | 8 |
| coupling | 5 |
| duplication | 1 |
| none / zero score | 4 |

Top package areas by total `S`:

| Area | Files | Total S |
| ---- | ----: | ------: |
| `.` | 25 | 25.00 |
| `doc` | 11 | 11.87 |

## Top hotspots

| Rank | Path | S | T | Dominant | Interpretation | Suggested next step |
| ---- | ---- | ---: | ---: | -------- | -------------- | ------------------- |
| 1 | `completions_test.go` | 2.87 | 3.87 | cyclomatic | Broad shell-completion test matrix | Preserve scenarios; inspect repeated setup only |
| 2 | `command_test.go` | 2.77 | 3.73 | cyclomatic | Command behavior test matrix | Useful verification surface, not an automatic refactor |
| 3 | `command.go` | 2.61 | 3.51 | cyclomatic | Core command execution path | Review carefully before changing command lifecycle |
| 4 | `completions.go` | 2.44 | 3.28 | cyclomatic | Completion generation logic | Candidate for phase-boundary inspection |
| 5 | `doc/rest_docs.go` | 1.90 | 2.56 | nesting | Documentation generator | Low urgency unless extending docs formats |
| 6 | `doc/yaml_docs.go` | 1.75 | 2.36 | cross_duplication | Similar docs-generation structure | Check whether duplication is intentional format symmetry |
| 7 | `flag_groups.go` | 1.69 | 2.28 | cyclomatic | Flag grouping behavior | Keep changes narrow and tested |
| 8 | `doc/md_docs.go` | 1.67 | 2.24 | nesting | Documentation generator | Low urgency |
| 9 | `doc/man_docs.go` | 1.65 | 2.22 | cyclomatic | Documentation generator | Low urgency |
| 10 | `args_test.go` | 1.61 | 2.17 | cyclomatic | Argument validation tests | Preserve test intent |

## Findings

### Finding 1: the command core is the main production hotspot

- Evidence: `command.go` is the hottest production file with `S = 2.61` and
  `T = 3.51`.
- Why it matters: command execution is Cobra's central abstraction, so changes
  here likely affect many users.
- Suggested action: treat `command.go` changes as high-context work with targeted
  tests around lifecycle and execution behavior.
- Caveat: a central framework file is expected to carry real branching.

### Finding 2: completions have a high verification surface

- Evidence: `completions_test.go` and `completions.go` are both near the top.
- Why it matters: shell completion behavior often has many edge cases and
  platform-specific expectations.
- Suggested action: before extracting helpers, check whether repeated test setup
  can be clarified without hiding cases.
- Caveat: test heat is not automatically production debt.

### Finding 3: docs generators show format symmetry

- Evidence: several `doc/*_docs.go` files appear in the top 10, with nesting and
  cross-file duplication signals.
- Why it matters: similar output formats can naturally create similar code
  structures.
- Suggested action: treat this as a review prompt only when adding another docs
  output format.
- Caveat: format-specific duplication may be clearer than a generic abstraction.

## Recommended next steps

1. Use `command.go` and `completions.go` as the first files to inspect in a
   maintainability walkthrough.
2. Keep test complexity visible; do not reduce completion/command scenarios just
   to lower the score.
3. If extending documentation formats, compare the docs generators for intentional
   symmetry vs avoidable copy-paste.
