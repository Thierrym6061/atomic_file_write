package atomic_file_write

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := Write(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := os.WriteFile(path, []byte("old contents that are longer"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := Write(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

func TestWriteAppliesPermissionBits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mode.txt")

	if err := Write(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("perm = %o, want %o", got, 0o600)
	}
}

func TestWriteZeroPermUsesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.txt")

	if err := Write(path, []byte("x"), 0); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != defaultPerm {
		t.Fatalf("perm = %o, want %o", got, defaultPerm)
	}
}

func TestWriteEmptyPath(t *testing.T) {
	err := Write("", []byte("x"), 0o644)
	if err == nil {
		t.Fatal("Write with empty path returned nil, want error")
	}
	if !strings.Contains(err.Error(), "empty path") {
		t.Fatalf("error = %v, want mention of empty path", err)
	}
}

func TestWriteMissingDirectoryLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope", "out.txt")

	err := Write(path, []byte("x"), 0o644)
	if err == nil {
		t.Fatal("Write into missing directory returned nil, want error")
	}

	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatalf("ReadDir: %v", rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("directory not clean after failure: %v", entries)
	}
}

// failingReader returns an error partway through so we can exercise the
// cleanup path in WriteReader without needing real I/O failures.
type failingReader struct {
	data []byte
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, errors.New("boom")
}

func TestWriteReaderCopiesAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reader.txt")

	if err := WriteReader(path, bytes.NewReader([]byte("streamed")), 0o644); err != nil {
		t.Fatalf("WriteReader returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "streamed" {
		t.Fatalf("content = %q, want %q", got, "streamed")
	}
}

func TestWriteReaderFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reader.txt")

	err := WriteReader(path, &failingReader{data: []byte("partial")}, 0o644)
	if err == nil {
		t.Fatal("WriteReader with failing reader returned nil, want error")
	}

	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatalf("destination exists after failure: %v", serr)
	}

	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatalf("ReadDir: %v", rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestWriteReaderFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reader.txt")

	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := WriteReader(path, &failingReader{data: []byte("partial")}, 0o644)
	if err == nil {
		t.Fatal("WriteReader with failing reader returned nil, want error")
	}

	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("ReadFile: %v", rerr)
	}
	if string(got) != "original" {
		t.Fatalf("original file was modified: %q", got)
	}
}

func TestWriteReplacesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")

	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := Write(link, []byte("replaced"), 0o644); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	// The rename replaces the symlink itself, not the file it points at.
	// This matches how os.Rename behaves and is the documented behaviour.
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("link is still a symlink; rename did not replace it")
	}
	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("ReadFile link: %v", err)
	}
	if string(got) != "replaced" {
		t.Fatalf("link content = %q, want %q", got, "replaced")
	}
	orig, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	if string(orig) != "target" {
		t.Fatalf("target was modified: %q", orig)
	}
}
