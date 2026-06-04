// Package statemultiplier detects PRs that add a state-multiplying
// parameter (bool or enum-typed) to an exported function or method —
// the O(2ⁿ) shape from docs/scaling.md.
//
// Algorithm:
//  1. For every changed .go file with both base and head blobs,
//     parse them with go/parser and pair their *ast.FuncDecl by
//     (receiver type name, function name).
//  2. For each pair where head has exactly one more parameter than
//     base AND that new parameter's type is bool or an enum-like
//     named type (>=2 const'ов своего типа в loaded packages),
//     register a candidate.
//  3. Count external call-sites via typesx (head graph). External =
//     use site's *types.Package differs from def's. Fire O(2ⁿ) when
//     external sites >= MinExternalSites.
//
// v0.2 simplification: the algorithm only catches the "appended
// parameter" case. Reordered, replaced, or middle-inserted parameters
// fall outside the heuristic and would need deeper signature diffing.
// Documented in docs/scaling.md §"Известные упрощения".
package statemultiplier

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/scaling"
	"github.com/pavlov061356/entrolint/internal/scaling/typesx"
)

const (
	// DefaultMinExternalSites: per docs/scaling.md, the signal needs
	// at least 2 callers outside the source module to count as a
	// public state explosion.
	DefaultMinExternalSites = 2

	name = "state_multiplier"
)

type Detector struct {
	MinExternalSites int
}

func New() *Detector {
	return &Detector{MinExternalSites: DefaultMinExternalSites}
}

func (*Detector) Name() string { return name }

func (d *Detector) Analyze(in scaling.Input) []scaling.Hit {
	if in.Root == "" {
		return nil
	}
	if len(in.BaseBlobs) == 0 || len(in.HeadBlobs) == 0 {
		return nil
	}
	pkgs, err := typesx.Default().Load(in.Root)
	if err != nil || len(pkgs) == 0 {
		return nil
	}
	enums := collectEnumTypeNames(pkgs)
	minSites := d.MinExternalSites
	if minSites < 1 {
		minSites = DefaultMinExternalSites
	}

	hits := make([]scaling.Hit, 0, len(in.Changes))
	for _, c := range in.Changes {
		base, hasBase := in.BaseBlobs[c.Path]
		head, hasHead := in.HeadBlobs[c.Path]
		if !hasBase || !hasHead {
			continue
		}
		hits = append(hits, d.analyzeFile(c.Path, base, head, pkgs, enums, in.Root, minSites)...)
	}
	return hits
}

func (d *Detector) analyzeFile(path string, base, head []byte, pkgs []*packages.Package, enums map[string]bool, root string, minSites int) []scaling.Hit {
	baseFile, ok := parseGo(path, base)
	if !ok {
		return nil
	}
	headFile, ok := parseGo(path, head)
	if !ok {
		return nil
	}
	baseFns := indexFuncDecls(baseFile)
	headFns := indexFuncDecls(headFile)

	hits := make([]scaling.Hit, 0, len(headFns))
	for key, headFn := range headFns {
		baseFn, ok := baseFns[key]
		if !ok {
			continue
		}
		added := addedStateParam(baseFn, headFn, enums)
		if added == "" {
			continue
		}
		sites := countExternalSites(pkgs, headFn.Name.Name, key.recv)
		if sites < minSites {
			continue
		}
		hits = append(hits, scaling.Hit{
			Detector: name,
			Class:    scaling.ClassO2N,
			Size:     sites,
			Path:     typesx.Relativize(root, path),
			Evidence: fmt.Sprintf("%s gained a %s param; %d external call-sites must update",
				renderFuncName(key), added, sites),
		})
	}
	return hits
}

// parseGo runs go/parser on the raw blob. Returns nil + false on any
// parse error — the file is treated as unanalyzable, matching the
// soft-skip philosophy used elsewhere in the pipeline.
func parseGo(path string, src []byte) (*ast.File, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	return f, true
}

// funcKey identifies a function by its receiver-type name (empty for
// free functions) and its declared name. Method overloading by
// receiver pointer/value doesn't matter for the diff: a value-receiver
// and pointer-receiver method with the same name are treated as the
// same function across the base/head pair.
type funcKey struct {
	recv string
	name string
}

func renderFuncName(k funcKey) string {
	if k.recv == "" {
		return k.name
	}
	return k.recv + "." + k.name
}

func indexFuncDecls(f *ast.File) map[funcKey]*ast.FuncDecl {
	out := make(map[funcKey]*ast.FuncDecl)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		out[funcKey{recv: receiverTypeName(fn), name: fn.Name.Name}] = fn
	}
	return out
}

