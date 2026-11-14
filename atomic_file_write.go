// Package atomic_file_write writes files atomically by staging to a temporary
// path in the same directory and renaming over the destination. A rename
// within a single filesystem is atomic on POSIX and on Windows (via
// MoveFileEx), so readers either see the old file or the new one, never a
// half-written file.
//
// The trade-off is that the temporary file must live on the same filesystem as
// the destination, otherwise the final rename is a copy and is no longer
// atomic. This package therefore always creates the temporary file in the
// destination's directory rather than in os.TempDir().
package atomic_file_write

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultPerm is the permission used when the caller does not specify one. It
// matches the mode os.WriteFile uses (0644) so the common case is unsurprising.
const defaultPerm os.FileMode = 0o644

// Write writes data to path atomically. It stages the data in a temporary
// file in the same directory as path, syncs it to stable storage, closes it,
// and renames it over path. If any step fails, the temporary file is removed
// and the original file (if any) is left untouched.
//
// The permission bits of the new file are perm & os.ModePerm. If perm is zero,
// defaultPerm is used instead. The permission argument is applied before the
// rename so the file never briefly appears with the process umask applied.
func Write(path string, data []byte, perm os.FileMode) error {
	return writeAll(path, perm, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// WriteReader writes the contents of r to path atomically. It reads r to
// completion while staging the data. Unlike Write it does not need the full
// payload in memory, but it still cannot avoid a temporary file because the
// destination must not be observable until the write completes.
func WriteReader(path string, r io.Reader, perm os.FileMode) error {
	return writeAll(path, perm, func(f *os.File) error {
		_, err := io.Copy(f, r)
		return err
	})
}

// writeAll is the shared implementation behind Write and WriteReader. It owns
// the temporary file lifecycle: create, populate, sync, close, rename. The
// populate callback is responsible only for producing the file's bytes.
func writeAll(path string, perm os.FileMode, populate func(*os.File) error) (err error) {
	if path == "" {
		return errors.New("atomic_file_write: empty path")
	}
	if perm == 0 {
		perm = defaultPerm
	}
	perm &= os.ModePerm

	dir := filepath.Dir(path)

	// CreateTemp uses a random suffix, so concurrent writers do not collide.
	// We deliberately do not pass a pattern tied to the destination name:
	// the random part is what makes the staging safe.
	tmp, err := os.CreateTemp(dir, ".atomic-file-write-*")
	if err != nil {
		return fmt.Errorf("atomic_file_write: create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// From here on any failure must remove the temporary file. We use a
	// deferred cleanup that is disarmed only after a successful rename.
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err = populate(tmp); err != nil {
		return fmt.Errorf("atomic_file_write: write temp: %w", err)
	}

	// fsync the file before renaming. Without this a crash after rename can
	// leave a file whose contents are not yet durable, which defeats the
	// point of an atomic write for callers that care about power loss.
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("atomic_file_write: sync temp: %w", err)
	}

	// Apply the requested mode explicitly. CreateTemp uses 0600, and the
	// process umask may further restrict it, so chmod is the only way to
	// honour the caller's perm exactly.
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("atomic_file_write: chmod temp: %w", err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("atomic_file_write: close temp: %w", err)
	}

	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic_file_write: rename temp to %s: %w", path, err)
	}

	// The rename succeeded, so the temporary name no longer exists and the
	// deferred cleanup must not run.
	cleanup = false

	// fsync the directory so the rename itself is durable. This is a no-op
	// on platforms that do not support directory fsync, and we tolerate
	// that rather than failing an otherwise successful write.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}
