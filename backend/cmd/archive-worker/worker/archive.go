package worker

import (
	"archive/zip"
	"compress/flate"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/services/cdn"
)

// storedExtensions are files compressed already: deflating them again only costs time.
var storedExtensions = map[string]bool{
	".gz": true, ".bz2": true, ".xz": true, ".zst": true, ".zip": true, ".7z": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// compressionMethod is zip.Store for a file compressed already, zip.Deflate otherwise.
// Extracting gives the exact bytes either way.
func compressionMethod(name string) uint16 {
	if storedExtensions[strings.ToLower(path.Ext(name))] {
		return zip.Store
	}
	return zip.Deflate
}

// countingWriter counts the bytes written through it.
type countingWriter struct {
	written int64
}

func (c *countingWriter) Write(data []byte) (int, error) {
	c.written += int64(len(data))
	return len(data), nil
}

// zipArchive writes one zip stream to out and keeps its size and SHA-256. Go's zip writer adds the
// zip64 records by itself once the archive passes 4 GiB or 65,535 entries.
type zipArchive struct {
	writer   *zip.Writer
	hasher   hash.Hash
	counter  *countingWriter
	modified time.Time
}

// newZipArchive starts a zip on out; every entry gets modified as its time, so the same selection
// gives the same entries.
func newZipArchive(out io.Writer, modified time.Time) *zipArchive {
	hasher := sha256.New()
	counter := &countingWriter{}
	writer := zip.NewWriter(io.MultiWriter(out, hasher, counter))
	writer.RegisterCompressor(zip.Deflate, func(target io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(target, flate.BestSpeed)
	})
	return &zipArchive{writer: writer, hasher: hasher, counter: counter, modified: modified.UTC()}
}

// add writes one entry at name with the bytes of content.
func (a *zipArchive) add(name string, content io.Reader) error {
	entry, err := a.writer.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   compressionMethod(name),
		Modified: a.modified,
	})
	if err != nil {
		return fmt.Errorf("start zip entry %s: %w", name, err)
	}
	if _, err := io.Copy(entry, content); err != nil {
		return fmt.Errorf("write zip entry %s: %w", name, err)
	}
	return nil
}

// close writes the central directory and returns the zip's size and SHA-256.
func (a *zipArchive) close() (int64, string, error) {
	if err := a.writer.Close(); err != nil {
		return 0, "", fmt.Errorf("finish zip: %w", err)
	}
	return a.counter.written, hex.EncodeToString(a.hasher.Sum(nil)), nil
}

// objectReader opens objects of the data bucket by key.
type objectReader interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// errStoredCopyMissing means the bucket has no object at a key the record names.
var errStoredCopyMissing = errors.New("our copy of the file is missing")

// errScratchDisk means the worker's own scratch disk failed (full, for example) while it held a
// file. It is not the file's fault: the attempt fails, and the file is never listed as read_failed.
var errScratchDisk = errors.New("the archive worker could not hold a file on its disk")

// scratchStore is where a scratch file's bytes live: an *os.File, or one that fails in tests.
type scratchStore interface {
	io.ReadWriteSeeker
	Truncate(size int64) error
	Close() error
	Name() string
}

// scratchFile holds one file's bytes on local disk while they are checked, so a file whose bytes
// differ from its record never reaches the zip.
type scratchFile struct {
	file scratchStore
}

// scratchWriter writes to the scratch file and marks its errors errScratchDisk, so a failing disk
// is told apart from a failing read of the object.
type scratchWriter struct {
	file scratchStore
}

func (w scratchWriter) Write(p []byte) (int, error) {
	written, err := w.file.Write(p)
	if err != nil {
		return written, fmt.Errorf("%w: write scratch file: %w", errScratchDisk, err)
	}
	return written, nil
}

func newScratchFile(dir string) (*scratchFile, error) {
	file, err := os.CreateTemp(dir, "archive-file-*")
	if err != nil {
		return nil, fmt.Errorf("create scratch file: %w", err)
	}
	return &scratchFile{file: file}, nil
}

// stagedBytes is what staging read: the bytes are in the scratch file, rewound.
type stagedBytes struct {
	sha256 string
	size   int64
}

// stage copies the object at key into the scratch file and hashes it. A missing object is
// errStoredCopyMissing and a failing scratch disk errScratchDisk, both at once; any other read
// error, a read that takes longer than timeout included, is tried again, attempts times in all,
// with a pause that doubles from firstPause.
func (s *scratchFile) stage(
	ctx context.Context,
	bucket objectReader,
	key string,
	attempts int,
	timeout time.Duration,
	firstPause time.Duration,
) (stagedBytes, error) {
	pause := firstPause
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		staged, err := s.stageOnce(ctx, bucket, key, timeout)
		switch {
		case err == nil:
			return staged, nil
		case errors.Is(err, cdn.ErrObjectNotFound):
			return stagedBytes{}, fmt.Errorf("%w: %s", errStoredCopyMissing, key)
		case errors.Is(err, errScratchDisk):
			return stagedBytes{}, fmt.Errorf("stage %s: %w", key, err)
		case ctx.Err() != nil:
			return stagedBytes{}, fmt.Errorf("stage %s: %w", key, ctx.Err())
		}
		lastErr = err
		if attempt < attempts {
			if err := sleepContext(ctx, pause); err != nil {
				return stagedBytes{}, fmt.Errorf("stage %s: %w", key, err)
			}
			pause *= 2
		}
	}
	return stagedBytes{}, fmt.Errorf("stage %s after %d attempts: %w", key, attempts, lastErr)
}

func (s *scratchFile) stageOnce(ctx context.Context, bucket objectReader, key string, timeout time.Duration) (stagedBytes, error) {
	if err := s.rewind(true); err != nil {
		return stagedBytes{}, err
	}
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	reader, err := bucket.Open(readCtx, key)
	if err != nil {
		return stagedBytes{}, fmt.Errorf("open object: %w", err)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(scratchWriter{file: s.file}, hasher), reader)
	closeErr := reader.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return stagedBytes{}, fmt.Errorf("copy object to scratch file: %w", err)
	}
	if err := s.rewind(false); err != nil {
		return stagedBytes{}, err
	}
	return stagedBytes{sha256: hex.EncodeToString(hasher.Sum(nil)), size: size}, nil
}

// rewind moves to the start of the scratch file, emptying it first when truncate is set.
func (s *scratchFile) rewind(truncate bool) error {
	if truncate {
		if err := s.file.Truncate(0); err != nil {
			return fmt.Errorf("%w: empty scratch file: %w", errScratchDisk, err)
		}
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("%w: rewind scratch file: %w", errScratchDisk, err)
	}
	return nil
}

// reader reads the staged bytes from the start.
func (s *scratchFile) reader() io.Reader {
	return s.file
}

// close removes the scratch file.
func (s *scratchFile) close() error {
	name := s.file.Name()
	return errors.Join(s.file.Close(), os.Remove(name))
}

// sleepContext waits for pause, or returns the context's error if it ends first.
func sleepContext(ctx context.Context, pause time.Duration) error {
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
