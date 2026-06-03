package pipeline

import (
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/scaling"

	// Blank-import to load shotgun's init() — production main.go does
	// the same; without it pipeline.Check would see an empty Registry
	// and the end-to-end gate path is untested.
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/shotgun"
)

// repeat builds a single rawRecord + numstat pair for `count` Go files
// touched as Modified, plus a unified-0 diff where each file's body
// is the same one-line addition. End-to-end this is exactly the
// shotgun pattern.
func shotgunPayload(count int, identicalLine string) (raw, num, unified string, paths []string) {
	var rawB, numB, uniB strings.Builder
	for i := 0; i < count; i++ {
		p := pathFor(i)
		paths = append(paths, p)
		rawB.WriteString(rawRecord("100644", "100644",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"M", p, ""))
		numB.WriteString(numRecord(1, 0, p, ""))
		uniB.WriteString("diff --git a/" + p + " b/" + p + "\n")
		uniB.WriteString("index aaaaaaa..bbbbbbb 100644\n")
		uniB.WriteString("--- a/" + p + "\n")
		uniB.WriteString("+++ b/" + p + "\n")
		uniB.WriteString("@@ -0,0 +1,1 @@\n")
		uniB.WriteString("+")
		uniB.WriteString(identicalLine)
		uniB.WriteString("\n")
	}
	return rawB.String(), numB.String(), uniB.String(), paths
}

func pathFor(i int) string {
	// Spread paths across multiple packages so the analyzer's
	// vendor/dot exclusions don't accidentally filter any.
	dirs := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"}
	return dirs[i%len(dirs)] + "/file.go"
}

func TestCheck_ShotgunFiresOnFivePackagesWithIdenticalEdit(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	const n = 5
	raw, num, uni, paths := shotgunPayload(n, "register(handler)")

	// Every changed file resolves to the same head blob — content is
	// trivial so the scoring side stays simple. Build blobs map.
	blob := []byte("package p\nfunc F() { register(handler) }\n")
	blobs := map[string][]byte{}
	for _, p := range paths {
		blobs["1111111111111111111111111111111111111111:"+p] = []byte("package p\nfunc F() {}\n")
		blobs["2222222222222222222222222222222222222222:"+p] = blob
	}

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:     []byte(raw),
		numstatOut:  []byte(num),
		unifiedDiff: []byte(uni),
		blobs:       blobs,
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if res.Scaling.Class != scaling.ClassON {
		t.Errorf("PR scaling.Class = %v, want O(n) (shotgun fires across %d identical patches)", res.Scaling.Class, n)
	}
	shotgunHits := 0
	for _, f := range res.Scaling.Files {
		for _, h := range f.Hits {
			if h.Detector == "shotgun" {
				shotgunHits++
				if h.Class != scaling.ClassON {
					t.Errorf("shotgun hit class = %v, want O(n)", h.Class)
				}
				if h.Size != n {
					t.Errorf("shotgun hit size = %d, want %d", h.Size, n)
				}
			}
		}
	}
	if shotgunHits != n {
		t.Errorf("expected %d shotgun hits across breakdown, got %d", n, shotgunHits)
	}

	// Two-axis gate: default config tolerates up to O(k), so O(n) fails.
	v := res.Verdict(config.Default())
	if !v.Failed {
		t.Errorf("PR with O(n) scaling must fail the default gate (max=O(k)); got %+v", v)
	}
}

func TestCheck_ShotgunSilentBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	const n = 4 // below DefaultMinFiles=5
	raw, num, uni, paths := shotgunPayload(n, "register(handler)")
	blob := []byte("package p\nfunc F() { register(handler) }\n")
	blobs := map[string][]byte{}
	for _, p := range paths {
		blobs["1111111111111111111111111111111111111111:"+p] = []byte("package p\nfunc F() {}\n")
		blobs["2222222222222222222222222222222222222222:"+p] = blob
	}

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:     []byte(raw),
		numstatOut:  []byte(num),
		unifiedDiff: []byte(uni),
		blobs:       blobs,
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Scaling.Class != scaling.ClassO1 {
		t.Errorf("4 identical edits must stay O(1) (below threshold), got %v", res.Scaling.Class)
	}
}
