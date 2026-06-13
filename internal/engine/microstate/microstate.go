// Package microstate defines the contract for entrolint entropy
// contributors and provides the per-microstate implementations.
//
// Microstates operate on Go source files exclusively. The File type
// and Microstate interface are deliberately Go-coupled here; a second
// language (and the cross-language abstraction it needs) is deferred to
// post-1.0 — pre-1.0 deepens the Go engine.
package microstate

import (
	"go/ast"
	"go/token"
)

// File is a parsed Go source file ready for microstate measurement.
// Src holds the raw source bytes so microstates that need physical
// line information (e.g. length) don't have to re-read from disk.
// ChurnCount holds the number of commits touching this file inside
// the calibration window; the analyzer populates it from gitx so
// microstates stay pure and free of subprocess I/O.
//
// Corpus is the optional cross-file context for the whole tree this
// file belongs to (at one git ref). Like ChurnCount it is pre-computed
// outside the microstate — by the corpus pre-pass — so a Measure call
// stays a pure single-file function. It is nil for files scored in
// isolation (unit tests, corpus-less paths); cross-file microstates
// must nil-guard and contribute 0 in that case.
type File struct {
	Path       string
	Src        []byte
	AST        *ast.File
	Fset       *token.FileSet
	ChurnCount int
	Corpus     CrossFileSource
}

// CrossFileSource exposes the cross-file signals a microstate needs
// about the file at File.Path, pre-computed over the whole tree at one
// ref. It is an interface (not a concrete type) so package microstate
// stays free of any dependency on the corpus pre-pass that produces it,
// keeping the dependency direction corpus → microstate.
type CrossFileSource interface {
	// CrossDupMass returns the size-weighted redundant mass attributed
	// to path from clone classes that span more than one file. 0 when
	// the path has no cross-file clones.
	CrossDupMass(path string) float64
}

// Microstate is one measurable contributor to the entropy score S.
// Implementations are stateless and return a raw scalar per file —
// normalization, weighting, and aggregation live in the thermo
// package, not in individual microstates.
type Microstate interface {
	Name() string
	Measure(File) float64
}
