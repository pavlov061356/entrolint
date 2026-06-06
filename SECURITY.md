# Security Policy

## Supported versions

`entrolint` is pre-1.0. Only the **latest minor release** receives
security fixes. Older versions are not patched — users on `v0.x.y`
should upgrade to the current `v0.X` line.

| Version | Supported          |
| ------- | ------------------ |
| 0.3.x   | :white_check_mark: |
| < 0.3   | :x:                |

## Reporting a vulnerability

**Do not open a public GitHub issue for security reports.** Use GitHub's
private vulnerability reporting:

➡️ <https://github.com/pavlov061356/entrolint/security/advisories/new>

A report should include:

- `entrolint --version` and the Go version it was built with.
- A minimal reproducer or the exact command and inputs that trigger the
  issue.
- Your expectation of the impact (RCE, DoS, information disclosure,
  supply-chain risk in vendored dependencies, etc.).

You will get an acknowledgement within **5 business days**. Fixes are
best-effort — `entrolint` is a single-author project, not a funded
security team. Critical fixes ship as a patch release plus a security
advisory; low-severity issues may be batched into the next regular
minor.

## Threat model & scope

`entrolint` is a **local static-analysis CLI**. The threat model is
intentionally narrow:

**In scope:**

- Vulnerabilities in `entrolint` itself that allow arbitrary code
  execution when scanning untrusted source.
- Vulnerabilities in `entrolint`'s git interactions (e.g., shell
  injection through crafted refs / paths) that affect the host running
  the tool.
- CVEs in transitive dependencies (`go.sum`) that materially affect
  `entrolint` users.

**Out of scope:**

- `entrolint` does **not** make network calls of its own. There is no
  telemetry, no licensing check, no auto-update channel. Reports of
  "missing TLS" or "missing auth" on a non-existent feature will be
  closed.
- Cache poisoning of `.entrolint.cache.json` requires write access to
  the user's repository — that is a pre-existing trust boundary, not a
  vulnerability in `entrolint`.
- The output of `scan` and `check` is advisory. Acting on a wrong score
  is a quality-of-results issue, not a security issue — file it as a
  regular bug.

## Disclosure timeline

For confirmed vulnerabilities:

1. Reporter is acknowledged within 5 business days.
2. Fix and advisory drafted privately.
3. Patch release published.
4. Public advisory and CHANGELOG entry within **14 days** of the patch
   release, naming the reporter if desired.
