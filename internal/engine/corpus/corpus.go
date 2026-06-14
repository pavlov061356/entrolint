// Package corpus builds the whole-tree cross-file context that backs the
// cross_duplication microstate. It reconstructs every analyzable Go file
// at a git ref from blobs — no checkout, no go/types — parses them, and
// precomputes per-file cross-file clone mass. The same builder serves
// `check` (run symmetrically on the base and head refs) and `scan` (over
// the already-parsed on-disk tree).
package corpus

import (
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/analyzer/golang"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
)

// Context is the cross-file pre-pass result for one tree (one ref). It
// implements microstate.CrossFileSource. A nil *Context is safe — every
// accessor returns 0 — so a corpus that failed to build degrades to "no
// cross-file signal" rather than a panic, keeping the engine hermetic.
type Context struct {
	crossDupMass map[string]float64
}

// CrossDupMass returns the cross-file duplication mass for path, or 0.
func (c *Context) CrossDupMass(path string) float64 {
	if c == nil {
		return 0
	}
	return c.crossDupMass[path]
}

// BuildFromFiles builds a Context from an already-parsed file corpus —
// the scan path, where the analyzer walked the on-disk tree.
func BuildFromFiles(files []microstate.File) *Context {
	return &Context{crossDupMass: microstate.CrossDupMassByFile(files)}
}

// BuildFromBlobs builds a Context from an in-memory whole-tree blob map
// keyed by repo-relative path. Each blob is parsed exactly once; a blob
// that is empty or doesn't parse is skipped, matching the analyzer's
// silent-skip. Paths in exclude are held out (check uses this to keep
// clone-class membership symmetric across refs — see issue #68 and
// microstate.CrossDupMassByFile). No git access: this is the seam that
// lets `check` feed the corpus the same bytes it already fetched, and that
// the staged coupling pre-pass (import-graph-lite) will reuse over the same
// blob set.
func BuildFromBlobs(blobs map[string][]byte, exclude map[string]bool) *Context {
	paths := make([]string, 0, len(blobs))
	for p := range blobs {
		if !exclude[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths) // deterministic parse order (the mass is order-independent regardless)
	files := make([]microstate.File, 0, len(paths))
	for _, p := range paths {
		if f, ok := golang.ParseGoBytes(p, blobs[p]); ok {
			files = append(files, f)
		}
	}
	return BuildFromFiles(files)
}

// Build reconstructs the whole tree at ref from git blobs and builds a
// Context. Paths are enumerated with the same IsAnalyzablePath filter the
// calibration walk uses (vendor/dot-dir excluded), fetched in one batch,
// and parsed. The ref is never checked out, so this works for a base ref
// that is not in the working tree.
//
// Returns ErrUnavailable / ErrInvalidRef wrapped (from gitx) when git
// cannot run or ref does not resolve.
func Build(r gitx.Runner, ref string) (*Context, error) {
	return BuildReusing(r, ref, nil, nil)
}

// BuildReusing is Build with two extras for `check`:
//
//   - have supplies blobs already fetched at this ref, keyed by their
//     ref-tree path (the changed files the scoring path pulled). They are
//     reused here instead of re-fetched, so each blob in the tree is read
//     from git exactly once across the whole check run — the scoring path
//     fetches the changed files, this fetches only the rest.
//   - exclude holds parse-asymmetric files out of the corpus (issue #68).
//
// Both may be nil. Returns ErrUnavailable / ErrInvalidRef wrapped (from
// gitx) when git cannot run or ref does not resolve.
func BuildReusing(r gitx.Runner, ref string, have map[string][]byte, exclude map[string]bool) (*Context, error) {
	paths, err := gitx.TreeFiles(r, ref)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(paths))
	for _, p := range paths {
		if !golang.IsAnalyzablePath(p) || exclude[p] {
			continue // excluded or already in hand — no fetch
		}
		if _, reused := have[p]; !reused {
			missing = append(missing, p)
		}
	}
	fetched, err := gitx.BlobsAtRef(r, ref, missing)
	if err != nil {
		return nil, err
	}
	// Merge reused + freshly fetched into one whole-tree blob map. have is
	// keyed by ref-tree path by construction (check re-keys a rename's base
	// side), so its keys line up with the tree enumeration above.
	blobs := make(map[string][]byte, len(missing)+len(have))
	for _, p := range missing {
		if b, ok := fetched[p]; ok {
			blobs[p] = b
		}
	}
	for p, b := range have {
		blobs[p] = b
	}
	return BuildFromBlobs(blobs, exclude), nil
}
