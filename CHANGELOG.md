# Changelog

All notable changes to `entrolint` are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [SemVer](https://semver.org/). Until 1.0 the formula and
weights are considered unstable — `S` values may shift between minor
versions.

## [Unreleased]

### Added

- `coupling` microstate — per-file `len(f.AST.Imports)` as a proxy for
  efferent coupling. Every import is counted: stdlib, third-party,
  intra-module, dot, blank side-effect, the CGO `"C"` pseudo-import,
  and aliased forms. v0.3 MVP — the full Robert-Martin Ca/Ce/instability
  graph metric is deferred (see ROADMAP §v0.3 and
  [docs/formula.md](docs/formula.md)).
- `cache.HasAll(names)` — checks that a cached `State` carries
  lognormal parameters for every currently active microstate.
- `CONTRIBUTING.md` — dev setup, gitflow conventions, PR checklist,
  scope notes.
- `SECURITY.md` — supported versions, private vulnerability reporting
  link, threat model, disclosure timeline.
- `.github/PULL_REQUEST_TEMPLATE.md` with a dedicated **Formula impact**
  section that asks PR authors to attach before/after scans when `S`
  changes.
- `.github/ISSUE_TEMPLATE/` — `config.yml` (redirects security reports
  and questions), `bug_report.yml` (structured form), `feature_request.yml`
  (area dropdown).

> **Post-merge maintainer checklist** — `config.yml` and `SECURITY.md`
> link to repo features that are not auto-enabled and only resolve once
> the repo is public. Do all of these before / at the moment of going
> public:
>
> - [ ] Settings → Features → enable **Discussions** (creates the target
>       for the `config.yml` Q&A link).
> - [ ] Settings → Code security → enable **Private vulnerability
>       reporting** (creates the target for the `SECURITY.md` link).
> - [ ] Create labels: `bug`, `enhancement`, `triage` (used by the
>       issue forms; GitHub will silently drop unknown labels otherwise).
> - [ ] Smoke-test: open `https://github.com/pavlov061356/entrolint/security/advisories/new` and `https://github.com/pavlov061356/entrolint/discussions` from a logged-out browser — both should load.

### Fixed

- License conflict: `LICENSE` was Apache-2.0 from the initial commit
  while `README.md` declared MIT. Resolved by replacing `LICENSE` with
  the canonical MIT text. MIT was the intended choice — `entrolint`
  has no patentable IP, the mainstream Go-CLI ecosystem (cobra, viper,
  golangci-lint, goreleaser) is MIT, and Apache's NOTICE / contributor
  obligations add adoption friction for no real protection in this
  context.

### Changed

- **Breaking: `S` values shift** — `Default()` now includes
  `coupling: 0.6`. Files with many imports score higher; absolute
  thresholds in `.entrolint.yaml` may need re-tuning.
- `pipeline.resolveEngine` now treats a cache missing lognormal entries
  for any active microstate (e.g. a v0.2 cache without `coupling`) as
  a miss and recalibrates fresh. Silent `S` degradation on upgrade is
  no longer possible.
- `docs/formula.md` synced: scope expanded to v0.1–v0.3, microstate
  table and default-weights table updated.

## [0.2.0] — 2026-06-04

### Added

- `internal/scaling/` package — predictive layer (PR-diff O-class
  classification) layered on top of the descriptive entropy.
- Five detectors: `shotgun`, `implementor_scan`, `switch_case_symmetry`,
  `identifier_fanout`, `state_multiplier`. Each emits a `scaling.Hit`
  with class O(1)/O(k)/O(n)/O(n·m)/O(2ⁿ).
- Shared helper package `internal/scaling/typesx/`: `Loader` with a
  per-root cache around `golang.org/x/tools/go/packages`, plus
  `FindOwningPackage`, `FindPackageByFile`, `CollectEnums`,
  `ChangedFileSet`, `PosInChanged`, `Relativize`.
- `scaling_class` in `check` output (text + JSON) — max across the
  detectors that fired.
- `scaling.Input` extended with `Root`, `BaseBlobs`, `HeadBlobs`,
  `Patches` — detectors get the PR-diff content without re-running
  `git cat-file`.
- `docs/scaling.md` — catalog of classes and v0.2 simplifications.

### Changed

- `pipeline/check` rewritten as single-pass `fetchAllBlobs` +
  `scoreFromCache`. Previously base/head blobs were fetched twice;
  one pass now with a clear soft/hard error boundary
  (`gitx.ErrNotAtRef` → soft skip, everything else → fatal).
- `golangci-lint` upgraded to v1; curated linter set + Makefile
  pre-commit hook (gofumpt → goimports → lint → race test).

### Fixed

- Cross-package name collisions in `state_multiplier`: `findFunc` is
  now anchored to the owning package via `typesx.FindPackageByFile`
  instead of searching across all loaded packages.
- Test-variant double counting: `packages.Load` runs with
  `Tests: false` (previously `Tests: true` synthesized a parallel
  `pkg [pkg.test]` with a distinct `*types.Named`, doubling enum
  counts).

## [0.1.0] — 2026-06-02

### Added

- Single binary `cmd/entrolint` for Go projects; analyzer built on
  `go/ast` / `go/parser` / `token.FileSet`.
- Four microstates: `cyclomatic`, `nesting`, `length` (contribute to
  `S`) plus `churn` (lives in temperature `T = S · ξ(churn)`).
- Formula `S = k · Σ wᵢ · ln(1 + mᵢ_norm)`; normalization via
  lognormal CDF with fixed floor 0.3; `k` calibrated to median = 1
  across the corpus.
- `scan` mode — `PATH/S/T/DOMINANT` table; flags `--top N`, `--json`,
  `--recalibrate`.
- `check` mode — `--base/--head`, aggregated `ΔS_total` and
  `ΔS_density`, exit 1 when `ΔS_density > delta_s_max`; JSON envelope
  `{verdict, threshold, result}`.
- Configuration via `.entrolint.yaml` (weights + `delta_s_max`);
  calibration cache `.entrolint.cache.json`, shared between `scan`
  and `check`.
- `docs/formula.md` — canonical formula specification.

[Unreleased]: https://github.com/pavlov061356/entrolint/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/pavlov061356/entrolint/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/pavlov061356/entrolint/releases/tag/v0.1.0
