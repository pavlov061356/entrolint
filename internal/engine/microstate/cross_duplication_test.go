package microstate

import (
	"go/parser"
	"go/token"
	"testing"
)

// parseF parses src into a File labeled path. Each file gets its own
// FileSet, mirroring ParseGoBytes / the corpus pre-pass.
func parseF(t *testing.T, path, src string) File {
	t.Helper()
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return File{Path: path, Src: []byte(src), AST: node, Fset: fset}
}

// A function whose body holds a ≥dupMinNodes structural block. Reused
// verbatim (modulo function name) so the block forms a clone class.
func cloneBody(funcName string) string {
	return `package p

func ` + funcName + `(x int) int {
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	return 0
}
`
}

type fakeCorpus map[string]float64

func (f fakeCorpus) CrossDupMass(path string) float64 { return f[path] }

func TestCrossDuplication_Measure(t *testing.T) {
	m := CrossDuplication{}
	if got := m.Measure(File{Path: "x.go"}); got != 0 {
		t.Errorf("nil corpus must yield 0, got %v", got)
	}
	f := File{Path: "x.go", Corpus: fakeCorpus{"x.go": 7}}
	if got := m.Measure(f); got != 7 {
		t.Errorf("attached corpus: got %v, want 7", got)
	}
	if got := m.Measure(File{Path: "other.go", Corpus: fakeCorpus{"x.go": 7}}); got != 0 {
		t.Errorf("path with no mass must yield 0, got %v", got)
	}
}

func TestCrossDupMassByFile_ChargesNonOriginalOnly(t *testing.T) {
	// The same block in two files: the lexicographically lowest path is
	// the free "original"; the other pays the class size.
	files := []File{
		parseF(t, "b_second.go", cloneBody("B")),
		parseF(t, "a_first.go", cloneBody("A")),
	}
	mass := CrossDupMassByFile(files)

	if mass["a_first.go"] != 0 {
		t.Errorf("original (lowest path) must be free, got %v", mass["a_first.go"])
	}
	if mass["b_second.go"] <= 0 {
		t.Errorf("non-original file must carry cross-file mass, got %v", mass["b_second.go"])
	}
}

func TestCrossDupMassByFile_IntraFileOnlyIsNotCharged(t *testing.T) {
	// The block is duplicated TWICE within a single file and appears in
	// no other file. That is intra-file duplication (owned by the
	// Duplication microstate) — cross_duplication must charge 0.
	src := `package p

func A(x int) int {
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	return 0
}
`
	files := []File{parseF(t, "solo.go", src)}
	mass := CrossDupMassByFile(files)
	if mass["solo.go"] != 0 {
		t.Errorf("intra-file-only clone must not be charged cross-file, got %v", mass["solo.go"])
	}

	// Sanity: the intra-file Duplication microstate DOES see it, so the
	// two microstates partition the clone (no double-count, no gap).
	if (Duplication{}).Measure(files[0]) <= 0 {
		t.Error("intra-file Duplication should score the in-file clone")
	}
}

func TestCrossDupMassByFile_PartitionWithDuplication(t *testing.T) {
	// Class appears 3× total: twice in dup.go, once in single.go.
	// N=3, D=2 distinct files. Intra-file part (N-D)=1 redundancy lives
	// in Duplication(dup.go); cross-file part (D-1)=1 lives here, charged
	// to the non-original file. Lowest path "dup.go" < "single.go" is the
	// original, so single.go pays exactly one class size.
	dup := `package p

func A(x int) int {
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	if x > 0 {
		y := x*2 + 1
		z := y * y
		return z - 1
	}
	return 0
}
`
	files := []File{
		parseF(t, "dup.go", dup),
		parseF(t, "single.go", cloneBody("B")),
	}
	mass := CrossDupMassByFile(files)
	if mass["dup.go"] != 0 {
		t.Errorf("original file pays no cross-file mass, got %v", mass["dup.go"])
	}
	if mass["single.go"] <= 0 {
		t.Errorf("non-original file must pay one class size, got %v", mass["single.go"])
	}

	// The cross-file charge equals one class size — the same size the
	// CloneIndex records for that digest.
	idx := CloneIndex(files)
	var classSize float64
	for _, cc := range idx {
		if len(distinctFiles(cc)) >= 2 && float64(cc.Size) > classSize {
			classSize = float64(cc.Size)
		}
	}
	if mass["single.go"] != classSize {
		t.Errorf("cross-file charge %v should equal one class size %v", mass["single.go"], classSize)
	}
}

func TestCloneIndex_RequiresTwoOccurrences(t *testing.T) {
	// A single unique file with no internal repetition yields no classes.
	files := []File{parseF(t, "lonely.go", cloneBody("A"))}
	if idx := CloneIndex(files); len(idx) != 0 {
		t.Errorf("a non-repeated tree must produce no clone classes, got %d", len(idx))
	}
}

// TestCloneIndex_CollectsEveryOccurrence locks the two-pass index: the
// counting pass keeps only recurring digests, and the collection pass must
// then gather ALL their occurrences — one per file here — not just the two
// that proved the class recurred.
func TestCloneIndex_CollectsEveryOccurrence(t *testing.T) {
	files := []File{
		parseF(t, "a.go", cloneBody("A")),
		parseF(t, "b.go", cloneBody("B")),
		parseF(t, "c.go", cloneBody("C")),
	}
	idx := CloneIndex(files)
	if len(idx) == 0 {
		t.Fatal("three identical bodies must form at least one clone class")
	}
	for h, cc := range idx {
		if len(cc.Occ) != 3 {
			t.Errorf("class %x: got %d occurrences, want 3 (one per file)", h, len(cc.Occ))
		}
	}
}

func TestCrossDupMassByFile_OrderIndependent(t *testing.T) {
	// File z holds the inner block both standalone AND nested inside an
	// outer block; the outer block and the inner block are each cross-file
	// clones (shared with other files). The cross mass must not depend on
	// whether the standalone copy appears before or after the nested one —
	// a regression guard against the earlier representative-occurrence
	// nesting heuristic, which produced different values per ordering.
	inner := "\tfor i := 0; i < n; i++ {\n\t\tacc = acc + i*i\n\t\tacc = acc - i\n\t\ttotal = total + acc\n\t}\n"
	outer := func(body string) string { return "\tif n > 0 {\n" + body + "\t}\n" }
	zNestedFirst := "package p\n\nfunc Z(n int) int {\n\tacc, total := 0, 0\n" + outer(inner) + inner + "\treturn total\n}\n"
	zStandaloneFirst := "package p\n\nfunc Z(n int) int {\n\tacc, total := 0, 0\n" + inner + outer(inner) + "\treturn total\n}\n"

	mass := func(zSrc string) float64 {
		files := []File{
			parseF(t, "a_outer.go", "package p\n\nfunc A(n int) int {\n\tacc, total := 0, 0\n"+outer(inner)+"\treturn total\n}\n"),
			parseF(t, "b_inner.go", "package p\n\nfunc B(n int) int {\n\tacc, total := 0, 0\n"+inner+"\treturn total\n}\n"),
			parseF(t, "z.go", zSrc),
		}
		return CrossDupMassByFile(files)["z.go"]
	}

	if a, b := mass(zNestedFirst), mass(zStandaloneFirst); a != b {
		t.Errorf("cross mass must not depend on source order: nestedFirst=%v standaloneFirst=%v", a, b)
	}
}

func distinctFiles(cc CloneClass) map[string]bool {
	s := make(map[string]bool)
	for _, o := range cc.Occ {
		s[o.Path] = true
	}
	return s
}
