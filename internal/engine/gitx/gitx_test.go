package gitx

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedContainer testcontainers.Container
	containerErr    error
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req := testcontainers.ContainerRequest{
		Image:      "alpine/git:latest",
		Entrypoint: []string{"sleep"},
		Cmd:        []string{"infinity"},
		WaitingFor: wait.ForExec([]string{"git", "--version"}).WithStartupTimeout(30 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		containerErr = err
	} else {
		sharedContainer = c
	}
	code := m.Run()
	if sharedContainer != nil {
		_ = sharedContainer.Terminate(context.Background())
	}
	os.Exit(code)
}

func requireContainer(t *testing.T) {
	t.Helper()
	if containerErr != nil {
		t.Skipf("container unavailable: %v", containerErr)
	}
}

// containerRunner adapts testcontainers.Container.Exec to the gitx.Runner contract.
type containerRunner struct {
	c   testcontainers.Container
	dir string
}

func (r containerRunner) Run(args ...string) ([]byte, error) {
	fullCmd := append([]string{"git", "-C", r.dir}, args...)
	code, reader, err := r.c.Exec(context.Background(), fullCmd, tcexec.Multiplexed())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	out, _ := io.ReadAll(reader)
	if code != 0 {
		return out, fmt.Errorf("git exit %d: %s", code, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func execInContainer(t *testing.T, args ...string) {
	t.Helper()
	code, reader, err := sharedContainer.Exec(context.Background(), args, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec %v: %v", args, err)
	}
	out, _ := io.ReadAll(reader)
	if code != 0 {
		t.Fatalf("exec %v code %d: %s", args, code, out)
	}
}

func newRepo(t *testing.T, gitInit bool) containerRunner {
	t.Helper()
	dir := "/work/" + strings.ReplaceAll(t.Name(), "/", "_")
	execInContainer(t, "rm", "-rf", dir)
	execInContainer(t, "mkdir", "-p", dir)
	r := containerRunner{c: sharedContainer, dir: dir}
	if !gitInit {
		return r
	}
	mustRun := func(args ...string) {
		t.Helper()
		if _, err := r.Run(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mustRun("init", "-q", "-b", "main")
	mustRun("config", "user.email", "test@example.com")
	mustRun("config", "user.name", "test")
	mustRun("config", "commit.gpgsign", "false")
	return r
}

func writeAt(t *testing.T, r containerRunner, file, content string) {
	t.Helper()
	cmd := []string{"sh", "-c", fmt.Sprintf("printf %%s '%s' > %s/%s", content, r.dir, file)}
	execInContainer(t, cmd...)
}

func commitAt(t *testing.T, r containerRunner, file, content, msg string, when time.Time) {
	t.Helper()
	writeAt(t, r, file, content)
	if _, err := r.Run("add", file); err != nil {
		t.Fatalf("git add: %v", err)
	}
	date := when.Format(time.RFC3339)
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("cd %s && GIT_AUTHOR_DATE='%s' GIT_COMMITTER_DATE='%s' git commit -q -m '%s'",
			r.dir, date, date, msg),
	}
	execInContainer(t, cmd...)
}

func TestChurnCount_OutsideRepo(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, false)
	writeAt(t, r, "x.go", "v1")
	got, err := ChurnCount(r, "x.go", 90)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Errorf("ChurnCount = %d, want 0", got)
	}
}

func TestChurnCount_UntrackedInRepo(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	writeAt(t, r, "never.go", "v1")
	got, err := ChurnCount(r, "never.go", 90)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Errorf("ChurnCount = %d, want 0", got)
	}
}

func TestChurnCount_RecentCommitInWindow(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "init", now.AddDate(0, 0, -5))
	got, err := ChurnCount(r, "a.go", 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 1 {
		t.Errorf("ChurnCount = %d, want 1", got)
	}
}

func TestChurnCount_CommitOutsideWindow(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "init", now.AddDate(0, 0, -30))
	got, err := ChurnCount(r, "a.go", 7)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 0 {
		t.Errorf("ChurnCount = %d, want 0", got)
	}
}

func TestChurnCount_MultipleCommitsWindow(t *testing.T) {
	requireContainer(t)
	r := newRepo(t, true)
	now := time.Now()
	commitAt(t, r, "a.go", "v1", "c1", now.AddDate(0, 0, -2))
	commitAt(t, r, "a.go", "v2", "c2", now.AddDate(0, 0, -1))
	commitAt(t, r, "a.go", "v3", "c3", now)
	got, err := ChurnCount(r, "a.go", 7)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 3 {
		t.Errorf("ChurnCount = %d, want 3", got)
	}
}

func TestChurnCount_SinceDaysNonPositive(t *testing.T) {
	r := containerRunner{}
	_, err := ChurnCount(r, "x", 0)
	if err == nil {
		t.Error("expected error for sinceDays=0, got nil")
	}
	_, err = ChurnCount(r, "x", -1)
	if err == nil {
		t.Error("expected error for sinceDays=-1, got nil")
	}
}
