package microstate

import (
	"go/scanner"
	"go/token"
)

// Length counts physical lines of code in a file, excluding blank
// lines and comment-only lines. Per the v0.1 formula spec, this is a
// file-level microstate (no per-function aggregation).
//
// Implementation: tokenize with go/scanner (mode 0 silently skips
// comments) and collect the distinct line numbers where any token
// appears. A line counts if it carries at least one non-comment
// token; blank lines have no tokens and so are excluded naturally.
type Length struct{}

// Name returns the microstate identifier used in config and reports.
func (Length) Name() string { return "length" }

// Measure returns the count of source lines that contain at least one
// non-comment token.
func (Length) Measure(f File) float64 {
	if len(f.Src) == 0 {
		return 0
	}
	fset := token.NewFileSet()
	tf := fset.AddFile(f.Path, -1, len(f.Src))
	var s scanner.Scanner
	s.Init(tf, f.Src, nil, 0)
	lines := make(map[int]struct{})
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		lines[fset.Position(pos).Line] = struct{}{}
	}
	return float64(len(lines))
}
