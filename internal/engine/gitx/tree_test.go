package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// gitStub is a Runner with canned per-command output/errors. It does NOT
// implement BatchRunner, so BlobsAtRef takes its per-file fallback.
type gitStub struct {
	out map[string][]byte
	err map[string]error
}

func (g *gitStub) Run(args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	if g.err != nil {
		if e, ok := g.err[key]; ok {
			return nil, e
		}
	}
	return g.out[key], nil
}

// gitBatchStub additionally implements BatchRunner, so BlobsAtRef streams
// through RunStdin and we can assert the stdin it was fed.
type gitBatchStub struct {
	*gitStub
	batchOut []byte
	gotStdin []byte
}

func (g *gitBatchStub) RunStdin(stdin []byte, _ ...string) ([]byte, error) {
	g.gotStdin = stdin
	return g.batchOut, nil
}

// batchRecord builds one found `cat-file --batch` record:
// `<oid> blob <size>\n<content>\n`.
func batchRecord(oid, content string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s blob %d\n", oid, len(content))
	b.WriteString(content)
	b.WriteByte('\n')
	return b.Bytes()
}

func TestParseBatch(t *testing.T) {
	paths := []string{"a.go", "missing.go", "multiline.go"}
	var stream bytes.Buffer
	stream.Write(batchRecord("aaaa", "package a\n"))
	stream.WriteString("deadbeef:missing.go missing\n") // no body
	stream.Write(batchRecord("cccc", "line1\nline2\n")) // embedded newline

	got := parseBatch(stream.Bytes(), paths)

	if string(got["a.go"]) != "package a\n" {
		t.Errorf("a.go: got %q", got["a.go"])
	}
	if _, ok := got["missing.go"]; ok {
		t.Error("missing.go must be absent from the result")
	}
	if string(got["multiline.go"]) != "line1\nline2\n" {
		t.Errorf("multiline.go (embedded newline): got %q", got["multiline.go"])
	}
}

func TestParseBatch_TruncatedStops(t *testing.T) {
	// A record claiming 99 bytes with only a few present must not read
	// out of bounds — parsing stops, later paths simply absent.
	stream := []byte("aaaa blob 99\nshort")
	got := parseBatch(stream, []string{"a.go", "b.go"})
	if len(got) != 0 {
		t.Errorf("truncated record must yield no entries, got %v", got)
	}
}

func TestTreeFiles(t *testing.T) {
	g := &gitStub{out: map[string][]byte{
		"ls-tree -r --name-only -z HEAD": []byte("a.go\x00sub/b.go\x00vendor/x.go\x00"),
	}}
	files, err := TreeFiles(g, "HEAD")
	if err != nil {
		t.Fatalf("TreeFiles: %v", err)
	}
	want := []string{"a.go", "sub/b.go", "vendor/x.go"} // raw; filtering is the caller's job
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", files, want)
	}
}

func TestTreeFiles_InvalidRef(t *testing.T) {
	g := &gitStub{err: map[string]error{
		"ls-tree -r --name-only -z bad": errors.New("fatal: Not a valid object name bad"),
	}}
	_, err := TreeFiles(g, "bad")
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("want ErrInvalidRef, got %v", err)
	}
}

func TestBlobsAtRef_PerFileFallback(t *testing.T) {
	g := &gitStub{
		out: map[string][]byte{
			"cat-file blob HEAD:a.go": []byte("package a\n"),
		},
		err: map[string]error{
			// FileAtRef maps this message shape to ErrNotAtRef → soft miss.
			"cat-file blob HEAD:gone.go": errors.New("fatal: path 'gone.go' does not exist in 'HEAD'"),
		},
	}
	blobs, err := BlobsAtRef(g, "HEAD", []string{"a.go", "gone.go"})
	if err != nil {
		t.Fatalf("BlobsAtRef: %v", err)
	}
	if string(blobs["a.go"]) != "package a\n" {
		t.Errorf("a.go: got %q", blobs["a.go"])
	}
	if _, ok := blobs["gone.go"]; ok {
		t.Error("absent path must be a soft miss, not present")
	}
}

func TestBlobsAtRef_BatchPath(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(batchRecord("aaaa", "package a\n"))
	stream.Write(batchRecord("bbbb", "package b\n"))
	g := &gitBatchStub{gitStub: &gitStub{}, batchOut: stream.Bytes()}

	blobs, err := BlobsAtRef(g, "HEAD", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatalf("BlobsAtRef: %v", err)
	}
	if string(blobs["a.go"]) != "package a\n" || string(blobs["b.go"]) != "package b\n" {
		t.Errorf("batch blobs: got %v", blobs)
	}
	// The batch was fed one `<ref>:<path>` spec per line on stdin.
	if !strings.Contains(string(g.gotStdin), "HEAD:a.go\n") || !strings.Contains(string(g.gotStdin), "HEAD:b.go\n") {
		t.Errorf("stdin missing specs: %q", g.gotStdin)
	}
}
