package implementorscan

import (
	"path/filepath"
	"sync"

	"golang.org/x/tools/go/packages"
)

// loader caches packages.Load results per root path. The Detector is
// registered as a process-global singleton via init(), so a naive
// sync.Once pinned to the FIRST root would silently return stale
// packages on any subsequent Check invocation with a different root
// (CI batch tools, future server mode, dogfood across worktrees).
// Keying by filepath.Clean(root) keeps the loader correct under those
// patterns without paying the load cost twice for the same repo.
type loader struct {
	mu    sync.Mutex
	cache map[string]loadResult
}

type loadResult struct {
	pkgs []*packages.Package
	err  error
}

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedSyntax |
	packages.NeedImports |
	packages.NeedDeps

func (l *loader) load(root string) ([]*packages.Package, error) {
	key := filepath.Clean(root)

	l.mu.Lock()
	if l.cache == nil {
		l.cache = make(map[string]loadResult)
	}
	if r, ok := l.cache[key]; ok {
		l.mu.Unlock()
		return r.pkgs, r.err
	}
	l.mu.Unlock()

	cfg := &packages.Config{
		Mode: loadMode,
		Dir:  key,
		// Tests=true brings _test.go files into the type graph so
		// in-package test doubles count as implementors and edits to
		// test files count toward the "touched" set.
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	pkgs = filterUsable(pkgs)

	l.mu.Lock()
	l.cache[key] = loadResult{pkgs: pkgs, err: err}
	l.mu.Unlock()

	return pkgs, err
}

// filterUsable drops packages whose type info is incomplete. Top-level
// (pkgs, err) hides per-package parse/type errors in pkg.IllTyped and
// pkg.Errors; using a partially-typed package makes types.Implements
// return false on impls whose methods failed to resolve, silently
// undercounting the ratio.
func filterUsable(pkgs []*packages.Package) []*packages.Package {
	out := pkgs[:0]
	for _, p := range pkgs {
		if p.Types == nil || p.IllTyped || len(p.Errors) > 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}
