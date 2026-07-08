# entrolint — Roadmap

A version-by-version roadmap. Each version = a meaningful user experience, not a
"technical intermediate commit". Versions follow semver; until 1.0 the formula
and weights are considered unstable.

## v0.1 — Minimum viable physics ✅

Released 2026-06-02. Tag `v0.1.0` on master.

**Goal:** get an honest `ΔS` for a single refactoring iteration on a Go project.
Prove that the metaphor turns into a number that doesn't contradict intuition.

- ✅ Implemented in Go, a single binary (`cmd/entrolint`).
- ✅ Go-only analyzer (`go/ast`, `go/parser`, `token.FileSet`).
- ✅ Microstates: `cyclomatic`, `nesting`, `length` contribute to S;
  `churn` goes into T (temperature = S · ξ(churn)).
- ✅ Formula `S = k · Σ wᵢ · ln(1 + mᵢ_norm)`; normalization via lognormal
  CDF with a fixed floor of 0.3, `k` calibrated to a corpus median of 1.
- ✅ `scan` mode — a `PATH/S/T/DOMINANT` table, sorted by T, flags `--top N`,
  `--json`, `--recalibrate`.
- ✅ `check` mode — `--base/--head`, aggregated ΔS_total and ΔS_density,
  exit 1 when ΔS_density > delta_s_max. JSON envelope `{verdict, threshold, result}`.
- ✅ Configuration: `.entrolint.yaml` with weights and `delta_s_max`. A
  calibration cache `.entrolint.cache.json`, shared between scan and check.
- 🟡 Dogfooding `entrolint` on entrolint itself — done manually before the
  release; integration into CI as a separate step is deferred to v0.2.

**Out of scope:** coupling, duplication, O-classification, HTML, SARIF, a second language.

## v0.2 — Predictive layer (scaling class) ✅

Released 2026-06-04. Tag `v0.2.0` on master.

**Goal:** add a predictive metric on top of the descriptive one. A PR gets
**two** answers: "how much dirtier is it now" and "how much will the next
similar change cost".

- ✅ `internal/scaling/` package — detecting O-classes from the diff, with a
  shared `typesx` helper (Loader, FindOwningPackage, FindPackageByFile,
  CollectEnums, ChangedFileSet, PosInChanged, Relativize).
- ✅ Using `go/types` via `golang.org/x/tools/go/packages` — finding interface
  implementations and cross-package references.
- ✅ Heuristics: `shotgun`, `implementor_scan`, `switch_case_symmetry`,
  `identifier_fanout`, `state_multiplier`. Catalog and v0.2 simplifications in
  `docs/scaling.md`.
- ✅ Extending the `check` report: a `scaling_class` line is added next to `ΔS`
  (max across the detectors that fired) — in both text and JSON output.
- ⏭ A reward for `class downgrade` — a negative contribution to ΔS — deferred
  to v0.3: it needs a baseline O-classification that stabilizes on v0.2 data.
- ⏭ The `// entrolint:scaling=O(implementors) reason="..."` annotation —
  deferred to v0.3 after the first feedback on false positives.

## v0.3 — Coupling & duplication ✅

Released 2026-06-07. Tag `v0.3.0` on master.

**Goal:** close the remaining two microstates from the README.

- ✅ `coupling`: per-file import count as an efferent-coupling proxy (v0.3 MVP,
  the `coupling` microstate, default weight 0.6). The full Ca/Ce/instability
  graph needs a whole-tree pre-pass on both refs in `check`; the v0.5 corpus
  pre-pass now provides that infrastructure, and the graph is staged on top of it
  (see v0.5).
- ✅ `duplication`: hashing AST subtrees (not lines) with a size threshold.
  The v0.3 MVP implements **intra-file** duplication: structurally-identical
  subtrees (hashed with identifier/literal normalization, a ≥12-node threshold,
  a class contributing `(n-1)·s`, default weight 0.7). Cross-file copy-paste needed
  the same whole-tree pre-pass on both refs; it shipped in v0.5 as the
  `cross_duplication` microstate.
- Coefficient weights are revisited as new signals appear — a breaking change to
  the `S` score is possible (noted in the CHANGELOG).

## v0.4 — CI integration ✅

