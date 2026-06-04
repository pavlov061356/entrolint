// Package statemultiplier detects PRs that add a state-multiplying
// parameter (bool or enum-typed) to an exported function or method —
// the O(2ⁿ) shape from docs/scaling.md.
//
// Algorithm:
//  1. For every changed .go file with both base and head blobs,
//     parse them with go/parser and pair their *ast.FuncDecl by
//     (receiver type name, function name).
//  2. For each pair where head has exactly one more parameter
//     appended at the end AND that new parameter's type is bool or
//     an enum-like named type, look the function up in the type graph
//     anchored at the changed file's owning package, then count
//     cross-package uses. Fire O(2ⁿ) when external sites ≥ min.
//
// v0.2 simplifications, documented in docs/scaling.md §"Известные
// упрощения" item 11:
//   - Only appended-at-end parameters are caught. Middle-insert,
//     retype, and variadic Options are deferred.
//   - typeKey is text-based: interface{} vs interface{F()} differ
//     by method count only, structs by field count, etc. Subtle
//     drift in earlier positions can still hide.
//   - Interface methods (added to *ast.InterfaceType.Methods) are
//     not yet diffed; only top-level *ast.FuncDecl pairs are walked.
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
	enums := typesx.CollectEnums(pkgs)
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
		absPath := c.Path
		owner := typesx.FindPackageByFile(pkgs, absPath)
		if owner == nil {
			// File isn't in any loaded package — typesx couldn't
			// anchor it. Skip rather than risk a wrong-pkg lookup.
			continue
		}
		hits = append(hits, d.analyzeFile(c.Path, base, head, pkgs, owner, enums, in.Root, minSites)...)
	}
	return hits
}

