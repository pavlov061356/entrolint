package pipeline

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
	"github.com/pavlov061356/entrolint/internal/engine/thermo"
)

// fakeRunner mimics gitx.Runner for the four invocations Check makes:
// rev-parse, diff --raw, diff --numstat, cat-file blob. Each canned
// payload is keyed by the meaningful arg slice so tests stay readable.
//
// Any call shape the fake doesn't recognize fails the test — we want
// the unhandled-shape signal loud, not silent.
type fakeRunner struct {
	t           *testing.T
	resolved    map[string]string // rev -> sha
	rawDiff     []byte
	numstatOut  []byte
	unifiedDiff []byte            // `git diff --unified=0 …` payload for DiffResult.Patches
	blobs       map[string][]byte // "<ref>:<path>" -> content
	// tree maps a ref (sha) to the paths `git ls-tree` reports there,
	// driving the corpus pre-pass's whole-tree enumeration. Left nil by
	// most tests — an empty tree yields an empty corpus, so
	// cross_duplication contributes 0 and does not perturb their ΔS.
	tree map[string][]string
	// catFileErr lets a test force a specific error for a given
	// "<ref>:<path>" key — used to exercise fatal-error propagation
	// (e.g. gitx.ErrUnavailable firing mid-loop) without ad-hoc
	// fakeRunner wrappers.
	catFileErr map[string]error
	// lsTreeCount counts `git ls-tree` invocations — the corpus pre-pass's
	// whole-tree enumeration — so a test can assert it is skipped when
	// cross_duplication is disabled.
	lsTreeCount int
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.t.Helper()
	switch args[0] {
	case "rev-parse":
		// args: ["rev-parse", "--verify", "<rev>^{commit}"]
		rev := strings.TrimSuffix(args[2], "^{commit}")
		sha, ok := f.resolved[rev]
		if !ok {
			return nil, fmt.Errorf("fatal: invalid object name '%s'", rev)
		}
		return []byte(sha + "\n"), nil
	case "diff":
		for _, a := range args {
			switch a {
			case "--raw":
				return f.rawDiff, nil
			case "--numstat":
				return f.numstatOut, nil
			case "--unified=0":
				return f.unifiedDiff, nil
			}
		}
		f.t.Fatalf("diff invocation with neither --raw nor --numstat nor --unified=0: %v", args)
	case "cat-file":
		// args: ["cat-file", "blob", "<ref>:<path>"]
		key := args[2]
		if f.catFileErr != nil {
			if _, hit := f.catFileErr[key]; hit {
				return nil, f.catFileErr[key]
			}
		}
		blob, ok := f.blobs[key]
		if !ok {
			return nil, fmt.Errorf("fatal: path '%s' does not exist in '%s'",
				strings.SplitN(key, ":", 2)[1], strings.SplitN(key, ":", 2)[0])
		}
		return blob, nil
	case "ls-tree":
		// args: ["ls-tree", "-r", "--name-only", "-z", "<ref>"]
		// Drives corpus.Build's whole-tree enumeration. The fake only
		// implements Run, so BlobsAtRef falls back to per-file cat-file
		// blob lookups against f.blobs — no batch stdin needed.
		f.lsTreeCount++
		ref := args[len(args)-1]
		var b strings.Builder
		for _, p := range f.tree[ref] {
			b.WriteString(p)
			b.WriteByte('\x00')
		}
		return []byte(b.String()), nil
	}
	// NOTE: `log` is not handled here — calibration uses LocalRunner via
	// scan.analyzeTree (analyzer hardcodes its ChurnRunner), so it never
	// reaches the fake. ChurnCount swallows the resulting non-repo error
	// from LocalRunner against a TempDir, so calibration still completes.
	f.t.Fatalf("unhandled fake-runner call: %v", args)
	return nil, nil
}

// rawRecord builds one `git diff --raw -z` entry. For Renamed status
// pass newPath; otherwise leave it empty.
func rawRecord(srcMode, dstMode, srcSHA, dstSHA, status, path, newPath string) string {
	rec := fmt.Sprintf(":%s %s %s %s %s\x00%s\x00", srcMode, dstMode, srcSHA, dstSHA, status, path)
	if newPath != "" {
		rec += newPath + "\x00"
	}
	return rec
}

