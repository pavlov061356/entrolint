// Package scaling computes the predictive scaling class (O-notation)
// of a PR alongside the descriptive ΔS. See docs/scaling.md for the
// formula. v0.2.0 ships the surface with an empty detector registry —
// every PR resolves to O(1); real detectors land in later v0.2.x.
package scaling

import (
	"encoding/json"
	"fmt"
)

type Class int

const (
	ClassO1 Class = iota
	ClassOk
	ClassON
	ClassONM
	ClassO2N
)

// String emits the typographic forms used in docs/scaling.md so a
// reader can copy-grep between the spec and a real --json payload.
// ParseClass also accepts the ASCII variants (O(n*m), O(2^n)) for
// keyboard-friendly config and shell pipelines.
func (c Class) String() string {
	switch c {
	case ClassO1:
		return "O(1)"
	case ClassOk:
		return "O(k)"
	case ClassON:
		return "O(n)"
	case ClassONM:
		return "O(n·m)"
	case ClassO2N:
		return "O(2ⁿ)"
	default:
		return "O(?)"
	}
}

func (c Class) Less(other Class) bool { return int(c) < int(other) }

func Max(a, b Class) Class {
	if a.Less(b) {
		return b
	}
	return a
}

func (c Class) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

func (c *Class) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseClass(s)
	if err != nil {
		// Unknown identifiers (e.g. an O(n log n) added in a future
		// release) collapse to O(1) so older binaries can still read
		// newer JSON without erroring.
		*c = ClassO1
		return nil
	}
	*c = parsed
	return nil
}

func ParseClass(s string) (Class, error) {
	switch s {
	case "O(1)":
		return ClassO1, nil
	case "O(k)":
		return ClassOk, nil
	case "O(n)":
		return ClassON, nil
	case "O(n*m)", "O(n·m)":
		return ClassONM, nil
	case "O(2^n)", "O(2ⁿ)":
		return ClassO2N, nil
	}
	return ClassO1, fmt.Errorf("scaling: unknown class %q", s)
}
