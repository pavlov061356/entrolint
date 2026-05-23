package microstate

import "go/ast"

// Nesting reports the maximum block nesting depth across all top-level
// function declarations in a file. Per the v0.1 formula spec the
// file-level aggregate is the maximum (not the sum) of per-function
// depths — a single deeply-nested function dominates a file of
// shallow ones.
//
// Depth convention: the function body itself is depth 0. Every nested
// BlockStmt (the body of if/for/range/switch/select, a closure, or a
// bare `{}` block) adds 1. Closures inside a function contribute to
// the parent function's depth — same scoping rule as cyclomatic.
type Nesting struct{}

// Name returns the microstate identifier used in config and reports.
func (Nesting) Name() string { return "nesting" }

// Measure returns the maximum nesting depth observed under any top-
// level function declaration in the file.
func (Nesting) Measure(f File) float64 {
	if f.AST == nil {
		return 0
	}
	maxD := 0
	for _, decl := range f.AST.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		d := nestingOfFunc(fn.Body)
		if d > maxD {
			maxD = d
		}
	}
	return float64(maxD)
}

func nestingOfFunc(body *ast.BlockStmt) int {
	maxD := 0
	for _, stmt := range body.List {
		ast.Walk(&nestingVisitor{depth: 0, max: &maxD}, stmt)
	}
	return maxD
}

type nestingVisitor struct {
	depth int
	max   *int
}

func (v *nestingVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if _, ok := node.(*ast.BlockStmt); ok {
		newDepth := v.depth + 1
		if newDepth > *v.max {
			*v.max = newDepth
		}
		return &nestingVisitor{depth: newDepth, max: v.max}
	}
	return v
}
