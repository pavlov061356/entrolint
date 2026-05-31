package gitx

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

// commitWithDate runs `git commit` inside the runner's dir with the
// given author/committer date and message. The working tree must
// already be staged.
func commitWithDate(t *testing.T, r containerRunner, msg string, when time.Time) {
	t.Helper()
	date := when.Format(time.RFC3339)
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("cd %s && GIT_AUTHOR_DATE='%s' GIT_COMMITTER_DATE='%s' git commit -q -m '%s'",
			r.dir, date, date, msg),
	}
	execInContainer(t, cmd...)
}

// gitRm commits a deletion of `file` dated at `when`.
func gitRm(t *testing.T, r containerRunner, file, msg string, when time.Time) {
	t.Helper()
	if _, err := r.Run("rm", "-q", file); err != nil {
		t.Fatalf("git rm %s: %v", file, err)
	}
	commitWithDate(t, r, msg, when)
}

// gitMv commits a rename old→new dated at `when`. No content change.
func gitMv(t *testing.T, r containerRunner, oldFile, newFile, msg string, when time.Time) {
	t.Helper()
	if _, err := r.Run("mv", oldFile, newFile); err != nil {
		t.Fatalf("git mv %s %s: %v", oldFile, newFile, err)
	}
	commitWithDate(t, r, msg, when)
}

// writeBytesAt writes raw bytes (NULs welcome) to `file`. Routes via
// `base64 -d` so arbitrary content survives shell escaping.
func writeBytesAt(t *testing.T, r containerRunner, file string, content []byte) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(content)
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("printf %%s '%s' | base64 -d > %s/%s", encoded, r.dir, file),
	}
	execInContainer(t, cmd...)
}

// commitBytesAt is writeBytesAt + add + dated commit.
func commitBytesAt(t *testing.T, r containerRunner, file string, content []byte, msg string, when time.Time) {
	t.Helper()
	writeBytesAt(t, r, file, content)
	if _, err := r.Run("add", file); err != nil {
		t.Fatalf("git add %s: %v", file, err)
	}
	commitWithDate(t, r, msg, when)
}

// chmodAt updates `file` to `mode` (no content change) and commits.
// Useful for SkipModeOnly fixtures.
func chmodAt(t *testing.T, r containerRunner, file string, mode int, msg string, when time.Time) {
	t.Helper()
	execInContainer(t, "chmod", fmt.Sprintf("%o", mode), r.dir+"/"+file)
	if _, err := r.Run("add", file); err != nil {
		t.Fatalf("git add %s: %v", file, err)
	}
	commitWithDate(t, r, msg, when)
}

// symlinkAt creates symlink `link` pointing at `target` (relative to
// the repo dir), stages it, and commits.
func symlinkAt(t *testing.T, r containerRunner, link, target, msg string, when time.Time) {
	t.Helper()
	execInContainer(t, "ln", "-sf", target, r.dir+"/"+link)
	if _, err := r.Run("add", link); err != nil {
		t.Fatalf("git add %s: %v", link, err)
	}
	commitWithDate(t, r, msg, when)
}

// commitFiles writes multiple files and bundles them into one dated
// commit. Used for mixed-change fixtures.
func commitFiles(t *testing.T, r containerRunner, files map[string]string, msg string, when time.Time) {
	t.Helper()
	for path, content := range files {
		writeAt(t, r, path, content)
		if _, err := r.Run("add", path); err != nil {
			t.Fatalf("git add %s: %v", path, err)
		}
	}
	commitWithDate(t, r, msg, when)
}

// headSHA returns the full SHA of HEAD inside the runner's repo.
func headSHA(t *testing.T, r containerRunner) string {
	t.Helper()
	out, err := r.Run("rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// commitLargeAt generates `nLines` repetitions of `line` (newline-
// terminated) inside the container and commits the result. Used for
// large-file fixtures that would overflow argv when piped through
// writeAt's printf wrapper.
func commitLargeAt(t *testing.T, r containerRunner, file, line string, nLines int, msg string, when time.Time) {
	t.Helper()
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("cd %s && yes '%s' | head -n %d > %s", r.dir, line, nLines, file),
	}
	execInContainer(t, cmd...)
	if _, err := r.Run("add", file); err != nil {
		t.Fatalf("git add %s: %v", file, err)
	}
	commitWithDate(t, r, msg, when)
}