// numRecord builds one `git diff --numstat -z` entry. For renames
// pass oldPath; otherwise leave it empty.
func numRecord(added, removed int, path, oldPath string) string {
	if oldPath != "" {
		return fmt.Sprintf("%d\t%d\t\x00%s\x00%s\x00", added, removed, oldPath, path)
	}
	return fmt.Sprintf("%d\t%d\t%s\x00", added, removed, path)
}

// runCheck builds a check pipeline against a working tree dir with the
// supplied fake runner. The dir doubles as the calibration corpus.
func runCheck(t *testing.T, dir string, fr *fakeRunner) (CheckResult, error) {
	t.Helper()
	return Check(CheckOptions{
		Root:        dir,
		Base:        "base",
		Head:        "head",
		ScanOptions: ScanOptions{Config: config.Default()},
		GitRunner:   fr,
	})
}

// calibrationCorpus returns a Go file set with enough variance across
// length/nesting/cyclomatic that fitMicrostate produces valid lognormal
// params (≥ 2 distinct positive samples per microstate, non-zero
// sigma). A degenerate corpus collapses every score to S=0 and the
// gate tests turn vacuous.
func calibrationCorpus() map[string]string {
	return map[string]string{
		"calibA.go": `package p

func A() {}
`,
		"calibB.go": `package p

func B(x int) int {
	if x > 0 {
		return x
	}
	return 0
}
`,
		"calibC.go": `package p

func C(xs []int) int {
	total := 0
	for _, v := range xs {
		if v > 0 {
			total += v
		}
	}
	return total
}
`,
		"calibD.go": `package p

func D(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			for j := 0; j < x; j++ {
				if j%2 == 0 {
					total += j
				} else if j%3 == 0 {
					total -= j
				} else {
					total++
				}
			}
		} else if x < 0 {
			total -= x
		}
	}
	return total
}
`,
		"calibE.go": `package p

func E1() int { return 1 }
func E2() int { return 2 }
func E3(x int) int {
	switch x {
	case 1:
		return 10
	case 2:
		return 20
	case 3:
		return 30
	default:
		return 0
	}
}
`,
	}
}

func TestCheck_AddedFileYieldsPositiveDelta(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "A", "new.go", "")),
		numstatOut: []byte(numRecord(20, 0, "new.go", "")),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:new.go": []byte(`package p
func New(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			for j := 0; j < x; j++ {
				total += j
			}
		}
	}
	return total
}
`),
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
	if fd.Path != "new.go" {
		t.Errorf("Path = %q, want new.go", fd.Path)
	}
	if fd.SBase != 0 {
		t.Errorf("SBase = %v, want 0 (added file)", fd.SBase)
	}
	if fd.SHead <= 0 {
		t.Errorf("SHead = %v, want > 0", fd.SHead)
	}
	if res.Delta.Total <= 0 {
		t.Errorf("Total = %v, want > 0 (added file grows entropy)", res.Delta.Total)
	}
	if res.Delta.LinesChanged != 20 {
		t.Errorf("LinesChanged = %d, want 20", res.Delta.LinesChanged)
	}
}

func TestCheck_RemovedFileYieldsNegativeDelta(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("100644", "000000", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "0000000000000000000000000000000000000000", "D", "gone.go", "")),
		numstatOut: []byte(numRecord(0, 15, "gone.go", "")),
		blobs: map[string][]byte{
			"1111111111111111111111111111111111111111:gone.go": []byte(`package p
