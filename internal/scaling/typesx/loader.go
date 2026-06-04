// Package typesx provides the shared go/types loading and lookup
// helpers used by every typed scaling detector. It exists because two
// independent detectors (implementor_scan, switch_case_symmetry) need
// the same fully-typed package graph rooted at scaling.Input.Root, and
// duplicating the loader plus its singleton/concurrency contract per
// detector invites drift.
//
// Loading is per-root and cached: a process-global Loader (returned by
// Default()) hands out (pkgs, err) keyed by filepath.Clean(root). Bad
// modules, IllTyped packages, and packages with parse errors are
// filtered before they leak into a detector — using a partially-typed
// package makes types.Implements (and similar queries) return false
// for impls whose methods didn't resolve.
package typesx

import (
	"path/filepath"
	"sync"

	"golang.org/x/tools/go/packages"
)

// LoadMode is the set of bits every typed detector needs. Kept as a
// single constant so detectors stay synced and any new detector that
// requires more (e.g. NeedModule) updates it once.
//
// Why each bit:
//   - NeedName, NeedFiles: enough to enumerate packages and their files.
//   - NeedTypes, NeedTypesInfo: implementor_scan walks types.Scope and
//     resolves types.Implements; switch_case_symmetry reads
//     TypesInfo.TypeOf(sw.Tag) to bind switch sites to enum types.
//   - NeedSyntax: switch_case_symmetry walks pkg.Syntax for switch ASTs.
//   - NeedImports + NeedDeps: an interface whose method signatures
//     mention imported types only resolves through types.Implements
//     when the dep is fully type-checked. Dropping these silently
//     under-counts implementors with no observable failure mode.
const LoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedSyntax |
	packages.NeedImports |
	packages.NeedDeps

// Loader caches packages.Load results per filepath.Clean(root).
//
// A process-global singleton (Default()) is the production path —
// every typed Detector calls .Load(root) on the same Loader, so a
// repository touched twice in one process loads once. Tests construct
// their own Loader{} (zero value) so fixtures don't share state.
type Loader struct {
	mu    sync.Mutex
	cache map[string]result
}

type result struct {
	pkgs []*packages.Package
	err  error
}

var (
	defaultOnce sync.Once
	defaultL    *Loader
)

// Default returns the process-wide Loader. Multiple detectors call it
// during Analyze and share the type-check work across one Check run.
func Default() *Loader {
	defaultOnce.Do(func() { defaultL = &Loader{} })
	return defaultL
}

// Load runs packages.Load for the working tree at root and caches the
// result. Subsequent calls with the same root return the cached
// packages. Different roots get their own cache entries.
//
// Tests=false is deliberate: setting it to true makes packages.Load
// synthesize parallel `pkg [pkg.test]` variants that re-include the
// same source files under a distinct *types.Package. Every typed
// query (types.Implements, TypesInfo.TypeOf) then runs across two
// keys for the same logical entity, double-counting implementors and
// switch sites. The v0.2 spec describes "switches over the type in
// the package", i.e. production code — _test.go is out of scope until
// a future PR opts in deliberately.
//
// Per-package parse/type errors (pkg.IllTyped or len(pkg.Errors) > 0)
// are filtered out: any consumer using a partially-typed package
// would silently undercount queries. Top-level err propagates.
//
// The returned slice is shared with the cache. Callers MUST treat it
// as read-only — do not sort, filter in place, or append with cap
// headroom.
func (l *Loader) Load(root string) ([]*packages.Package, error) {
	key := filepath.Clean(root)

	l.mu.Lock()
	if l.cache == nil {
		l.cache = make(map[string]result)
	}
	if r, ok := l.cache[key]; ok {
		l.mu.Unlock()
		return r.pkgs, r.err
	}
	l.mu.Unlock()

	cfg := &packages.Config{
		Mode:  LoadMode,
		Dir:   key,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	pkgs = filterUsable(pkgs)

	l.mu.Lock()
	l.cache[key] = result{pkgs: pkgs, err: err}
	l.mu.Unlock()
	return pkgs, err
}

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
