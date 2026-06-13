package pipeline

import (
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

// Three clone "bodies" of increasing block size. Each is reused verbatim
// (modulo function name, which the structural hash normalizes away) so a
// pair of files sharing one body forms a cross-file clone class. Distinct
// sizes give cross_duplication ≥2 distinct positive calibration samples,
// so its lognormal fit is valid (a single sample would floor to 0).
func body1(fn string) string {
	return `package p

func ` + fn + `(x int) int {
	if x > 0 {
		a := x * 2
		b := a + 1
		return b
	}
	return 0
}
`
}

func body2(fn string) string {
	return `package p

func ` + fn + `(x int) int {
	if x > 0 {
		a := x * 2
		b := a + 1
		c := b * 3
		return c
	}
	return 0
}
`
}

func body3(fn string) string {
	return `package p

func ` + fn + `(x int) int {
	if x > 0 {
		a := x * 2
		b := a + 1
		c := b * 3
		d := c - 2
		e := d + a
		return e
	}
	return 0
}
`
}

// crossCalibCorpus is a working tree with three cross-file clone pairs of
// increasing block size, plus the varied calibrationCorpus for the other
// microstates. The lower path in each pair is the free "original"; the
// higher path carries the class size, giving cross_duplication a spread
// of positive samples to fit.
func crossCalibCorpus() map[string]string {
	files := calibrationCorpus()
	files["c1a.go"] = body1("C1A")
	files["c1b.go"] = body1("C1B")
	files["c2a.go"] = body2("C2A")
	files["c2b.go"] = body2("C2B")
	files["c3a.go"] = body3("C3A")
	files["c3b.go"] = body3("C3B")
	return files
}

// TestScan_CrossDuplicationSurfaces verifies the scan path attaches the
// corpus, calibrates on cross-file mass, and surfaces a positive
// cross_duplication contribution for the non-original file of the
// largest clone pair (large enough to clear the normalization floor),
// while the original pays nothing.
func TestScan_CrossDuplicationSurfaces(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, crossCalibCorpus())

	res, err := Scan(ScanOptions{Root: dir, Config: config.Default()})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byPath := make(map[string]FileScore, len(res.Files))
	for _, f := range res.Files {
		byPath[f.Path] = f
	}

	payer, ok := byPath["c3b.go"]
	if !ok {
		t.Fatal("c3b.go missing from scan output")
	}
	if payer.Contributions["cross_duplication"] <= 0 {
		t.Errorf("c3b.go (non-original of the largest clone) should have positive cross_duplication, got %v",
			payer.Contributions["cross_duplication"])
	}

	original, ok := byPath["c3a.go"]
	if !ok {
		t.Fatal("c3a.go missing from scan output")
	}
	if original.Contributions["cross_duplication"] != 0 {
		t.Errorf("c3a.go (the original) must pay no cross_duplication, got %v",
			original.Contributions["cross_duplication"])
	}
}

// TestCheck_CrossFileCloneRaisesHeadEntropy is a differential test: the
// SAME added file scores HIGHER at head when the head tree contains a
// clone partner than when it doesn't. The only thing that differs between
// the two runs is the presence of the partner in the head tree, so the
// SHead gap is attributable to cross_duplication alone — proving the
// check path builds the head corpus and threads it into scoring.
func TestCheck_CrossFileCloneRaisesHeadEntropy(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, crossCalibCorpus())

	const headSHA = "2222222222222222222222222222222222222222"
	const baseSHA = "1111111111111111111111111111111111111111"

	// z_new.go is added; its body is a clone of body3. Identical blob in
	// both runs, so every non-cross microstate is identical.
	added := body3("Znew")

	newRunner := func(headTree []string) *fakeRunner {
		return &fakeRunner{
			t:          t,
			resolved:   map[string]string{"base": baseSHA, "head": headSHA},
			rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "A", "z_new.go", "")),
			numstatOut: []byte(numRecord(12, 0, "z_new.go", "")),
			blobs: map[string][]byte{
				headSHA + ":z_new.go":      []byte(added),
				headSHA + ":a_existing.go": []byte(body3("Aexist")),
			},
			tree: map[string][]string{headSHA: headTree},
		}
	}

	withPartner, err := runCheck(t, dir, newRunner([]string{"a_existing.go", "z_new.go"}))
	if err != nil {
		t.Fatalf("Check (with partner): %v", err)
	}
	noPartner, err := runCheck(t, dir, newRunner([]string{"z_new.go"}))
	if err != nil {
		t.Fatalf("Check (no partner): %v", err)
	}

	sHeadWith := withPartner.Delta.Files[0].SHead
	sHeadNo := noPartner.Delta.Files[0].SHead
	if sHeadWith <= sHeadNo {
		t.Errorf("a head cross-file clone must raise SHead: with partner %v, without %v",
			sHeadWith, sHeadNo)
	}
}

