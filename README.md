# entrolint

[![CI](https://github.com/pavlov061356/entrolint/actions/workflows/ci.yml/badge.svg)](https://github.com/pavlov061356/entrolint/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/pavlov061356/entrolint.svg)](https://pkg.go.dev/github.com/pavlov061356/entrolint)
[![Go version](https://img.shields.io/github/go-mod/go-version/pavlov061356/entrolint)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-entrolint--check-2ea44f?logo=githubactions&logoColor=white)](https://github.com/marketplace/actions/entrolint-check)

> Code rots toward disorder. `entrolint` measures the entropy — and keeps it from growing on every PR.

`entrolint` gauges code quality and maintainability through the metaphor of
**entropy from statistical physics**. Clean, ordered, easy-to-change code is a
system with **low entropy**. Tangled, tightly-coupled, hard-to-maintain code is
a system with **high entropy**.

And just as in the second law of thermodynamics, the entropy of a codebase only
grows when left unattended. `entrolint`'s job is to measure it and keep it from
growing unnoticed.

> 🔒 Runs entirely locally — no network calls, no telemetry, ever.

## Why

Ordinary linters catch individual style violations. `entrolint` looks at the
whole picture: it gives you a single number — entropy — that reflects how hard
the code will be to maintain, and shows how that number changes with every change.
For a deeper positioning against traditional linters and quality dashboards, see
[docs/why-entrolint.md](docs/why-entrolint.md).

## Two modes

### `scan` — a heat map of the whole repo

Walks the codebase, computes an entropy score (S) for every file and package, and
highlights **hotspots** — the places with the highest entropy. These are the
first candidates for refactoring. `entrolint scan --html out/` renders the same
data as a self-contained HTML treemap (tile area = `S`, colour = `T`) — see
[docs/heatmap.md](docs/heatmap.md).

### `check` — a pull-request gate

Computes **ΔS** (the entropy delta) between base and head: did the change raise
disorder or lower it. It plugs into CI and blocks PRs that worsen maintainability
beyond a configured threshold.

> Positive ΔS = the code became harder to maintain.

## Installation

```bash
go install github.com/pavlov061356/entrolint/cmd/entrolint@latest
```

Or download a prebuilt binary from the [latest release](https://github.com/pavlov061356/entrolint/releases/latest).

## Quick start

```bash
# The 10 hottest files in the repo:
entrolint scan --top 10

# A self-contained HTML heat map of the whole repo (writes out/index.html):
entrolint scan --html out/

# PR gate: compare a feature branch against dev, fail if the threshold is exceeded.
entrolint check --base dev --head HEAD

# Machine-readable report for CI / a PR bot:
entrolint check --base origin/dev --head HEAD --format json > delta.json
```

`scan` prints a table with the columns `PATH | S | T | DOMINANT`. `check` prints
a verdict line (`PASS`/`FAIL`) plus a per-file breakdown and returns exit code 1
when `delta_s_max` is exceeded.

### Output formats

Both commands take `--format`:

| Command | Formats                               | Notable use                    |
| ------- | ------------------------------------- | ------------------------------ |
| `scan`  | `table` (default), `json`, `sarif`    | `sarif` → GitHub Code Scanning |
| `check` | `table` (default), `json`, `markdown` | `markdown` → a PR-comment body |

(`--json` is a deprecated alias for `--format json`.)

`scan --html <dir>` is separate from `--format`: it writes a self-contained HTML
heat map to `<dir>/index.html` (a squarified treemap with a per-microstate
drill-down, no external assets), rendering the whole repo rather than the table.

## Diagnostic workflow

For a maintainability review, run `scan --html` first, inspect the largest and
hottest tiles, then turn the findings into a short engineering report rather
than a raw metric dump. The reusable structure lives in
[docs/diagnostic-report-template.md](docs/diagnostic-report-template.md), with a
set of filled examples in [docs/examples](docs/examples/README.md).

## Configuration

`.entrolint.yaml` at the repository root (optional — defaults are used when the
file is absent):

```yaml
weights:
  cyclomatic:        1.0
  nesting:           0.8
  coupling:          0.6
  length:            0.5
  duplication:       0.7
  cross_duplication: 0.7
delta_s_max:       0.05   # ΔS_density threshold for check
churn_since_days:  90     # window for the churn factor (lives in T, not S)
```

## Using entrolint in CI

entrolint ships a composite GitHub Action that posts a sticky PR comment with
ΔS, the scaling class and the hottest changed files, and (optionally) uploads a
SARIF log to GitHub Code Scanning. No Go toolchain is needed on the runner — the
Action downloads the released binary.

📦 Available on the [GitHub Marketplace](https://github.com/marketplace/actions/entrolint-check).

```yaml
# .github/workflows/entrolint.yml
name: entrolint
on:
  pull_request:
permissions:
  contents: read
  pull-requests: write     # post the PR comment
  security-events: write   # upload SARIF (optional)
jobs:
  entrolint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - run: git fetch --no-tags origin "+refs/heads/${{ github.base_ref }}:refs/remotes/origin/${{ github.base_ref }}"
      - uses: pavlov061356/entrolint@v0
        with:
          comment: true
          upload-sarif: true
          fail-on-gate: false   # set true to block PRs that raise entropy
```

| Input          | Default           | Description                                                        |
| -------------- | ----------------- | ------------------------------------------------------------------ |
| `version`      | `latest`          | `latest`, a tag (`v0.6.0`), or `source` (build from the checkout). |
| `base`         | PR base branch    | Base ref for ΔS (`origin/<base_ref>` on PRs).                      |
| `head`         | `HEAD`            | Head ref.                                                          |
| `config`       | `.entrolint.yaml` | Path to the config; defaults to `.entrolint.yaml` at the root.     |
| `comment`      | `true`            | Post / update the sticky PR comment.                               |
| `upload-sarif` | `false`           | Upload the `scan` SARIF to Code Scanning.                          |
| `fail-on-gate` | `false`           | Fail the job when the ΔS / scaling gate trips.                     |
| `github-token` | `github.token`    | Token used to post the PR comment and upload SARIF.                |

> Fork PRs get a read-only `GITHUB_TOKEN`, so the comment step is skipped on PRs
> from forks (the Action stays on `pull_request`, not `pull_request_target`).

> The Action runs on the Node 24 runtime, so it needs an Actions runner ≥ v2.327.1.
> GitHub-hosted runners are fine; self-hosted runners must be reasonably current.

## The entropy model

Entropy is a weighted sum of "microstates" — individual measurable factors of
disorder. In the spirit of Boltzmann's formula **S = k · ln(W)**: the more ways
the code can be tangled, the higher the entropy.

Microstates that contribute to S:

| Microstate             | What it measures                          | Since |
| ---------------------- | ----------------------------------------- | ----- |
| Cyclomatic complexity  | how many branches there are in the code   | v0.1  |
| Nesting depth          | how deeply blocks are nested              | v0.1  |
| Function / file length | the size of code units                    | v0.1  |
| Coupling               | how many import specs a file has          | v0.3  |
| Duplication            | repeated AST subtrees within a file       | v0.3  |
| Cross-file duplication | identical AST subtrees copy-pasted across files | v0.5 |

Churn (how often a file changes) feeds the **temperature** T = S · ξ(churn) — the
hottest spots are not merely complex but also frequently rewritten.

Upcoming microstates and milestones are tracked in the [ROADMAP](ROADMAP.md).

More background:

- [Why entrolint](docs/why-entrolint.md) — where it fits next to ordinary
  linters and quality dashboards.
- [Diagnostic report template](docs/diagnostic-report-template.md) — a reusable
  structure for heat-map walkthroughs and maintainability reviews.
- [Diagnostic examples](docs/examples/README.md) — the template filled from
  `entrolint scan` on this repository and selected public Go projects.
- [Formula](docs/formula.md) — the current entropy math.
- [Scaling classes](docs/scaling.md) — the predictive PR-level signal.

## Terminology

- **Entropy (S)** — a measure of disorder and maintenance difficulty. Higher = worse.
- **ΔS** — the change in entropy introduced by a pull request.
- **Hotspot** — a file or package with high entropy.
- **Microstate** — an individual factor contributing to S.
- **Temperature** — a file's normalized entropy, used for the heat map.

## Status

📦 **v0.6** (latest release) — visualization: an HTML **heat map** you can show
the team. `entrolint scan --html out/` writes a self-contained squarified treemap
of the repo (tile area = entropy `S`, colour = temperature `T`), grouped by
package, with file labels and a click-through per-microstate breakdown. The same
release hardens the v0.5 engine — a `ΔS`-symmetry fix and a markedly cheaper
`check` cross-file pre-pass. Builds on the v0.5 `cross_duplication` microstate
(structurally-identical code copy-pasted **across** files), the v0.4 CI
integration (the drop-in `entrolint-check` GitHub Action — ΔS / scaling class /
hotspots as a PR comment + SARIF to Code Scanning, plus `--format markdown|sarif`),
the v0.3 structural microstates (`coupling`, `duplication`), and the v0.2
predictive scaling class (O-class detectors `shotgun`, `implementor_scan`,
`switch_case_symmetry`, `identifier_fanout`, `state_multiplier` emit a
`scaling_class` line next to `ΔS`). The formula, weights, and threshold are
considered unstable until v1.0 — `S` values may shift between releases.

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). This project
follows a [Code of Conduct](CODE_OF_CONDUCT.md), and security reports go through
[SECURITY.md](SECURITY.md).

## License

MIT (see [LICENSE](LICENSE)).
