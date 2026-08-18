# Pre-1.0 calibration harness

`entrolint-calibrate` is a development-only command for the v1.0 formula freeze.
It does not change the released `entrolint` CLI and is not built by GoReleaser.

The command has two complementary modes:

- `balance` compares contribution shares on current checkout trees.
- `history` tests whether a pre-fix `S` ranking places files from later
  corrective commits above untouched files.

Both modes reuse the production Go parser, microstates, normalization, and
ranking path. Candidate files use the `.entrolint.yaml` schema; omitted weights
inherit the compiled defaults.

Both modes include the compiled weights as candidate `default` unless
`--include-default=false` is passed. Excluding the default requires at least one
`--candidate`.

## Balance mode

Run the current defaults on one or more local repositories:

```bash
go run ./cmd/entrolint-calibrate balance /path/to/repo-a /path/to/repo-b
```

The original command form without the explicit `balance` subcommand remains
supported:

```bash
go run ./cmd/entrolint-calibrate /path/to/repo-a /path/to/repo-b
```

Compare candidate config files:

```bash
go run ./cmd/entrolint-calibrate balance \
  --candidate tuned=/tmp/tuned.yaml \
  --candidate no-cross=/tmp/no-cross.yaml \
  /path/to/repo-a /path/to/repo-b
```

Each candidate reports:

- `files` — number of analyzed Go files.
- `total S`, `median S`, `p90 S` — entropy distribution after per-repo
  calibration.
- `dominant` — the microstate that is the largest contributor most often.
- one percentage per microstate — its share of aggregate `S`.

This is a contribution-balance audit, not an outcome model. Equal contribution
shares are not a goal by themselves.

## Corrective-history mode

Run the bounded default protocol:

```bash
go run ./cmd/entrolint-calibrate history \
  --candidate tuned=/tmp/tuned.yaml \
  --format json \
  /path/to/repo-a /path/to/repo-b > history-validation.json
```

Selection can be inspected or changed with `--ref`, `--search-limit`,
`--samples-per-repo`, `--max-changed-files`, and `--subject-regexp`.

The default protocol is:

1. Search the 500 most recent commits reachable from `HEAD`.
2. Match likely corrective subjects with
   `(?i)\b(fix|fixes|fixed|bug|bugfix|revert|reverted)\b`.
3. Keep single-parent commits with 1–10 pre-existing changed Go paths accepted
   by entrolint's analyzer path filter.
4. Exclude added-only files and pure renames. For an edited rename, label the
   old path from the parent tree.
5. Select the latest 10 eligible commits per repository.
6. Resolve the repository's current `HEAD` once and calibrate each candidate on
   that committed Git tree. Score every selected commit's parent tree in this
   fixed frame. `--ref` controls the searched history; it does not change the
   calibration frame.
7. Treat the successfully analyzed parent-tree files modified, removed, or
   edited-renamed by the corrective commit as positive labels. All other scored
   Go files are negatives. If any selected label cannot be analyzed in the
   parent tree, retain an auditable skipped record instead of scoring a partial
   label set.

The corrective commit's tree is never scored as the evaluation sample, so its
repaired contents are not used directly as predictors. The normalization frame
is nevertheless fitted on the resolved current `HEAD`, which postdates the
historical samples and can contain information from later repository evolution.
This is a fixed future-fitted frame, not a leakage-free estimate of prospective
performance. Dirty and untracked working-tree files do not affect it.

The report contains:

- `mean AUC` — probability that a matched labeled file ranks above an untouched
  scored file; random ranking has expected AUC 0.5. Each scored commit has equal
  weight.
- `top 10 recall` and `top 20 recall` — share of matched labeled files captured
  in the highest-scoring `ceil(0.10 × N)` and `ceil(0.20 × N)` files, where `N`
  is the number of scored files in that commit. Under random ranking the
  per-commit expected baselines are therefore `ceil(0.10 × N) / N` and
  `ceil(0.20 × N) / N`, approaching 10% and 20% for large `N`.
- `median positive percentile` — median rank percentile of matched labeled
  files; 100% is the top of the ranking.

Ties receive fractional credit at the top-k boundary and 0.5 credit in AUC.
Aggregate recall, its random baseline, and the median percentile pool matched
labeled files across commits.

JSON records the resolved `frame_sha`, effective candidate `weights`, original
`labels`, `matched_labels`, `unmatched_labels`, and whether each selected commit
was `scored`. A skipped record includes `skip_reason`; it remains in `commits`
but is excluded from `summary`. Thus `selected_commits` can be larger than
`summary.commits` without hiding why.

## Decision rule

