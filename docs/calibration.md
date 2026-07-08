# Pre-1.0 calibration harness

`entrolint-calibrate` is a development-only command for the v1.0 formula freeze.
It does not change the released `entrolint` CLI and is not built by GoReleaser.

The command compares candidate `.entrolint.yaml` weight sets over one or more
local checkout roots. It reuses the production `pipeline.Scan` path with cache
disabled, then reports how much each microstate contributes to total `S`.

## Usage

Run the current defaults on one or more local repositories:

```bash
go run ./cmd/entrolint-calibrate /path/to/repo-a /path/to/repo-b
```

Compare candidate config files:

```bash
go run ./cmd/entrolint-calibrate \
  --candidate tuned=/tmp/tuned.yaml \
  --candidate no-cross=/tmp/no-cross.yaml \
  /path/to/repo-a /path/to/repo-b
```

The compiled defaults are included as the `default` baseline unless
`--include-default=false` is passed.

Emit JSON for later analysis:

```bash
go run ./cmd/entrolint-calibrate --format json /path/to/repo > calibration.json
```

Candidate files use the same schema as `.entrolint.yaml`; omitted weights inherit
the compiled defaults.

## What the report means

Each candidate produces:

- `files` — number of analyzed Go files.
- `total S`, `median S`, `p90 S` — entropy distribution after the candidate
  weights and per-repo self-calibration.
- `dominant` — the microstate that is the largest contributor most often.
- one percentage column per microstate — share of total `S` contributed by that
  microstate.

The first pass is a contribution-balance audit, not a learned model. A useful
candidate should not make one microstate dominate every repository unless that is
an intentional formula decision. The final v1.0 decision still needs a corpus
choice and, if weights change, a justification tied to maintainability outcomes
such as bug-fix or revert history.

## Current decision rule

Before v1.0, use this harness to decide between two explicit outcomes:

1. Keep the current default weights and document them as the v1.0 baseline.
2. Change default weights, document the corpus and rationale, and treat the
   resulting `S` shift as part of the formula freeze.

## Initial expanded smoke run

First expanded run, 2026-07-08, shallow HEAD checkouts:

| Repository | SHA | Files |
| ---------- | --- | ----: |
| entrolint | `ebd5c4b` | 89 |
| spf13/cobra | `ad460ea` | 36 |
| gin-gonic/gin | `34dac20` | 99 |
| go-chi/chi | `8b258c7` | 78 |
| gohugoio/hugo | `a198116` | 901 |
| grafana/k6 | `c0bc819` | 817 |
| caddyserver/caddy | `4e62095` | 322 |
| etcd-io/etcd | `d6ff4aa` | 1106 |
| prometheus/prometheus | `39359eb` | 725 |
| golangci/golangci-lint | `9b5e24c` | 1098 |
| hashicorp/terraform | `f7f5f16` | 1981 |
| kubernetes/kubectl | `884d276` | 427 |

Aggregate result for the current default weights:

| files | total S | median S | p90 S | dominant | cyclomatic | nesting | coupling | length | duplication | cross_duplication |
| ----: | ------: | -------: | ----: | -------- | ---------: | ------: | -------: | -----: | ----------: | ----------------: |
| 7679 | 9768.000 | 1.000 | 2.828 | cyclomatic 30.9% | 21.5% | 19.0% | 17.1% | 15.5% | 11.6% | 15.4% |

Interpretation: on this corpus the current defaults are reasonably balanced by
total contribution. No microstate dominates total `S`; `duplication` is the
lowest share, but still above 10%. The main repo-level outlier is
`golangci-lint`, where `coupling` is dominant most often. This is not enough to
freeze the weights by itself, but it argues against changing them without a
stronger bug-fix/revert-history signal.
