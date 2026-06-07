package microstate

import (
	"go/parser"
	"go/token"
	"testing"
)

// parseDup builds a File from src the way the analyzer does (with
// comments attached), so the duplication walk's comment-skipping is
// exercised.
func parseDup(t *testing.T, src string) File {
	t.Helper()
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "t.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return File{Path: "t.go", Src: []byte(src), AST: node, Fset: fset}
}

// A non-trivial body (> dupMinNodes) used as the clone unit.
const dupBodyAlpha = `
	x := compute(1, 2)
	y := compute(3, 4)
	if x > y {
		return x + y
	}
	return x - y`

// Same structure, different identifiers and literals — a Type-2 clone.
const dupBodyRenamed = `
	p := compute(9, 8)
	q := compute(7, 6)
	if p > q {
		return p + q
	}
	return p - q`

// Same size class but structurally different (subtraction/&& shape).
const dupBodyOther = `
	a := lookup(5)
	b := lookup(6)
	for a < b && a > 0 {
		a = a * b
	}
	return a`

func TestDuplication_Name(t *testing.T) {
	if got := (Duplication{}).Name(); got != "duplication" {
		t.Errorf("Name() = %q, want duplication", got)
	}
}

func TestDuplication_NilAST(t *testing.T) {
	if got := (Duplication{}).Measure(File{}); got != 0 {
		t.Errorf("Measure(nil AST) = %v, want 0", got)
	}
}

func TestDuplication_CleanFileIsZero(t *testing.T) {
	src := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc delta() int {" + dupBodyOther + "\n}\n"
	if got := (Duplication{}).Measure(parseDup(t, src)); got != 0 {
		t.Errorf("Measure(no clones) = %v, want 0", got)
	}
}

func TestDuplication_IdenticalBodiesCounted(t *testing.T) {
	src := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc beta() int {" + dupBodyAlpha + "\n}\n"
	if got := (Duplication{}).Measure(parseDup(t, src)); got <= 0 {
		t.Errorf("Measure(two identical bodies) = %v, want > 0", got)
	}
}

// Renaming identifiers and changing literals must not hide the clone:
// the renamed file scores exactly the same as the byte-identical one.
func TestDuplication_NormalizationMatchesRenamedClones(t *testing.T) {
	same := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc beta() int {" + dupBodyAlpha + "\n}\n"
	renamed := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc gamma() int {" + dupBodyRenamed + "\n}\n"
	gotSame := (Duplication{}).Measure(parseDup(t, same))
	gotRenamed := (Duplication{}).Measure(parseDup(t, renamed))
	if gotRenamed <= 0 {
		t.Fatalf("renamed clone not detected: got %v", gotRenamed)
	}
	if gotSame != gotRenamed {
		t.Errorf("renamed clone scored %v, identical clone scored %v; want equal (identifier/literal normalization)", gotRenamed, gotSame)
	}
}

// A third copy is more redundant mass than a second.
func TestDuplication_MonotonicInCopies(t *testing.T) {
	two := "package p\nfunc a() int {" + dupBodyAlpha + "\n}\nfunc b() int {" + dupBodyAlpha + "\n}\n"
	three := two + "func c() int {" + dupBodyAlpha + "\n}\n"
	got2 := (Duplication{}).Measure(parseDup(t, two))
	got3 := (Duplication{}).Measure(parseDup(t, three))
	if !(got3 > got2) {
		t.Errorf("three copies = %v, two copies = %v; want three > two", got3, got2)
	}
}

// Comments must not change the structural score.
func TestDuplication_CommentsIgnored(t *testing.T) {
	plain := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc beta() int {" + dupBodyAlpha + "\n}\n"
	withComments := "package p\n// alpha does a thing\nfunc alpha() int {" + dupBodyAlpha + "\n}\n\n// beta does the same thing, copy-pasted\nfunc beta() int {" + dupBodyAlpha + " // inline note\n}\n"
	gotPlain := (Duplication{}).Measure(parseDup(t, plain))
	gotComments := (Duplication{}).Measure(parseDup(t, withComments))
	if gotPlain != gotComments {
		t.Errorf("with comments = %v, without = %v; want equal (comments skipped)", gotComments, gotPlain)
	}
}

// Tiny identical bodies are below the size threshold and must not fire.
func TestDuplication_BelowThresholdIsZero(t *testing.T) {
	src := "package p\nfunc a() { return }\nfunc b() { return }\n"
	if got := (Duplication{}).Measure(parseDup(t, src)); got != 0 {
		t.Errorf("Measure(tiny identical bodies) = %v, want 0 (below dupMinNodes)", got)
	}
}

func TestDuplication_Deterministic(t *testing.T) {
	src := "package p\nfunc alpha() int {" + dupBodyAlpha + "\n}\nfunc beta() int {" + dupBodyAlpha + "\n}\n"
	f := parseDup(t, src)
	first := (Duplication{}).Measure(f)
	second := (Duplication{}).Measure(f)
	if first != second {
		t.Errorf("non-deterministic: %v then %v", first, second)
	}
}
