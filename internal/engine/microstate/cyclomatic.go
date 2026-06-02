package microstate

import (
	"go/ast"
	"go/token"
)

// Cyclomatic counts decision points across all top-level function
// declarations in a Go file, per the v0.1 formula spec:
// if, for, range, case (in switch and select), && and ||, plus
// defer recover().
type Cyclomatic struct{}

// Name returns the microstate identifier used in config and reports.
func (Cyclomatic) Name() string { return "cyclomatic" }

// Measure returns the total decision-point count for the file.
// Functions with a nil body (e.g. assembly declarations) contribute 0.
func (Cyclomatic) Measure(f File) float64 {
	if f.AST == nil {
		return 0
	}
	total := 0
	for _, decl := range f.AST.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		total += decisionPoints(fn.Body)
	}
	return float64(total)
}

func decisionPoints(body *ast.BlockStmt) int {
	n := 0
	ast.Inspect(body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt:
			n++
		case *ast.ForStmt:
			n++
		case *ast.RangeStmt:
			n++
		case *ast.CaseClause:
			n++
		case *ast.CommClause:
			n++
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		case *ast.DeferStmt:
			if isRecoverCall(x.Call) {
				n++
			}
		}
		return true
	})
	return n
}

func isRecoverCall(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "recover"
}
