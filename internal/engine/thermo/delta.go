package thermo

import (
	"encoding/json"
	"math"
)

// DeltaKind classifies how a file contributes to ΔS. Renames fold
// into DeltaModified — both reduce to "subtract S_base from S_head"
// regardless of whether the path changed alongside the content.
type DeltaKind int

const (
	// DeltaModified — file exists at both refs; Δ = S_head − S_base.
	DeltaModified DeltaKind = iota
	// DeltaAdded — file exists only at head; Δ = S_head.
	DeltaAdded
	// DeltaRemoved — file exists only at base; Δ = −S_base.
	DeltaRemoved
)

// String returns the lowercase identifier used in reports and JSON.
func (k DeltaKind) String() string {
	switch k {
	case DeltaAdded:
		return "added"
	case DeltaRemoved:
		return "removed"
	default:
		return "modified"
	}
}

// MarshalJSON renders DeltaKind as a human-readable string so report
// consumers don't need to know the iota order.
func (k DeltaKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// FileDelta is one file's contribution to ΔS_total.
//
// Precondition on inputs: SBase and SHead are expected to be ≥ 0
// (S is non-negative by the formula spec). Callers passing negative
// values produce undefined ΔS — the package does not clamp.
//
// Path is the head-side path for Modified/Added, the base-side path
// for Removed. Delta is computed by MakeFileDelta with the correct
// sign per Kind; ComputeDelta recomputes it from Kind/SBase/SHead so
// a hand-constructed FileDelta literal with a desynced Delta cannot
// poison the aggregate.
type FileDelta struct {
	Path  string    `json:"path"`
	Kind  DeltaKind `json:"kind"`
	SBase float64   `json:"s_base"`
	SHead float64   `json:"s_head"`
	Delta float64   `json:"delta"`
}

// Delta is the aggregated PR-level entropy change.
//
// Total is the signed Σ of all file contributions. Density is
// Total / max(1, LinesChanged) per docs/formula.md §"ΔS для режима
// check" — a small positive Density on a large PR fails less
// aggressively than the same Total on a tiny PR. Files holds an
// owned (copied) breakdown — safe for callers to sort, mutate, or
// hand to a goroutine.
type Delta struct {
	Total        float64     `json:"total"`
	Density      float64     `json:"density"`
	LinesChanged int         `json:"lines_changed"`
	Files        []FileDelta `json:"files"`
}

// MakeFileDelta builds a FileDelta with sign convention baked in:
//   - DeltaModified: Delta = sHead − sBase
//   - DeltaAdded:    Delta = sHead;    sBase is forced to 0
//   - DeltaRemoved:  Delta = −sBase;   sHead is forced to 0
//
// Forcing the absent side to zero keeps JSON output unambiguous for
// downstream report consumers.
func MakeFileDelta(path string, kind DeltaKind, sBase, sHead float64) FileDelta {
	switch kind {
	case DeltaAdded:
		sBase = 0
	case DeltaRemoved:
		sHead = 0
	}
	return FileDelta{
		Path:  path,
		Kind:  kind,
		SBase: sBase,
		SHead: sHead,
		Delta: signedDelta(kind, sBase, sHead),
	}
}

// signedDelta is the canonical sign convention used by both
// MakeFileDelta and ComputeDelta — sharing it makes the math
// self-correcting: ComputeDelta ignores a caller-supplied Delta in
// favor of the recomputed value, so manual FileDelta literals cannot
// desync the aggregate.
func signedDelta(kind DeltaKind, sBase, sHead float64) float64 {
	switch kind {
	case DeltaAdded:
		return sHead
	case DeltaRemoved:
		return -sBase
	default: // DeltaModified
		return sHead - sBase
	}
}

// ComputeDelta aggregates per-file contributions into Total and
// Density. linesChanged is the PR-wide sum of changed lines (as
// reported by gitx.Change.LinesChanged across all entries).
//
// Total is recomputed from Kind/SBase/SHead, not read from
// FileDelta.Delta, so a malformed entry cannot poison the aggregate.
// The returned Delta owns its Files slice (defensive copy) — callers
// of either side may sort or mutate without affecting the other.
func ComputeDelta(files []FileDelta, linesChanged int) Delta {
	var total float64
	for _, f := range files {
		total += signedDelta(f.Kind, f.SBase, f.SHead)
	}
	denom := linesChanged
	if denom < 1 {
		denom = 1
	}
	var owned []FileDelta
	if len(files) > 0 {
		owned = make([]FileDelta, len(files))
		copy(owned, files)
	}
	return Delta{
		Total:        total,
		Density:      total / float64(denom),
		LinesChanged: linesChanged,
		Files:        owned,
	}
}

// Fails reports whether the PR exceeds the gate threshold.
//
// Strict `Density > densityMax` per formula doc §"Гейт срабатывает
// по плотности". Two fail-closed guards: NaN Density (degenerate
// upstream score / mixed ±Inf cancellation) and +Inf Density both
// trip the gate so corruption in the pipeline cannot silently pass.
// A PR that reduces entropy (negative Density) never fails for any
// non-negative threshold — the only threshold shape the spec
// sanctions.
func (d Delta) Fails(densityMax float64) bool {
	if math.IsNaN(d.Density) {
		return true
	}
	if math.IsInf(d.Density, 1) {
		return true
	}
	return d.Density > densityMax
}
