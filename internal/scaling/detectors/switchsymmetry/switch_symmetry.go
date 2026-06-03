// Package switchsymmetry detects PRs that touch ≥ a configurable
// fraction of `switch` statements over the same enum-like named type
// — the O(switch arms) shape from docs/scaling.md.
//
// v0.2 approximation: the spec says "added enum-case AND edited ≥ 50%
// switches over it". Diffing base vs head AST for the case-add half
// requires extra plumbing (gitx.FileAtRef + double parse). The MVP
// drops that condition and fires purely on the symmetric-edit signal:
// when many switches over the same enum are touched in one PR, the
// pattern is almost always either a freshly added case or a renamed
// constant, both legitimate O(k) ripples. Documented in
// docs/scaling.md §"Известные упрощения".
package switchsymmetry

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/scaling"
	"github.com/pavlov061356/entrolint/internal/scaling/typesx"
)

const (
	// DefaultMinSwitches: an enum with a single switch never produces
	// a "symmetric" pattern by definition; 2 is the smallest size
	// where "≥ 50% touched" is a meaningful signal.
	DefaultMinSwitches = 2

	// DefaultTouchedRatio: v0.2 starting threshold per docs/scaling.md.
	DefaultTouchedRatio = 0.5

	name = "switch_case_symmetry"
)

type Detector struct {
	MinSwitches  int
	TouchedRatio float64
}

func New() *Detector {
	return &Detector{
		MinSwitches:  DefaultMinSwitches,
		TouchedRatio: DefaultTouchedRatio,
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

	minSwitches, ratio := d.thresholds()
	enums := collectEnums(pkgs)
	if len(enums) == 0 {
		return nil
	}

	groups := collectSwitches(pkgs, enums)
	hits := make([]scaling.Hit, 0, len(groups))
	for named, sites := range groups {
		if len(sites) < minSwitches {
			continue
		}
		touched := countTouched(sites, changed)
		if float64(touched)/float64(len(sites)) < ratio {
			continue
		}
		owner := typesx.FindOwningPackage(pkgs, named.Obj())
		if owner == nil {
			continue
		}
		declPath := typesx.Relativize(in.Root, owner.Fset.Position(named.Obj().Pos()).Filename)
		hits = append(hits, scaling.Hit{
			Detector: name,
			Class:    scaling.ClassOk,
			Size:     len(sites),
			Path:     declPath,
			Evidence: fmt.Sprintf("%d of %d switches over %s touched",
				touched, len(sites), named.Obj().Name()),
		})
	}
	return hits
}

func (d *Detector) thresholds() (int, float64) {
	min := d.MinSwitches
	if min < 2 {
		min = DefaultMinSwitches
	}
	ratio := d.TouchedRatio
	if ratio <= 0 || ratio > 1 {
		ratio = DefaultTouchedRatio
	}
	return min, ratio
}

// switchSite is one `switch x` statement found in source. The filename
// is the cleaned absolute path so countTouched can match against
// typesx.ChangedFileSet output directly.
type switchSite struct {
	filename string
}

// collectEnums returns named types in loaded packages that look like
// Go enums: there are at least 2 *types.Const objects declared in the
// same package whose type resolves to the named type. Generic named
// types are skipped — types.Implements is undefined on them, and the
// same generic-skipped policy applies here for consistency.
func collectEnums(pkgs []*packages.Package) map[*types.Named]bool {
	out := make(map[*types.Named]bool)
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		consts := constByNamed(pkg)
		for named, count := range consts {
			if count < 2 {
				continue
			}
			if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
				continue
			}
			out[named] = true
		}
	}
	return out
}

func constByNamed(pkg *packages.Package) map[*types.Named]int {
	out := make(map[*types.Named]int)
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		c, ok := scope.Lookup(n).(*types.Const)
		if !ok {
			continue
		}
		// Unalias before the *types.Named assertion: under Go 1.23+
		// `gotypesalias=1` (the default), `type AliasKind = Kind`
		// surfaces a *types.Alias here, not a *types.Named, and the
		// const would otherwise be silently dropped from the enum
		// count.
		named, ok := types.Unalias(c.Type()).(*types.Named)
		if !ok {
			continue
		}
		out[named]++
	}
	return out
}

// collectSwitches walks the syntax of every loaded package and records
// every `switch` whose tag expression has a static type matching one
// of the enum named types. Type-switches (`switch x.(type)`) are
// excluded — their semantics don't match the enum-over-named-type
// pattern this detector targets.
func collectSwitches(pkgs []*packages.Package, enums map[*types.Named]bool) map[*types.Named][]switchSite {
	out := make(map[*types.Named][]switchSite)
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			gatherSwitches(pkg, file, enums, out)
		}
	}
	return out
}

func gatherSwitches(pkg *packages.Package, file *ast.File, enums map[*types.Named]bool, into map[*types.Named][]switchSite) {
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil {
			return true
		}
		t := pkg.TypesInfo.TypeOf(sw.Tag)
		if t == nil {
			return true
		}
		// Same unalias dance as constByNamed: an aliased enum tag
		// (`var x AliasKind`) would otherwise present as *types.Alias
		// and miss the enums map.
		named, ok := types.Unalias(t).(*types.Named)
		if !ok || !enums[named] {
			return true
		}
		pos := pkg.Fset.Position(sw.Pos())
		if !pos.IsValid() {
			return true
		}
		into[named] = append(into[named], switchSite{filename: filepath.Clean(pos.Filename)})
		return true
	})
}

func countTouched(sites []switchSite, changed map[string]bool) int {
	n := 0
	for _, s := range sites {
		if changed[s.filename] {
			n++
		}
	}
	return n
}

func init() {
	scaling.Registry = append(scaling.Registry, New())
}