// receiverTypeName returns the type name of fn's receiver (without
// the * for pointer receivers), or "" for free functions.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	// Generic receivers (`func (s *S[T]) F()`) come through as
	// *ast.IndexExpr / *ast.IndexListExpr — strip and return the base.
	switch t := expr.(type) {
	case *ast.IndexExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// addedStateParam returns a short description of the newly added
// state-multiplying parameter (the type name) when head has exactly
// one more parameter than base and that extra parameter is bool or
// an enum-like named type. Returns "" otherwise.
func addedStateParam(baseFn, headFn *ast.FuncDecl, enums map[string]bool) string {
	if !headFn.Name.IsExported() {
		return ""
	}
	baseParams := flattenParams(baseFn)
	headParams := flattenParams(headFn)
	if len(headParams) != len(baseParams)+1 {
		return ""
	}
	// Find the first position where the lists diverge; that's the
	// added parameter. For the v0.2 MVP we only accept "appended at
	// the end" — the simpler case that covers the bulk of real-world
	// API growth.
	for i, b := range baseParams {
		if i >= len(headParams) {
			return ""
		}
		if !sameTypeExpr(b, headParams[i]) {
			return ""
		}
	}
	added := headParams[len(headParams)-1]
	return stateMultiplierKind(added, enums)
}

func flattenParams(fn *ast.FuncDecl) []ast.Expr {
	if fn.Type == nil || fn.Type.Params == nil {
		return nil
	}
	var out []ast.Expr
	for _, field := range fn.Type.Params.List {
		// `func F(a, b int)` — one Field with two Names; replicate.
		repeat := len(field.Names)
		if repeat == 0 {
			repeat = 1
		}
		for i := 0; i < repeat; i++ {
			out = append(out, field.Type)
		}
	}
	return out
}

// stateMultiplierKind returns "bool" or "enum <TypeName>" if expr's
// type qualifies as state-multiplying, "" otherwise. enums is the set
// of named-type identifiers that have >=2 const'ов of their own type
// in the loaded type graph.
func stateMultiplierKind(expr ast.Expr, enums map[string]bool) string {
	switch t := expr.(type) {
	case *ast.Ident:
		if t.Name == "bool" {
			return "bool"
		}
		if enums[t.Name] {
			return "enum " + t.Name
		}
	case *ast.SelectorExpr:
		// Qualified enum from an imported package: pkg.Kind. The enum
		// map is keyed by unqualified identifier — this catches the
		// common case where the enum is declared in the analyzed
		// module.
		if t.Sel != nil && enums[t.Sel.Name] {
			return "enum " + t.Sel.Name
		}
	}
	return ""
}

func sameTypeExpr(a, b ast.Expr) bool {
	return typeKey(a) == typeKey(b)
}

// typeKey produces a stable string for an ast.Expr suitable for
// shallow equality on parameter types. Doesn't try to resolve
// imports — same source spelling means same type for the MVP.
func typeKey(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeKey(t.X)
	case *ast.SelectorExpr:
		return typeKey(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + typeKey(t.Elt)
	case *ast.MapType:
		return "map[" + typeKey(t.Key) + "]" + typeKey(t.Value)
	case *ast.Ellipsis:
		return "..." + typeKey(t.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	}
	return fmt.Sprintf("%T", expr)
}

// collectEnumTypeNames returns the set of unqualified named-type
// identifiers that have >=2 const'ов of their own type in the loaded
// graph. Same definition switch_case_symmetry uses for "enum-like".
func collectEnumTypeNames(pkgs []*packages.Package) map[string]bool {
	counts := make(map[string]int)
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, n := range scope.Names() {
			c, ok := scope.Lookup(n).(*types.Const)
			if !ok {
				continue
			}
			named, ok := types.Unalias(c.Type()).(*types.Named)
			if !ok {
				continue
			}
			counts[named.Obj().Name()]++
		}
	}
	out := make(map[string]bool, len(counts))
	for name, count := range counts {
		if count >= 2 {
			out[name] = true
		}
	}
	return out
}

// countExternalSites walks loaded packages for uses of the target
// function or method, counting only those whose use-site package
// differs from the def's package. Method names disambiguate via
// recvType: a method `(*Foo).Send` matches uses through any receiver
// expression whose type is Foo.
func countExternalSites(pkgs []*packages.Package, funcName, recvType string) int {
	target := findFunc(pkgs, funcName, recvType)
	if target == nil || target.Pkg() == nil {
		return 0
	}
	defPkg := target.Pkg()
	n := 0
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil || pkg.Types == defPkg {
			continue
		}
		for _, obj := range pkg.TypesInfo.Uses {
			if obj == target {
				n++
			}
		}
	}
	return n
}

func findFunc(pkgs []*packages.Package, funcName, recvType string) *types.Func {
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		if fn := lookupFunc(pkg.Types.Scope(), funcName, recvType); fn != nil {
			return fn
		}
	}
	return nil
}

func lookupFunc(scope *types.Scope, funcName, recvType string) *types.Func {
	if recvType == "" {
		if fn, ok := scope.Lookup(funcName).(*types.Func); ok {
			return fn
		}
		return nil
	}
	tn, ok := scope.Lookup(recvType).(*types.TypeName)
	if !ok {
		return nil
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil
	}
	for i := 0; i < named.NumMethods(); i++ {
		if m := named.Method(i); m.Name() == funcName {
			return m
		}
	}
	return nil
}

func init() {
	scaling.Registry = append(scaling.Registry, New())
}
