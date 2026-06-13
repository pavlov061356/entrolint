# `entrolint` — cross-file analysis design (v0.5)

This document is the canonical design record for entrolint's **cross-file
engine**: the shared whole-tree pre-pass that lets a microstate see beyond a
single file, and the first metric built on it — **cross-file duplication**. It
covers the v0.5 release series; before 1.0 the metric, weights, and the staged
boundary below are considered unstable.

## Decision

v0.5 ships **one** real cross-file metric — repo-wide structurally-identical AST
duplication (`cross_duplication`) — on a new blob-corpus pre-pass. The full
Robert-Martin `Ca/Ce/instability` coupling graph is **deferred and staged**, not
abandoned: the same pre-pass infrastructure makes it an additive follow-up.

This re-sequences the original [ROADMAP](ROADMAP.md) v0.5 ("cross-file coupling
**and** duplication"). The reason is below.

## Why duplication first, coupling deferred

The v0.3 microstates are per-file MVPs (see
[formula.md](formula.md#microstates)):

- `coupling` = `len(f.AST.Imports)` — a per-file import count.
- `duplication` = intra-file AST-subtree clone mass.

Both were deferred to "a whole-tree pre-pass on both refs in `check`". That
pre-pass is what v0.5 builds. But the two metrics have very different
requirements once you try to cross file boundaries:

- **Cross-file duplication needs no type information.** A copy-pasted block is a
  structural fact: the existing intra-file clone kernel
  (`internal/engine/microstate/duplication.go`) already computes a position-
  independent FNV-1a Merkle digest over each AST subtree, with identifier and
  literal normalization (Type-2 clones). That digest is **globally comparable** —
  the same hash that finds a clone inside one file finds it across files, with no
  algorithm change. All it needs is the ASTs of the whole tree.

- **Real coupling genuinely wants types.** A Martin-style `Ca/Ce` graph needs a
  package-level import graph with intra-module-vs-external classification —
  `go.mod` resolution or a full `packages.Load`. `packages.Load` (the
  `internal/scaling/typesx` loader) is **working-tree-only**: it shells out to
  `go list`, which reads real files on disk. In `check`, the base ref is **not**
  checked out. A true base-ref package graph would force materializing the base
  tree into a temporary `git worktree`, plus a second full type-check — roughly
  doubling `check` latency and injecting go-toolchain / `GOPROXY` /
  module-resolution failures into a previously hermetic engine path.

So v0.5 ships the metric whose syntactic pre-pass is genuinely sufficient, and
defers the metric that genuinely wants types — keeping the engine type-info-free
for one more release while building the infrastructure coupling will reuse.

A second, decisive property: `cross_duplication` is **additive**. The v0.3
`duplication` and `coupling` microstates are left untouched; the new microstate
counts only clone classes spanning more than one file. v0.5 therefore cannot
regress the existing `S` signal.

## The corpus pre-pass

A new package `internal/engine/corpus` produces a `Context` carrying one artifact
in v0.5: a cross-file clone index. It is built from a git ref **without checking
the ref out**:

1. **Enumerate the whole tree at a ref.** A new `gitx` helper
   `TreeFiles(runner, ref)` runs `git ls-tree -r --name-only -z <ref>` and filters
   with `golang.IsAnalyzablePath`, so the corpus mirrors the calibration walk's
   `vendor/` and dot-dir exclusions exactly.
2. **Bulk-fetch every blob in one subprocess.** A new helper
   `BlobsAtRef(runner, ref, paths)` runs a single `git cat-file --batch`, feeding
   `<ref>:<path>` lines on stdin and parsing the `<sha> blob <size>\n<bytes>`
   records. One process per ref — not one `cat-file` per file. This is the
   dominant performance lever; the existing `check` path only ever fetched the
   blobs of *changed* files (`FileAtRef`, one fork each).
3. **Parse each blob.** `golang.ParseGoBytes` (already used by `check` for changed
   blobs) turns each blob into a `microstate.File` — the same scorable shape
   `analyzeTree` produces from disk, just sourced from blobs. No I/O, no
   `go/types`.
4. **Build the clone index.** A new exported `microstate.CloneIndex(files)` walks
   every file's AST with the existing private kernel (`dupSubtrees`, `dupHash`,
   `dupTag`, `dupMinNodes=12`, `dupNested`) and groups `(hash → {count, size,
   members})` across **all** files.
5. **Attribute mass back per file.** `CrossDupMass(path)` charges a file the
   size-weighted redundant mass of the cross-file clone classes it participates
   in (every copy past a deterministic first pays; nested clones suppressed).

### Running on both refs in `check`

`check` builds **two** corpora — `corpus.Build(runner, baseSHA)` and
`corpus.Build(runner, headSHA)` — each from its own ref's blobs. The base-side
file gets the base corpus, the head-side file the head corpus, before
`engine.Score`. Because the index is rebuilt at **both** SHAs, `ΔS` for
`cross_duplication` is real, not a head-only approximation. `scan` reuses the
same `CloneIndex` over `analyzeTree`'s on-disk `[]File` — no git needed.

### Base-ref strategy

This is the constraint the pre-pass exists to solve. The base ref is never
checked out, but cross-file duplication needs only ASTs — so the base tree is
reconstructed in memory from git objects: `ls-tree` reads the tree object,
`cat-file --batch` streams every base blob, `ParseGoBytes` parses each. Each ref
is enumerated by its **own** paths, so renames need no special handling at the
corpus level (the base corpus is keyed by base-side names). A base blob that
fails to parse is dropped (matching `analyzeTree`'s silent-skip and `check`'s
symmetric soft-miss); a parse-broken base tree yields a smaller index, never a
crash.

## The `cross_duplication` microstate

- **Kernel reuse, zero behavioral change.** `CloneIndex` lifts the
  `duplication.go` kernel verbatim. `dupMinNodes=12` (≈3–5 lines) is inherited, so
  `if err != nil { return err }` stays out of the signal.
- **No double-count.** A clone class is *cross-file* iff it spans ≥2 distinct
  files. `cross_duplication` counts only those; same-file mass stays owned by the
  unchanged `duplication` microstate. The two partition the clone space.
- **Pure single-arg `Measure`.** `CrossDuplication.Measure(f)` is
  `f.Corpus.CrossDupMass(f.Path)` with a nil-corpus guard returning 0, so a `File`
  scored without a corpus (a unit test) contributes 0 and never panics. The
  corpus is attached to `microstate.File` exactly as `ChurnCount` is — the
  analyzer/pipeline pre-computes a cross-cutting value; `Measure` stays a pure
  function and the two-method `Microstate` interface is unchanged.
- **Auto-participation.** Registered in `structuralMicrostates()` with a default
  weight (`cross_duplication: 0.7`, mirroring `duplication`). Normalization,
  `k`-scaling, and weighting are keyed by `Name()`, so the new microstate joins
  `S` automatically. The calibration cache auto-invalidates (`HasAll` sees a new
  `Name()` absent) — no schema bump.

## The `CorpusContext` seam: staged coupling

The pre-pass is deliberately a one-artifact struct so coupling slots in **without
re-plumbing**:

- **v0.6 — import-graph-lite.** `corpus.Build` gains a second artifact, a package
  import graph computed from the **same** blob corpus the clone index already
  parses: `Ce` = distinct imported packages, `Ca` = inbound count, instability
  `I = Ce/(Ca+Ce)`, with intra-module-vs-external classification by import-path
  string-matching against the module path read from `<ref>:go.mod`. No
  `go/types`, no worktree — reuses the identical base+head pre-pass.
- **v0.7+ — typed package graph.** Interface-satisfaction-aware, promoted-method
  coupling — the tail that finally justifies materializing a base worktree and a
  second type-check.

So v0.5 ships the infrastructure that makes coupling a follow-up of bounded
scope, not a rewrite.

## Implementation plan

1. **`gitx` whole-tree-at-ref primitives** — `TreeFiles` (`ls-tree -r
   --name-only -z`) and `BlobsAtRef` (single `cat-file --batch`); soft-miss on
   malformed/`missing` records; `LC_ALL=C` pinned as `LocalRunner` already does.
   Fake-`Runner` tests plus a real run against this repo.
2. **Attribution interface in `microstate`** — add a small `CrossFileSource`
   interface (`CrossDupMass(path string) float64`) and a nil-safe `Corpus` field
   on `File`; export `CloneIndex` + `CloneClass`, keep the `dup*` kernel private
   and shared. (This breaks the import cycle up front: `corpus` imports
   `microstate`, so `File` must hold an interface, not a concrete `corpus` type.)
3. **`internal/engine/corpus` package** — `Context` implementing
   `CrossFileSource`; `Build(runner, ref)` for `check`, `BuildFromFiles([]File)`
   for `scan`; per-file `CrossDupMass` attribution with deterministic first-copy
   ordering (sort by path, then pos) applied identically at both refs.
4. **`cross_duplication` microstate** — counts only cross-file mass; nil-corpus
   guard; test asserting a block copy-pasted between two files yields mass on the
   second file with no intra-file double-count.
5. **Register + weight** — append to `structuralMicrostates()`; add
   `cross_duplication: 0.7` to `config.Default()`.
6. **Attach corpus on the `scan` path** — set `f.Corpus` on every file **before**
   the slice reaches `thermo.Calibrate`/`Score`. Load-bearing: calibration must
   see real per-file mass or the lognormal fits on all-zeros.
7. **Attach base/head corpora on the `check` path** — build both corpora; attach
   base to the base-side `File` (keyed by `OldPath` for renames) and head to the
   head-side `File`; ensure `calibrateForCheck`'s working-tree files also get the
   head corpus. Check-level test: a head tree that differs from base by an
   introduced cross-file clone yields positive `ΔS`.
8. **Docs** — supersede the deferral notes in [formula.md](formula.md#microstates)
   and the `structuralMicrostates()` comment; record the staged coupling boundary.

## Risks

- **Calibration-frame mismatch (highest).** If the working-tree corpus is not
  attached before `thermo.Calibrate` in **both** `scan` and `calibrateForCheck`,
  `cross_duplication` calibrates on all-zeros and silently contributes nothing.
  Steps 6 and 7 must land together, with an assertion that the calibration corpus
  has a non-nil `Corpus`.
- **Base-side path keying.** The base corpus is keyed by base-side `ls-tree`
  paths; `check`'s scoring loop uses head-side `c.Path`. For renames the base
  lookup must use `basePath(c) = c.OldPath`, or renamed files mis-pair and
  manufacture spurious `ΔS`.
- **Import cycle.** `microstate.File` cannot hold a concrete `corpus` type; the
  `CrossFileSource` interface (step 2) must land first.
- **Latency on large repos.** `check` now does two whole-tree blob fetches +
  parses on top of the head calibration walk. `cat-file --batch` (one subprocess
  per ref) and parse-only (no type-check) keep it cheap at entrolint scale, but a
  large monorepo is real PR-gate cost — measure before assuming negligible. A
  changed-package-only or size-capped corpus is a deferred optimization.
- **Soft-miss asymmetry.** A file present at only one ref still shifts other
  files' clone mass at that ref, so a one-sided parse failure can nudge `ΔS` for
  unrelated changed files. Same class of imprecision the single head-fit engine
  already tolerates; document it, and reuse `check`'s symmetric-drop rule for the
  changed file itself.

## Open questions

- **Weight / floor — resolved.** A calibration pass over several real Go
  repositories validated `0.7` (mirroring `duplication`). Contrary to the original
  worry, cross-file clone mass is **not** heavier-tailed than intra-file mass — it
  is consistently smaller — so an equal weight does not let it dominate `ΔS`, and
  the flagged cross-file clones are genuine, recognizable duplication (high
  precision). The weight may be re-tuned under v0.8's learned calibration.
- **De-duplication as a reward.** When a PR extracts a shared helper (the intended
  reward), the canonical first copy may shift files between base and head. Confirm
  the deterministic `(path, pos)` ordering + symmetric base/head computation
  yields a net negative `ΔS` for a de-dup PR — and decide whether de-dup should be
  a first-class refactor reward (like scaling's downgrade bonus) or stay emergent
  from `ΔS`.
- **Double-count guarantee.** Confirm the `>1-distinct-file` class filter alone
  guarantees no overlap with intra-file `duplication` (same-file-only classes →
  `duplication`; multi-file classes → `cross_duplication`).
- **Reporting.** Surface `cross_duplication` in `scan`'s per-file contribution
  breakdown and/or list the clone pairs (which files share a block)? The index has
  the member paths for free. Tracked in #59 — the calibration pass showed the clone
  pairs are recognizable and actionable, so this is a strong candidate for v0.5.
- **Large-repo deferral trigger.** Decide the repo size at which the
  whole-tree-at-both-refs fetch needs the changed-package-only optimization.

## Out of scope for v0.5

All coupling work (the v0.3 per-file import-count proxy stays); any type-checked
analysis in the engine path; a configurable `dupMinNodes`; per-package corpus
optimization; cross-language (TypeScript) corpus. The `Context` is intentionally a
one-artifact struct so each of these is an additive change, not a redesign.
