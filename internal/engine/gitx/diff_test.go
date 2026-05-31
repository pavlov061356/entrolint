package gitx

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// findByPath returns the Change with the given Path or nil.
func findByPath(files []Change, path string) *Change {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

func TestDiff_SingleModified(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -2))
	commitAt(t, r, "a.go", "v1\nextra\n", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("Files=%v Skipped=%v", res.Files, res.Skipped)
	}
	c := res.Files[0]
	if c.Kind != ChangeModified || c.Path != "a.go" {
		t.Errorf("Kind=%v Path=%q", c.Kind, c.Path)
	}
	if c.LinesAdded == 0 {
		t.Errorf("LinesAdded=%d, want > 0", c.LinesAdded)
	}
}

func TestDiff_AddedFile(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -2))
	commitAt(t, r, "b.go", "x\ny\n", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files=%v", res.Files)
	}
	c := res.Files[0]
	if c.Kind != ChangeAdded || c.Path != "b.go" {
		t.Errorf("Kind=%v Path=%q", c.Kind, c.Path)
	}
	if c.LinesAdded != 2 {
		t.Errorf("LinesAdded=%d, want 2", c.LinesAdded)
	}
}

func TestDiff_RemovedFile(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitFiles(t, r, map[string]string{
		"a.go": "kept\n",
		"b.go": "x\ny\n",
	}, "c1", now.AddDate(0, 0, -2))
	gitRm(t, r, "b.go", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files=%v", res.Files)
	}
	c := res.Files[0]
	if c.Kind != ChangeRemoved || c.Path != "b.go" {
		t.Errorf("Kind=%v Path=%q", c.Kind, c.Path)
	}
	if c.LinesRemoved != 2 {
		t.Errorf("LinesRemoved=%d, want 2", c.LinesRemoved)
	}
}

func TestDiff_RenamedNoEdits(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "old.go", "v1\nv2\n", "c1", now.AddDate(0, 0, -2))
	gitMv(t, r, "old.go", "new.go", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files=%v", res.Files)
	}
	c := res.Files[0]
	if c.Kind != ChangeRenamed {
		t.Errorf("Kind=%v, want Renamed", c.Kind)
	}
	if c.Path != "new.go" || c.OldPath != "old.go" {
		t.Errorf("Path=%q OldPath=%q", c.Path, c.OldPath)
	}
	if c.LinesAdded != 0 || c.LinesRemoved != 0 {
		t.Errorf("expected zero line counts for pure rename, got +%d/-%d", c.LinesAdded, c.LinesRemoved)
	}
}

func TestDiff_MixedChange(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitFiles(t, r, map[string]string{
		"a.go":   "a1\n",
		"b.go":   "b1\nb2\n",
		"old.go": "o1\no2\no3\n",
	}, "c1", now.AddDate(0, 0, -3))

	writeAt(t, r, "a.go", "a1\na2\n")
	if _, err := r.Run("add", "a.go"); err != nil {
		t.Fatalf("add a.go: %v", err)
	}
	writeAt(t, r, "c.go", "c1\n")
	if _, err := r.Run("add", "c.go"); err != nil {
		t.Fatalf("add c.go: %v", err)
	}
	if _, err := r.Run("rm", "-q", "b.go"); err != nil {
		t.Fatalf("rm b.go: %v", err)
	}
	if _, err := r.Run("mv", "old.go", "new.go"); err != nil {
		t.Fatalf("mv old.go new.go: %v", err)
	}
	commitWithDate(t, r, "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 4 {
		t.Fatalf("Files=%+v", res.Files)
	}
	byPath := map[string]Change{}
	for _, c := range res.Files {
		byPath[c.Path] = c
	}
	if c, ok := byPath["a.go"]; !ok || c.Kind != ChangeModified {
		t.Errorf("a.go missing or wrong kind: %+v", c)
	}
	if c, ok := byPath["c.go"]; !ok || c.Kind != ChangeAdded {
		t.Errorf("c.go missing or wrong kind: %+v", c)
	}
	if c, ok := byPath["b.go"]; !ok || c.Kind != ChangeRemoved {
		t.Errorf("b.go missing or wrong kind: %+v", c)
	}
	if c, ok := byPath["new.go"]; !ok || c.Kind != ChangeRenamed || c.OldPath != "old.go" {
		t.Errorf("new.go missing or wrong: %+v", c)
	}
}

func TestDiff_AcrossMultipleCommits(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1\n", "c1", now.AddDate(0, 0, -3))
	commitAt(t, r, "a.go", "v1\nv2\n", "c2", now.AddDate(0, 0, -2))
	commitAt(t, r, "a.go", "v1\nv2\nv3\n", "c3", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~2", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files=%v", res.Files)
	}
	c := res.Files[0]
	if c.LinesAdded != 2 {
		t.Errorf("net LinesAdded=%d, want 2", c.LinesAdded)
	}
}

