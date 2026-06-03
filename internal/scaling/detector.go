package scaling

import "github.com/pavlov061356/entrolint/internal/engine/gitx"

// Input is the bag of evidence the check pipeline assembles for a PR
// and hands to every detector. Started life as a `[]gitx.Change`
// parameter — once the second axis (raw patches for shotgun) arrived,
// a wrapper became cheaper than a growing parameter list. Future axes
// (types.Package for implementor_scan, references map for fanout)
// will land here without re-touching call sites.
type Input struct {
	Changes []gitx.Change
	// Patches is keyed by head-side path (or base-side for deletions)
	// and contains the raw `git diff --unified=0 -z` payload for that
	// file. Empty map is valid — detectors that don't need patches
	// (none yet, but kept for future asymmetry) ignore it.
	Patches map[string][]byte
}

// Hit is one detector firing on one site. Size is the architectural
// span the class claims (k implementors, n cases, n files for shotgun).
// JSON field names track docs/scaling.md §"Per-change vs aggregate" —
// the spec doc is the canonical reference for the wire shape.
type Hit struct {
	Detector string `json:"name"`
	Class    Class  `json:"class"`
	Size     int    `json:"size,omitempty"`
	Path     string `json:"path"`
	Evidence string `json:"evidence,omitempty"`
}

type Detector interface {
	Name() string
	Analyze(Input) []Hit
}

type FileResult struct {
	Path  string `json:"path"`
	Class Class  `json:"class"`
	Hits  []Hit  `json:"detectors,omitempty"`
}

// Result is the PR-level scaling verdict. Class is the max over all
// per-file classes. DowngradeBonus is the negative ΔS contribution
// from class downgrades — formula in docs/scaling.md §"Downgrade reward".
type Result struct {
	Class          Class        `json:"class"`
	Files          []FileResult `json:"scaling_breakdown"`
	Acknowledged   []Hit        `json:"acknowledged_scaling,omitempty"`
	DowngradeBonus float64      `json:"downgrade_bonus"`
}