func Gone(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			total += x
		}
	}
	return total
}
`),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Delta.Total >= 0 {
		t.Errorf("Total = %v, want < 0 (removed file reduces entropy)", res.Delta.Total)
	}
	if res.Delta.Density >= 0 {
		t.Errorf("Density = %v, want < 0", res.Delta.Density)
	}
	if res.Delta.Fails(config.Default().DeltaSMax) {
		t.Error("a pure-removal PR must not fail the gate")
	}
	// Pin the per-file shape: a regression that classified a removal
	// as Modified with sHead=0 would produce the same Total but the
	// wrong Kind, silently violating the report contract.
	if len(res.Delta.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(res.Delta.Files))
	}
	fd := res.Delta.Files[0]
	if fd.Kind != thermo.DeltaRemoved {
		t.Errorf("Kind = %v, want DeltaRemoved", fd.Kind)
	}
	if fd.SBase <= 0 || fd.SHead != 0 {
		t.Errorf("SBase=%v SHead=%v, want SBase>0 SHead=0", fd.SBase, fd.SHead)
	}
	if res.Delta.LinesChanged != 15 {
		t.Errorf("LinesChanged = %d, want 15", res.Delta.LinesChanged)
	}
}

func TestCheck_ModifiedRefactorYieldsNegativeDelta(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	complex := []byte(`package p
func F(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			for j := 0; j < x; j++ {
				if j%2 == 0 {
					total += j
				}
			}
		}
	}
	return total
}
`)
	simpler := []byte(`package p
func F(xs []int) int { return len(xs) }
`)

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("100644", "100644", "cccccccccccccccccccccccccccccccccccccccc", "dddddddddddddddddddddddddddddddddddddddd", "M", "a.go", "")),
		numstatOut: []byte(numRecord(2, 12, "a.go", "")),
		blobs: map[string][]byte{
			"1111111111111111111111111111111111111111:a.go": complex,
			"2222222222222222222222222222222222222222:a.go": simpler,
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
	if fd.SBase <= fd.SHead {
		t.Errorf("expected SBase (%v) > SHead (%v) for refactor", fd.SBase, fd.SHead)
	}
	if res.Delta.Total >= 0 {
		t.Errorf("Total = %v, want < 0 (refactor lowers entropy)", res.Delta.Total)
	}
}

func TestCheck_NonGoChangesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff: []byte(
			rawRecord("100644", "100644", "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222", "M", "README.md", "") +
				rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "3333333333333333333333333333333333333333", "A", "added.go", ""),
		),
		numstatOut: []byte(
			numRecord(100, 50, "README.md", "") +
				numRecord(5, 0, "added.go", ""),
		),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:added.go": []byte("package p\nfunc Tiny() {}\n"),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Delta.Files) != 1 || res.Delta.Files[0].Path != "added.go" {
		t.Fatalf("Files = %+v, want only added.go", res.Delta.Files)
	}
	if res.Delta.LinesChanged != 5 {
		t.Errorf("LinesChanged = %d, want 5 (README.md must not enter the denominator)", res.Delta.LinesChanged)
	}
}

func TestCheck_InvalidBaseSurfacesErrInvalidRef(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t:        t,
		resolved: map[string]string{"head": "2222222222222222222222222222222222222222"},
	}

	_, err := runCheck(t, dir, fr)
	if err == nil {
		t.Fatal("expected error for invalid base ref, got nil")
	}
	if !errors.Is(err, gitx.ErrInvalidRef) {
		t.Errorf("expected ErrInvalidRef in chain, got %v", err)
	}
}

func TestCheck_UnparseableBlobOnAddIsDropped(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "abababababababababababababababababababab", "A", "broken.go", "")),
		numstatOut: []byte(numRecord(3, 0, "broken.go", "")),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:broken.go": []byte("this is not valid Go"),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Delta.Files) != 0 {
		t.Errorf("Files = %+v, want empty (unparseable add dropped)", res.Delta.Files)
	}
	if res.Delta.Total != 0 {
		t.Errorf("Total = %v, want 0", res.Delta.Total)
	}
}

func TestCheck_FailsGateWhenDensityExceedsThreshold(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	// One added complex file but only one line counted as changed —
	// the density is artificially huge.
	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "abababababababababababababababababababab", "A", "huge.go", "")),
		numstatOut: []byte(numRecord(1, 0, "huge.go", "")),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:huge.go": []byte(`package p
