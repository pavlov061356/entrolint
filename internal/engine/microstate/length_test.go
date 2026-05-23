package microstate

import "testing"

func TestLength_Name(t *testing.T) {
	if got := (Length{}).Name(); got != "length" {
		t.Errorf("Name() = %q, want %q", got, "length")
	}
}

func TestLength_Measure(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want float64
	}{
		{
			name: "single-line package only",
			src:  `package p`,
			want: 1,
		},
		{
			name: "package and one function",
			src: `package p

func F() {}
`,
			want: 2,
		},
		{
			name: "blank lines are excluded",
			src: `package p


func F() {


	x := 1
	_ = x


}
`,
			want: 5,
		},
		{
			name: "line comments excluded",
			src: `package p

// top-level comment

func F() {
	// inside body
	x := 1
	_ = x // trailing — line still counts
}
`,
			want: 5,
		},
		{
			name: "block comments excluded",
			src: `package p

/* multi
   line
   block comment */

func F() {
	x := 1
	_ = x
}
`,
			want: 5,
		},
		{
			name: "line with code and trailing comment counts once",
			src: `package p

func F() { return } // closing brace and comment
`,
			want: 2,
		},
		{
			name: "imports count",
			src: `package p

import (
	"fmt"
	"os"
)

func F() { fmt.Println(os.Args) }
`,
			want: 6,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Length{}.Measure(parseFile(t, tc.src))
			if got != tc.want {
				t.Errorf("Measure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLength_EmptySrc(t *testing.T) {
	got := Length{}.Measure(File{Path: "x.go"})
	if got != 0 {
		t.Errorf("Measure(empty File) = %v, want 0", got)
	}
}
