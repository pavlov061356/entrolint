# Changelog

All notable changes to `entrolint` are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [SemVer](https://semver.org/). Until 1.0 the formula and
weights are considered unstable — `S` values may shift between minor
versions.

## [Unreleased]

### Added

- Development-only `cmd/entrolint-calibrate` harness for the pre-1.0 weight
  calibration gate. It compares candidate `.entrolint.yaml` weight sets across
  local checkout roots and reports per-microstate contribution shares without
  changing the released `entrolint` CLI.
- Bounded `entrolint-calibrate history` validation. It scores corrective
  commits' parent trees without checkout, reports AUC/top-k/percentile metrics,
  and emits auditable per-commit labels in JSON. The 11-repository v1.0 run
  found no meaningful advantage over the current default weights, so the
  defaults remain unchanged.

### Fixed

- Calibration cache now records the formula/config signature (active
  microstates, weights, normalization floor, and alpha) and recalibrates when
  those inputs change instead of reusing a stale `K`.

## [0.6.1] — 2026-07-08

### Added

- Phase portrait: `entrolint history [ref] --limit N` samples recent commits
  without checking them out and emits total repository entropy `S(t)` as table
  or JSON. The command scores historical git trees through `log` / `ls-tree` /
  `cat-file` in a single current-tree calibration frame, so points are
  comparable across the timeline.
- `entrolint history --html <dir>` writes a self-contained SVG/HTML phase
  portrait to `<dir>/index.html`. The X axis is a real commit-time scale:
  periods without commits appear as horizontal gaps rather than synthetic
  no-op points.
- Product proof pack documentation: `docs/why-entrolint.md`, the maintainability
  diagnostic report template, and filled diagnostic examples for entrolint,
  Cobra, Gin, and Chi.

### Changed

- README and ROADMAP now document the three-command product shape:
  `scan`, `check`, and `history`.
- Dependencies and CI maintenance: `actions/checkout` v6 → v7,
  `github.com/testcontainers/testcontainers-go` 0.42 → 0.43, and
  `golang.org/x/tools` 0.46 → 0.47.

## [0.6.0] — 2026-06-14

### Added

- HTML heat map: `entrolint scan --html <dir>` writes a self-contained
  `index.html` — a squarified treemap of the repository where each file's
  rectangle **area is its entropy `S`** and its **colour is its temperature
  `T`**, with files grouped into their package regions, file-name labels
  painted on the tiles, and a click-through per-microstate breakdown (the v0.5
  `cross_duplication` signal included). No network or external assets and
  deterministic output, so it works offline and is safe to publish as a CI
  artifact. Pure report layer — no engine change. (v0.6)

### Changed

- `check`'s cross-file pre-pass is markedly cheaper: changed blobs are fetched
  in one batched `cat-file` instead of one subprocess per file, the corpus
  reuses those blobs rather than re-fetching them (each blob is read from git
  once), `CloneIndex` is two-pass (occurrence slices are materialised only for
  digests that recur), and the whole pre-pass is skipped when the
  `cross_duplication` weight is ≤ 0. No `ΔS` change. (#69)
- The calibration cache no longer forces a full recalibration on every run of a
  repo where a microstate has no signal (e.g. `cross_duplication` on a
  clone-free repo): a present-but-degenerate fit now counts as cached,
  consistent with the "calibrate once, manual `--recalibrate`" contract. (#70)
- SARIF severity is capped at `warning` until v1.0. An admittedly-unstable
  metric must not raise `error`, which GitHub Code Scanning treats as a
  **blocking** check failure — contradicting entrolint's advisory stance. A hard
  error band remains available to consumers via an explicit `ErrorAt`.

### Fixed

- `cross_duplication` could perturb the `ΔS` of an **unchanged** file when a
  clone partner parsed on only one ref (a mid-edit syntax error, an unreadable
  blob): the shifting partner moved a clone class's free "original" between
  refs. Such files are now held out of **both** ref corpora, keeping
  clone-class membership symmetric across base and head. (#68)

## [0.5.1] — 2026-06-13

### Changed

- Documentation only. Reconciled the docs and code comments with the
  shipped v0.5.0 state (the v0.5.0 release had carried several stale
  docs): README's microstate table and `.entrolint.yaml` weights example
  now include `cross_duplication`, and a Contributing section links the
  community-health files; the ROADMAP marks v0.5 released and refreshes
  the coverage bar; `docs/formula.md` and `docs/scaling.md` cover v0.5,
  mark the (still-deferred) downgrade reward and scaling annotation as
  not-yet-shipped, and de-pin staged coupling from specific versions.
  No code or behaviour change.

## [0.5.0] — 2026-06-13

### Added

- Cross-file duplication: a new `cross_duplication` microstate detects
  structurally-identical AST subtrees shared **across** files, not just
  within one. It is built on a whole-tree blob-corpus pre-pass
  (`internal/engine/corpus`) that reconstructs each ref from git blobs
  (`ls-tree` + a single `cat-file --batch`) with no checkout and no
  `go/types`, run symmetrically on the base and head refs in `check`. New
  `gitx.TreeFiles` / `gitx.BlobsAtRef` plumbing backs it. Default weight
  0.7. See `docs/crossfile.md`. (v0.5)

### Changed

- Adding the `cross_duplication` microstate recalibrates `S`: files that
  share copy-pasted blocks across the tree now score higher. The v0.3
  `duplication` and `coupling` microstates are unchanged, and
  `cross_duplication` charges only clone classes spanning more than one
  file, so the two partition the clone space without double-counting.
  Existing calibration caches auto-invalidate (the new microstate's
  lognormal is absent) and recalibrate on the next run. Full cross-file
  `coupling` (Ca/Ce) stays deferred and staged on the same pre-pass.
- Dependencies: `golang.org/x/tools` 0.45 → 0.46. The composite
  `entrolint-check` Action's bundled steps were bumped to
  `actions/github-script@v9`, `github/codeql-action@v4`, and
  `goreleaser/goreleaser-action@v7`, which run on the Node 24 Actions
  runtime — **the Action now requires an Actions runner ≥ v2.327.1**.
  GitHub-hosted runners satisfy this; only outdated self-hosted runners
  are affected. Dependabot now targets the `dev` branch.

## [0.4.3] — 2026-06-07

### Changed

- Docs only. README gains a GitHub Marketplace badge and link (the
  `entrolint-check` Action is now published on the GitHub Marketplace).
  The ROADMAP is re-sequenced: TypeScript is deferred to post-1.0 and the
  pre-1.0 line deepens the Go engine — v0.5 is now cross-file `coupling`
  (Ca/Ce/instability graph) + cross-file `duplication`.

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

[Unreleased]: https://github.com/pavlov061356/entrolint/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/pavlov061356/entrolint/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/pavlov061356/entrolint/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/pavlov061356/entrolint/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/pavlov061356/entrolint/compare/v0.4.3...v0.5.0
[0.4.3]: https://github.com/pavlov061356/entrolint/compare/v0.4.2...v0.4.3
[0.4.2]: https://github.com/pavlov061356/entrolint/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/pavlov061356/entrolint/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/pavlov061356/entrolint/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/pavlov061356/entrolint/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/pavlov061356/entrolint/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/pavlov061356/entrolint/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/pavlov061356/entrolint/releases/tag/v0.1.0
