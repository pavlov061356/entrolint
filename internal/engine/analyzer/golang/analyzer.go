// Package golang walks a Go module/directory and produces parsed
// microstate.File values ready for scoring.
//
// Exclusions in v0.1: `vendor/` and any directory whose name starts
// with `.` (e.g. `.git`, `.idea`, `.vscode`). `_test.go` files ARE
// included — they're real code with their own complexity. Generated
// files are NOT special-cased; if they're noisy hotspots, surfacing
// them is informative rather than a bug.
package golang

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/microstate"
)

const defaultChurnSinceDays = 90

// Analyzer walks a Go source tree and returns parsed microstate.File
// values. The zero value is usable; ChurnRunner can be left nil when
// the caller doesn't need churn populated.
type Analyzer struct {
	// ChurnRunner is the gitx.Runner used to populate File.ChurnCount.
	// If nil, ChurnCount stays 0 — useful for static-only scans.
	ChurnRunner gitx.Runner

	// ChurnSinceDays is the window passed to gitx.ChurnCount. If zero,
	// defaults to 90.
	ChurnSinceDays int
}

// Analyze walks `root` (file or directory) and returns parsed files.
// Files that fail to read or parse are silently skipped; errors from
// the walk itself (e.g. root doesn't exist) surface as the returned
// error. File.Path is relative to root.
func (a Analyzer) Analyze(root string) ([]microstate.File, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var files []microstate.File
	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return skipDir(path, rootAbs)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if f, ok := a.parseGoFile(path, rootAbs); ok {
			files = append(files, f)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

func skipDir(path, rootAbs string) error {
	if path == rootAbs {
		return nil
	}
	base := filepath.Base(path)
	if base == "vendor" || strings.HasPrefix(base, ".") {
		return fs.SkipDir
	}
	return nil
}

func (a Analyzer) parseGoFile(path, rootAbs string) (microstate.File, bool) {
	src, err := os.ReadFile(path) // #nosec G304 -- path comes from the user-specified analysis root.
	if err != nil {
		return microstate.File{}, false
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return microstate.File{}, false
	}
	relPath := path
	if rel, err := filepath.Rel(rootAbs, path); err == nil {
		relPath = rel
	}
	f := microstate.File{
		Path: relPath,
		Src:  src,
		AST:  node,
		Fset: fset,
	}
	a.attachChurn(&f)
	return f, true
}

func (a Analyzer) attachChurn(f *microstate.File) {
	if a.ChurnRunner == nil {
		return
	}
	days := a.ChurnSinceDays
	if days == 0 {
		days = defaultChurnSinceDays
	}
	if c, err := gitx.ChurnCount(a.ChurnRunner, f.Path, days); err == nil {
		f.ChurnCount = c
	}
}
