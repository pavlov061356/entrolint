package scaling

import (
	"sort"

	"github.com/pavlov061356/entrolint/internal/engine/gitx"
)

// Registry is the list of detectors Analyze fans hits across. v0.2.0
// ships it empty — every PR resolves to O(1). New detectors are
// appended here (or injected via AnalyzeWith) as they land.
var Registry []Detector

func Analyze(in Input) Result {
	return AnalyzeWith(Registry, in)
}

// AnalyzeWith runs the given detector list against in and aggregates
// hits per file, then maxes file classes into the PR-level class.
// Exposed so tests (and future plug-in registrations) can inject a
// custom detector set without touching the global Registry.
func AnalyzeWith(detectors []Detector, in Input) Result {
	hitsByPath := make(map[string][]Hit)
	for _, d := range detectors {
		for _, h := range d.Analyze(in) {
			hitsByPath[h.Path] = append(hitsByPath[h.Path], h)
		}
	}

	paths := make([]string, 0, len(in.Changes))
	for _, c := range in.Changes {
		paths = append(paths, c.Path)
	}
	for p := range hitsByPath {
		if _, ok := changePath(in.Changes, p); !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	files := make([]FileResult, 0, len(paths))
	prClass := ClassO1
	for _, p := range paths {
		hits := hitsByPath[p]
		fileClass := ClassO1
		for _, h := range hits {
			fileClass = Max(fileClass, h.Class)
		}
		files = append(files, FileResult{Path: p, Class: fileClass, Hits: hits})
		prClass = Max(prClass, fileClass)
	}

	return Result{Class: prClass, Files: files}
}

func changePath(changes []gitx.Change, p string) (int, bool) {
	for i, c := range changes {
		if c.Path == p {
			return i, true
		}
	}
	return -1, false
}
