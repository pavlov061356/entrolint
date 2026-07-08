# `entrolint` — phase portrait data (v0.6.1)

`entrolint history` samples recent git commits and emits the data behind the
phase portrait: total structural entropy `S` over time.

```bash
entrolint history --limit 30
entrolint history dev --limit 50 --format json > entropy-history.json
entrolint history dev --limit 50 --html entropy-history/
```

The command does not check out old commits. It reads commit metadata with
`git log`, enumerates each tree with `git ls-tree`, fetches blobs with
`git cat-file`, and scores every analyzable Go file in memory.

## What it shows

The default table output is:

```text
SHA      DATE        FILES  S      SUBJECT
2386edb  2026-06-14  78     84.55  merge release hardening
```

- **SHA** is the short commit hash.
- **DATE** is the commit date.
- **FILES** is the number of parsed Go files at that commit.
- **S** is the total structural entropy for the whole tree.
- **SUBJECT** is the commit subject from git.

JSON output returns the same points under a stable envelope:

```json
{
  "ref": "dev",
  "points": [
    {
      "sha": "2386edb...",
      "short_sha": "2386edb",
      "commit_time": "2026-06-14T12:00:00+03:00",
      "subject": "merge release hardening",
      "s": 84.55,
      "file_count": 78
    }
  ]
}
```

`--html <dir>` writes a self-contained `index.html` phase portrait: an SVG line
chart for total `S(t)` plus the same commit table. The file has inline CSS only,
no JavaScript, no CDN, and no network access.

The X axis is a real commit-time scale. Commits are the only points; periods
without commits are shown as horizontal gaps, not as synthetic zero-change dots.

## Calibration frame

For the points to be comparable, `history` calibrates the entropy engine once on
the current working tree (`--root`, default `.`) and then scores every historical
tree in that same frame.

This is intentional: calibrating every commit against its own tree would make
each point use a different ruler. The trade-off is that older commits are judged
through today's calibration curve, which is good enough for a phase portrait and
consistent with the current pre-1.0 formula stance.

## Defaults and flags

- `--limit 30` samples the 30 most-recent commits.
- `--first-parent=true` follows the mainline branch history by default.
- `--format table|json` chooses human-readable or machine-readable output.
- `--html out/` writes a self-contained phase portrait to `out/index.html`.
- `--config` and `--recalibrate` follow the same calibration behavior as `scan`.
- `--root` sets the working tree used for calibration and git command anchoring.
