// Package identifierfanout detects PRs that touch ≥ TouchedRatio of
// the call-sites of an exported symbol the PR also defines — the
// O(refs) shape from docs/scaling.md.
//
// Algorithm:
//  1. Load the working-tree type graph via typesx.
//  2. Find every exported symbol (Func/TypeName/Var/Const) whose decl
//     file is in the PR's changed set — those are the "touched" defs.
//  3. For each, walk pkg.TypesInfo.Uses across all loaded packages
//     and count references per file.
//  4. If total references ≥ MinReferences AND touched refs / total
//     ≥ TouchedRatio, fire O(k) at the decl site with size = total.
//
// v0.2 simplification documented in docs/scaling.md §"Известные
// упрощения": we require the def to be touched (proxy for "PR
// changed the symbol") rather than a per-edit AST diff. Self-package
// uses (a function calling itself, or one symbol referencing another
// in the same file) count toward both numerator and denominator —
// this slightly skews ratio when the def file is in Changes, but
// keeps the algorithm a single-pass walk.
package identifierfanout

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/scaling"
	"github.com/pavlov061356/entrolint/internal/scaling/typesx"
)

const (
	// DefaultMinReferences: under this floor a "ripple" is small
	// enough to refactor by hand without architectural cost; firing
	// would surface every 2-caller helper edit as O(k) noise.
	DefaultMinReferences = 8

	// DefaultTouchedRatio: v0.2 starting threshold per
	// docs/scaling.md row identifier_fanout.
	DefaultTouchedRatio = 0.8

	name = "identifier_fanout"
)

type Detector struct {
	MinReferences int
	TouchedRatio  float64
}

func New() *Detector {
	return &Detector{
		MinReferences: DefaultMinReferences,
		TouchedRatio:  DefaultTouchedRatio,
	}
}

func (*Detector) Name() string { return name }

func (d *Detector) Analyze(in scaling.Input) []scaling.Hit {
	if in.Root == "" {
		return nil
	}
	pkgs, err := typesx.Default().Load(in.Root)
	if err != nil || len(pkgs) == 0 {
		return nil
	}
	changed := typesx.ChangedFileSet(in)
	if len(changed) == 0 {
		return nil
	}
	minRefs, ratio := d.thresholds()

	candidates := collectTouchedSymbols(pkgs, changed)
	if len(candidates) == 0 {
		return nil
	}
	hits := make([]scaling.Hit, 0, len(candidates))
	for _, sym := range candidates {
		if hit, ok := evaluate(pkgs, sym, changed, minRefs, ratio, in.Root); ok {
			hits = append(hits, hit)
		}
	}
	return hits
}

func (d *Detector) thresholds() (int, float64) {
	minRefs := d.MinReferences
	if minRefs < 2 {
		minRefs = DefaultMinReferences
	}
	ratio := d.TouchedRatio
	if ratio <= 0 || ratio > 1 {
		ratio = DefaultTouchedRatio
	}
	return minRefs, ratio
}

// collectTouchedSymbols returns the exported package-level symbols
// whose declaration file is in the changed set. These are the
// candidates for "PR changed the symbol", the v0.2 proxy for the
// spec's explicit "def edited" check.
func collectTouchedSymbols(pkgs []*packages.Package, changed map[string]bool) []types.Object {
	out := make([]types.Object, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, touchedSymbolsIn(pkg, pkgs, changed)...)
	}
	return out
}

func touchedSymbolsIn(pkg *packages.Package, all []*packages.Package, changed map[string]bool) []types.Object {
	if pkg.Types == nil {
		return nil
	}
	var out []types.Object
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		if obj, ok := touchedExported(scope, n, all, changed); ok {
			out = append(out, obj)
		}
	}
	return out
}

func touchedExported(scope *types.Scope, n string, pkgs []*packages.Package, changed map[string]bool) (types.Object, bool) {
	obj := scope.Lookup(n)
	if obj == nil || !obj.Exported() || !isFanoutKind(obj) {
		return nil, false
	}
	owner := typesx.FindOwningPackage(pkgs, obj)
	if owner == nil || !typesx.PosInChanged(owner, obj.Pos(), changed) {
		return nil, false
	}
	return obj, true
}

// isFanoutKind keeps only the *types.Object varieties that have a
// meaningful "call-site" notion. Packages, labels, builtins, and
// type parameters don't fit the spec's references-of-a-symbol shape.
func isFanoutKind(obj types.Object) bool {
	switch obj.(type) {
	case *types.Func, *types.TypeName, *types.Var, *types.Const:
		return true
	}
	return false
}

// evaluate counts references to target across all loaded packages,
// then decides whether the PR's touched-files-share crosses the
// configured ratio.
func evaluate(pkgs []*packages.Package, target types.Object, changed map[string]bool, minRefs int, ratio float64, root string) (scaling.Hit, bool) {
	totalRefs, touchedRefs := countReferences(pkgs, target, changed)
	if totalRefs < minRefs {
		return scaling.Hit{}, false
	}
	if float64(touchedRefs)/float64(totalRefs) < ratio {
		return scaling.Hit{}, false
	}
	owner := typesx.FindOwningPackage(pkgs, target)
	if owner == nil {
		return scaling.Hit{}, false
	}
	declPath := typesx.Relativize(root, owner.Fset.Position(target.Pos()).Filename)
	return scaling.Hit{
		Detector: name,
		Class:    scaling.ClassOk,
		Size:     totalRefs,
		Path:     declPath,
		Evidence: fmt.Sprintf("%d of %d call-sites of %s touched",
			touchedRefs, totalRefs, target.Name()),
	}, true
}

// countReferences walks every TypesInfo.Uses entry in every loaded
// package and tallies how many resolve to target, splitting by
// whether the use site's file is in the changed set.
func countReferences(pkgs []*packages.Package, target types.Object, changed map[string]bool) (total, touched int) {
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		t, u := countReferencesIn(pkg, target, changed)
		total += t
		touched += u
	}
	return total, touched
}

func countReferencesIn(pkg *packages.Package, target types.Object, changed map[string]bool) (total, touched int) {
	for ident, obj := range pkg.TypesInfo.Uses {
		if obj != target {
			continue
		}
		if !ident.Pos().IsValid() {
			continue
		}
		total++
		if typesx.PosInChanged(pkg, ident.Pos(), changed) {
			touched++
		}
	}
	return total, touched
}

func init() {
	scaling.Registry = append(scaling.Registry, New())
}
