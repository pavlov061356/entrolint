# `entrolint` — HTML heat map (v0.6)

`entrolint scan --html <dir>` writes a single self-contained `index.html`: a
treemap of the whole repository that turns the `scan` numbers into a picture you
can hand to the team. This document records what it shows and why.

## What you see

A **squarified treemap** (Bruls/Huizing/van Wijk) of every analysed file, with
files nested into the directory (package) regions they belong to. Two channels
encode the metric:

- **Area = entropy `S`.** A file's rectangle is sized by its structural entropy,
  so the heavy files are literally big. A directory's block is the sum of its
  files' `S` — the package's total entropy mass.
- **Colour = temperature `T`** on a green→red ramp (cool→hot). The upper bound is
  the 95th percentile of `T`, so a single runaway-hot file doesn't wash out the
  gradient.

Because `T = S · ξ(churn)`, **area and colour are decoupled**: a big *cool* tile
is complex but stable; a small *hot* tile is simple but volatile; a big *red*
tile is the refactor target — exactly where `scan` says the budget should go.

Each tile carries the file name (and an `S · T` line where it fits); a tile too
small for a label keeps a hover tooltip. Clicking a tile opens a side panel with
its `S`, `T`, dominant microstate, and a per-microstate breakdown — the same
`cyclomatic … cross_duplication` contributions the table mode prints.

## Properties

- **Self-contained & hermetic.** CSS, JavaScript, and the data are all inlined —
  no CDN, no network. It works offline and leaks nothing, matching the engine's
  no-telemetry stance. Drop it in a CI artifact or open it from disk.
- **Deterministic.** No embedded timestamps and a stable ordering, so the output
  is byte-stable for a fixed input — safe to diff and to publish.
- **Whole repo.** `--html` renders every file, not just `--top` (a terminal-table
  convenience). The directory padding (`dirPad`) draws the gaps between package
  regions.

## Architecture

The heat map is a **report-layer** feature (`internal/report/html.go` +
`html.tmpl`), built from the existing `[]pipeline.FileScore` that `scan` already
produces — no engine change. The squarified layout is computed in Go and emitted
as SVG; the only JavaScript is the click-to-drill-down panel.

## Not yet: the phase portrait

The ROADMAP's second visualization — a **phase portrait**, an `S(t)` curve over
git history — is **deferred to v0.6.1**. It needs a multi-commit replay (scan at
each point in a window), a distinct capability from the single-scan treemap, so
it ships on its own rather than holding up the heat map. The v0.6.1 design is
documented in [phase-portrait.md](phase-portrait.md).
