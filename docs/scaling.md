# `entrolint` — scaling class specification (v0.2)

This document is the canonical reference for the "scaling class": the predictive
metric that `entrolint` emits for a PR alongside the descriptive `ΔS`. It covers
the v0.2 release series; before 1.0 the classes, heuristics, and coefficients are
considered unstable.

## Scope

- **v0.2** — the only analysis language: Go. The AST source is the stdlib
  `go/ast`; the type source is `golang.org/x/tools/go/packages`.
- Five O-classes: `O(1)`, `O(k)`, `O(n)`, `O(n·m)`, `O(2ⁿ)`.
- The heuristics are static, no LLM. Each returns a class for a single "touched
  site"; the PR level is aggregated by maximum.
- The downgrade-bonus weights are hardcoded defaults in the binary, overridable
  via `.entrolint.yaml`.

## Why a separate class alongside `ΔS`

`ΔS` is a **descriptive** metric: how much dirtier the system became *right now*.
The scaling class is **predictive**: how expensive the *next similar* change will
be.

The two metrics are not interchangeable:

- A PR with a microscopic `ΔS` (5 lines in a 100k project) can wire in
  `O(implementors)` — a new enum case that must now appear in every `switch`. Its
  future cost is catastrophic, and the current `ΔS` doesn't see it.
- A PR with a large positive `ΔS` (a new complex package) can stay `O(1)` —
  locked within its own boundaries. Heavy now, cheap later.
- A refactor that collapses a switch-over-enum into polymorphism lowers the class
  from `O(k)` to `O(1)`. That's a structural win that `ΔS` — through cyclomatic
  and length — catches only partially.