func TestDiff_EmptySameRef(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Files) != 0 || len(res.Skipped) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestDiff_BinaryFile(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -2))
	commitBytesAt(t, r, "blob.bin", []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0x00, 0x10}, "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipBinary {
		t.Errorf("expected one SkipBinary entry, got %+v", res.Skipped)
	}
	if findByPath(res.Files, "blob.bin") != nil {
		t.Errorf("binary should not appear in Files")
	}
}

func TestDiff_Symlink(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "real\n", "c1", now.AddDate(0, 0, -2))
	symlinkAt(t, r, "link", "a.go", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipSymlink {
		t.Errorf("expected one SkipSymlink, got %+v", res.Skipped)
	}
}

func TestDiff_ModeOnlyChange(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	// core.filemode must be true for chmod to surface in diff.
	if _, err := r.Run("config", "core.filemode", "true"); err != nil {
		t.Fatalf("config core.filemode: %v", err)
	}
	commitAt(t, r, "script.sh", "echo hello\n", "c1", now.AddDate(0, 0, -2))
	chmodAt(t, r, "script.sh", 0o755, "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipModeOnly {
		t.Errorf("expected one SkipModeOnly, got %+v", res.Skipped)
	}
	if len(res.Files) != 0 {
		t.Errorf("expected no Files (content unchanged), got %+v", res.Files)
	}
}

func TestDiff_RenameWithModeFlip(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	if _, err := r.Run("config", "core.filemode", "true"); err != nil {
		t.Fatalf("config core.filemode: %v", err)
	}
	commitAt(t, r, "old.go", "kept\n", "c1", now.AddDate(0, 0, -2))
	if _, err := r.Run("mv", "old.go", "new.go"); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	execInContainer(t, "chmod", "0755", r.dir+"/new.go")
	if _, err := r.Run("add", "new.go"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	commitWithDate(t, r, "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// Content unchanged (same blob SHA); only mode flipped. The rename
	// is bookkeeping git did internally — semantically the file has no
	// content delta, so SkipModeOnly is the right contract.
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipModeOnly {
		t.Errorf("expected SkipModeOnly, got Skipped=%+v Files=%+v", res.Skipped, res.Files)
	}
	if len(res.Files) != 0 {
		t.Errorf("expected no Files entries, got %+v", res.Files)
	}
}

func TestDiff_RenamedBinaryFile(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitBytesAt(t, r, "blob.bin", []byte{0x00, 0x01, 0xFF, 0xFE, 0x00}, "c1", now.AddDate(0, 0, -2))
	gitMv(t, r, "blob.bin", "moved.bin", "c2", now.AddDate(0, 0, -1))

	res, err := Diff(r, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// git classifies any binary entry as `-\t-` in numstat, even for
	// pure renames — "binary" is intrinsic to the blob, not the delta.
	// classifySkip orders SkipBinary before SkipModeOnly so the rename
	// surfaces as a skipped binary keyed on the head path.
	if len(res.Files) != 0 {
		t.Errorf("expected no Files (binary), got %+v", res.Files)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipBinary {
		t.Fatalf("expected one SkipBinary, got %+v", res.Skipped)
	}
	if res.Skipped[0].Path != "moved.bin" {
		t.Errorf("Skipped Path=%q, want moved.bin (head-side key)", res.Skipped[0].Path)
	}
}

func TestDiff_BadBaseRef(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	_, err := Diff(r, "no-such-ref", "HEAD")
	if err == nil {
		t.Fatal("expected error for unknown base ref")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want ErrInvalidRef", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, should NOT be ErrUnavailable", err)
	}
}

func TestDiff_TripleDotSemantics(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1\n", "c0", now.AddDate(0, 0, -4))
	if _, err := r.Run("branch", "feature"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	commitAt(t, r, "a.go", "v1\nmain-line\n", "main extends a", now.AddDate(0, 0, -3))
	if _, err := r.Run("checkout", "-q", "feature"); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	commitAt(t, r, "b.go", "feature\n", "feature adds b", now.AddDate(0, 0, -2))

	res, err := Diff(r, "main", "feature")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// triple-dot: compare merge-base (c0) to feature → only b.go.
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file (triple-dot), got %+v", res.Files)
	}
	if res.Files[0].Path != "b.go" || res.Files[0].Kind != ChangeAdded {
		t.Errorf("got %+v, want b.go Added", res.Files[0])
	}
}

func TestFileAtRef_PresentAtHead(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "package a\n", "c1", now.AddDate(0, 0, -1))

	got, err := FileAtRef(r, "HEAD", "a.go")
	if err != nil {
		t.Fatalf("FileAtRef: %v", err)
	}
	if string(got) != "package a\n" {
		t.Errorf("content = %q, want %q", got, "package a\n")
	}
}

func TestFileAtRef_PresentAtOlderRef(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1\n", "c1", now.AddDate(0, 0, -2))
	commitAt(t, r, "a.go", "v2\n", "c2", now.AddDate(0, 0, -1))

	got, err := FileAtRef(r, "HEAD~1", "a.go")
	if err != nil {
		t.Fatalf("FileAtRef: %v", err)
	}
	if string(got) != "v1\n" {
		t.Errorf("content = %q, want %q", got, "v1\n")
	}
}

func TestFileAtRef_AbsentAtBase(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1\n", "c1", now.AddDate(0, 0, -2))
	commitAt(t, r, "b.go", "added\n", "c2", now.AddDate(0, 0, -1))

	_, err := FileAtRef(r, "HEAD~1", "b.go")
	if err == nil {
		t.Fatal("expected ErrNotAtRef, got nil")
	}
	if !errors.Is(err, ErrNotAtRef) {
		t.Errorf("err = %v, want ErrNotAtRef", err)
	}
}

func TestFileAtRef_RemovedFilePresentAtBase(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "b.go", "kept\n", "c1", now.AddDate(0, 0, -2))
	gitRm(t, r, "b.go", "c2", now.AddDate(0, 0, -1))

	got, err := FileAtRef(r, "HEAD~1", "b.go")
	if err != nil {
		t.Fatalf("FileAtRef: %v", err)
	}
	if string(got) != "kept\n" {
		t.Errorf("content = %q, want %q", got, "kept\n")
	}
}

func TestFileAtRef_BadRef(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	_, err := FileAtRef(r, "no-such-ref", "a.go")
	if err == nil {
		t.Fatal("expected error for bad ref")
	}
	if errors.Is(err, ErrNotAtRef) {
		t.Errorf("err = %v, should NOT be ErrNotAtRef", err)
	}
}

func TestFileAtRef_PathIsDirectory(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	execInContainer(t, "mkdir", "-p", r.dir+"/sub")
	commitAt(t, r, "sub/file.go", "package sub\n", "c1", now.AddDate(0, 0, -1))

	_, err := FileAtRef(r, "HEAD", "sub")
	if err == nil {
		t.Fatal("expected error when path is a directory, got nil")
	}
	// Either ErrNotAtRef (synthesis-recommended contract — directories
	// are not blobs at this ref) or a plain non-sentinel error is
	// acceptable for v0.1; what's NOT acceptable is ErrInvalidRef
	// (the ref does resolve) or a panic.
	if errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, should not be ErrInvalidRef (ref resolves)", err)
	}
}

