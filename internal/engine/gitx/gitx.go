// Package gitx wraps the local git operations entrolint needs.
//
// All operations go through a Runner so tests can swap a hermetic
// container in place of host git without touching production code.
// Production builds use LocalRunner, which shells out to the system
// git binary via os/exec.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrUnavailable wraps any error where git itself cannot be invoked
// (binary missing, daemon down). Distinct from a non-zero git exit
// (untracked file, not a repository), which surfaces as a plain error
// and is interpreted by ChurnCount as "0 commits".
var ErrUnavailable = errors.New("git unavailable")

// Runner executes git commands inside a specific repository. The Dir
// is configured at construction; arguments to Run are the git
// subcommand and its flags only (no `-C`).
type Runner interface {
	Run(args ...string) ([]byte, error)
}

// BatchRunner is an optional Runner capability: running a git command
// with bytes piped to its stdin. It exists for `cat-file --batch`,
// which reads object names from stdin. LocalRunner implements it;
// BlobsAtRef type-asserts for it and falls back to a per-file loop
// (FileAtRef) when a runner — e.g. a hermetic test fake — only
// implements Run.
type BatchRunner interface {
	Runner
	RunStdin(stdin []byte, args ...string) ([]byte, error)
}

// LocalRunner runs git via the host system's git binary, anchored at
// Dir via `git -C`.
type LocalRunner struct {
	Dir string
}

// Run executes `git -C <Dir> <args...>` and returns stdout. The
// returned error wraps ErrUnavailable if git cannot be invoked; for
// non-zero git exits the error is plain and stdout is still returned.
//
// LC_ALL/LANG/LANGUAGE are forced to "C" so callers that inspect
// stderr substrings (FileAtRef's ErrNotAtRef detection, Diff's
// ErrInvalidRef classification) see stable English messages
// regardless of the user's locale.
func (r LocalRunner) Run(args ...string) ([]byte, error) {
	return r.run(nil, args...)
}

// RunStdin runs `git -C <Dir> <args...>` with `stdin` piped to the
// process, returning stdout. Used by cat-file --batch. Error semantics
// match Run. Implements BatchRunner.
func (r LocalRunner) RunStdin(stdin []byte, args ...string) ([]byte, error) {
	return r.run(stdin, args...)
}

func (r LocalRunner) run(stdin []byte, args ...string) ([]byte, error) {
	// #nosec G204 -- git arguments are constructed internally from
	// fixed log flags and a path supplied by trusted callers; entrolint
	// never passes user input directly into git invocations.
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "LANGUAGE=C")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, execErr)
		}
		return stdout.Bytes(), fmt.Errorf("git exit: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ChurnCount returns the number of commits that touched `path` in the
// last `sinceDays` days, following renames. The path is resolved
// inside the runner's repository — caller passes a path relative to
// the runner's Dir (or just the basename when --follow can resolve
// renames from a single name).
//
// Returns (0, nil) for untracked files or paths outside a repository.
// Returns ErrUnavailable wrapped only when git itself cannot run.
func ChurnCount(r Runner, path string, sinceDays int) (int, error) {
	if sinceDays <= 0 {
		return 0, fmt.Errorf("sinceDays must be positive, got %d", sinceDays)
	}
	out, err := r.Run(
		"log",
		fmt.Sprintf("--since=%d.days.ago", sinceDays),
		"--follow",
		"--pretty=format:%H",
		"--", path,
	)
	if errors.Is(err, ErrUnavailable) {
		return 0, err
	}
	if err != nil {
		return 0, nil
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, nil
	}
	return strings.Count(s, "\n") + 1, nil
}
