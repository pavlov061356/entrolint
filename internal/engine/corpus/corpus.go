// Package corpus builds the whole-tree cross-file context that backs the
// cross_duplication microstate. It reconstructs every analyzable Go file
// at a git ref from blobs — no checkout, no go/types — parses them, and
// precomputes per-file cross-file clone mass. The same builder serves
// `check` (run symmetrically on the base and head refs) and `scan` (over
// the already-parsed on-disk tree).
package corpus

import (
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

// Build reconstructs the whole tree at ref from git blobs and builds a
// Context from it. Paths are enumerated with the same IsAnalyzablePath
// filter the calibration walk uses (vendor/dot-dir excluded), fetched in
// one batch, and parsed; a blob that is absent or doesn't parse is
// skipped, matching the analyzer's silent-skip. The ref is never checked
// out, so this works for a base ref that is not in the working tree.
//
// Returns ErrUnavailable / ErrInvalidRef wrapped (from gitx) when git
// cannot run or ref does not resolve.
func Build(r gitx.Runner, ref string) (*Context, error) {
	paths, err := gitx.TreeFiles(r, ref)
	if err != nil {
		return nil, err
	}
	wanted := make([]string, 0, len(paths))
	for _, p := range paths {
		if golang.IsAnalyzablePath(p) {
			wanted = append(wanted, p)
		}
	}
	blobs, err := gitx.BlobsAtRef(r, ref, wanted)
	if err != nil {
		return nil, err
	}
	files := make([]microstate.File, 0, len(blobs))
	for _, p := range wanted { // deterministic order
		blob, ok := blobs[p]
		if !ok {
			continue
		}
		if f, ok := golang.ParseGoBytes(p, blob); ok {
			files = append(files, f)
		}
	}
	return BuildFromFiles(files), nil
}
