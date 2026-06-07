// Package shotgun detects scatter-PR patterns: one logical change
// scattered across ≥ MinFiles Go files with no common AST parent.
// See docs/scaling.md#heuristics.
//
// The detector groups files by the fingerprint of their canonicalized
// added/removed lines (trimmed of leading/trailing whitespace, empty
// lines dropped). When a group reaches MinFiles, every file in it
// fires an O(n) hit with size = group size — `awk`-equivalent edits
// of the same line in five places signal shotgun surgery.
//
// gofumpt-only / whitespace-only diffs are filtered before grouping
// to keep formatter PRs from tripping the heuristic.
package shotgun

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/pavlov061356/entrolint/internal/scaling"
)

const (
	// DefaultMinFiles is the v0.2 starting threshold per docs/scaling.md.
	DefaultMinFiles = 5

	name = "shotgun"
)

type Detector struct {
	MinFiles int
}

func New() *Detector { return &Detector{MinFiles: DefaultMinFiles} }

func (*Detector) Name() string { return name }

func (d *Detector) Analyze(in scaling.Input) []scaling.Hit {
	threshold := d.MinFiles
	if threshold <= 0 {
		threshold = DefaultMinFiles
	}

	byFP := make(map[string][]string)
	for _, c := range in.Changes {
		if !strings.HasSuffix(c.Path, ".go") {
			continue
		}
		patch := in.Patches[c.Path]
		if len(patch) == 0 {
			continue
		}
		fp, empty := fingerprintPatch(patch)
		if empty {
			continue
		}
		byFP[fp] = append(byFP[fp], c.Path)
	}

	groups := make([][]string, 0, len(byFP))
	for _, paths := range byFP {
		if len(paths) >= threshold {
			sort.Strings(paths)
			groups = append(groups, paths)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })

	var hits []scaling.Hit
	for _, paths := range groups {
		size := len(paths)
		evidence := fmt.Sprintf("%d files share an identical add/remove pattern", size)
		for _, p := range paths {
			hits = append(hits, scaling.Hit{
				Detector: name,
				Class:    scaling.ClassON,
				Size:     size,
				Path:     p,
				Evidence: evidence,
			})
		}
	}
	return hits
}

// fingerprintPatch hashes canonicalized add/remove lines. empty=true
// when the diff carries only whitespace/format changes — those don't
// fire the heuristic per docs/scaling.md#known-simplifications-v02.
func fingerprintPatch(patch []byte) (string, bool) {
	var added, removed []string
	for _, line := range strings.Split(string(patch), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "index ") {
			continue
		}
		var bucket *[]string
		var body string
		switch line[0] {
		case '+':
			bucket = &added
			body = strings.TrimSpace(line[1:])
		case '-':
			bucket = &removed
			body = strings.TrimSpace(line[1:])
		default:
			continue
		}
		if body == "" {
			continue
		}
		*bucket = append(*bucket, body)
	}
	if len(added) == 0 && len(removed) == 0 {
		return "", true
	}
	sort.Strings(added)
	sort.Strings(removed)
	h := sha256.New()
	for _, s := range added {
		h.Write([]byte("+" + s + "\n"))
	}
	for _, s := range removed {
		h.Write([]byte("-" + s + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), false
}

func init() {
	scaling.Registry = append(scaling.Registry, New())
}
