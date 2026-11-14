# atomic_file_write

Writes files atomically by staging to a temporary path in the destination
directory and renaming over the destination, syncing before the rename and
cleaning up the temporary file on any error.

## Usage

```go
package main

import (
	"log"
	"os"

	afw "atomic_file_write"
)

func main() {
	if err := afw.Write("config.json", []byte(`{"ok":true}`), 0o644); err != nil {
		log.Fatal(err)
	}

	f, err := os.Open("large-input.bin")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if err := afw.WriteReader("copy.bin", f, 0o600); err != nil {
		log.Fatal(err)
	}
}
```

## Why this exists

`os.WriteFile` truncates the destination and then writes into it. A reader
that opens the file between those two steps sees a truncated or partially
written file, and a crash mid-write leaves the destination in that state.
Staging the bytes in a sibling temporary file and renaming over the
destination means readers only ever observe the old contents or the new
contents.

The trade-off is placement: the temporary file is created in the destination
directory, not in `os.TempDir()`. A rename is only atomic within a single
filesystem, so putting the staging file next to the destination is what makes
the final step atomic. The cost is that the destination directory must be
writable, which is the same requirement `os.WriteFile` already imposes.

## The awkward edge

Renames replace the destination entry itself. If the destination is a
symlink, `Write` replaces the symlink with a regular file; it does not follow
the link and overwrite the target. This is the same behaviour as
`os.Rename` and is the only behaviour that can be atomic, but it is worth
knowing if you were expecting `Write` to behave like `os.WriteFile` on a
symlinked path.

## API

- `func Write(path string, data []byte, perm os.FileMode) error`
- `func WriteReader(path string, r io.Reader, perm os.FileMode) error`

If `perm` is zero, `0644` is used. The permission bits are applied to the
temporary file before the rename, so the destination never appears with the
process umask applied.
