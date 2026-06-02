package microstate

import "testing"

func TestNesting_Name(t *testing.T) {
	if got := (Nesting{}).Name(); got != "nesting" {
		t.Errorf("Name() = %q, want %q", got, "nesting")
	}
}

func TestNesting_Measure(t *testing.T) {
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
			name: "linear statements no nesting",
			src: `package p
func F() {
	x := 1
	_ = x
}`,
			want: 0,
		},
		{
			name: "single if",
			src: `package p
func F(x int) { if x > 0 { _ = x } }`,
			want: 1,
		},
		{
			name: "nested if-if",
			src: `package p
func F(x int) {
	if x > 0 {
		if x > 1 {
			_ = x
		}
	}
}`,
			want: 2,
		},
		{
			name: "for loop",
			src: `package p
func F() { for i := 0; i < 10; i++ { _ = i } }`,
			want: 1,
		},
		{
			name: "for in for",
			src: `package p
func F() {
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			_ = i + j
		}
	}
}`,
			want: 2,
		},
		{
			name: "range",
			src: `package p
func F(xs []int) { for _, x := range xs { _ = x } }`,
			want: 1,
		},
		{
			name: "switch case body counts as depth one",
			src: `package p
func F(x int) {
	switch x {
	case 1:
		_ = x
	}
}`,
			want: 1,
		},
		{
			name: "if inside switch case is depth two",
			src: `package p
func F(x int) {
	switch x {
	case 1:
		if x > 0 {
			_ = x
		}
	}
}`,
			want: 2,
		},
		{
			name: "if-else is depth one",
			src: `package p
func F(x int) {
	if x > 0 {
		_ = x
	} else {
		_ = -x
	}
}`,
			want: 1,
		},
		{
			name: "else-if chain stays at depth one",
			src: `package p
func F(x int) {
	if x > 0 {
		_ = x
	} else if x < 0 {
		_ = -x
	} else {
		_ = 0
	}
}`,
			want: 1,
		},
		{
			name: "closure inside body adds depth",
			src: `package p
func F() {
	g := func() {
		_ = 1
	}
	_ = g
}`,
			want: 1,
		},
		{
			name: "if inside closure adds two",
			src: `package p
func F() {
	g := func(x int) {
		if x > 0 {
			_ = x
		}
	}
	_ = g
}`,
			want: 2,
		},
		{
			name: "bare block",
			src: `package p
func F() {
	{
		_ = 1
	}
}`,
			want: 1,
		},
		{
			name: "max wins across functions",
			src: `package p
func A() { _ = 1 }
func B(x int) {
	if x > 0 {
		if x > 1 {
			_ = x
		}
	}
}
func C(x int) { if x > 0 { _ = x } }`,
			want: 2,
		},
		{
			name: "select case body is depth one",
			src: `package p
func F(ch chan int) {
	select {
	case <-ch:
		_ = 1
	}
}`,
			want: 1,
		},
		{
			name: "function without body",
			src: `package p
func F()`,
			want: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Nesting{}.Measure(parseFile(t, tc.src))
			if got != tc.want {
				t.Errorf("Measure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNesting_NilAST(t *testing.T) {
	got := Nesting{}.Measure(File{Path: "x.go"})
	if got != 0 {
		t.Errorf("Measure(empty File) = %v, want 0", got)
	}
}