Released 2026-06-07. Tag `v0.4.0` on master.

**Goal:** turn the tool into a product inside the work loop.

- ✅ An `entrolint-check` composite GitHub Action — drop-in (`uses: pavlov061356/entrolint@v0`),
  configured by a single file. Downloads the released binary; no Go on the runner.
- ✅ A PR-comment bot: ΔS, scaling class, and the top-3 hotspots among the changed
  files, posted as a sticky comment (`check --format markdown`).
- ✅ SARIF output for GitHub Code Scanning (`scan --format sarif`); uploaded via the
  Action's `upload-sarif` input. If Code Scanning isn't enabled the SARIF can be
  kept as a CI artifact and read by humans.
- ✅ Dogfooding: entrolint runs on its own PRs (advisory — comments + SARIF, does
  not block), closing the v0.1-deferred CI-integration item.
- ⏭ An entropy badge (static SVG / shields endpoint) — deferred; low value for the
  infra cost.

> **Direction (revised 2026-06-07).** A second language is deferred out of the
> pre-1.0 line. entrolint's edge is the depth of the entropy model and the gate,
> not the breadth of languages — and a second-language analyzer is best built
> with real fluency in that language. So the pre-1.0 roadmap deepens the Go
> engine and the physics metaphor (the parts where entrolint is unique);
> TypeScript and other languages move to post-1.0 / on-demand.

## v0.5 — Engine depth: cross-file duplication ✅

Released 2026-06-13. Tag `v0.5.0` on master.

**Goal:** build the shared whole-tree pre-pass deferred since v0.3, and ship the
first real cross-file metric on it — the biggest accuracy win still on the table,
squarely in Go's wheelhouse.

- A blob-corpus **pre-pass** (`internal/engine/corpus`): the whole tree at a ref,
  reconstructed from git blobs (`ls-tree` + a single `cat-file --batch`) and
  parsed with the existing `ParseGoBytes` — no checkout, no `go/types`. It runs
  symmetrically on **both** the base and head refs in `check`.
- Cross-file **`duplication`** (`cross_duplication` microstate): structurally-
  identical AST subtrees detected *across* files, by lifting the existing
  intra-file Merkle clone kernel to a corpus-wide index. Additive — the v0.3
  per-file `duplication` and `coupling` microstates are untouched, so `S` cannot
  regress.
- Recalibration with a CHANGELOG-noted `S` shift as the new signal lands.

Full design: [docs/crossfile.md](docs/crossfile.md).

