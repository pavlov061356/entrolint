// Package cache persists thermo calibration parameters to
// `.entrolint.cache.json` so subsequent scans don't have to refit the
// lognormal distributions or recompute k. The cache is per-repo and
// invalidated manually via `entrolint recalibrate`.
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
	K           float64                           `json:"k"`
	Alpha       float64                           `json:"alpha"`
	Microstates map[string]thermo.LogNormalParams `json:"microstates"`
}

// StateFromEngine extracts the calibration data needed to reconstruct
// the engine later via thermo.NewEngine.
func StateFromEngine(e *thermo.Engine) State {
	return State{
		Version:     SchemaVersion,
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

// HasAll reports whether the cached state has lognormal params for
// every microstate in ms. A missing entry means the cache was written
// before a new microstate was added — thermo.normalize would silently
// return 0 for the missing one, dropping its weighted contribution
// from S. Callers should treat HasAll=false as a cache miss and
// recalibrate.
func (s State) HasAll(names []string) bool {
	for _, n := range names {
		if p, ok := s.Microstates[n]; !ok || !p.Valid {
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
