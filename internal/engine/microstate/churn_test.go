package microstate

import "testing"

func TestChurn_Name(t *testing.T) {
	if got := (Churn{}).Name(); got != "churn" {
		t.Errorf("Name() = %q, want %q", got, "churn")
	}
}

func TestChurn_Measure(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want float64
	}{
		{"zero churn", 0, 0},
		{"single commit", 1, 1},
		{"many commits", 42, 42},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Churn{}.Measure(File{Path: "x.go", ChurnCount: tc.in})
			if got != tc.want {
				t.Errorf("Measure(ChurnCount=%d) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