> **Coupling, staged (revised 2026-06-13).** Original v0.5 was "cross-file
> coupling **and** duplication". Duplication needs no type information; real
> Martin-style `Ca/Ce/instability` does (a type-checked or `go.mod`-resolved
> package graph), and the base ref isn't checked out — the typed path needs a
> materialized base worktree plus a second type-check, too much risk for one
> release. So coupling is split off and staged on top of the v0.5 pre-pass: an
> *import-graph-lite* increment (efferent/afferent by import-path matching on the
> same blob corpus, no `go/types`) first, then the typed package graph
> (interface-satisfaction-aware) as the tail. Both are additive `CorpusContext`
> artifacts — see [docs/crossfile.md](docs/crossfile.md#the-corpuscontext-seam-staged-coupling).
> The v0.3 per-file import-count proxy stays meanwhile.

## v0.6 — HTML heatmap & visualization ✅

Released 2026-06-14. Tag `v0.6.0` on master.

**Goal:** a heat map you wouldn't be ashamed to show the team.

- ✅ A static, **self-contained** HTML report: `entrolint scan --html out/`
  (one `index.html`, all CSS/JS/data inlined — offline, no external assets).
- ✅ A tree heatmap (squarified treemap): tile **area = `S`**, **colour = `T`**,
  files grouped into per-package regions, with file-name labels and a
  click-through per-microstate breakdown (the v0.5 cross-file `cross_duplication`
  signal included). Deterministic output, safe as a CI artifact.
- ⏳ A phase portrait — an `S(t)` graph over git history — is **deferred to
  v0.6.1**: it needs a multi-commit replay, a separate capability from the
  single-scan treemap.

Full design: [docs/heatmap.md](docs/heatmap.md).

This release also folds in the post-v0.5 engine hardening merged to `dev`: a
`cross_duplication` `ΔS`-symmetry fix under one-sided parse (#68), a markedly
cheaper `check` cross-file pre-pass (#69), a test/doc/quality pass (#70), and a
SARIF severity cap at `warning` while the formula is unstable (no blocking
`error` until v1.0).

## v0.6.1 — Phase portrait data layer

**Goal:** make entropy visible over time, not only at one scan point or one PR.

- ✅ `entrolint history [ref] --limit N` samples recent commits without checking
  them out, scores each tree in one calibration frame, and emits total
  repository entropy `S(t)`.
- ✅ Table and JSON output for the first phase-portrait data layer.
- ✅ Mainline history by default via `--first-parent=true`.
- ✅ `history --html out/` writes a self-contained SVG/HTML phase portrait.
- ⏳ Trend annotations and heat-map links for selected commits.

Full design: [docs/phase-portrait.md](docs/phase-portrait.md).

## v0.7 — The physics layer

**Goal:** lean into the thermodynamic metaphor that makes entrolint unique.

- A reward for a `class downgrade` — a negative contribution to ΔS when a change
  lowers the scaling class. The wire fields (`downgrade_bonus`,
  `acknowledged_scaling`) already exist; this adds the producer. (Deferred from v0.2.)
- A `// entrolint:scaling=O(...) reason="..."` annotation — justified exceptions.
  (Deferred from v0.2.)
- A **Maxwell log**: commits with `ΔS < 0` over a period — who cooled the system
  down.
- Free energy `F = S − α·V` — the `check` threshold adapts to the repo's pace.

## v0.8 — Experimental microstates

**Goal:** test subtler signals; keep only those that survive on dogfooding data.

- `shannon_identifiers` (entropy over identifier names), `shannon_ast` (over
  AST-subtree shapes), `comment_anomaly` (comment-to-code ratio deviation),
  `todo_density` (`TODO`/`FIXME` frequency, age-weighted) — behind a
  `--experimental` flag, off in `S` unless enabled in `.entrolint.yaml`.
- Weight calibration is not a post-freeze feature; it is part of the v1.0 gate
  below because changing weights changes the public `S` formula.

## v1.0 — A stable formula

**Goal:** freeze the public contract. Before 1.0 the formula and weights may
change; from 1.0 on — only per semver.

- A calibration corpus from public Go repositories (entrolint analyzes them).
  The author forms the regression ground truth, not an external community.
- Weight calibration: learn the formula weights from real bug-fix history, or
  explicitly keep the current defaults and document why. This must happen before
  the formula freeze.
- The `docs/formula.md` document — final math, weight justification, calibration
  protocol.
- The `docs/scaling-classes.md` document — a catalog of O-classes with examples.
- Guarantee: `S` numbers are comparable across 1.x versions.

## Post-1.0 — second language & beyond

Unprioritized; by whatever proves valuable in practice.

- **A second language (TypeScript, via tree-sitter).** Moved here deliberately
  (see the note at the top): the formula is already language-agnostic by design,
  so this proves it on a second target rather than blocking the Go-side roadmap.
  Best tackled with real TS fluency or a contributor who has it.
- Support for more languages on demand: Python, Rust, Java.
- Smart suggestion mode: for each hotspot, suggest which microstate is pushing
  the score up, and a refactoring template.
- An IDE plugin as a local VSIX / JetBrains plugin zip — a file's temperature in
  the gutter in real time.
- Server mode: a background daemon tracking entropy between runs, shipping
  metrics to Prometheus.

## Quality bars & distribution

Standing goals, pursued continuously rather than tied to one version — also the
prerequisites for an [awesome-go](https://github.com/avelino/awesome-go) listing.

- **Test coverage ≥ 80%** across non-trivial packages — **cleared** (now ~82%
  overall). `internal/cli`, `internal/engine/gitx`, and the `state_multiplier`
  detector still lag and could be raised further.
- **Go Report Card A-/A/A+**, with the badge in the README.
- **Listed in awesome-go** (category *Code Analysis*). awesome-go requires
  ≥5 months of repository history, so entrolint is eligible from ~late October
  2026; the coverage and Go Report Card bars above are the prerequisites to
  clear before submitting.