func (d *Detector) analyzeFile(path string, base, head []byte, pkgs []*packages.Package, owner *packages.Package, enums map[*types.Named]bool, root string, minSites int) []scaling.Hit {
	baseFile, ok := parseGo(path, base)
	if !ok {
		return nil
	}
	headFile, ok := parseGo(path, head)
	if !ok {
		return nil
	}
	enumNames := enumNamesVisibleTo(owner, enums)
	baseFns := indexFuncDecls(baseFile)
	headFns := indexFuncDecls(headFile)

	hits := make([]scaling.Hit, 0, len(headFns))
	for key, headFn := range headFns {
		baseFn, ok := baseFns[key]
		if !ok {
			continue
		}
		added := addedStateParam(baseFn, headFn, enumNames)
		if added == "" {
			continue
		}
		sites := countExternalSites(pkgs, owner, headFn.Name.Name, key.recv)
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

// enumNamesVisibleTo flattens the *types.Named set down to the
// short identifiers a stand-alone go/parser AST would have produced
// for parameters in owner's scope. We include owner-declared enums by
// their unqualified name, and enums imported into owner's scope by
// the same unqualified name (`pkg.Kind` → "Kind", looked up via the
// SelectorExpr branch of stateMultiplierKind).
func enumNamesVisibleTo(owner *packages.Package, enums map[*types.Named]bool) map[string]bool {
	out := make(map[string]bool, len(enums))
	for n := range enums {
		obj := n.Obj()
		if obj == nil {
			continue
		}
		// Owner-declared enum: short ident "Kind".
		// Imported enum: matched by short ident on SelectorExpr.Sel.
		if obj.Pkg() == owner.Types || obj.Exported() {
			out[obj.Name()] = true
		}
	}
	return out
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
// state-multiplying parameter when head appends exactly one extra
// parameter past base, that parameter is bool or an enum-like named
// type, and the function is exported. Returns "" otherwise.
func addedStateParam(baseFn, headFn *ast.FuncDecl, enums map[string]bool) string {
	if !headFn.Name.IsExported() {
		return ""
	}
	baseParams := flattenParams(baseFn)
	headParams := flattenParams(headFn)
	if len(headParams) != len(baseParams)+1 {
		return ""
	}
	// Prefix must match by shallow text comparison — v0.2 MVP.
	for i, b := range baseParams {
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
// type qualifies as state-multiplying, "" otherwise. Pointer-to-bool
// and slice-of-bool are intentionally NOT state-multiplying — they
// model nil-as-default and variadic options, not branching flags.
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
		// Qualified enum from an imported package: pkg.Kind.
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
// shallow equality on parameter types. Text-level rendering only —
// doesn't resolve imports.
func typeKey(expr ast.Expr) string {
	if s, ok := simpleTypeKey(expr); ok {
		return s
	}
	if s, ok := compositeTypeKey(expr); ok {
		return s
	}
	if s, ok := containerTypeKey(expr); ok {
		return s
	}
	return fmt.Sprintf("%T", expr)
}

func simpleTypeKey(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, true
	case *ast.StarExpr:
		return "*" + typeKey(t.X), true
	case *ast.SelectorExpr:
		return typeKey(t.X) + "." + t.Sel.Name, true
	case *ast.ParenExpr:
		return typeKey(t.X), true
	case *ast.Ellipsis:
		return "..." + typeKey(t.Elt), true
	}
	return "", false
}

func compositeTypeKey(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeKey(t.Elt), true
		}
		return "[N]" + typeKey(t.Elt), true
	case *ast.MapType:
		return "map[" + typeKey(t.Key) + "]" + typeKey(t.Value), true
	case *ast.ChanType:
		return chanKey(t), true
	case *ast.StructType:
		return structKey(t), true
	}
	return "", false
}

func containerTypeKey(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.InterfaceType:
		return interfaceKey(t), true
	case *ast.FuncType:
		return funcTypeKey(t), true
	case *ast.IndexExpr:
		return typeKey(t.X) + "[" + typeKey(t.Index) + "]", true
	case *ast.IndexListExpr:
		return typeKey(t.X) + "[" + indicesKey(t.Indices) + "]", true
	}
	return "", false
}

func chanKey(t *ast.ChanType) string {
	dir := "chan "
	switch t.Dir {
	case ast.SEND:
		dir = "chan<- "
	case ast.RECV:
		dir = "<-chan "
	}
	return dir + typeKey(t.Value)
}

func structKey(t *ast.StructType) string {
	fields := 0
	if t.Fields != nil {
		fields = len(t.Fields.List)
	}
	return fmt.Sprintf("struct{%d}", fields)
}

// interfaceKey distinguishes empty interface{} from method-bearing
// interface{Read()}: the method count keeps them from colliding in
// sameTypeExpr (full method-set walk would be v0.3 work).
func interfaceKey(t *ast.InterfaceType) string {
	methods := 0
	if t.Methods != nil {
		methods = len(t.Methods.List)
	}
	return fmt.Sprintf("interface{%d}", methods)
}

func funcTypeKey(t *ast.FuncType) string {
	params, results := 0, 0
	if t.Params != nil {
		params = len(t.Params.List)
	}
	if t.Results != nil {
		results = len(t.Results.List)
	}
	return fmt.Sprintf("func(%d)(%d)", params, results)
}

func indicesKey(indices []ast.Expr) string {
	parts := make([]string, 0, len(indices))
	for _, idx := range indices {
		parts = append(parts, typeKey(idx))
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// countExternalSites walks loaded packages for uses of the target
// function or method, counting only those whose use-site package
// differs from the def's package. owner is the package that owns the
// edited file — used to resolve the target obj unambiguously.
func countExternalSites(pkgs []*packages.Package, owner *packages.Package, funcName, recvType string) int {
	target := findFunc(owner, funcName, recvType)
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

// findFunc looks up the target function or method in owner's scope
// only — name-based lookup across all loaded packages would conflate
// same-named functions across modules. owner is the *packages.Package
// that owns the file containing the edited FuncDecl.
func findFunc(owner *packages.Package, funcName, recvType string) *types.Func {
	if owner == nil || owner.Types == nil {
		return nil
	}
	return lookupFunc(owner.Types.Scope(), funcName, recvType)
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