func Huge(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			for j := 0; j < x; j++ {
				if j%2 == 0 {
					total += j
				} else if j%3 == 0 {
					total -= j
				}
			}
		}
	}
	return total
}
`),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.Delta.Fails(config.Default().DeltaSMax) {
		t.Errorf("expected gate failure: Total=%v Density=%v threshold=%v",
			res.Delta.Total, res.Delta.Density, config.Default().DeltaSMax)
	}
}

func TestCheck_PassesGateOnTrivialDiff(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "abababababababababababababababababababab", "A", "tiny.go", "")),
		numstatOut: []byte(numRecord(50, 0, "tiny.go", "")),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:tiny.go": []byte("package p\nfunc Tiny() {}\n"),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Delta.Fails(config.Default().DeltaSMax) {
		t.Errorf("trivial diff should pass gate: %+v", res.Delta)
	}
}

func TestCheck_InvalidHeadSurfacesErrInvalidRef(t *testing.T) {
	// Mirror of TestCheck_InvalidBaseSurfacesErrInvalidRef — without
	// it, a regression that dropped the head-side ResolveRef wrap (or
	// swapped base/head) would slip through unchallenged.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t:        t,
		resolved: map[string]string{"base": "1111111111111111111111111111111111111111"},
	}

	_, err := runCheck(t, dir, fr)
	if err == nil {
		t.Fatal("expected error for invalid head ref, got nil")
	}
	if !errors.Is(err, gitx.ErrInvalidRef) {
		t.Errorf("expected ErrInvalidRef in chain, got %v", err)
	}
}

func TestCheck_RenameCountedAsModified(t *testing.T) {
	// gitx classifies the entry as ChangeRenamed with OldPath=old.go
	// and Path=new.go; scoreChange reads base:old.go and head:new.go
	// and emits a single FileDelta with Kind=DeltaModified at the
	// head path. The rename branch had no coverage until this test.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	complex := []byte(`package p
func F(xs []int) int {
	total := 0
	for _, x := range xs {
		if x > 0 {
			total += x
		}
	}
	return total
}
`)
	simpler := []byte(`package p
func F(xs []int) int { return len(xs) }
`)

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("100644", "100644", "cccccccccccccccccccccccccccccccccccccccc", "dddddddddddddddddddddddddddddddddddddddd", "R100", "old.go", "new.go")),
		numstatOut: []byte(numRecord(1, 2, "new.go", "old.go")),
		blobs: map[string][]byte{
			"1111111111111111111111111111111111111111:old.go": complex,
			"2222222222222222222222222222222222222222:new.go": simpler,
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
	if fd.Kind != thermo.DeltaModified {
		t.Errorf("Kind = %v, want DeltaModified (renames fold into Modified)", fd.Kind)
	}
	if fd.Path != "new.go" {
		t.Errorf("Path = %q, want new.go (the head-side path)", fd.Path)
	}
	// SBase must come from the OldPath blob — if scoreChange wired
	// the rename branch to head-path on both sides, SBase would land
	// at 0 (no base-side blob exists at the new path). The complex
	// base content forces a strictly positive SBase under any
	// reasonable calibration.
	if fd.SBase <= 0 {
		t.Errorf("SBase = %v, want > 0 (base-side blob fetched from OldPath)", fd.SBase)
	}
	// And the rename net effect must be a refactor (SHead < SBase
	// for this content pair).
	if fd.Delta >= 0 {
		t.Errorf("Delta = %v, want < 0 (rename + simplification)", fd.Delta)
	}
}

func TestCheck_ModifiedOneSideParseFailureIsDropped(t *testing.T) {
	// A PR that introduces broken Go in the head version must NOT
	// look like a refactor of -sBase. Same defense applies to a base
	// version that was unparseable — head's full sHead should not be
	// blamed as fresh debt because there's no valid prior.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	valid := []byte(`package p
