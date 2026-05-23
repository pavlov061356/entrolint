package microstate

import (
	"go/parser"
	"go/token"
	"testing"
)

func parseFile(t *testing.T, src string) File {
	t.Helper()
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return File{Path: "test.go", AST: node, Fset: fset}
}

func TestCyclomatic_Name(t *testing.T) {
	if got := (Cyclomatic{}).Name(); got != "cyclomatic" {
		t.Errorf("Name() = %q, want %q", got, "cyclomatic")
	}
}

func TestCyclomatic_Measure(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want float64
	}{
		{
			name: "empty function",
			src: `package p
func F() {}`,
			want: 0,
		},
		{
			name: "single if",
			src: `package p
func F(x int) { if x > 0 {} }`,
			want: 1,
		},
		{
			name: "if-else counts once",
			src: `package p
func F(x int) { if x > 0 {} else {} }`,
			want: 1,
		},
		{
			name: "for loop",
			src: `package p
func F() { for i := 0; i < 10; i++ {} }`,
			want: 1,
		},
		{
			name: "range loop",
			src: `package p
func F(xs []int) { for range xs {} }`,
			want: 1,
		},
		{
			name: "switch with three cases including default",
			src: `package p
func F(x int) {
	switch x {
	case 1:
	case 2:
	default:
	}
}`,
			want: 3,
		},
		{
			name: "select with two cases",
			src: `package p
func F(ch chan int) {
	select {
	case <-ch:
	case ch <- 1:
	}
}`,
			want: 2,
		},
		{
			name: "logical and",
			src: `package p
func F(a, b bool) bool { return a && b }`,
			want: 1,
		},
		{
			name: "logical or",
			src: `package p
func F(a, b bool) bool { return a || b }`,
			want: 1,
		},
		{
			name: "chained logical operators",
			src: `package p
func F(a, b, c bool) bool { return a && b && c || a }`,
			want: 3,
		},
		{
			name: "defer recover counts",
			src: `package p
func F() { defer recover() }`,
			want: 1,
		},
		{
			name: "defer non-recover does not count",
			src: `package p
import "fmt"
func F() { defer fmt.Println("done") }`,
			want: 0,
		},
		{
			name: "two functions sum",
			src: `package p
func A(x int) { if x > 0 {} }
func B(y int) { if y < 0 {} else if y == 0 {} }`,
			want: 3,
		},
		{
			name: "method on a type",
			src: `package p
type T struct{}
func (T) F(x int) { if x > 0 {} }`,
			want: 1,
		},
		{
			name: "closure decision points roll up to parent",
			src: `package p
func F() {
	g := func(x int) {
		if x > 0 {}
	}
	g(1)
}`,
			want: 1,
		},
		{
			name: "nested combinations",
			src: `package p
func F(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 && x < 10 {
			total++
		}
	}
	return total
}`,
			want: 3, // range + if + &&
		},
		{
			name: "function without body (e.g. assembly stub)",
			src: `package p
func F()`,
			want: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Cyclomatic{}.Measure(parseFile(t, tc.src))
			if got != tc.want {
				t.Errorf("Measure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCyclomatic_NilAST(t *testing.T) {
	got := Cyclomatic{}.Measure(File{Path: "x.go"})
	if got != 0 {
		t.Errorf("Measure(empty File) = %v, want 0", got)
	}
}