func TestFileAtRef_LargeFile(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	const line = "line of approximately forty characters!!"
	const nLines = 2000
	commitLargeAt(t, r, "big.go", line, nLines, "c1", now.AddDate(0, 0, -1))

	got, err := FileAtRef(r, "HEAD", "big.go")
	if err != nil {
		t.Fatalf("FileAtRef: %v", err)
	}
	want := strings.Repeat(line+"\n", nLines)
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d", len(got), len(want))
	}
	if string(got) != want {
		t.Errorf("content mismatch (first 64: %q, last 64: %q)", got[:64], got[len(got)-64:])
	}
}

func TestResolveRef_Branch(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	sha, err := ResolveRef(r, "main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if len(sha) != 40 || !isHex(sha) {
		t.Errorf("sha = %q, want 40-char hex", sha)
	}
}

func TestResolveRef_HEADEqualsMain(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	a, err := ResolveRef(r, "HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	b, err := ResolveRef(r, "main")
	if err != nil {
		t.Fatalf("ResolveRef main: %v", err)
	}
	if a != b {
		t.Errorf("HEAD=%s main=%s", a, b)
	}
}

func TestResolveRef_ShortSHA(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	full := headSHA(t, r)
	short := full[:8]
	got, err := ResolveRef(r, short)
	if err != nil {
		t.Fatalf("ResolveRef short: %v", err)
	}
	if got != full {
		t.Errorf("short=%s resolved to %s, want %s", short, got, full)
	}
}

func TestResolveRef_AnnotatedTag(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))
	if _, err := r.Run("tag", "-a", "v1.0", "-m", "release"); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	commitSHA := headSHA(t, r)

	got, err := ResolveRef(r, "v1.0")
	if err != nil {
		t.Fatalf("ResolveRef tag: %v", err)
	}
	if got != commitSHA {
		t.Errorf("tag resolved to %s, want commit SHA %s", got, commitSHA)
	}
}

func TestResolveRef_Unknown(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -1))

	_, err := ResolveRef(r, "no-such-ref")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("err = %v, want ErrInvalidRef", err)
	}
}

func isHex(s string) bool {
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return false
		}
	}
	return true
}
