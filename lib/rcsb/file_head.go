package rcsb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// CoordinateFile is the name of an entry's mmCIF coordinate file at RCSB, as archive models link
// it: https://files.rcsb.org/download/<PDB ID>.cif.
func CoordinateFile(pdbID string) string {
	return strings.ToUpper(strings.TrimSpace(pdbID)) + ".cif"
}

// FileHead is the start of an RCSB file.
type FileHead struct {
	Contents []byte
	// Truncated is true when the file is longer than the limit the head was read with.
	Truncated bool
}

// DownloadFileHead reads at most limit bytes of an RCSB file. It asks RCSB for one byte more
// than limit, which tells whether the file goes on, and stops reading there even when RCSB
// sends the whole file. Heads skip the response cache.
func (c *RemoteClient) DownloadFileHead(ctx context.Context, pdbID string, file string, limit int64) (FileHead, error) {
	if limit <= 0 {
		return FileHead{}, errors.New("RCSB file head read limit must be positive")
	}
	artifact, err := c.GetFile(ctx, pdbID, file)
	if err != nil {
		return FileHead{}, err
	}
	contents, err := retryGet(ctx, func() ([]byte, error) {
		return c.getHeadOnce(ctx, artifact.URI, limit)
	})
	if err != nil {
		return FileHead{}, fmt.Errorf("download head of RCSB file %s: %w", artifact.Filename, err)
	}
	if int64(len(contents)) > limit {
		return FileHead{Contents: contents[:limit], Truncated: true}, nil
	}
	return FileHead{Contents: contents}, nil
}

// getHeadOnce returns up to limit+1 bytes from the start of url. A 416 answer means the file is
// empty, since the requested range starts at byte 0.
func (c *RemoteClient) getHeadOnce(ctx context.Context, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Range", "bytes=0-"+strconv.FormatInt(limit, 10))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		contents, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
		closeErr := response.Body.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return contents, nil
	case http.StatusRequestedRangeNotSatisfiable:
		if err := response.Body.Close(); err != nil {
			return nil, fmt.Errorf("close response: %w", err)
		}
		return []byte{}, nil
	case http.StatusNotFound:
		if err := response.Body.Close(); err != nil {
			return nil, fmt.Errorf("%w: close response: %w", ErrNotFound, err)
		}
		return nil, ErrNotFound
	default:
		if err := response.Body.Close(); err != nil {
			return nil, fmt.Errorf("unexpected HTTP status %d and close response: %w", response.StatusCode, err)
		}
		return nil, statusError{status: response.StatusCode}
	}
}
