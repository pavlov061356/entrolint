# Contributing to entrolint

Thanks for considering a contribution. `entrolint` is a small, opinionated
project — please read this file before opening a PR.

## Before you start

- **The formula is unstable until v1.0.** Microstate weights, the
  lognormal normalization, and the `ΔS_max` threshold may change between
  minor versions. If your contribution touches the formula or weights,
  expect extra review and a note in [CHANGELOG.md](CHANGELOG.md) under
  `### Changed`.
- **No telemetry, no call-home, no licensing checks.** This is a
  load-bearing trust commitment — PRs that add network I/O for any
  reason other than git operations will be rejected.
- **Quality > coverage.** Tests should pin invariants worth pinning. Don't
  pad coverage with assertions that just re-state code structure.

## Dev setup

- Go 1.26+ (`go version`).
- `make` for the standard targets.
- A working `git` (some tests in `internal/engine/gitx` use
  testcontainers — Docker required for the full `-race` suite).

```bash
git clone https://github.com/pavlov061356/entrolint
cd entrolint
make tools       # install golangci-lint, gofumpt, goimports
make ci          # golangci-lint + race tests (gate used in CI)
make pre-commit  # gofumpt + goimports + lint + tests (formats too)
```

`make ci` is what the CI workflow runs and what blocks a PR. `make
pre-commit` adds in-place formatting — handy locally, do NOT confuse it
with `ci` (formatters mutate the worktree, so they can't be the gate).

## Workflow

`entrolint` uses **gitflow with `dev` as the integration branch**:

- Feature branches: `feature/<short-name>` off `dev`.
- Bug fixes: `fix/<short-name>` off `dev`.
- Releases: `release/v<X.Y.Z>` off `dev`, PR'd into `master`, tagged on
  `master`, back-merged into `dev`.
- Hotfixes: `hotfix/<short-name>` off `master`.
- All merges into `dev` and `master` use `--no-ff` to preserve the
  branching history.

Commits follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(scaling): add identifier_fanout detector
fix(check): treat stale cache as miss when microstate set changes
docs: clarify lognormal floor in formula spec
```

Title-only by default — keep the body for the *why*, not for restating
the diff.

## Pull request checklist

Before opening a PR:

- [ ] `make ci` is green (lint + race tests).
- [ ] New behavior has a test; new microstate / detector has both unit
      tests and an integration test in `internal/engine/pipeline`.
- [ ] Public API changes are reflected in [docs/formula.md](docs/formula.md)
      or [docs/scaling.md](docs/scaling.md) as appropriate.
- [ ] An entry is added to [CHANGELOG.md](CHANGELOG.md) under
      `## [Unreleased]`.
- [ ] If the PR changes `S`, `T`, or `ΔS` values for any file, attach
      `entrolint scan --top 10` output before and after.
- [ ] Dogfood: `go run ./cmd/entrolint check --base dev --head HEAD`
      from your feature branch passes the local gate.

## Filing issues

Use the issue templates:

- **Bug report** — include `entrolint --version`, Go version, OS, the
  exact command run, your `.entrolint.yaml` if any, expected vs actual
  output, and a minimal repro.
- **Feature request** — describe the user problem first, then the
  proposed shape. If it changes the formula, explain how `S` should
  shift for which files.

Security-sensitive issues go to GitHub's private vulnerability
reporting, not the public tracker. See [SECURITY.md](SECURITY.md).

## Scope

`entrolint` is intentionally focused. Out of scope right now:

- A second analysis language (Go-only for now; another language is deferred to
  post-1.0 — pre-1.0 effort deepens Go).
- A hosted web UI, dashboard, or server. (The static, self-contained HTML heat
  map shipped in v0.6 — `scan --html` — but it is a generated file, not a
  running service.)
- Anything requiring network I/O.
- Coverage padding, feature flags, or backwards-compatibility shims for
  pre-1.0 internal APIs.

When unsure whether your idea fits, open a Discussion before writing code.
