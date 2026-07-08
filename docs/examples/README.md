# Diagnostic examples

These examples show how to turn `entrolint scan` output into short
maintainability diagnostics.

They are point-in-time architectural profiles, not quality rankings. A hot file
is a place to inspect with context, not a verdict about a project.

## Examples

| Example | Repository | Focus |
| ------- | ---------- | ----- |
| [entrolint self-diagnostic](entrolint-self-diagnostic.md) | `github.com/pavlov061356/entrolint` | Self-scan of this repository and proof that the template is fillable. |
| [Cobra diagnostic](cobra-diagnostic.md) | `github.com/spf13/cobra` | Compact CLI framework: command core, completions, and docs generators. |
| [Gin diagnostic](gin-diagnostic.md) | `github.com/gin-gonic/gin` | Web framework: HTTP context/engine core, binding, render, and tests. |
| [Chi diagnostic](chi-diagnostic.md) | `github.com/go-chi/chi` | Router/middleware library: routing tree, mux, middleware, and examples. |

## Reproduce the shape

For each repository:

```bash
entrolint scan --top 10
entrolint scan --format json > entrolint-scan.json
entrolint scan --html entrolint-heatmap/
```

Then fill [the diagnostic report template](../diagnostic-report-template.md).
