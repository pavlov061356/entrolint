package gitx

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

// TreeFiles lists every file path tracked at ref, recursively, relative
// to the repository root. It reads the tree object directly (no checkout
// or working tree needed), so it works for any rev — including a base
// commit that is not checked out.
//
// Returns ErrUnavailable wrapped when git cannot run and ErrInvalidRef
// wrapped when ref does not resolve.
func TreeFiles(r Runner, ref string) ([]string, error) {
	out, err := r.Run("ls-tree", "-r", "--name-only", "-z", ref)
	if err != nil {
		return nil, wrapDiffErr(err)
	}
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// BlobsAtRef returns the contents of each path at ref, keyed by path.
// Paths absent at ref (or unreadable as blobs) are silently omitted —
// the corpus pre-pass treats a missing blob the same as a parse failure.
//
// When the runner implements BatchRunner (production LocalRunner), every
// blob is streamed through a single `git cat-file --batch` subprocess —
// the dominant performance lever versus one fork per file. A runner that
// only implements Run (a hermetic test fake) falls back to a per-path
// FileAtRef loop.
//
// Returns ErrUnavailable wrapped when git cannot run and ErrInvalidRef
// wrapped when ref does not resolve.
func BlobsAtRef(r Runner, ref string, paths []string) (map[string][]byte, error) {
	if len(paths) == 0 {
		return map[string][]byte{}, nil
	}
	if br, ok := r.(BatchRunner); ok {
		return blobsBatch(br, ref, paths)
	}
	return blobsPerFile(r, ref, paths)
}

// blobsBatch feeds every `<ref>:<path>` spec to one `cat-file --batch`
// subprocess and parses the size-delimited records back, in order.
func blobsBatch(r BatchRunner, ref string, paths []string) (map[string][]byte, error) {
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(ref)
		in.WriteByte(':')
		in.WriteString(p)
		in.WriteByte('\n')
	}
	out, err := r.RunStdin(in.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, wrapDiffErr(err)
	}
	return parseBatch(out, paths), nil
}

// parseBatch decodes `git cat-file --batch` output. Records arrive in
// the same order as the requested paths, so we walk `paths` and consume
// one record each. A found record is `<oid> <type> <size>\n<size bytes>\n`;
// a missing object is `<spec> missing\n` with no body. A malformed or
// truncated record stops parsing (the rest of the stream can't be
// re-synced safely), leaving later paths simply absent from the map.
func parseBatch(data []byte, paths []string) map[string][]byte {
	out := make(map[string][]byte, len(paths))
	buf := data
	for _, path := range paths {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			break
		}
		header := string(buf[:nl])
		buf = buf[nl+1:]
		if strings.HasSuffix(header, " missing") {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) < 3 {
			break // unexpected header shape; stop rather than desync
		}
		size, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || size < 0 || len(buf) < size+1 {
			break
		}
		content := make([]byte, size)
		copy(content, buf[:size])
		buf = buf[size+1:] // skip content + its trailing newline
		out[path] = content
	}
	return out
}

// blobsPerFile is the fallback for runners without batch support: one
// FileAtRef per path. ErrNotAtRef is a soft miss (path omitted); fatal
// errors (git unavailable, invalid ref) propagate.
func blobsPerFile(r Runner, ref string, paths []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(paths))
	for _, p := range paths {
		blob, err := FileAtRef(r, ref, p)
		if err != nil {
			if errors.Is(err, ErrNotAtRef) {
				continue
			}
			return nil, err
		}
		out[p] = blob
	}
	return out, nil
}
