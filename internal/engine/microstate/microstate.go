// Package microstate defines the contract for entrolint entropy
// contributors and provides the per-microstate implementations.
//
// In v0.1 microstates operate on Go source files exclusively. The
// File type and Microstate interface are deliberately Go-coupled
// here; cross-language abstraction lands together with the v0.5
// TypeScript analyzer.
package microstate

import (
	"go/ast"
	"go/token"
)

// File is a parsed Go source file ready for microstate measurement.
// Src holds the raw source bytes so microstates that need physical
// line information (e.g. length) don't have to re-read from disk.
type File struct {
	Path string
	Src  []byte
	AST  *ast.File
	Fset *token.FileSet
}

// Microstate is one measurable contributor to the entropy score S.
// Implementations are stateless and return a raw scalar per file —
// normalization, weighting, and aggregation live in the thermo
// package, not in individual microstates.
type Microstate interface {
	Name() string
	Measure(File) float64
}
