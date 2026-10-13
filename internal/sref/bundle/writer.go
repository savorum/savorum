package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"time"
)

// archiveEpoch is the timestamp every bundle entry carries. The specification
// does not require canonical bytes, but the writer avoids adding clock
// variation when given the same exact member bytes in the same order.
var archiveEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// Writer streams a bundle. Each member is written straight into the archive
// while its digest and size are measured, and the manifest, written last from
// those measurements, needs no buffering.
type Writer struct {
	archive *zip.Writer
	entries []Entry
	failed  error
	closed  bool
}

// NewWriter begins a bundle. The caller closes it to write the manifest.
func NewWriter(destination io.Writer) *Writer {
	return &Writer{archive: zip.NewWriter(destination)}
}

// Add writes one member. The callback must produce a complete `.sref` package,
// which is measured but not inspected. Members are stored, not deflated, since
// they are already compressed.
func (w *Writer) Add(recipeID string, write func(io.Writer) error) error {
	if w.failed != nil {
		return w.failed
	}

	if w.closed {
		return w.fail(fmt.Errorf("%w: the bundle is already closed", ErrInvalidBundle))
	}

	if !ValidPortableID(recipeID) {
		return w.fail(fmt.Errorf("%w: %q is not a portable recipe identifier", ErrInvalidBundle, recipeID))
	}

	memberID := fmt.Sprintf("r%06d", len(w.entries)+1)
	path := MemberPath(memberID)

	entry, err := w.archive.CreateHeader(&zip.FileHeader{
		Name: path, Method: zip.Store, Modified: archiveEpoch,
	})
	if err != nil {
		return w.fail(fmt.Errorf("write %q into the bundle: %w", path, err))
	}

	measured := &measuringWriter{destination: entry, digest: sha256.New()}
	if err := write(measured); err != nil {
		return w.fail(err)
	}

	w.entries = append(w.entries, Entry{
		MemberID: memberID,
		RecipeID: recipeID,
		Path:     path,
		SHA256:   hex.EncodeToString(measured.digest.Sum(nil)),
		Size:     measured.size,
	})

	return nil
}

// Close writes the manifest and finishes the archive. A bundle with no members
// is refused.
func (w *Writer) Close() error {
	if w.failed != nil {
		return w.failed
	}

	if w.closed {
		return nil
	}

	w.closed = true

	manifest, err := Manifest{Format: Format, Version: Version, Recipes: w.entries}.Encode()
	if err != nil {
		return w.fail(err)
	}

	entry, err := w.archive.CreateHeader(&zip.FileHeader{
		Name: ManifestPath, Method: zip.Deflate, Modified: archiveEpoch,
	})
	if err != nil {
		return w.fail(fmt.Errorf("write the bundle manifest: %w", err))
	}

	if _, err := entry.Write(manifest); err != nil {
		return w.fail(fmt.Errorf("write the bundle manifest: %w", err))
	}

	if err := w.archive.Close(); err != nil {
		return w.fail(fmt.Errorf("close the bundle: %w", err))
	}

	return nil
}

// Manifest reports what was written, in the order it was written.
func (w *Writer) Manifest() Manifest {
	return Manifest{Format: Format, Version: Version, Recipes: append([]Entry(nil), w.entries...)}
}

// fail records the first failure so a later call cannot append to a bundle
// that is already wrong, and so the caller sees the original cause rather than
// a consequence of it.
func (w *Writer) fail(err error) error {
	if w.failed == nil {
		w.failed = err
	}

	return w.failed
}

type measuringWriter struct {
	destination io.Writer
	digest      hash.Hash
	size        int64
}

func (w *measuringWriter) Write(data []byte) (int, error) {
	written, err := w.destination.Write(data)
	if written > 0 {
		w.digest.Write(data[:written])
		w.size += int64(written)
	}

	if err != nil {
		return written, fmt.Errorf("write measured bundle bytes: %w", err)
	}

	return written, nil
}
