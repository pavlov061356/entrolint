// Package implementorscan detects PRs that ripple through ≥ a
// configurable fraction of an interface's implementors — the
// canonical O(implementors) shape from docs/scaling.md.
//
// Algorithm: load the working-tree type graph (cached per Root),
// walk every interface declared in the analyzed module, ask
// types.Implements for its implementors across the loaded packages,
// then check what fraction of those implementors' source files appear
// in the PR diff. A hit fires at the interface's declaration site so
// the report points the reader at the cause, not at every leaf.
//
// v0.2 limitations (documented in docs/scaling.md §"Известные
// упрощения" — code comments below name the exact handling):
//   - Generic interfaces (with type parameters) are skipped:
//     types.Implements is undefined on them.
//   - Stdlib and external-dep interfaces are never candidates:
//     packages.Load with "./..." only returns root packages. A PR
//     touching project types that satisfy io.Reader/http.Handler/etc.
//     will not fire. Document, don't fix in v0.2.
//   - Embedded/promoted methods ARE counted via types.NewMethodSet,
//     not via named.Method() which only exposes directly-declared
//     methods.
package implementorscan

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/scaling"
)

const (
	// DefaultMinImplementors is the floor on interface size — 1-impl
	// "interfaces" are trivially 100% touched whenever the impl is
	// edited; flagging them is noise.
	DefaultMinImplementors = 2

	// DefaultTouchedRatio is the v0.2 starting threshold per
	// docs/scaling.md §"Эвристики".
	DefaultTouchedRatio = 0.5

	name = "implementor_scan"
)

type Detector struct {
	MinImplementors int
	TouchedRatio    float64

	loader loader
}

func New() *Detector {
	return &Detector{
		MinImplementors: DefaultMinImplementors,
		TouchedRatio:    DefaultTouchedRatio,
	}
}

func (*Detector) Name() string { return name }

func (d *Detector) Analyze(in scaling.Input) []scaling.Hit {
	if in.Root == "" {
		return nil
	}
	pkgs, err := d.loader.load(in.Root)
	if err != nil || len(pkgs) == 0 {
		// Soft-skip: a broken go.mod or missing module shouldn't fail
		// the whole gate. shotgun and any future AST-only detectors
		// still run.
		return nil
	}

	ctx := scanCtx{
		pkgs:    pkgs,
		changed: d.changedFileSet(in),
		seen:    make(map[*types.Interface]bool),
		root:    in.Root,
	}
	if len(ctx.changed) == 0 {
		return nil
	}
	ctx.minImpls, ctx.ratio = d.thresholds()

	var hits []scaling.Hit
	for _, pkg := range pkgs {
		hits = append(hits, ctx.scanPackage(pkg)...)
	}
	return hits
}

func (d *Detector) thresholds() (int, float64) {
	minImpls := d.MinImplementors
	if minImpls < 2 {
		minImpls = DefaultMinImplementors
	}
	ratio := d.TouchedRatio
	if ratio <= 0 || ratio > 1 {
		ratio = DefaultTouchedRatio
	}
	return minImpls, ratio
}

// scanCtx carries the state shared between the outer Analyze loop and
// its per-package helpers. Packing it into a struct keeps each helper
// to a single receiver argument instead of a 7-parameter signature.
type scanCtx struct {
	pkgs     []*packages.Package
	changed  map[string]bool
	seen     map[*types.Interface]bool
	root     string
	minImpls int
	ratio    float64
}

func (c *scanCtx) scanPackage(pkg *packages.Package) []scaling.Hit {
	if pkg.Types == nil {
		return nil
	}
	var hits []scaling.Hit
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		if hit, ok := c.maybeHit(pkg, scope, n); ok {
			hits = append(hits, hit)
		}
	}
	return hits
}

func (c *scanCtx) maybeHit(pkg *packages.Package, scope *types.Scope, n string) (scaling.Hit, bool) {
	tn, ok := scope.Lookup(n).(*types.TypeName)
	if !ok || tn.IsAlias() {
		// Skip alias TypeNames so the hit always lands at the canonical
		// interface declaration: `type X = pkg.Y` shares the same
		// *types.Interface pointer as Y, and whichever scope we visit
		// first would otherwise own the report.
		return scaling.Hit{}, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return scaling.Hit{}, false
	}
	if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
		// types.Implements is undefined on parameterized types; skip.
		return scaling.Hit{}, false
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok || iface.NumMethods() == 0 || c.seen[iface] {
		return scaling.Hit{}, false
	}
	c.seen[iface] = true

	impls := findImplementors(c.pkgs, iface)
	if len(impls) < c.minImpls {
		return scaling.Hit{}, false
	}
	touched := countTouched(c.pkgs, impls, c.changed)
	if float64(touched)/float64(len(impls)) < c.ratio {
		return scaling.Hit{}, false
	}
	return scaling.Hit{
		Detector: name,
		Class:    scaling.ClassOk,
		Size:     len(impls),
		Path:     relativize(c.root, pkg.Fset.Position(tn.Pos()).Filename),
		Evidence: fmt.Sprintf("%d of %d implementors of %s touched",
			touched, len(impls), tn.Name()),
	}, true
}

