// Package cache persists thermo calibration parameters to
// `.entrolint.cache.json` so subsequent scans don't have to refit the
// lognormal distributions or recompute k. The cache is per-repo and
// invalidated on formula/config mismatches or manually via `--recalibrate`.
package cache

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

// SchemaVersion is the cache file schema version. Bumping this
// invalidates older caches; loaders reject unknown versions rather
// than guessing at compatibility.
const SchemaVersion = 1

// DefaultPath is the conventional filename relative to the repo root.
const DefaultPath = ".entrolint.cache.json"

// State is the serialized calibration of a thermo.Engine.
type State struct {
	Version     int                               `json:"version"`
	Signature   Signature                         `json:"signature"`
	K           float64                           `json:"k"`
	Alpha       float64                           `json:"alpha"`
	Microstates map[string]thermo.LogNormalParams `json:"microstates"`
}

// Signature identifies the formula/config contract a calibration was fitted
// under. It is deliberately narrower than the whole user config: only values
// that affect S calibration belong here.
type Signature struct {
	FormulaVersion     int                `json:"formula_version"`
	Microstates        []string           `json:"microstates"`
	Weights            map[string]float64 `json:"weights"`
	NormalizationFloor float64            `json:"normalization_floor"`
	Alpha              float64            `json:"alpha"`
}

// StateFromEngine extracts the calibration data needed to reconstruct
// the engine later via thermo.NewEngine.
func StateFromEngine(e *thermo.Engine, sig Signature) State {
	return State{
		Version:     SchemaVersion,
		Signature:   cloneSignature(sig),
		K:           e.K(),
		Alpha:       e.Alpha(),
		Microstates: e.LogNormalByName(),
	}
}

// Load reads a cache file from path and returns the parsed state.
// Errors include os.PathError for missing files (callers can check
// via errors.Is(err, fs.ErrNotExist)) and version mismatch.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the user's cache file.
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse cache: %w", err)
	}
	if s.Version != SchemaVersion {
		return State{}, fmt.Errorf("cache schema version mismatch: got %d, want %d", s.Version, SchemaVersion)
	}
	return s, nil
}

// HasAll reports whether the cached state was calibrated against every
// microstate in names. It checks only that each key is PRESENT: a missing
// key means the cache predates that microstate (a stale cache), so callers
// treat HasAll=false as a miss and recalibrate. A present-but-degenerate
// entry (Valid:false) is NOT a miss — it is a legitimately cached result
// for a microstate that had no signal in the corpus it was fit on (e.g.
// cross_duplication or duplication on a clone-free repo).
//
// Counting a degenerate fit as "absent" would force a full recalibration on
// every run of any repo with a sparse microstate — and would be inconsistent
// with the project's "calibrate once, manual `--recalibrate`"
// contract, under which a Valid:true fit also goes stale silently as the
// repo evolves. The cached degenerate params are safe to keep: thermo.normalize
// returns 0 for a !Valid microstate, so it correctly contributes nothing to S
// (which is exactly right while that microstate has no signal).
func (s State) HasAll(names []string) bool {
	for _, n := range names {
		if _, ok := s.Microstates[n]; !ok {
			return false
		}
	}
	return true
}

// ValidFor reports whether this cache can be reused for the given formula
// signature. Old caches without a signature intentionally miss here.
func (s State) ValidFor(sig Signature) bool {
	return s.Signature.Equal(sig) && s.HasAll(sig.Microstates)
}

// Equal compares two signatures without depending on map iteration order.
func (s Signature) Equal(other Signature) bool {
	if s.FormulaVersion != other.FormulaVersion ||
		s.NormalizationFloor != other.NormalizationFloor ||
		s.Alpha != other.Alpha ||
		len(s.Microstates) != len(other.Microstates) ||
		len(s.Weights) != len(other.Weights) {
		return false
	}
	for i := range s.Microstates {
		if s.Microstates[i] != other.Microstates[i] {
			return false
		}
	}
	for name, weight := range s.Weights {
		if other.Weights[name] != weight {
			return false
		}
	}
	return true
}

// Save writes state to path, creating or truncating the file.
func Save(path string, s State) error {
	if s.Version == 0 {
		s.Version = SchemaVersion
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644) // #nosec G306 -- cache file is per-user, no secrets.
}

func cloneSignature(sig Signature) Signature {
	out := Signature{
		FormulaVersion:     sig.FormulaVersion,
		NormalizationFloor: sig.NormalizationFloor,
		Alpha:              sig.Alpha,
		Microstates:        append([]string(nil), sig.Microstates...),
	}
	if sig.Weights != nil {
		out.Weights = make(map[string]float64, len(sig.Weights))
		for k, v := range sig.Weights {
			out.Weights[k] = v
		}
	}
	return out
}
