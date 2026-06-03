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
