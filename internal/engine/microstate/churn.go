package microstate

// Churn reads the pre-computed commit count for a file from the
// calibration window. Per the v0.1 formula spec churn is NOT a
// contributor to S; it lives in S's temperature T = S · ξ(churn).
// This type exists so callers can drive churn through the uniform
// Microstate interface alongside the structural microstates.
//
// The actual git operation runs in internal/engine/gitx and the
// analyzer populates File.ChurnCount before measurement, keeping
// microstates pure and free of subprocess I/O.
type Churn struct{}

// Name returns the microstate identifier used in config and reports.
func (Churn) Name() string { return "churn" }

// Measure returns the pre-computed commit count for the file as a
// float64.
func (Churn) Measure(f File) float64 {
	return float64(f.ChurnCount)
}