// changedFileSet builds the set of absolute paths git diff says were
// changed (head-side). Detector compares this against the absolute
// paths the type loader reports for implementor declarations, so both
// sides need the same normalization.
func (d *Detector) changedFileSet(in scaling.Input) map[string]bool {
	out := make(map[string]bool, len(in.Changes))
	for _, c := range in.Changes {
		if !strings.HasSuffix(c.Path, ".go") {
			continue
		}
		abs := c.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(in.Root, c.Path)
		}
		out[filepath.Clean(abs)] = true
	}
	return out
}

func findImplementors(pkgs []*packages.Package, iface *types.Interface) []*types.Named {
	// Dedupe by canonical Obj identity: `type Foo = pkg.Bar` re-exports
	// the same *types.Named via a different alias TypeName, and we
	// don't want to inflate both Size and the denominator on a single
	// concrete type.
	seen := make(map[types.Object]bool)
	var out []*types.Named
	for _, pkg := range pkgs {
		for _, named := range implementorsIn(pkg, iface) {
			obj := named.Obj()
			if seen[obj] {
				continue
			}
			seen[obj] = true
			out = append(out, named)
		}
	}
	return out
}

func implementorsIn(pkg *packages.Package, iface *types.Interface) []*types.Named {
	if pkg.Types == nil {
		return nil
	}
	var out []*types.Named
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		if named, ok := namedImplementor(scope, n, iface); ok {
			out = append(out, named)
		}
	}
	return out
}

// namedImplementor returns the *types.Named at scope[n] if it's a
// concrete (non-alias, non-interface, non-generic) type whose value
// or pointer receivers satisfy iface. Aliases skip so the canonical
// TypeName is the one we return.
func namedImplementor(scope *types.Scope, n string, iface *types.Interface) (*types.Named, bool) {
	tn, ok := scope.Lookup(n).(*types.TypeName)
	if !ok || tn.IsAlias() {
		return nil, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, false
	}
	if _, isIface := named.Underlying().(*types.Interface); isIface {
		return nil, false
	}
	if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
		// Skip generic concretes too: types.Implements requires a
		// fully instantiated type to give a defined answer.
		return nil, false
	}
	// Value and pointer receivers both count.
	if types.Implements(named, iface) || types.Implements(types.NewPointer(named), iface) {
		return named, true
	}
	return nil, false
}

func countTouched(pkgs []*packages.Package, impls []*types.Named, changed map[string]bool) int {
	touched := 0
	for _, named := range impls {
		if hasChangedSite(pkgs, named, changed) {
			touched++
		}
	}
	return touched
}

// hasChangedSite reports whether the implementor's declaration OR
// any of its methods (DIRECT or PROMOTED via embedded fields) live in
// a changed file. types.NewMethodSet returns the full method set —
// named.NumMethods/Method only enumerates directly-declared methods,
// missing the common Go pattern of `type S struct{ *Base }` where S
// satisfies an interface entirely through Base's methods. Edits to
// Base.go would otherwise silently fail to count S as touched.
func hasChangedSite(pkgs []*packages.Package, named *types.Named, changed map[string]bool) bool {
	pkg := findOwningPackage(pkgs, named.Obj())
	if pkg == nil {
		return false
	}
	if posInChanged(pkg, named.Obj().Pos(), changed) {
		return true
	}
	// Walk both value- and pointer-receiver method sets; embedded
	// pointer fields show up only in the pointer view.
	for _, t := range []types.Type{named, types.NewPointer(named)} {
		if methodSetTouched(pkgs, t, changed) {
			return true
		}
	}
	return false
}

func methodSetTouched(pkgs []*packages.Package, t types.Type, changed map[string]bool) bool {
	mset := types.NewMethodSet(t)
	for i := 0; i < mset.Len(); i++ {
		fn, ok := mset.At(i).Obj().(*types.Func)
		if !ok {
			continue
		}
		fnPkg := findOwningPackage(pkgs, fn)
		if fnPkg == nil {
			continue
		}
		if posInChanged(fnPkg, fn.Pos(), changed) {
			return true
		}
	}
	return false
}

func posInChanged(pkg *packages.Package, pos token.Pos, changed map[string]bool) bool {
	p := pkg.Fset.Position(pos)
	return p.IsValid() && changed[filepath.Clean(p.Filename)]
}

func findOwningPackage(pkgs []*packages.Package, obj types.Object) *packages.Package {
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	target := obj.Pkg().Path()
	for _, p := range pkgs {
		if p.Types != nil && p.Types.Path() == target {
			return p
		}
	}
	return nil
}

// relativize returns path relative to root, normalized. Falls back to
// the absolute path if Rel fails (different volume on Windows, etc.).
func relativize(root, path string) string {
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	return rel
}

func init() {
	scaling.Registry = append(scaling.Registry, New())
}
