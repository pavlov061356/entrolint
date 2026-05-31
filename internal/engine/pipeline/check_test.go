package pipeline

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pavlov061356/entrolint/internal/engine/config"
	"github.com/pavlov061356/entrolint/internal/engine/gitx"
)

// fakeRunner mimics gitx.Runner for the four invocations Check makes:
// rev-parse, diff --raw, diff --numstat, cat-file blob. Each canned
// payload is keyed by the meaningful arg slice so tests stay readable.
//
// Any call shape the fake doesn't recognize fails the test — we want
// the unhandled-shape signal loud, not silent.
type fakeRunner struct {
	t          *testing.T
	resolved   map[string]string // rev -> sha
	rawDiff    []byte
	numstatOut []byte
	blobs      map[string][]byte // "<ref>:<path>" -> content
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
		// args[1]: "--raw" or "--numstat"
		for _, a := range args {
			switch a {
			case "--raw":
				return f.rawDiff, nil
			case "--numstat":
				return f.numstatOut, nil
			}
		}
		f.t.Fatalf("diff invocation with neither --raw nor --numstat: %v", args)
	case "cat-file":
		// args: ["cat-file", "blob", "<ref>:<path>"]
		key := args[2]
		blob, ok := f.blobs[key]
		if !ok {
			return nil, fmt.Errorf("fatal: path '%s' does not exist in '%s'",
				strings.SplitN(key, ":", 2)[1], strings.SplitN(key, ":", 2)[0])
		}
		return blob, nil
	case "log":
		// Called by ChurnCount during the calibration tree walk. Return
		// empty so churn is zero — irrelevant for ΔS.
		return nil, nil
	}
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