The current defaults remain the v1.0 baseline unless another interpretable
candidate produces a reproducible outcome improvement. Contribution balance
alone is insufficient, and differences small enough to disappear across
repositories do not justify shifting public `S` values.

## Initial balance run

The first expanded balance run, 2026-07-08, covered 7,679 Go files in 12 public
repositories. Aggregate result for the current defaults:

| files | total S | median S | p90 S | dominant | cyclomatic | nesting | coupling | length | duplication | cross_duplication |
| ----: | ------: | -------: | ----: | -------- | ---------: | ------: | -------: | -----: | ----------: | ----------------: |
| 7679 | 9768.000 | 1.000 | 2.828 | cyclomatic 30.9% | 21.5% | 19.0% | 17.1% | 15.5% | 11.6% | 15.4% |

No microstate dominated aggregate `S`, so this pass supplied no reason to
change the defaults without stronger outcome evidence.

## Bounded history validation

The 2026-07-17 run used clean checkouts at the following pinned heads. The
protocol then found 105 eligible commits and 221 labeled files. Its clean working
trees contained the same files as their committed `HEAD` trees, so moving the
calibration source from disk to the committed tree does not change those frame
inputs. The recorded run nevertheless predates the current report schema:
`frame_sha`, exact random-baseline fields, and the strict rule that skips a whole
commit when any label is unmatched were added later. The figures below have not
been regenerated under that stricter audit rule; they are the historical evidence
used for the weight decision, not a fresh result from the current implementation:

| Repository | HEAD | Commits | Labels |
| ---------- | ---- | ------: | -----: |
| entrolint | `e501540` | 5 | 29 |
| spf13/cobra | `adbc881` | 10 | 16 |
| gin-gonic/gin | `34dac20` | 10 | 20 |
| go-chi/chi | `8b258c7` | 10 | 20 |
| caddyserver/caddy | `986753a` | 10 | 19 |
| go-git/go-git | `5f90b84` | 10 | 29 |
| uber-go/zap | `5b81b37` | 10 | 18 |
| prometheus/client_golang | `78262a7` | 10 | 18 |
| stretchr/testify | `001eb79` | 10 | 21 |
| gorilla/mux | `db9d1d0` | 10 | 17 |
| redis/go-redis | `cdae48b` | 10 | 14 |

Three interpretable candidates were compared:

| Candidate | cyclomatic | nesting | coupling | length | duplication | cross_duplication |
| --------- | ----------: | ------: | -------: | -----: | ----------: | ----------------: |
| default | 1.00 | 0.80 | 0.60 | 0.50 | 0.70 | 0.70 |
| equal | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| share-balanced | 0.78 | 0.70 | 0.58 | 0.54 | 1.00 | 0.76 |

`share-balanced` was derived from the initial contribution shares. On the
2,014-file history corpus it narrowed the contribution-share range from
12.6–22.1% to 15.6–18.1%, so it did achieve its intended balance objective.

History outcomes:

| Candidate | Mean AUC | Top-10 recall | Top-20 recall | Median positive percentile |
| --------- | -------: | ------------: | ------------: | -------------------------: |
| default | 0.7586 | 35.3% | 52.5% | 82.0% |
| equal | 0.7586 | 36.2% | 52.5% | 81.6% |
| share-balanced | 0.7562 | 36.2% | 52.5% | 81.3% |

At repository level, mean paired AUC difference was effectively zero:

- default minus equal: −0.00014, approximate 95% interval
  [−0.00171, 0.00142].
- default minus share-balanced: +0.00254, approximate 95% interval
  [−0.00114, 0.00621].

The intervals are descriptive across 11 repository means, not a claim of
population-level statistical significance.

**Decision:** retain the current default weights for v1.0. All candidates rank
corrective files far above random, but neither alternative shows a meaningful,
stable improvement. Changing public scores to gain contribution symmetry would
therefore add compatibility cost without demonstrated outcome value.

## Limitations

- Commit-message matching is transparent but noisy: the sample includes
  comment, lint, and test fixes as well as runtime bugs, races, leaks, and
  regressions.
- Every changed pre-existing Go path accepted by the analyzer filter is selected
  as a candidate label. Co-changed support and test files are associations, not
  proof that structural entropy caused the defect. Samples with any unparseable
  or otherwise unscored label are reported but excluded from metrics.
- New files, merge commits, changes above the file-count bound, and corrective
  commits outside the recent search window are excluded.
- Historical trees use normalization fitted on the resolved current `HEAD` Git
  tree. This keeps one frame within a repository and ignores working-tree
  dirtiness, but it is future-fitted and can carry survivorship effects when
  repository structure changes substantially.
