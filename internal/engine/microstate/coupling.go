package microstate

// Coupling counts import specs in a Go file as a per-file proxy for
// efferent coupling. v0.3 MVP: every entry of f.AST.Imports — stdlib,
// third-party, intra-module, dot-imports, and blank side-effect imports
// alike — contributes 1. No package classification, no go.mod parsing.
//
// The real Robert-Martin metric (Ca, Ce, instability on the package
// graph) is deferred: it needs a pre-pass over the whole tree and
// breaks the per-file Microstate contract that check uses to score
// individual base/head blobs without the surrounding repo. The
// per-file import count is honest about what it measures — "how many
// other places this file is wired to" — and degrades gracefully when
// imports are removed.
type Coupling struct{}

// Name returns the microstate identifier used in config and reports.
func (Coupling) Name() string { return "coupling" }

// Measure returns the number of import specs in the file. Files
// with a nil AST (parse failure) contribute 0, matching Cyclomatic.
func (Coupling) Measure(f File) float64 {
	if f.AST == nil {
		return 0
	}
	return float64(len(f.AST.Imports))
}
