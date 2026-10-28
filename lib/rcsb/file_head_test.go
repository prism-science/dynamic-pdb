package rcsb

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_whole_file_when_rcsb_file_is_shorter_than_limit(t *testing.T) {
	// given
	var rangeHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader = r.Header.Get("Range")
		serveText(w, r, "/download/5AMF.cif", "data_5amf\n")
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 64)

	// then
	require.NoError(t, err)
	assert.Equal(t, "data_5amf\n", string(head.Contents))
	assert.False(t, head.Truncated)
	assert.Equal(t, "bytes=0-64", rangeHeader)
}

func Test_should_read_limit_bytes_and_mark_head_truncated_when_rcsb_file_is_longer(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveText(w, r, "/download/5AMF.cif", strings.Repeat("a", 100))
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 10)

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", 10), string(head.Contents))
	assert.True(t, head.Truncated)
}

func Test_should_not_mark_head_truncated_when_rcsb_file_is_exactly_the_limit(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveText(w, r, "/download/5AMF.cif", strings.Repeat("a", 10))
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 10)

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", 10), string(head.Contents))
	assert.False(t, head.Truncated)
}

func Test_should_read_only_limit_bytes_when_rcsb_ignores_range(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeText(t, w, strings.Repeat("a", 100))
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 10)

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", 10), string(head.Contents))
	assert.True(t, head.Truncated)
}

func Test_should_return_empty_head_when_rcsb_file_is_empty(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveText(w, r, "/download/5AMF.cif", "")
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 10)

	// then
	require.NoError(t, err)
	assert.Empty(t, head.Contents)
	assert.False(t, head.Truncated)
}

func Test_should_return_not_found_when_rcsb_has_no_file_for_head(t *testing.T) {
	// given
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	_, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 10)

	// then
	require.ErrorIs(t, err, ErrNotFound)
}

func Test_should_retry_head_when_rcsb_returns_transient_error(t *testing.T) {
	// given
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		serveText(w, r, "/download/5AMF.cif", "data_5amf\n")
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL))

	// when
	head, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 64)

	// then
	require.NoError(t, err)
	assert.Equal(t, "data_5amf\n", string(head.Contents))
	assert.Equal(t, 2, requests)
}

func Test_should_read_head_from_rcsb_every_time_when_response_cache_is_on(t *testing.T) {
	// given
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		serveText(w, r, "/download/5AMF.cif", "data_5amf\n")
	}))
	defer server.Close()
	client := NewClient(WithBaseURLs(server.URL, server.URL, server.URL, server.URL), WithCacheEntries(16))

	// when
	_, firstErr := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 64)
	_, secondErr := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 64)

	// then
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, 2, requests)
}

func Test_should_reject_head_when_limit_is_not_positive(t *testing.T) {
	// given
	client := NewClient()

	// when
	_, err := client.DownloadFileHead(context.Background(), "5amf", "5amf.cif", 0)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "read limit must be positive")
}

// serveText serves contents at path with Range support, as files.rcsb.org does.
func serveText(w http.ResponseWriter, r *http.Request, path string, contents string) {
	if r.URL.Path != path {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, path, time.Time{}, bytes.NewReader([]byte(contents)))
}

func Test_should_name_coordinate_file_like_archive_models_link_it_when_pdb_id_is_lower_case(t *testing.T) {
	// when
	name := CoordinateFile(" 1abc ")

	// then
	assert.Equal(t, "1ABC.cif", name)
}
