// Package modelfiles opens the model files the coordinate-reading jobs read: it finds a model
// file's bytes in our bucket (the artifact's own file, or the checksum job's copy of an archive
// file), unzips them, and tells mmCIF from PDB format.
package modelfiles

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const (
	// sniffBytes is how much of a file without a declared format is looked at to tell mmCIF from
	// PDB format.
	sniffBytes     = 4096
	readBufferSize = 64 * 1024
)

// StoredFiles is our bucket: stored files are read by key.
type StoredFiles interface {
	StoredKey(uri string) (string, bool)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// Location is where a model file's bytes may be: the artifact's own file, and the checksum job's
// copy of an external file (an archive model file on RCSB), which is in our bucket at StoredURI
// when CopyStatus is copied. CopyStatus is nil until the checksum job has read the file.
type Location struct {
	URI        *string
	CopyStatus *models.CopyStatus
	StoredURI  *string
}

// ReadFrom names the object a file was read from.
type ReadFrom string

const (
	ReadFromOwnFile      ReadFrom = "own_file"
	ReadFromChecksumCopy ReadFrom = "checksum_copy"
)

// PermanentError is a reason no retry can change: the file is not ours, or not a structure file.
type PermanentError struct {
	reason string
}

func (e *PermanentError) Error() string {
	return e.reason
}

// Permanent returns a PermanentError with a formatted reason.
func Permanent(format string, arguments ...any) error {
	return &PermanentError{reason: fmt.Sprintf(format, arguments...)}
}

// IsPermanent reports whether err, or an error it wraps, is a PermanentError.
func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

// StoredKey returns the bucket key a model file is read from: the artifact's own file when it is
// in our bucket, otherwise the checksum job's copy of the external file.
func StoredKey(files StoredFiles, location Location) (string, ReadFrom, error) {
	if location.URI == nil || strings.TrimSpace(*location.URI) == "" {
		return "", "", Permanent("artifact has no uri")
	}
	if key, stored := files.StoredKey(*location.URI); stored {
		return key, ReadFromOwnFile, nil
	}
	if location.CopyStatus == nil {
		return "", "", Permanent("the file is not in our bucket, and the checksum job has not copied it")
	}
	switch *location.CopyStatus {
	case models.CopyStatusCopied:
		if location.StoredURI == nil {
			return "", "", Permanent("the checksum job copied the file but recorded no uri for the copy")
		}
		key, stored := files.StoredKey(*location.StoredURI)
		if !stored {
			return "", "", Permanent("the checksum job's copy of the file is not in our bucket")
		}
		return key, ReadFromChecksumCopy, nil
	case models.CopyStatusOwnStorage,
		models.CopyStatusOverLimit,
		models.CopyStatusMismatch,
		models.CopyStatusCopyFailed,
		models.CopyStatusCopyOff:
		return "", "", Permanent("the file is not in our bucket, and the checksum job kept no copy of it (copy status %s)",
			*location.CopyStatus)
	default:
		return "", "", fmt.Errorf("unknown copy status %q", *location.CopyStatus)
	}
}

// OpenStructure returns the structure text of a file, gunzipped when its first bytes are a gzip
// header, and its format: the declared one, or for a file without one, what its first bytes show.
// A declared format that is not a structure format fails for good.
func OpenStructure(file io.Reader, declared *string) (io.Reader, cif.StructureFormat, error) {
	buffered := bufio.NewReaderSize(file, readBufferSize)
	body := io.Reader(buffered)
	if magic, err := buffered.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		unzipped, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, 0, Permanent("the file has a gzip header but cannot be unzipped: %v", err)
		}
		body = bufio.NewReaderSize(unzipped, readBufferSize)
	}
	if declared != nil && strings.TrimSpace(*declared) != "" {
		format, err := declaredFormat(*declared)
		if err != nil {
			return nil, 0, fmt.Errorf("declared format: %w", err)
		}
		return body, format, nil
	}
	peeker, ok := body.(*bufio.Reader)
	if !ok {
		return nil, 0, errors.New("the file cannot be looked at before it is read")
	}
	head, err := peeker.Peek(sniffBytes)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, 0, fmt.Errorf("look at the start of the file: %w", err)
	}
	format, err := sniffFormat(head)
	if err != nil {
		return nil, 0, fmt.Errorf("first bytes: %w", err)
	}
	return body, format, nil
}

// declaredFormat reads an artifact's format as the website and the CLI write it.
func declaredFormat(value string) (cif.StructureFormat, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cif", "mmcif":
		return cif.FormatMMCIF, nil
	case "pdb", "ent":
		return cif.FormatPDB, nil
	default:
		return 0, Permanent("unknown model file format %q", value)
	}
}

// pdbRecords are record names a PDB-format file starts with.
var pdbRecords = []string{"HEADER", "TITLE ", "COMPND", "REMARK", "CRYST1", "ATOM  ", "HETATM", "MODEL ", "SEQRES", "EXPDTA", "AUTHOR"}

// sniffFormat tells mmCIF (a data_ block header first) from PDB format (a known record name
// first), skipping blank lines and, in mmCIF, comment lines.
func sniffFormat(head []byte) (cif.StructureFormat, error) {
	for _, line := range bytes.Split(head, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		if len(trimmed) >= 5 && strings.EqualFold(string(trimmed[:5]), "data_") {
			return cif.FormatMMCIF, nil
		}
		for _, record := range pdbRecords {
			if bytes.HasPrefix(line, []byte(record)) {
				return cif.FormatPDB, nil
			}
		}
		return 0, Permanent("the file starts like neither mmCIF nor PDB format")
	}
	return 0, Permanent("the file is empty")
}
