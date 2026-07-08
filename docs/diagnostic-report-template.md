# Maintainability diagnostic report template

This template turns `entrolint` output into a short engineering report. It is
designed for a repository walkthrough, a technical-lead discussion, or a
pre-refactoring review.

It is intentionally not a scorecard for ranking projects. A high-entropy file can
be justified; the goal is to find where maintainability pressure concentrates
and decide what, if anything, deserves attention.

For a filled demonstration, see the
[diagnostic examples](examples/README.md).

## Report metadata

- Repository:
- Commit / ref:
- Date:
- entrolint version:
- Config path:
- Scope:
- Reviewer:

## Executive summary

Write 3-5 bullets. Keep them concrete and falsifiable.

- The main maintainability pressure is concentrated in:
- The hottest file/package is:
- The most important dominant microstate is:
- The clearest refactoring candidate is:
- The main caveat is:

## Commands run

```bash
entrolint scan --top 20
entrolint scan --format json > entrolint-scan.json
entrolint scan --html entrolint-heatmap/
```

For PR-oriented diagnostics:

```bash
entrolint check --base origin/dev --head HEAD
entrolint check --base origin/dev --head HEAD --format json > entrolint-check.json
```

## Repository-level snapshot

- Total analyzed Go files:
- Total entropy `S`:
- Hottest package:
- Hottest file:
- Dominant repository-level theme:
- Notable skipped paths:

Interpretation:

> One short paragraph. Explain what the numbers suggest, but do not over-claim.
> Tie the metric back to maintainability: change cost, review friction,
> duplication, ownership, or refactoring budget.

## Heat map observations

Attach or link the generated `entrolint-heatmap/index.html`.

### Pattern 1: large and hot

- File/package:
- Why it stands out:
- Dominant microstate:
- Team context to verify:
- Possible next step:

### Pattern 2: large but cool

- File/package:
- Why it stands out:
- Dominant microstate:
- Why it may be acceptable:
- Possible next step:

### Pattern 3: small but hot

- File/package:
- Why it stands out:
- Dominant microstate:
- Recent churn explanation:
- Possible next step:

## Top hotspots

| Rank | Path | S | T | Dominant | Interpretation | Suggested next step |
| ---- | ---- | --- | --- | -------- | -------------- | ------------------- |
| 1 | | | | | | |
| 2 | | | | | | |
| 3 | | | | | | |
| 4 | | | | | | |
| 5 | | | | | | |

Use this table as a shortlist, not as a verdict. The best refactoring target is
usually a hotspot that is both technically dense and important to current work.

## Microstate breakdown

### Cyclomatic complexity

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

### Nesting

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

### Length

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

### Coupling

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

### Duplication

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

### Cross-file duplication

- Where it appears:
- Why it matters:
- Example to inspect:
- Recommendation:

## PR gate view

Use this section when the diagnostic is attached to a branch or pull request.

- Base:
- Head:
- Verdict:
- `Delta S_total`:
- `Delta S_density`:
- Threshold:
- Scaling class:
- Scaling class max:
- Lines changed:
- Files changed:

Interpretation:

> Explain whether the PR raises entropy, lowers entropy, or stays neutral.
> Mention scaling-class hits separately from `Delta S`; they describe future
> change cost, not the same thing as current structural entropy.

## Findings

### Finding 1

- Evidence:
- Why it matters:
- Confidence:
- Suggested action:
- Expected benefit:
- Risk / caveat:

### Finding 2

- Evidence:
- Why it matters:
- Confidence:
- Suggested action:
- Expected benefit:
- Risk / caveat:

### Finding 3

- Evidence:
- Why it matters:
- Confidence:
- Suggested action:
- Expected benefit:
- Risk / caveat:

## Recommended next steps

Keep this list small. Prefer work that can be reviewed and validated.

1. First action:
2. Second action:
3. Third action:

## Follow-up checks

- Re-run `entrolint scan --html` after the change.
- Compare `entrolint check --base <before> --head <after>`.
- Confirm that the refactor did not only move entropy elsewhere.
- Check tests, benchmarks, and product risk separately.

## Caveats

- `entrolint` is advisory until v1.0; formula weights may change.
- A high score is not a quality ranking.
- Generated code, stable protocol tables, parsers, and migration-heavy files may
  need special interpretation.
- Metrics should be paired with code ownership, incident history, and team
  knowledge.