// TestCheck_AsymmetricParseOfPartnerDoesNotPerturbSibling is the issue #68
// regression: a clone class's lowest-path partner is unparseable at base
// only, and the assertion is that an UNCHANGED sibling clone's ΔS is not
// perturbed by the partner's parse-flip.
//
//   - a_partner.go is Modified: a valid body3 clone at head, a syntax error
//     at base. It exists on both refs, so it is NOT a legitimate one-sided
//     add/remove — only its parseability differs.
//   - m_sibling.go is Modified with a byte-identical body3 clone on both
//     refs, so every microstate including cross_duplication MUST score it
//     identically and SBase must equal SHead.
//
// Without the symmetric-exclusion fix, a_partner.go is in the head corpus
// but not the base corpus: at head m_sibling.go pays the class size (the
// lower-path a_partner.go is the free original), at base it pays nothing
// (no partner) — a spurious positive ΔS. Holding a_partner.go out of both
// corpora restores SBase == SHead.
func TestCheck_AsymmetricParseOfPartnerDoesNotPerturbSibling(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, crossCalibCorpus())

	const baseSHA = "1111111111111111111111111111111111111111"
	const headSHA = "2222222222222222222222222222222222222222"

	clone := body3("Sibling")                   // identical on both refs
	brokenBase := "package p\nfunc Broken( {\n" // unparseable
	validHead := body3("Partner")

	fr := &fakeRunner{
		t:        t,
		resolved: map[string]string{"base": baseSHA, "head": headSHA},
		rawDiff: []byte(
			rawRecord("100644", "100644", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "M", "a_partner.go", "") +
				rawRecord("100644", "100644", "cccccccccccccccccccccccccccccccccccccccc", "cccccccccccccccccccccccccccccccccccccccc", "M", "m_sibling.go", ""),
		),
		numstatOut: []byte(
			numRecord(3, 3, "a_partner.go", "") +
				numRecord(0, 0, "m_sibling.go", ""),
		),
		blobs: map[string][]byte{
			baseSHA + ":a_partner.go": []byte(brokenBase),
			headSHA + ":a_partner.go": []byte(validHead),
			baseSHA + ":m_sibling.go": []byte(clone),
			headSHA + ":m_sibling.go": []byte(clone),
		},
		tree: map[string][]string{
			baseSHA: {"a_partner.go", "m_sibling.go"},
			headSHA: {"a_partner.go", "m_sibling.go"},
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	// a_partner.go fails to parse at base, so it is dropped from ΔS; only
	// the unchanged sibling is scored.
	var sib *thermo.FileDelta
	for i := range res.Delta.Files {
		if res.Delta.Files[i].Path == "m_sibling.go" {
			sib = &res.Delta.Files[i]
		}
	}
	if sib == nil {
		t.Fatalf("m_sibling.go missing from ΔS; files = %+v", res.Delta.Files)
	}
	if sib.SBase != sib.SHead {
		t.Errorf("unchanged sibling perturbed by partner's one-sided parse failure: SBase=%v SHead=%v (issue #68)",
			sib.SBase, sib.SHead)
	}
}

// TestCheck_PureRenameOfCloneKeysBaseByOldPath guards the base-side path
// keying. A file that is a cross-file clone is renamed with NO content
// change: at base it is a clone under its old name, at head under its new
// name, with the same partner, so every microstate — cross_duplication
// included — must score identically and SBase must equal SHead. If the
// base corpus were queried with the head-side path instead of OldPath,
// the base clone mass would read 0 and the pure rename would manufacture
// a spurious ΔS.
func TestCheck_PureRenameOfCloneKeysBaseByOldPath(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, crossCalibCorpus())

	const baseSHA = "1111111111111111111111111111111111111111"
	const headSHA = "2222222222222222222222222222222222222222"
	const sameSHA = "cccccccccccccccccccccccccccccccccccccccc" // R100: identical blob

	clone := body3("X") // identical content on both sides
	partner := body3("P")

	fr := &fakeRunner{
		t:          t,
		resolved:   map[string]string{"base": baseSHA, "head": headSHA},
		rawDiff:    []byte(rawRecord("100644", "100644", sameSHA, sameSHA, "R100", "old_clone.go", "new_clone.go")),
		numstatOut: []byte(numRecord(0, 0, "new_clone.go", "old_clone.go")),
		blobs: map[string][]byte{
			baseSHA + ":old_clone.go": []byte(clone),
			headSHA + ":new_clone.go": []byte(clone),
			baseSHA + ":a_partner.go": []byte(partner),
			headSHA + ":a_partner.go": []byte(partner),
		},
		tree: map[string][]string{
			baseSHA: {"a_partner.go", "old_clone.go"},
			headSHA: {"a_partner.go", "new_clone.go"},
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Delta.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(res.Delta.Files))
	}
	fd := res.Delta.Files[0]
	if fd.SBase != fd.SHead {
		t.Errorf("pure rename of a cross-clone must be neutral (base keyed by OldPath): SBase=%v SHead=%v",
			fd.SBase, fd.SHead)
	}
}