func F(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}
`)
	broken := []byte("this is not valid Go")

	for _, tc := range []struct {
		name               string
		baseBlob, headBlob []byte
	}{
		{"base valid, head broken", valid, broken},
		{"base broken, head valid", broken, valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{
				t: t,
				resolved: map[string]string{
					"base": "1111111111111111111111111111111111111111",
					"head": "2222222222222222222222222222222222222222",
				},
				rawDiff:    []byte(rawRecord("100644", "100644", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "M", "x.go", "")),
				numstatOut: []byte(numRecord(5, 5, "x.go", "")),
				blobs: map[string][]byte{
					"1111111111111111111111111111111111111111:x.go": tc.baseBlob,
					"2222222222222222222222222222222222222222:x.go": tc.headBlob,
				},
			}
			res, err := runCheck(t, dir, fr)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if len(res.Delta.Files) != 0 {
				t.Errorf("Files = %+v, want empty (one-sided parse failure drops the change)", res.Delta.Files)
			}
			if res.Delta.Total != 0 {
				t.Errorf("Total = %v, want 0", res.Delta.Total)
			}
		})
	}
}

func TestCheck_FatalErrorMidLoopPropagates(t *testing.T) {
	// ErrUnavailable firing mid-loop (git daemon crash, shallow clone
	// lost a pack) must NOT silently drop the file from ΔS — the
	// gate's verdict would then be computed from a partial corpus.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "abababababababababababababababababababab", "A", "new.go", "")),
		numstatOut: []byte(numRecord(5, 0, "new.go", "")),
		catFileErr: map[string]error{
			"2222222222222222222222222222222222222222:new.go": fmt.Errorf("%w: simulated git crash", gitx.ErrUnavailable),
		},
	}

	_, err := runCheck(t, dir, fr)
	if err == nil {
		t.Fatal("expected fatal error propagation, got nil")
	}
	if !errors.Is(err, gitx.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable in chain, got %v", err)
	}
}

func TestCheck_VendorPathExcluded(t *testing.T) {
	// vendor/ files are excluded from calibration (Analyzer.skipDir);
	// they must also be excluded from scoring so the two halves of
	// the pipeline agree on what counts as the corpus.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())

	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff: []byte(
			rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "abababababababababababababababababababab", "A", "vendor/dep/x.go", "") +
				rawRecord("000000", "100644", "0000000000000000000000000000000000000000", "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", "A", "real.go", ""),
		),
		numstatOut: []byte(
			numRecord(100, 0, "vendor/dep/x.go", "") +
				numRecord(3, 0, "real.go", ""),
		),
		blobs: map[string][]byte{
			"2222222222222222222222222222222222222222:real.go": []byte("package p\nfunc R() {}\n"),
		},
	}

	res, err := runCheck(t, dir, fr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Delta.Files) != 1 || res.Delta.Files[0].Path != "real.go" {
		t.Fatalf("Files = %+v, want only real.go", res.Delta.Files)
	}
	if res.Delta.LinesChanged != 3 {
		t.Errorf("LinesChanged = %d, want 3 (vendor/dep/x.go must be excluded)", res.Delta.LinesChanged)
	}
}

func TestFetchAllBlobs_PropagatesFatalErrors(t *testing.T) {
	// ErrUnavailable while fetching blobs is fatal — pipeline.Check
	// must surface it, not silently drop the blob from the maps.
	// This pins the contract that fetchAllBlobs propagates while
	// scoring on cached blobs.
	dir := t.TempDir()
	writeTree(t, dir, calibrationCorpus())
	fr := &fakeRunner{
		t: t,
		resolved: map[string]string{
			"base": "1111111111111111111111111111111111111111",
			"head": "2222222222222222222222222222222222222222",
		},
		rawDiff:    []byte(rawRecord("100644", "100644", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "M", "x.go", "")),
		numstatOut: []byte(numRecord(5, 5, "x.go", "")),
		catFileErr: map[string]error{
			"1111111111111111111111111111111111111111:x.go": fmt.Errorf("%w: simulated git crash", gitx.ErrUnavailable),
		},
	}
	_, err := runCheck(t, dir, fr)
	if err == nil {
		t.Fatal("expected fatal ErrUnavailable from base-side fetch, got nil")
	}
	if !errors.Is(err, gitx.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable in chain, got %v", err)
	}
}
