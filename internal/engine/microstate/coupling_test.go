package microstate

import "testing"

func TestCoupling_Name(t *testing.T) {
	if got := (Coupling{}).Name(); got != "coupling" {
		t.Errorf("Name() = %q, want %q", got, "coupling")
	}
}

func TestCoupling_Measure(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want float64
	}{
		{
			name: "no imports",
			src:  `package p`,
			want: 0,
		},
		{
			name: "single import",
			src: `package p

import "fmt"

func F() { fmt.Println() }
`,
			want: 1,
		},
		{
			name: "grouped imports each count",
			src: `package p

import (
	"fmt"
	"os"
	"strings"
)

func F() { _ = fmt.Sprintf("%s", os.Args[0]); _ = strings.ToLower }
`,
			want: 3,
		},
		{
			name: "blank side-effect import counts",
			src: `package p

import (
	"fmt"
	_ "net/http/pprof"
)

func F() { fmt.Println() }
`,
			want: 2,
		},
		{
			name: "dot import counts",
			src: `package p

import (
	"fmt"
	. "strings"
)

var _ = ToLower
var _ = fmt.Sprintf
`,
			want: 2,
		},
		{
			name: "aliased import counts",
			src: `package p

import (
	"fmt"
	x "os"
)

func F() { fmt.Println(x.Args) }
`,
			want: 2,
		},
		{
			name: "separate import statements aggregate",
			src: `package p

import "fmt"
import "os"

func F() { fmt.Println(os.Args) }
`,
			want: 2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Coupling{}.Measure(parseFile(t, tc.src))
			if got != tc.want {
				t.Errorf("Measure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCoupling_NilAST(t *testing.T) {
	got := Coupling{}.Measure(File{Path: "x.go"})
	if got != 0 {
		t.Errorf("Measure(nil AST) = %v, want 0", got)
	}
}
