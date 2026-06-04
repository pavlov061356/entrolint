package gitx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ChangeKind classifies how a path changed between base and head.
//
// Renames are first-class via git's -M50 detection; copies (-C) are
// deliberately not enabled — they're semantically covered by
// ChangeAdded for ΔS purposes.
type ChangeKind int

const (
	// ChangeModified — path exists at both refs with different content.
	ChangeModified ChangeKind = iota
	// ChangeAdded — path exists at head but not at base.
	ChangeAdded
	// ChangeRemoved — path exists at base but not at head.
	ChangeRemoved
	// ChangeRenamed — path moved from OldPath (base) to Path (head),
	// with or without edits.
	ChangeRenamed
)

// Change is one entry in the base..head diff that the check pipeline
// can act on. Paths gitx cannot reason about (binary, submodule,
// symlink, mode-only) do not appear here — they surface on
// DiffResult.Skipped so the gate report can be transparent about what
// was excluded from ΔS.
//
// Path is the head-side path. For ChangeRemoved, Path is the base-side
// path (the only one that exists). For ChangeRenamed, OldPath holds
// the base-side name and LinesAdded/LinesRemoved may be zero (pure
// rename) or non-zero (rename with edits).
//
// LinesAdded and LinesRemoved come from git diff --numstat; their sum
// (LinesChanged) feeds the ΔS_density denominator.
type Change struct {
	Kind         ChangeKind
	Path         string
	OldPath      string
	LinesAdded   int
	LinesRemoved int
}

// LinesChanged returns LinesAdded + LinesRemoved.
func (c Change) LinesChanged() int { return c.LinesAdded + c.LinesRemoved }

// SkipReason explains why a path that appeared in git diff was
// excluded from DiffResult.Files.
type SkipReason int

const (
	// SkipBinary — git's text/binary heuristic flagged it (numstat -\t-).
	SkipBinary SkipReason = iota + 1
	// SkipSubmodule — mode 160000 on either side.
	SkipSubmodule
	// SkipSymlink — mode 120000 on either side.
	SkipSymlink
	// SkipModeOnly — content unchanged, mode bits differ.
	SkipModeOnly
)

// SkippedPath is one excluded diff entry.
type SkippedPath struct {
	Path   string     `json:"path"`
	Reason SkipReason `json:"reason"`
}

// String renders SkipReason as a stable identifier suitable for reports
// and JSON output. Unknown values fall through to "unknown" so a future
// reason added before the report layer catches up doesn't crash.
func (r SkipReason) String() string {
	switch r {
	case SkipBinary:
		return "binary"
	case SkipSubmodule:
		return "submodule"
	case SkipSymlink:
		return "symlink"
	case SkipModeOnly:
		return "mode_only"
	default:
		return "unknown"
	}
}

// MarshalJSON renders SkipReason as its string identifier so JSON
// consumers don't have to mirror the iota order. Delegates to
// encoding/json to handle escaping symmetrically with DeltaKind —
// today every name is safe ASCII, but a future identifier with a
// quote or non-ASCII rune must not silently emit invalid JSON.
func (r SkipReason) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON accepts the same identifiers MarshalJSON emits;
// unknown values resolve to SkipReason(0) so older readers can still
// parse newer JSON.
func (r *SkipReason) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	switch s {
	case "binary":
		*r = SkipBinary
	case "submodule":
		*r = SkipSubmodule
	case "symlink":
		*r = SkipSymlink
	case "mode_only":
		*r = SkipModeOnly
	default:
		*r = 0
	}
	return nil
}

// DiffResult is the typed output of Diff. Files plus Skipped together
// account for every path git reported. rng is the base...head range
// the result was built from — Patches uses it to fetch hunks without
// the caller re-passing the refs.
type DiffResult struct {
	Files   []Change
	Skipped []SkippedPath
	rng     string
}

var (
	// ErrInvalidRef indicates a rev cannot be resolved to a commit
	// (typo, unfetched branch, removed tag).
	ErrInvalidRef = errors.New("gitx: invalid ref")

	// ErrNotAtRef is returned by FileAtRef when ref:path is absent
	// — e.g. asking for the base-side content of an added file.
	ErrNotAtRef = errors.New("gitx: path not present at ref")
)

// Diff returns the classified changes between base and head, in that
// order. Triple-dot semantics (base...head) are used so the comparison
// matches what GitHub shows on a PR. Rename detection is on (-M50).
//
// Returns ErrUnavailable wrapped when the git binary cannot run.
// Returns ErrInvalidRef wrapped when base or head fails to resolve.
// Other git failures surface as plain errors.
func Diff(r Runner, base, head string) (DiffResult, error) {
	rng := base + "..." + head
	rawOut, err := r.Run("diff", "--raw", "-z", "-M50", rng)
	if err != nil {
		return DiffResult{}, wrapDiffErr(err)
	}
	numOut, err := r.Run("diff", "--numstat", "-z", "-M50", rng)
	if err != nil {
		return DiffResult{}, wrapDiffErr(err)
	}
	res := mergeDiff(parseRaw(rawOut), parseNumstat(numOut))
	res.rng = rng
	return res, nil
}