So v0.2 emits both numbers and both affect the gate — but they affect it
**differently** (see [Class upgrades are not penalized in `ΔS`](#class-upgrades-are-not-penalized-in-δs)).

## Classes

| Class            | When                                                                                       | Example                                                               |
| ---------------- | ------------------------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| `O(1)`           | a local change with no architectural fan-out                                               | a new private helper, a refactor inside a function                    |
| `O(k)`           | symmetric edits across all `k` known sites (interface implementations, callers, enum)      | a new method on an interface → edits in every implementation          |
| `O(n)`           | scales with the size of the file/package                                                    | renaming a symbol (sweep), walking every function of a package        |
| `O(n·m)`         | two independent enumerations intersect                                                      | supporting a new message type × all of its delivery channels          |
| `O(2ⁿ)`          | a new bool/enum in a public signature, multiplying the state space                          | a `legacy bool` flag through 6 packages; a new branch in each business rule |

The order is strict: `O(1) < O(k) < O(n) < O(n·m) < O(2ⁿ)`. PR-level aggregation
is the maximum across the classes of all touched sites.

### Why these five

In real-world Go PRs these five patterns cover the overwhelming majority of
architectural risks. A finer notation (`O(n log n)`, `O(n²)` from a mistaken
nested loop, `Θ` vs `O`) is theoretically cleaner but yields nothing on the
observable heuristics. v0.2 deliberately fixes a coarse-grained lattice and tunes
it on dogfooding.

## Heuristics

Each detector works in one known plane and doesn't interfere with the others.
Aggregation resolves conflicts (the maximum class wins).

| Detector                  | Class if it fires                  | Signal                                                                                                                              | Needs types |
| ------------------------- | ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ----------- |
| `implementor_scan`        | `O(k)`, where k = #implementors    | a PR changes an interface method AND in the same PR ≥ 50% of its implementations are changed                                       | yes         |
| `identifier_fanout`       | `O(refs)`, where refs = #call sites | for a touched exported symbol: pre-PR `references(symbol) ≥ N` (N=8 by default) and the PR touched ≥ 80% of call sites             | yes         |
| `switch_case_symmetry`    | `O(k)`, where k = #switch arms     | an enum case is added AND ≥ 50% of the `switch`es over that type in the package are edited                                         | yes         |
| `state_multiplier`        | `O(2ⁿ)`                            | a `bool` or a new enum value is added to a public signature, and there are ≥ 2 call sites outside the originating module           | yes         |
| `shotgun`                 | `O(n)`, where n = #files           | a single logical change touched ≥ N files (N=5 by default) with no common AST parent                                              | no          |

A "logical change" in `shotgun` is a PR-level signal: we look at the diff as a
whole, not at each file independently. The heuristic specifically looks for "the
same line replacement in N files" / "identical function signatures added in N
places" — the shotgun-surgery patterns from Code Complete.

A "touched interface method" in `implementor_scan` is determined by an
`*ast.FuncDecl` with a receiver whose name matches one of the methods of some
`*types.Interface` in the graph.

All thresholds (`8`, `80%`, `50%`, `5`, `2`) are starting assumptions. In v0.2.x
they are hardcoded; in v0.8 they are calibrated together with the microstate
weights.

## Per-change vs aggregate

In the `check --json` report each changed file gets its own block with the list
of detectors that fired and the resulting per-change class. The PR-level
`scaling_class` field equals the maximum of the per-change classes.

```json
{
  "scaling_class": "O(k)",
  "scaling_breakdown": [
    {
      "path": "internal/foo/bar.go",
      "class": "O(k)",
      "detectors": [
        { "name": "implementor_scan", "class": "O(k)", "size": 7 }
      ]
    }
  ]
}
```

The text output shows the PR-level class in the verdict line; on failure — the
list of reasons; and, when present, each non-`O(1)` detector hit on its own line:

```
PASS  ΔS_total=1.2500  ΔS_density=0.0250  threshold=0.0500  scaling_class=O(1)  lines_changed=50  files=2
```

```
FAIL  ΔS_total=1.4200  ΔS_density=0.0234  threshold=0.0050  scaling_class=O(k)  lines_changed=60  files=3
  reason: ΔS_density 0.0234 > 0.0050
  scaling: implementor_scan O(k) in internal/proto/codec.go (size=7) — 7 implementors touched
```

The verdict-line fields are stable in order: `verdict`, `ΔS_total`,
`ΔS_density`, `threshold`, `scaling_class`, `lines_changed`, `files`. Fields are
separated by two spaces — `awk` on `"\t"` won't work, but
`grep -oE 'scaling_class=\S+'` and similar field-name regexes are stable.

## Downgrade reward — proportional

> **Status (not yet shipped).** The wire field (`downgrade_bonus`) and the gate
> handling exist, but the producer that computes a non-zero bonus is deferred to
> v0.7 (see [ROADMAP](../ROADMAP.md#v07--the-physics-layer)). Until then
> `downgrade_bonus` is always `0`; the rest of this section describes the
> intended behaviour.

If a PR **lowers** the class of a touched site (was `O(k)`, became `O(1)`), that
gives a **negative contribution to `ΔS_total`** — refactoring is rewarded right
in the gate.

Formula:

```
ΔS_scaling_bonus = - β · log2(size_before / size_after)
```

- `size_before` — the size of the architecture in the original class (e.g. the
  number of implementations for `O(implementors)`, the number of cases for
  `O(switch)`).
- `size_after = 1` for a downgrade to `O(1)`; otherwise the corresponding size.
- `β` — the global bonus coefficient, default `0.5`.
- `log2` — because the gain from removing 16 implementations should be larger
  than from removing 2, but not proportionally (the law of diminishing returns).

Examples with the default `β = 0.5`:

| Scenario                                              | size_before | size_after | Bonus   |
| ----------------------------------------------------- | ----------- | ---------- | ------- |
| Collapsed a switch with 4 cases into polymorphism     | 4           | 1          | `-1.00` |
| Removed an interface with 8 implementations           | 8           | 1          | `-1.50` |
| Lowered `O(n·m)` to `O(n)` (removed one axis)         | n·m         | n          | `-0.5 · log2(m)` |
| Local refactor inside a function                      | 1           | 1          | `0`     |

The bonus is **not** automatically merged with the positive contributions of
other microstates — it is added to `ΔS_total` as a separate term:

```
ΔS_total = Σ_files (S_head - S_base) + Σ_scaling_bonuses
ΔS_density = ΔS_total / max(1, lines_changed)
```

In the report the scaling bonus is shown on a separate line, so the user can see
**why** the overall `ΔS_total` is negative even when the structural part is
positive.

### Class upgrades are not penalized in `ΔS`

Symmetrically: a PR that introduces a new `O(implementors)` dependency gets no
**extra** positive contribution to `ΔS` beyond what the microstates already
describe (length, cyclomatic, shotgun-pattern files, etc.). A class upgrade fails
the PR through a **separate gate** (see [Gate](#gate)), not through `ΔS`.

The reason: otherwise the "new code increases complexity" signal would be counted
**twice** — through the rise in microstates and through scaling. Double counting
makes calibrating the threshold impossible. The symmetric rule (only a downgrade
affects `ΔS`) gives a clean separation.

## Gate

The `check` gate becomes two-dimensional:

```
fail if ΔS_density > delta_s_max
fail if scaling_class > scaling_class_max
```

Defaults in `.entrolint.yaml`:

```yaml
delta_s_max:        0.05
scaling_class_max:  O(k)   # O(n) and above fail the PR
scaling_bonus_beta: 0.5
```

`scaling_class_max = O(k)` means: `O(1)` and `O(k)` pass, everything above
(including `O(n)`, `O(n·m)`, `O(2ⁿ)`) fails.

The logic is "or", not "and": the PR fails when any threshold is exceeded. This
matches the fact that the metrics catch different classes of risk.

## Escape hatch — the annotation

> **Status (not yet shipped).** The report carries the wire field
> (`acknowledged_scaling`), but the `// entrolint:scaling=…` parser is deferred to
> v0.7 (see [ROADMAP](../ROADMAP.md#v07--the-physics-layer)). The annotation
> described below is the intended design, not yet active.

Sometimes `O(implementors)` is a justified architectural decision (e.g. an SPI
with a known set of drivers). So such cases don't poison the signal, an
annotation is supported in a comment above the touched function / type /
interface:

```go
// entrolint:scaling=O(implementors) reason="every storage backend must implement the full API"
type Storage interface {
    // ...
}
```

The annotation:

- **takes the method/type out from under the detector** — it doesn't raise the
  PR-level class;
- but **stays in the JSON report** under an `acknowledged_scaling` field — the
  pattern is visible on any dashboard, just without consequences for the gate.

The annotation parser is a simple grep over comments above `*ast.FuncDecl`,
`*ast.TypeSpec`, `*ast.InterfaceType`. No formal grammar — the format is
deliberately kept tiny.

## Known simplifications (v0.2)

So as not to pretend everything is smooth:

1. **The heuristics catch patterns, not intent.** Sometimes the `O(k)` heuristic
   fires on a change that is semantically local (a code generator updated all
   files in sync). The annotation is the only escape.
2. **The architecture size is computed on base.** If a PR simultaneously
   increases the number of implementations and touches half of the old ones,
   `size_before` is the old count. This understates the `O(k)` complexity. Not
   critical for v0.2; accounted for in v0.8 calibration.
3. **`shotgun` without types yields false positives on formatting changes.**
   Workaround: gofumpt-only diffs are filtered out before the heuristic via a
   simple "are there changes outside whitespace" check.
4. **Cross-package references are slow.** A `go/packages` load with full type
   checking can take seconds on large repositories. v0.2.0 accepts this; v0.3+ —
   parallel package loading and a `references(symbol)` cache in
   `.entrolint.cache.json`.
5. **The default thresholds (50%, 80%, ...) are unjustified.** In v0.8 they are
   trained on a corpus together with the microstate weights; until then — starting
   assumptions, pinned in this documentation.
6. **`implementor_scan` sees only the interfaces of its own module.**
   `packages.Load("./...")` returns only the root module's packages; stdlib and
   external dependencies are reachable via `pkg.Imports` but don't enter the
   iteration. A PR that touches ≥50% of the project types implementing
   `io.Reader`/`http.Handler`/other popular stdlib interfaces **won't fire** —
   this is a deliberate v0.2 limitation, not a bug. Transitive traversal and a
   `GOROOT` filter are under consideration for v0.3.
7. **Generic interfaces are skipped.** `types.Implements` is undefined on
   parameterized types; the detector silently skips `interface[T]{...}`. v0.3+ may
   instantiate via `pkg.TypesInfo.Instances`.
8. **`switch_case_symmetry` doesn't check that "a case was added", only the
   symmetric edit.** The full heuristic spec is "an enum case is added AND ≥ 50%
   of the switches over that type are edited". The v0.2 MVP implements only the
   second half: "≥ 50% of the switches over an enum type are touched in the PR".
   Diffing the base/head AST on const declarations in order to separate "a new
   case" from "renaming/refactoring a case" is deferred to v0.3 — for now both
   situations are treated as equally architecturally expensive and both fail at
   `O(k)`. This yields false positives on bulk refactors within one enum without
   adding cases; the `entrolint:scaling` annotation is the escape.
9. **`identifier_fanout` uses "the def file is in Changes" as a proxy for "the PR
   changed the symbol".** The full heuristic spec is "the PR edited the definition
   AND ≥ 80% of call sites". The v0.2 MVP only checks that the *file* with the
   declaration is in the diff (without an AST diff on the specific fields of the
   declaration). The canonical case "renamed a function + updated all its calls"
   is caught correctly; but "accidentally touched format.go for an import and
   updated 80% of callers for your own reasons" could formally fire — an
   infrequent scenario, escape via the annotation.
10. **`identifier_fanout` doesn't distinguish self-references from cross-package
    references.** If an exported function calls itself recursively or references
    its own fields in the same file, those uses land in both the numerator and
    denominator of the ratio. With the def file touched, this slightly inflates
    the ratio toward triggering. v0.3 may subtract an out-of-changed-files counter
    or maintain a separate "cross-package fan-out" metric.
11. **`state_multiplier` catches only parameters added at the end of the
    signature.** A full AST diff on parameters is harder than v0.2 requires:
    a middle insert (`f(a, NEW, b)`), a type swap without an arity change
    (`f(int)` → `f(bool)`), a new parameter via variadic (`...Option`) — these are
    all v0.3 extensions. The MVP checks: head has exactly +1 parameter, and it is
    `bool` or an `<enum-like type>`, while the first N parameters are bit-for-bit
    equal to base by `typeKey` (a textual AST comparison). It fires on the
    canonical case "added a legacy-behavior flag to a public API"; the rest are
    deferred. The detector also doesn't distinguish `bool` from `*bool` or
    `[]bool` — the latter are not considered state-multiplying (semantically an
    option, not a flag).

## What changes in v0.8 ("Weight calibration")

In v0.8 the parameters of the scaling heuristics stop being fixed constants and
become part of a trained model.

- **The regression target** is future revert commits in files that received
  `scaling_class ≥ O(k)` in a passing PR. The hypothesis: "a PR with a high
  scaling class more often requires a revert".
- **The heuristic parameters** — thresholds `references_min=8`,
  `implementor_ratio=0.5`, etc. — are trained on a public corpus.
- **The downgrade bonus `β`** — also trained; interpretable (a single number, a
  linear contribution).
- **Per-detector confidence** — introduced as an ML prediction of "how well a hit
  of this detector predicts a revert". The gate starts looking only at
  high-confidence hits.

The architectural consequence for the v0.2 code: thresholds and `β` are loaded
from the config as data, not used as magic numbers in Go code. Then moving to
ML calibration is just a change of the parameter source, not a rewrite of the
detectors.

## What's out of scope for v0.2

- **TypeScript** — the second language is deferred to post-1.0; scaling detectors
  for TS would require tree-sitter cross-references, and the pre-1.0 line deepens
  the Go engine instead.
- **`O(n log n)`, `O(n²)`, etc.** — a finer lattice. It's not proven that the
  heuristics can distinguish them statically without false positives.
- **Comparing PRs against each other** ("this PR is in the top 5% by scaling
  class this quarter") — that's dashboard level, beyond the CLI.
- **IDE integration** — showing the class right in the editor. Deferred to after
  the v0.6 HTML heatmap, then an IDE plugin (post-1.0).

## Terminology

- **Scaling class** — the O-class of the future cost of a repeated similar change.
- **Detector** — a static heuristic that returns `(class, size, evidence)` for a
  single touched site.
- **Detector hit** — a detector firing at a specific site.
- **Size** — the size of the architecture the class is tied to (k for
  implementors/cases, n for shotgun).
- **Downgrade reward** — a negative contribution to `ΔS_total`, awarded to a PR
  that lowers the scaling class of a touched site.
- **Acknowledged hit** — a detector hit marked with an `// entrolint:scaling=…`
  annotation — doesn't affect the gate, visible in the report.
