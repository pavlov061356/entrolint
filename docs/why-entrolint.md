# Why entrolint

`entrolint` is not a replacement for ordinary Go linters. It answers a
different question.

Traditional linters mostly ask:

> Is this code locally suspicious?

`entrolint` asks:

> Where is maintainability pressure accumulating, and did this change make the
> system harder to evolve?

That makes it useful as an architectural diagnostic layer on top of the tools a
team should already run.

## What ordinary linters are good at

Tools such as `golangci-lint`, `staticcheck`, `govet`, `gocyclo`, and
SonarQube are strongest when a problem has a local shape:

- an unchecked error;
- a dead branch;
- a risky API call;
- a formatting or style mismatch;
- a function whose complexity crosses a fixed threshold;
- a security issue or dependency vulnerability.

Those checks are valuable because they are precise and actionable. If a linter
flags one line, the next step is usually clear.

## What entrolint adds

`entrolint` looks for a larger maintenance pattern. It combines structural
signals into an entropy score `S`, multiplies it by churn to produce
temperature `T`, and compares pull requests through `Delta S`.

The useful questions are therefore different:

- Which files are both complex and frequently changed?
- Did this PR raise or lower the repository's maintainability cost?
- Is the next similar change likely to stay local, or spread across many sites?
- Is duplication concentrated inside one file, or copied across files?
- Which hotspot should a team inspect first when it has limited refactoring
  budget?

The HTML heat map makes those questions visible: tile area is entropy `S`, tile
colour is temperature `T`, and the side panel shows which microstate dominates
each file.

## Comparison

| Tool family | Main question | Typical output | entrolint's role |
| ----------- | ------------- | -------------- | ---------------- |
| `golangci-lint`, `staticcheck`, `govet` | Is there a local bug, style issue, or suspicious construct? | Diagnostics on specific lines | Keep running them; entrolint starts after local correctness checks. |
| `gocyclo` and similar complexity checks | Is this function too branchy? | Per-function complexity threshold | entrolint includes cyclomatic complexity, but combines it with nesting, length, coupling, duplication, cross-file duplication, and churn. |
| Coverage and test reports | Which code is exercised by tests? | Package/file/function coverage | Complementary signal: coverage says how guarded code is, not where maintainability pressure accumulates. |
| SonarQube-style dashboards | What issues and code smells exist across the project? | Issue lists, quality gates, trend dashboards | entrolint is smaller and local-first: one binary, no telemetry, with a PR-focused `Delta S` gate and a Go-specific entropy model. |

## When entrolint is useful

It is most useful when a team already has basic linting and wants to reason
about maintainability debt:

- reviewers keep seeing the same "scary" files;
- refactoring budget is limited and the team needs a shortlist;
- a large Go repository has hotspots no single linter warning explains;
- duplicated structures are copied across packages;
- PRs are individually reasonable, but the system keeps getting harder to
  change;
- technical leaders need a concrete artifact for a refactoring discussion.

`scan` is the discovery mode: it finds hotspots across the repository.

`check` is the control loop: it keeps entropy growth visible on every PR.

## When not to use it as the only signal

Until v1.0, the formula and weights are intentionally unstable. Treat the score
as an advisory maintainability signal, not an absolute quality grade.

In particular:

- a high-entropy file is not automatically "bad"; generated code, parsers,
  protocol tables, and stable core packages can be legitimately dense;
- a low-entropy file is not automatically safe; correctness, security, and
  domain risk live outside this model;
- `Delta S` should not replace code review;
- the heat map is a starting point for inspection, not a verdict about a team or
  project.

The strongest use is interpretive: combine the score with ownership, product
context, recent incidents, and the team's own knowledge of the codebase.

## A practical diagnostic workflow

1. Run the usual Go linters and tests first.
2. Run `entrolint scan --html out/` and inspect the largest red tiles.
3. For each hotspot, check the dominant microstate: complexity, nesting, length,
   coupling, duplication, or cross-file duplication.
4. Compare the heat map against team intuition: which hotspots are surprising,
   and which confirm known pain?
5. Use `entrolint check --base origin/dev --head HEAD` on PRs to see whether
   entropy is rising or falling.
6. Turn the findings into a short diagnostic report rather than a raw metric
   dump.

For a reusable structure, see the
[diagnostic report template](diagnostic-report-template.md).

## Related docs

- [Formula](formula.md) explains how `S`, `T`, and `Delta S` are computed.
- [Scaling classes](scaling.md) explain the predictive PR-level signal.
- [HTML heat map](heatmap.md) explains the visualization.
- [Cross-file design](crossfile.md) explains the corpus pre-pass behind
  cross-file duplication.