// Patches returns the unified-0 patch body for every changed file in
// this DiffResult, keyed by head-side path (or base-side for pure
// deletions, where the head side is /dev/null). Skipped paths from
// the raw diff don't appear here. Detectors that need the actual
// added/removed lines (e.g. shotgun) call this — others stay on the
// metadata-only DiffResult.Files.
//
// Returns ErrUnavailable wrapped when git can't run.
func (d DiffResult) Patches(r Runner) (map[string][]byte, error) {
	if d.rng == "" {
		return nil, errors.New("gitx: DiffResult was not produced by Diff()")
	}
	out, err := r.Run("diff", "--unified=0", "--no-color", "-M50", d.rng)
	if err != nil {
		return nil, wrapDiffErr(err)
	}
	return splitPatchesByFile(out), nil
}

// splitPatchesByFile carves a raw `git diff` payload into per-file
// chunks. Each `diff --git a/<src> b/<dst>` line opens a new entry;
// the chunk body is everything until the next such line. Deletions
// key on the base-side path because b/<dst> is `/dev/null`.
func splitPatchesByFile(data []byte) map[string][]byte {
	out := make(map[string][]byte)
	chunks := strings.Split("\n"+string(data), "\ndiff --git ")
	for _, chunk := range chunks[1:] {
		nl := strings.IndexByte(chunk, '\n')
		if nl < 0 {
			continue
		}
		header := chunk[:nl]
		body := []byte(chunk[nl+1:])

		fields := strings.Fields(header)
		if len(fields) < 2 {
			continue
		}
		aPath := strings.TrimPrefix(fields[0], "a/")
		bPath := strings.TrimPrefix(fields[1], "b/")
		key := bPath
		if bPath == "/dev/null" {
			key = aPath
		}
		out[key] = body
	}
	return out
}

// FileAtRef returns the contents of path at ref. ref can be any rev
// (branch, tag, full or short SHA, HEAD~N).
//
// Returns (nil, ErrNotAtRef wrapped) when ref:path does not exist.
// Returns ErrUnavailable wrapped when the git binary cannot run.
// Returns ErrInvalidRef wrapped when ref itself is invalid.
func FileAtRef(r Runner, ref, path string) ([]byte, error) {
	out, err := r.Run("cat-file", "blob", ref+":"+path)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return nil, err
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "exists on disk, but not in"):
		return nil, fmt.Errorf("%w: %s", ErrNotAtRef, path)
	case strings.Contains(msg, "path '"+path+"' does not exist"):
		return nil, fmt.Errorf("%w: %s", ErrNotAtRef, path)
	case strings.Contains(msg, "Not a valid object name"):
		// Covers both ref-not-found and path-not-at-ref shapes.
		// Heuristic: if the failing token mentions ":<path>", treat
		// as missing path; otherwise as invalid ref.
		if strings.Contains(msg, ":"+path) {
			return nil, fmt.Errorf("%w: %s", ErrNotAtRef, path)
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidRef, ref)
	case strings.Contains(msg, "fatal: invalid object name") ||
		strings.Contains(msg, "unknown revision") ||
		strings.Contains(msg, "ambiguous argument"):
		return nil, fmt.Errorf("%w: %s", ErrInvalidRef, ref)
	}
	return nil, err
}

// ResolveRef expands rev (branch, tag, short SHA, HEAD~N) to its full
// 40-char commit SHA. Annotated tags are peeled to their commit via
// ^{commit}.
//
// Returns ErrInvalidRef wrapped when the rev does not resolve.
func ResolveRef(r Runner, rev string) (string, error) {
	out, err := r.Run("rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", ErrInvalidRef, rev)
	}
	return strings.TrimSpace(string(out)), nil
}

func wrapDiffErr(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, "unknown revision") ||
		strings.Contains(msg, "ambiguous argument") ||
		strings.Contains(msg, "bad revision") ||
		strings.Contains(msg, "Not a valid object name") {
		return fmt.Errorf("%w: %v", ErrInvalidRef, err)
	}
	return err
}

// rawEntry is one parsed `git diff --raw -z` record.
type rawEntry struct {
	srcMode string
	dstMode string
	srcSHA  string
	dstSHA  string
	status  string // "M", "A", "D", "R100", "C90", "T", ...
	path    string
	newPath string // only for R/C statuses
}

