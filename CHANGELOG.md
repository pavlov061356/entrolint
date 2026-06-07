# Changelog

All notable changes to `entrolint` are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [SemVer](https://semver.org/). Until 1.0 the formula and
weights are considered unstable — `S` values may shift between minor
versions.

## [Unreleased]

## [0.4.2] — 2026-06-07

### Changed

- Internal: all CLI output rendering is centralized in `internal/report`
  — `CheckTable` / `CheckJSON` / `ScanTable` / `ScanJSON` join the
  existing Markdown and SARIF renderers, and `internal/cli` is now a thin
  format-dispatch layer. The table renderers are rebuilt on
  `strings.Builder`, dropping the per-write error-check boilerplate, and
  the non-O(1) scaling-hit iteration is a single shared `nonO1Hits`
  helper (previously duplicated between the table and Markdown
  renderers). Output is byte-for-byte unchanged (verified against the
  v0.4.0 binary); this is the "cooling" refactor of the `check.go` /
  `scan.go` hotspots entrolint flagged on itself.

### Fixed

- Action `description` shortened to under 125 characters so the Action
  passes GitHub Marketplace publishing validation.

## [0.4.1] — 2026-06-07

Tagged on the v0.4.0 commit by mistake (the tag was pushed before the
release PR merged), so this release is byte-identical to v0.4.0 and
carries none of the changes listed under [0.4.2] — those ship in v0.4.2.
The tag is immutable (already cached by the Go module proxy), so it is
left in place rather than re-pointed.

## [0.4.0] — 2026-06-07

### Added

- **`entrolint-check` composite GitHub Action** (`action.yml`) — drop-in
  `uses: pavlov061356/entrolint@v0`. Downloads the released binary for the
  runner's OS/arch (or builds from source with `version: source`), runs
  `check`, and posts a sticky PR comment (ΔS, scaling class, top-3 changed-file
  hotspots) keyed on the `<!-- entrolint-report -->` marker so re-runs update in
  place. Inputs: `version`, `base`, `head`, `config`, `comment`,
  `fail-on-gate`, `upload-sarif`, `github-token`. The gate verdict is derived
  from the JSON `verdict` field, distinguishing a tripped gate from a real
  error. Fork PRs (read-only token) skip the comment — the Action uses
  `pull_request`, not `pull_request_target`.
- **`.github/workflows/entrolint.yml`** — dogfoods entrolint on its own PRs
  (advisory: comments + uploads SARIF, never blocks; not a required status
  check). Closes the v0.1-deferred "dogfood entrolint in CI" item.
- `--format` flag on `scan` and `check`. `scan` accepts
  `table` (default), `json`, `sarif`; `check` accepts `table`,
  `json`, `markdown`. The formats are per-command — `sarif` is
  scan-only, `markdown` is check-only.
- `internal/report` package rendering the typed engine results into
  integration formats: a GitHub-flavored Markdown PR-comment body
  (`check --format markdown` — verdict, ΔS summary, scaling signals,
  changed-file ΔS table, top-3 changed-file hotspots, with a hidden
  `<!-- entrolint-report -->` marker for sticky-comment updates) and a
  hand-rolled SARIF 2.1.0 log (`scan --format sarif`) for GitHub Code
  Scanning — one `entrolint/high-entropy` result per file whose
  temperature T clears a band floor (note ≥ 1.0, warning ≥ 1.5,
  error ≥ 3.0), located at the file (line 1; entrolint scores per
  file, not per line).

### Deprecated

- `--json` on `scan`/`check` — use `--format json`. The flag still
  works as an alias and now prints a one-line deprecation notice to
  stderr (stdout stays clean JSON). Passing both `--json` and
  `--format` is an error.

### Fixed

- README license badge switched from the dynamic shields `github/license`
  endpoint (which intermittently failed with "repo not found" /
  "Unable to select next GitHub token from pool" when shields' shared
  GitHub token pool was rate-limited) to a static MIT badge — the license
  is fixed, so there is nothing to query.
- Markdown PR comment now sanitizes file paths before wrapping them in
  inline code spans / table cells, so a path containing a backtick or a
  `|` can't break out of the span or split a table column.

### Security

- The Action's source build (`version: source`) runs without the
  `github-token` in its environment, and the input is documented as
  trusted-refs-only — it compiles the checked-out tree, which must not be
  untrusted fork code. The token is scoped to the release-download path.

## [0.3.1] — 2026-06-07

### Fixed

- README Status section still advertised v0.2 after the v0.3.0 release — updated to v0.3.

## [0.3.0] — 2026-06-07

### Added

- `coupling` microstate — per-file `len(f.AST.Imports)` as a proxy for
  efferent coupling. Every import is counted: stdlib, third-party,
  intra-module, dot, blank side-effect, the CGO `"C"` pseudo-import,
  and aliased forms. v0.3 MVP — the full Robert-Martin Ca/Ce/instability
  graph metric is deferred (see ROADMAP §v0.3 and
  [docs/formula.md](docs/formula.md)).
- `duplication` microstate — size-weighted mass of structurally-identical
  AST subtrees repeated WITHIN a file. Matching is structural (a Merkle
  FNV-1a hash of subtree shape with identifier names and literal values
  normalized away, so renamed/re-valued copies still collide — Type-2
  clones), gated by a ≥12-node size threshold; a class of `n` copies of
  size `s` contributes `(n-1)·s`, with nested clones counted once. v0.3
  MVP — INTRA-file only; cross-file clone detection is deferred to v0.4+
  for the same reason as the coupling graph (needs a whole-tree pre-pass
  `check` cannot provide). Default weight `0.7`. Adding it shifts `S`
  (k recalibrates); stale caches auto-recalibrate via `HasAll`.
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
- `.goreleaser.yml` (schema v2) — single binary, linux/darwin × amd64/arm64,
  CGO off, tar.gz archives with bundled `LICENSE` / `README.md` /
  `CHANGELOG.md`, SHA-256 checksums, GitHub-native release notes.
- `.github/workflows/release.yml` — runs `goreleaser release --clean` on
  every `v*.*.*` tag push; `contents: write` scoped to the release job
  only, `GITHUB_TOKEN` is the sole secret.
- `make vuln` — runs `govulncheck ./...` via `go run` (no separate
  install step). Now part of `make ci`, so CI fails on any known CVE in
  the call graph.

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
- `golangci-lint` upgraded **v1.64.8 → v2.12.2**. Config rewritten to
  the v2 schema (`version: "2"`, formatters split from linters,
  exclusion presets). Two latent findings surfaced and fixed:
  `prealloc` in `identifierfanout.collectTouchedSymbols` and
  `staticcheck ST1023` in `scaling_test.go`. No detector set changes.
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

[Unreleased]: https://github.com/pavlov061356/entrolint/compare/v0.4.2...HEAD
[0.4.2]: https://github.com/pavlov061356/entrolint/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/pavlov061356/entrolint/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/pavlov061356/entrolint/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/pavlov061356/entrolint/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/pavlov061356/entrolint/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/pavlov061356/entrolint/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/pavlov061356/entrolint/releases/tag/v0.1.0
