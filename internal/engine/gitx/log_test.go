package gitx

import (
	"errors"
	"strings"
	"testing"
)

func TestLogCommits_ParsesChronologicalRecords(t *testing.T) {
	g := &gitStub{out: map[string][]byte{
		"log --reverse --date=iso-strict --pretty=format:%H%x1f%h%x1f%cI%x1f%s%x1e --max-count=2 --first-parent HEAD": []byte(
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x1faaaaaaa\x1f2026-07-01T10:00:00+03:00\x1fstart\x1e" +
				"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x1fbbbbbbb\x1f2026-07-02T10:00:00+03:00\x1fgrow\x1e",
		),
	}}

	got, err := LogCommits(g, "HEAD", 2, true)
	if err != nil {
		t.Fatalf("LogCommits: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if got[0].ShortSHA != "aaaaaaa" || got[1].Subject != "grow" {
		t.Errorf("parsed commits = %+v", got)
	}
	if got[0].CommitTime.Format("2006-01-02") != "2026-07-01" {
		t.Errorf("commit time not parsed: %v", got[0].CommitTime)
	}
}

func TestLogCommits_DefaultsEmptyRefToHEAD(t *testing.T) {
	g := &gitStub{out: map[string][]byte{
		"log --reverse --date=iso-strict --pretty=format:%H%x1f%h%x1f%cI%x1f%s%x1e HEAD": nil,
	}}
	if _, err := LogCommits(g, "", 0, false); err != nil {
		t.Fatalf("LogCommits empty ref: %v", err)
	}
}

func TestLogCommits_InvalidRef(t *testing.T) {
	g := &gitStub{err: map[string]error{
		"log --reverse --date=iso-strict --pretty=format:%H%x1f%h%x1f%cI%x1f%s%x1e bad": errors.New("fatal: ambiguous argument 'bad': unknown revision"),
	}}
	_, err := LogCommits(g, "bad", 0, false)
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("want ErrInvalidRef, got %v", err)
	}
}

func TestSingleParent(t *testing.T) {
	t.Run("returns the sole parent", func(t *testing.T) {
		g := &gitStub{out: map[string][]byte{
			"rev-list --parents --max-count=1 child": []byte("child parent\n"),
		}}
		parent, ok, err := SingleParent(g, "child")
		if err != nil {
			t.Fatalf("SingleParent: %v", err)
		}
		if !ok || parent != "parent" {
			t.Fatalf("SingleParent = %q, %v; want parent, true", parent, ok)
		}
	})

	t.Run("rejects root and merge commits", func(t *testing.T) {
		g := &gitStub{out: map[string][]byte{
			"rev-list --parents --max-count=1 root":  []byte("root\n"),
			"rev-list --parents --max-count=1 merge": []byte("merge left right\n"),
		}}
		for _, rev := range []string{"root", "merge"} {
			parent, ok, err := SingleParent(g, rev)
			if err != nil {
				t.Fatalf("SingleParent(%q): %v", rev, err)
			}
			if ok || parent != "" {
				t.Fatalf("SingleParent(%q) = %q, %v; want empty, false", rev, parent, ok)
			}
		}
	})

	t.Run("rejects empty output", func(t *testing.T) {
		g := &gitStub{out: map[string][]byte{
			"rev-list --parents --max-count=1 missing": nil,
		}}
		if _, _, err := SingleParent(g, "missing"); err == nil {
			t.Fatal("SingleParent must reject empty rev-list output")
		}
	})
}

func TestParseCommitLog_RejectsMalformedRecords(t *testing.T) {
	_, err := parseCommitLog([]byte("sha\x1fshort\x1fmissing-subject\x1e"))
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected malformed record error, got %v", err)
	}
}
