package typesx

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pavlov061356/entrolint/internal/scaling"
)

// ChangedFileSet builds the set of cleaned absolute paths from
// in.Changes filtered to `.go` files only. Detectors compare against
// this when deciding whether a types.Object lives in a touched file.
// Both sides go through filepath.Clean so paths like `./mem.go` and
// `mem.go` collapse identically.
func ChangedFileSet(in scaling.Input) map[string]bool {
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

// FindOwningPackage returns the *packages.Package that owns obj.
// Matches by *types.Package identity, not by import path: when
// packages.Load synthesizes test variants (Tests=true), the regular
// package and the augmented `pkg [pkg.test]` variant share the same
// Path but have distinct *types.Package instances, and decoding obj's
// pos through the wrong FileSet yields a phantom filename. We keep
// Tests=false in the loader as defense in depth, but the identity
// match is the principled invariant.
//
// Object positions only make sense relative to the FileSet of the
// package that owns them.
func FindOwningPackage(pkgs []*packages.Package, obj types.Object) *packages.Package {
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	target := obj.Pkg()
	for _, p := range pkgs {
		if p.Types == target {
			return p
		}
	}
	return nil
}

// PosInChanged reports whether the given pos (decoded via pkg.Fset)
// resolves to a filename in the changed set.
func PosInChanged(pkg *packages.Package, pos token.Pos, changed map[string]bool) bool {
	p := pkg.Fset.Position(pos)
	return p.IsValid() && changed[filepath.Clean(p.Filename)]
}

// Relativize returns path relative to root. Falls back to the cleaned
// absolute path if Rel fails (different volume on Windows, root that
// is itself not absolute, etc.).
func Relativize(root, path string) string {
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	return rel
}

// FindPackageByFile returns the *packages.Package whose GoFiles contains
// the given path. PR-diff paths are repo-relative while GoFiles entries
// are absolute, so the match runs in two passes: (1) cleaned-string
// equality for absolute-path callers, (2) suffix match on the relative
// path for repo-relative callers — `internal/foo/bar.go` matches
// `/abs/root/internal/foo/bar.go`.
//
// Anchoring detectors on the right package is critical: name-based
// lookup across all loaded packages conflates same-named symbols
// across modules.
func FindPackageByFile(pkgs []*packages.Package, path string) *packages.Package {
	want := filepath.Clean(path)
	relSuffix := string(filepath.Separator) + want
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			if filepath.Clean(f) == want {
				return p
			}
			if !filepath.IsAbs(want) && strings.HasSuffix(filepath.Clean(f), relSuffix) {
				return p
			}
		}
	}
	return nil
}

// CollectEnums returns named types that look like Go enums: at least
// 2 *types.Const objects declared in the same package have the named
// type. Keys are *types.Named identities (not unqualified strings)
// so same-named enums in different packages stay distinct. Generic
// named types are skipped — types.Implements is undefined on them
// and switchsymmetry follows the same policy.
func CollectEnums(pkgs []*packages.Package) map[*types.Named]bool {
	counts := make(map[*types.Named]int)
	for _, pkg := range pkgs {
		tallyEnums(pkg, counts)
	}
	out := make(map[*types.Named]bool, len(counts))
	for n, count := range counts {
		if count >= 2 {
			out[n] = true
		}
	}
	return out
}

func tallyEnums(pkg *packages.Package, counts map[*types.Named]int) {
	if pkg.Types == nil {
		return
	}
	scope := pkg.Types.Scope()
	for _, n := range scope.Names() {
		if named, ok := enumConstNamed(scope, n); ok {
			counts[named]++
		}
	}
}

// enumConstNamed returns the *types.Named that scope[n] is a const of,
// provided it's a non-generic named type. Unaliases under gotypesalias=1.
func enumConstNamed(scope *types.Scope, n string) (*types.Named, bool) {
	c, ok := scope.Lookup(n).(*types.Const)
	if !ok {
		return nil, false
	}
	named, ok := types.Unalias(c.Type()).(*types.Named)
	if !ok {
		return nil, false
	}
	if named.TypeParams() != nil && named.TypeParams().Len() > 0 {
		return nil, false
	}
	return named, true
}
