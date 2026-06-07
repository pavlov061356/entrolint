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
  graph needs a whole-tree pre-pass on both refs in `check` — deferred to v0.4+,
  once such pre-pass infrastructure is needed for another detector.
- ✅ `duplication`: hashing AST subtrees (not lines) with a size threshold.
  The v0.3 MVP implements **intra-file** duplication: structurally-identical
  subtrees (hashed with identifier/literal normalization, a ≥12-node threshold,
  a class contributing `(n-1)·s`, default weight 0.7). Cross-file copy-paste needs
  the same whole-tree pre-pass on both refs as the full coupling graph — deferred
  to v0.4+.
- Coefficient weights are revisited as new signals appear — a breaking change to
  the `S` score is possible (noted in the CHANGELOG).

## v0.4 — Internal CI integration

**Goal:** turn the tool into a product inside the work loop, without depending on
the repository being public.

- An `entrolint-check` GitHub Action as a workflow inside the repo — drop-in,
  configured by a single file.
- A bot that leaves a PR comment: ΔS, scaling class, top-3 hotspots in the
  changed files.
- SARIF output for GitHub Code Scanning. If GHAS isn't enabled — SARIF is written
  as a CI artifact and read by humans.
- An internal badge: a static SVG generated in CI and stored as an artifact or in
  a private `gh-pages` branch — for internal dashboards.
- A public shields.io badge — only once the repo goes public.

## v0.5 — A second language (TypeScript)

**Goal:** prove the formula is language-agnostic, not Go-specific.

- Wiring up tree-sitter (via cgo bindings) or an external tsc.
- An `internal/analyzer/typescript/` analyzer.
- Microstates are shared; the parsing implementation differs.
- Calibration against new percentiles from a TS corpus.
- `coupling` and `scaling class` — partially, to the extent tree-sitter provides
  cross-reference information.

## v0.6 — Experimental microstates

**Goal:** test subtler signals and keep only those that survive on dogfooding
data.

- `shannon_identifiers` — Shannon entropy over a module's identifier names.
- `shannon_ast` — entropy over AST-subtree shapes.
- `comment_anomaly` — deviation of the comment-to-code ratio.
- `todo_density` — frequency of `TODO`/`FIXME`, weighted by age.
- All are enabled by a `--experimental` flag and don't enter the main `S` without
  being explicitly turned on in `.entrolint.yaml`.

## v0.7 — HTML heatmap & visualization

**Goal:** a heat map you wouldn't be ashamed to show the team.

- A static HTML report `entrolint scan --html out/`.
- A tree heatmap (squarified treemap) by T.
- Drill-down: click a file to see the per-microstate breakdown.
- A phase portrait: an `S(t)` graph over git history for a window (can be
  precomputed in CI as an artifact).

## v1.0 — A stable formula

**Goal:** freeze the public contract. Before 1.0 the formula and weights may
change; from 1.0 on — only per semver.

- A calibration corpus from public Go/TS repositories (entrolint analyzes them). The author forms the regression ground truth, not an
  external community.
- The `docs/formula.md` document — final math, weight justification, calibration
  protocol.
- The `docs/scaling-classes.md` document — a catalog of O-classes with examples.
- Guarantee: `S` numbers are comparable across 1.x versions.

## Post-1.0 — potential directions

Unprioritized, by whatever proves valuable in practice:

- Free energy `F = S − α·V` — the `check` threshold adapts to the repo's pace.
- A Maxwell log: a list of commits with `ΔS < 0` over a period, who cooled the
  system down.
- An IDE plugin as a local VSIX / JetBrains plugin zip — a file's temperature in
  the gutter in real time. Publishing to the VS Code Marketplace / JetBrains
  Marketplace — only if the repository goes public.
- Server mode: a background daemon tracking entropy between runs, shipping
  metrics to Prometheus.
- Support for more languages on demand: Python, Rust, Java.
- Smart suggestion mode: for each hotspot, suggest which microstate is pushing
  the score up, and a refactoring template.
