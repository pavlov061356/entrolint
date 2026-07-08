package gitx

import (
	"fmt"
	"strings"
	"time"
)

const (
	logFieldSep  = "\x1f"
	logRecordSep = "\x1e"
)

// Commit is one git commit as needed by history-oriented reports.
type Commit struct {
	SHA        string    `json:"sha"`
	ShortSHA   string    `json:"short_sha"`
	CommitTime time.Time `json:"commit_time"`
	Subject    string    `json:"subject"`
}

// LogCommits returns commits reachable from ref in chronological order.
// limit caps the number of most-recent commits before reversing them, so
// the returned slice is suitable for an S(t) timeline.
func LogCommits(r Runner, ref string, limit int, firstParent bool) ([]Commit, error) {
	if strings.TrimSpace(ref) == "" {
		ref = "HEAD"
	}
	args := []string{
		"log",
		"--reverse",
		"--date=iso-strict",
		"--pretty=format:%H%x1f%h%x1f%cI%x1f%s%x1e",
	}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	if firstParent {
		args = append(args, "--first-parent")
	}
	args = append(args, ref)

	out, err := r.Run(args...)
	if err != nil {
		return nil, wrapDiffErr(err)
	}
	return parseCommitLog(out)
}

func parseCommitLog(out []byte) ([]Commit, error) {
	records := strings.Split(string(out), logRecordSep)
	commits := make([]Commit, 0, len(records))
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.Split(record, logFieldSep)
		if len(fields) != 4 {
			return nil, fmt.Errorf("parse git log: malformed record %q", record)
		}
		t, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("parse git log time %q: %w", fields[2], err)
		}
		commits = append(commits, Commit{
			SHA:        fields[0],
			ShortSHA:   fields[1],
			CommitTime: t,
			Subject:    fields[3],
		})
	}
	return commits, nil
}
