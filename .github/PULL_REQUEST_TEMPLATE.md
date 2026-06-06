<!--
Thanks for the PR. Keep the title in Conventional Commits style:
  feat(<scope>): <subject>     fix(<scope>): <subject>     docs: <subject>
-->

## Summary

<!-- One paragraph: what does this PR do, and why? -->

## Linked issue

<!-- "Closes #123" or "Refs #123". Skip if this is a self-initiated
     improvement with no tracking issue. -->

## Type

- [ ] feat — new user-visible behavior
- [ ] fix — bug fix
- [ ] refactor — code cleanup, no behavior change
- [ ] docs — documentation only
- [ ] chore — tooling, CI, dependencies

## Formula impact

Does this PR change the value of `S`, `T`, or `ΔS` for any file?

- [ ] No — pure refactor / docs / tooling.
- [ ] Yes — describe below and attach `entrolint scan --top 10` before
      and after on a representative repo (the entrolint repo itself
      is a fine target).

<!--
If yes:
- Which microstate(s) are affected?
- Direction of the shift (S goes up / down for which kind of files)?
- Is the default `delta_s_max` still sensible after the shift?
- Did the [Unreleased] CHANGELOG entry land under `### Changed` with
  the "Breaking" prefix?
-->

## Checklist

- [ ] `make ci` passes locally (lint + race tests).
- [ ] Tests cover the new behavior (new microstate / detector → unit
      tests AND a pipeline-level integration test).
- [ ] `CHANGELOG.md` has an entry under `## [Unreleased]`.
- [ ] If the PR changes the formula or weights:
      [docs/formula.md](docs/formula.md) is updated.
- [ ] If the PR adds or changes a scaling detector:
      [docs/scaling.md](docs/scaling.md) is updated.
- [ ] Dogfood: `entrolint check --base dev --head HEAD` passes the
      local gate from this branch.
- [ ] No new network I/O, telemetry, or auto-update channels.
