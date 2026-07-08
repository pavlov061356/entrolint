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

func TestParseCommitLog_RejectsMalformedRecords(t *testing.T) {
	_, err := parseCommitLog([]byte("sha\x1fshort\x1fmissing-subject\x1e"))
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected malformed record error, got %v", err)
	}
}
