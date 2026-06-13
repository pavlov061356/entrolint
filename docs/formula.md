# `entrolint` — formula specification (v0.1–v0.5 structural microstates)

This document is the canonical reference for the math of computing entropy `S`,
temperature `T`, and a PR's `ΔS`. Before 1.0 the formula and weights are
considered unstable — the concrete numbers may change from one minor version to
the next.

## Scope

- **v0.1** — the only analysis language: Go. The AST source is the stdlib
  `go/ast`.
- Structural microstates: `cyclomatic`, `nesting`, `length` (v0.1) +
  `coupling`, `duplication` (v0.3) + `cross_duplication` (v0.5) — see
  [Microstates](#microstates). `churn` feeds only the temperature `T`, not `S`.
- The weights `wᵢ` are hardcoded defaults in the binary. In v0.8 they are
  learned on a public corpus (see [What changes in v0.8](#what-changes-in-v08-weight-calibration)).

## Aggregation levels

Entropy lives at four levels, each a simple bottom-up summation:

```
S_func     →   S_file = Σ S_func + S_file_structural
S_package  →   S_package = Σ S_file
S_repo     →   S_repo    = Σ S_package
```

`S_file_structural` is the set of terms that don't localize to a function (file
length, coupling, and intra-/cross-file duplication). That's why `S_file`
is not a pure sum of functions but the sum of functions plus file-level add-ons.
This matters for `ΔS`: if a PR changed a single function, we recompute its
contribution and leave the rest untouched — the delta is cheap to compute.

## Base form

```
S_file = k · Σᵢ wᵢ · ln(1 + mᵢ_norm)
```

- `mᵢ_norm ∈ [0, 1]` — the normalized value of the i-th microstate.
- `wᵢ` — the microstate's weight (from `.entrolint.yaml` or the default).
- `k` — the project's normalization constant (see [The `k` constant](#the-k-constant)).

### Why `ln(1 + x)`

- `ln(1 + 0) = 0` — perfectly clean code contributes zero from each microstate.
- Growth is sublinear: a function with `cyclomatic = 20` is not twice as bad as
  one with `cyclomatic = 10` — it's worse, but not catastrophically. This is
  consistent with the Boltzmann intuition `S = k · ln(W)` and tames outliers.
- Smooth and differentiable — convenient for ΔS.
- The `+1` under the logarithm prevents `ln(0) = -∞`.

### Why an additive sum over microstates

In statistical physics the entropy of independent subsystems adds up. Here we
**postulate** that `cyclomatic` is independent of `nesting`, etc. — a
simplification, but an honest one. In reality the microstates correlate (see
[Known simplifications](#known-simplifications-v01)); in v0.8 the correlation is accounted for via
regression weights.

## Microstates

| Microstate     | Since    | Measured at | Raw value                                                                                                 | File-level aggregation  |
| -------------- | -------- | ----------- | --------------------------------------------------------------------------------------------------------- | ----------------------- |
| `cyclomatic`   | v0.1     | function    | number of decision points: `if`, `for`, `case`, `&&`, `\|\|`, `range`, `select case`, `defer recover`     | sum over functions      |
| `nesting`      | v0.1     | function    | maximum block nesting depth                                                                               | **maximum** over functions |
| `length`       | v0.1     | file        | LOC excluding comments and blank lines                                                                    | direct                  |
| `coupling`     | v0.3     | file        | `len(f.AST.Imports)` — the number of import specs (including stdlib, dot, blank, CGO `"C"`, aliased)      | direct                  |
| `duplication`  | v0.3     | file        | size-weighted mass of repeated AST subtrees within the file (structural hash, identifier/literal normalization, threshold ≥12 nodes) | direct |
| `cross_duplication` | v0.5 | file   | size-weighted mass of AST subtrees the file shares with **other** files (same structural hash, ≥12 nodes); the lowest-path copy in each clone class is the free "original", every other file pays the class size once | direct |
| `churn`        | v0.1     | file        | number of commits that touched the file in a 90-day window (`git log --follow --since`)                   | direct                  |

`coupling` in v0.3 is an MVP: a per-file import count as a proxy for efferent
coupling. The full Martin-style Ca/Ce/instability graph needs a whole-tree
pre-pass on both refs in `check` (where only the blobs of the changed files are
available). v0.5 builds that pre-pass (for cross-file duplication first); real
coupling is staged on top of it — an import-graph-lite step, then a typed package
graph. See the [cross-file design](crossfile.md#the-corpuscontext-seam-staged-coupling).

`duplication` in v0.3 is also an MVP: **intra-file** duplication only.
Structurally-identical AST subtrees (a hash, not lines; identifiers and literals
are normalized — so it catches copy-paste even after a rename) are counted as
repeats; a class of `n` copies of a subtree of size `s` contributes `(n-1)·s`,
and nested clones are not counted twice (only the outermost one counts).
Cross-file copy-paste ships in v0.5 as the separate **`cross_duplication`**
microstate (above): the same structural hash, run over a whole-tree blob-corpus
pre-pass on both refs in `check`, charging each clone class's redundant mass to
every file past the lowest-path "original". The two microstates partition the
clone space — same-file classes are `duplication`'s, multi-file classes are
`cross_duplication`'s — so they never double-count. See the
[cross-file design](crossfile.md).

Note: `nesting` is aggregated by maximum, not by sum. Deep nesting is a local
defect: a file with one horrendously nested function is worse than a file where
every function has depth 3. A sum would hide that signal.

Note also: `churn` is not part of `S` — it lives in the temperature `T` (see
[Temperature `T_file`](#temperature-t_file)).

## Normalization: lognormal CDF with a floor

For each microstate `i`:

1. On the first scan of the repo we fit the parameters `(μᵢ, σᵢ)` of a
   lognormal distribution over the raw values of all files.
2. We normalize via the CDF: `Φ_lognormal(m_raw_i; μᵢ, σᵢ) ∈ [0, 1]`.
3. We apply a "floor" so the median file of a clean repo doesn't report as 0.5:

   ```
   m_norm = max(0, Φ - 0.3) / 0.7
   ```

   The 30th percentile maps to 0, the 100th to 1.

4. The parameters `(μᵢ, σᵢ)` for all `i` are cached in
   `.entrolint.cache.json`. A manual recompute is `entrolint recalibrate`.

### A known weakness

The self-calibration is circular — a uniformly clean or uniformly dirty repo
flattens the signal. We accept this as a v0.1 limitation; v0.8 moves to a trained
baseline corpus.

### Why lognormal specifically

Empirically `cyclomatic`, `length`, `churn` are lognormally distributed (a long
right tail — a few files differ sharply from the bulk). The lognormal CDF
captures the tail better than linear normalization or a plain percentile.

## The `k` constant

The project's analogue of the Boltzmann constant — a **unit of temperature**
chosen so the numbers in the report read intuitively.

Calibration:

```
k = 1 / median(S_file_unscaled across the repo)
```

After this the median file of the repo gets `S ≈ 1.0`. Then the phrase "S > 2 is
hot" literally means "twice as dirty as the median", and the units don't need
explaining.

`k` is cached in `.entrolint.cache.json` together with `(μᵢ, σᵢ)`. It is
recomputed only by the `entrolint recalibrate` command.

## Default weights

Hardcoded constants in the binary, overridable via `.entrolint.yaml`:

| Microstate          | Default | Since |
| ------------------- | ------- | ----- |
| `cyclomatic`        | 1.0     | v0.1  |
| `nesting`           | 0.8     | v0.1  |
| `coupling`          | 0.6     | v0.3  |
| `length`            | 0.5     | v0.1  |
| `duplication`       | 0.7     | v0.3  |
| `cross_duplication` | 0.7     | v0.5  |

`churn` is absent from this table — it's not part of `S` (see below).

The v0.5 `cross_duplication` weight (0.7, mirroring `duplication`) was validated
by a calibration pass over several real Go repositories. Cross-file clone mass is
**not** heavier-tailed than intra-file `duplication` — across the sampled repos it
is consistently *smaller* — so a weight equal to `duplication`'s does not let it
dominate `ΔS` (and the per-microstate lognormal CDF absorbs the distribution
either way). The cross-file clones flagged on real codebases are genuine,
recognizable duplication, not coincidental structural matches. The weight may
still be re-tuned under v0.8's learned calibration.

## Temperature `T_file`

A fundamental architectural decision: **`churn` is not a microstate of `S`**. It
lives only in the temperature:

```
S_file = static structural entropy (cyclomatic + nesting + length + coupling + duplication + cross_duplication)
T_file = S_file · ξ(churn_count)
ξ(c)   = 1 + α · ln(1 + c),  α ≈ 0.5
```

Result: a file with no commits in 90 days gets `T = S` (cold). A file with 50
commits gets `T ≈ 3·S` (hot).

The `scan` command ranks refactoring candidates by **`T`**, not by `S`. The
mapping to physics is honest: `S` is the state of the system, `T` is how often it
gets perturbed. The refactoring budget goes to high `T`, not high `S`.

The specific value `α = 0.5` is a starting assumption, refined on dogfooding
data.

## `ΔS` for `check` mode

```
ΔS_total   = Σ_changed_files (S_head - S_base)
             + Σ_added_files   S_added
             - Σ_removed_files S_removed

ΔS_density = ΔS_total / max(1, lines_changed)
```

The churn factor `ξ` is **not applied** in `ΔS` — the gate asks about structure,
not about activity.

### The gate fires on density

```
fail if ΔS_density > ΔS_max     (default 0.05)
```

The reason: any large PR of clean code would fail on absolute `ΔS` purely on
volume. Density catches the real signal — "the new code is dirtier than what
exists".

The report shows **both numbers**: `ΔS_total` for human intuition, `ΔS_density`
for the actual fail/pass decision.

## Known simplifications (v0.1)

So as not to pretend everything is smooth:

1. **The microstates are not independent.** `cyclomatic` correlates with
   `length`, `nesting` with `cyclomatic`. An additive sum double-counts.
   PCA decorrelation or regression-fitted weights are v0.8.
2. **The self-calibration is circular.** A uniformly clean or uniformly dirty
   repo flattens the signal. Moving to a trained corpus is v0.8.
3. **The `+1` in `ln(1+x)`** at large weights blurs the difference between
   "0 problems" and "1 problem". It could be replaced with `ln(c+x)` for a
   larger `c`, but that adds another knob — deferred.
4. **The `churn` window is fixed at 90 days.** In projects with differing
   release cadences this works worse; auto-tuning to release cadence is v1.0.

## What changes in v0.8 ("Weight calibration")

In v0.8 the weights `wᵢ` and the shape of the normalization stop being fixed
constants and become a trained model.

- **The regression target** is future bug-fix commits on the file within a
  +90-day window (identified by regex-parsing commit messages: `fix:`, `bug:`,
  `hotfix:`). A secondary validation is PR revision count.
- **The default weights** are trained on a public corpus of Go repositories and
  compiled into the binary.
- **An optional per-repo fine-tune** — `entrolint train --local`, with the
  result written to `.entrolint.cache.json`.
- **The model family** starts with linear regression (the weights map directly
  onto `wᵢ`); then a small GBM with SHAP explanations for cross-microstate
  interactions. No black boxes — interpretability is load-bearing for the tool.

The architectural consequence for the v0.1 code: weights must be loaded from the
config as data, not used as magic numbers. Then moving to a trained model is a
change of source, not a refactor.
