package s3

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeObjectStore answers the S3 calls the object reader and writer make and records them.
type fakeObjectStore struct {
	t         *testing.T
	mu        sync.Mutex
	objects   map[string]string
	requests  []string
	parts     []string
	completed string
}

func newFakeObjectStore(t *testing.T) (*fakeObjectStore, *RemoteBucket) {
	t.Helper()
	store := &fakeObjectStore{t: t, objects: map[string]string{}}
	bucket := newTestBucketForServer(t, store.handle)
	return store, bucket
}

func (s *fakeObjectStore) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/dynamic-pdb/")
	query := r.URL.Query()
	switch {
	case r.Method == http.MethodGet:
		s.requests = append(s.requests, "get "+key)
		object, found := s.objects[key]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			s.write(w, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			return
		}
		s.write(w, object)
	case r.Method == http.MethodPost && query.Has("uploads"):
		s.requests = append(s.requests, "create "+key)
		s.write(w, `<InitiateMultipartUploadResult><Bucket>dynamic-pdb</Bucket><Key>`+key+
			`</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && query.Get("uploadId") != "":
		s.requests = append(s.requests, "part "+query.Get("partNumber"))
		s.parts = append(s.parts, string(body))
		w.Header().Set("ETag", `"etag-`+query.Get("partNumber")+`"`)
	case r.Method == http.MethodPut:
		s.requests = append(s.requests, "put "+key)
		s.objects[key] = string(body)
	case r.Method == http.MethodPost && query.Get("uploadId") != "":
		s.requests = append(s.requests, "complete "+key)
		s.completed = string(body)
		s.objects[key] = strings.Join(s.parts, "")
		s.write(w, `<CompleteMultipartUploadResult><Key>`+key+`</Key></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodDelete && query.Get("uploadId") != "":
		s.requests = append(s.requests, "abort "+key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s.t.Errorf("unexpected S3 request %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (s *fakeObjectStore) write(w http.ResponseWriter, body string) {
	_, err := io.WriteString(w, body)
	assert.NoError(s.t, err)
}

func Test_should_stream_whole_object_when_object_opened(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	store.objects["entry/artifacts/a/model.cif"] = "data_model\n"

	// when
	reader, err := bucket.OpenObject(context.Background(), "entry/artifacts/a/model.cif")

	// then
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, "data_model\n", string(data))
}

func Test_should_return_not_found_error_when_opened_object_is_missing(t *testing.T) {
	// given
	_, bucket := newFakeObjectStore(t)

	// when
	_, err := bucket.OpenObject(context.Background(), "entry/artifacts/a/missing.cif")

	// then
	require.ErrorIs(t, err, ErrObjectNotFound)
}

func Test_should_put_object_in_one_request_when_stream_is_shorter_than_a_part(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	writer, err := bucket.newObjectWriter(context.Background(), "entry/artifacts/a/small.cif", 8)
	require.NoError(t, err)

	// when
	_, err = writer.Write([]byte("small"))
	require.NoError(t, err)
	err = writer.Complete()

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"put entry/artifacts/a/small.cif"}, store.requests)
	assert.Equal(t, "small", store.objects["entry/artifacts/a/small.cif"])
}

func Test_should_store_empty_object_when_stream_has_no_bytes(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	writer, err := bucket.newObjectWriter(context.Background(), "entry/artifacts/a/empty.cif", 8)
	require.NoError(t, err)

	// when
	err = writer.Complete()

	// then
	require.NoError(t, err)
	content, found := store.objects["entry/artifacts/a/empty.cif"]
	assert.True(t, found)
	assert.Empty(t, content)
}

func Test_should_send_parts_and_complete_when_stream_is_longer_than_a_part(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	writer, err := bucket.newObjectWriter(context.Background(), "entry/artifacts/a/big.cif", 4)
	require.NoError(t, err)

	// when
	_, err = writer.Write([]byte("abcdef"))
	require.NoError(t, err)
	_, err = writer.Write([]byte("ghij"))
	require.NoError(t, err)
	err = writer.Complete()

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"create entry/artifacts/a/big.cif", "part 1", "part 2", "part 3", "complete entry/artifacts/a/big.cif"}, store.requests)
	assert.Equal(t, []string{"abcd", "efgh", "ij"}, store.parts)
	assert.Contains(t, store.completed, "<PartNumber>3</PartNumber>")
	assert.Equal(t, "abcdefghij", store.objects["entry/artifacts/a/big.cif"])
}

func Test_should_abort_multipart_upload_when_writer_aborted(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	writer, err := bucket.newObjectWriter(context.Background(), "entry/artifacts/a/cut.cif", 4)
	require.NoError(t, err)
	_, err = writer.Write([]byte("abcdef"))
	require.NoError(t, err)

	// when
	err = writer.Abort()

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"create entry/artifacts/a/cut.cif", "part 1", "abort entry/artifacts/a/cut.cif"}, store.requests)
	_, found := store.objects["entry/artifacts/a/cut.cif"]
	assert.False(t, found)
	assert.Error(t, writer.Complete())
}

func Test_should_send_nothing_when_writer_aborted_before_a_full_part(t *testing.T) {
	// given
	store, bucket := newFakeObjectStore(t)
	writer, err := bucket.newObjectWriter(context.Background(), "entry/artifacts/a/tiny.cif", 8)
	require.NoError(t, err)
	_, err = writer.Write([]byte("abc"))
	require.NoError(t, err)

	// when
	err = writer.Abort()

	// then
	require.NoError(t, err)
	assert.Empty(t, store.requests)
}