// parseRaw splits NUL-delimited `git diff --raw -z` output.
//
// Header: `:<srcmode> <dstmode> <srcsha> <dstsha> <status>` followed
// by NUL, then the path, then (for R/C) another NUL and the new path,
// then another NUL terminating the record.
func parseRaw(data []byte) []rawEntry {
	tokens := strings.Split(string(data), "\x00")
	var out []rawEntry
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) == 0 || tok[0] != ':' {
			continue
		}
		fields := strings.Fields(tok[1:])
		if len(fields) < 5 {
			continue
		}
		entry := rawEntry{
			srcMode: fields[0],
			dstMode: fields[1],
			srcSHA:  fields[2],
			dstSHA:  fields[3],
			status:  fields[4],
		}
		i++
		if i >= len(tokens) {
			break
		}
		entry.path = tokens[i]
		if len(entry.status) > 0 && (entry.status[0] == 'R' || entry.status[0] == 'C') {
			i++
			if i >= len(tokens) {
				break
			}
			entry.newPath = tokens[i]
		}
		out = append(out, entry)
	}
	return out
}

// numstatEntry is one parsed `git diff --numstat -z` record.
type numstatEntry struct {
	added   int
	removed int
	binary  bool
	path    string
	newPath string // populated when rename was detected
}

// parseNumstat splits NUL-delimited `git diff --numstat -z` output.
//
// Normal record: `<added>\t<removed>\t<path>\0`. Binary: `-\t-\t<path>\0`.
// Rename: `<added>\t<removed>\t\0<oldpath>\0<newpath>\0`.
func parseNumstat(data []byte) []numstatEntry {
	tokens := strings.Split(string(data), "\x00")
	var out []numstatEntry
	i := 0
	for i < len(tokens) {
		entry, next, ok := parseOneNumstat(tokens, i)
		i = next
		if ok {
			out = append(out, entry)
		}
	}
	return out
}

func parseOneNumstat(tokens []string, i int) (numstatEntry, int, bool) {
	tok := tokens[i]
	if len(tok) == 0 {
		return numstatEntry{}, i + 1, false
	}
	parts := strings.SplitN(tok, "\t", 3)
	if len(parts) < 2 {
		return numstatEntry{}, i + 1, false
	}
	e := numstatCounts(parts[0], parts[1])
	if len(parts) >= 3 && parts[2] != "" {
		e.path = parts[2]
		return e, i + 1, true
	}
	if i+2 >= len(tokens) {
		return e, len(tokens), false
	}
	e.path = tokens[i+1]
	e.newPath = tokens[i+2]
	return e, i + 3, true
}

func numstatCounts(addStr, remStr string) numstatEntry {
	var e numstatEntry
	if addStr == "-" && remStr == "-" {
		e.binary = true
		return e
	}
	e.added, _ = strconv.Atoi(addStr)
	e.removed, _ = strconv.Atoi(remStr)
	return e
}

// mergeDiff combines raw and numstat entries into the final DiffResult.
// Keyed by head-side path (or base path for deletions).
func mergeDiff(raw []rawEntry, num []numstatEntry) DiffResult {
	numByKey := make(map[string]numstatEntry, len(num))
	for _, n := range num {
		key := n.path
		if n.newPath != "" {
			key = n.newPath
		}
		numByKey[key] = n
	}

	var result DiffResult
	for _, r := range raw {
		key := r.path
		if r.newPath != "" {
			key = r.newPath
		}
		n := numByKey[key]

		if reason, skipped := classifySkip(r, n); skipped {
			result.Skipped = append(result.Skipped, SkippedPath{Path: key, Reason: reason})
			continue
		}

		kind, path, oldPath := classifyChange(r)
		result.Files = append(result.Files, Change{
			Kind:         kind,
			Path:         path,
			OldPath:      oldPath,
			LinesAdded:   n.added,
			LinesRemoved: n.removed,
		})
	}
	return result
}

func classifySkip(r rawEntry, n numstatEntry) (SkipReason, bool) {
	if r.srcMode == "160000" || r.dstMode == "160000" {
		return SkipSubmodule, true
	}
	if r.srcMode == "120000" || r.dstMode == "120000" {
		return SkipSymlink, true
	}
	if n.binary {
		return SkipBinary, true
	}
	// SHA equality alone implies no content delta — covers mode-only
	// modifications and pure renames where the blob is unchanged but
	// the mode bits flipped. The status code (M/R/C) is irrelevant.
	if r.srcSHA != "" && r.srcSHA == r.dstSHA && r.srcMode != r.dstMode {
		return SkipModeOnly, true
	}
	return 0, false
}

func classifyChange(r rawEntry) (ChangeKind, string, string) {
	if len(r.status) == 0 {
		return ChangeModified, r.path, ""
	}
	switch r.status[0] {
	case 'A':
		return ChangeAdded, r.path, ""
	case 'D':
		return ChangeRemoved, r.path, ""
	case 'R', 'C':
		return ChangeRenamed, r.newPath, r.path
	default: // M, T, fallback
		return ChangeModified, r.path, ""
	}
}
