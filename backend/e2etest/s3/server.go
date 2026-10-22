package s3

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

type StubServer struct {
	server *httptest.Server
	mu     sync.Mutex

	createPath       string
	completePath     string
	completeUploadID string
	ifNoneMatch      string
	completeBody     string
	abortPath        string
	abortUploadID    string

	objects map[string][]byte

	createWithoutUploadID bool
	abortStatus           int

	// readGate, when set, holds every object GET until it is closed;
	// readsStarted gets one value per GET that is waiting at the gate.
	readGate     chan struct{}
	readsStarted chan struct{}
}

type Requests struct {
	CreatePath       string
	CompletePath     string
	CompleteUploadID string
	IfNoneMatch      string
	CompleteBody     string
	AbortPath        string
	AbortUploadID    string
}

func NewStubServer() *StubServer {
	server := &StubServer{objects: map[string][]byte{}}
	server.server = httptest.NewServer(http.HandlerFunc(server.handle))
	server.Reset()
	return server
}

func (s *StubServer) URL() string {
	return s.server.URL
}

func (s *StubServer) Close() {
	s.server.Close()
}

func (s *StubServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.createPath = ""
	s.completePath = ""
	s.completeUploadID = ""
	s.ifNoneMatch = ""
	s.completeBody = ""
	s.abortPath = ""
	s.abortUploadID = ""
	s.createWithoutUploadID = false
	s.abortStatus = http.StatusNoContent
	s.readGate = nil
	s.readsStarted = nil
}

// HoldObjectReads makes object GETs wait until release is called. started
// receives one value for each GET that is waiting.
func (s *StubServer) HoldObjectReads() (started <-chan struct{}, release func()) {
	gate := make(chan struct{})
	waiting := make(chan struct{}, 16)
	s.mu.Lock()
	s.readGate = gate
	s.readsStarted = waiting
	s.mu.Unlock()
	var once sync.Once
	return waiting, func() { once.Do(func() { close(gate) }) }
}

// PutObject stores an object that ranged GETs on urlPath (for example
// "/dynamic-pdb/entry/artifacts/id/model.cif") return.
func (s *StubServer) PutObject(urlPath string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[urlPath] = data
}

func (s *StubServer) ReturnNoUploadID() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.createWithoutUploadID = true
}

func (s *StubServer) ReturnAbortStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.abortStatus = status
}

func (s *StubServer) Requests() Requests {
	s.mu.Lock()
	defer s.mu.Unlock()

	return Requests{
		CreatePath:       s.createPath,
		CompletePath:     s.completePath,
		CompleteUploadID: s.completeUploadID,
		IfNoneMatch:      s.ifNoneMatch,
		CompleteBody:     s.completeBody,
		AbortPath:        s.abortPath,
		AbortUploadID:    s.abortUploadID,
	}
}

func (s *StubServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "uploads"):
		s.handleCreateMultipartUpload(w, r)
	case r.Method == http.MethodPost && r.URL.Query().Get("uploadId") != "":
		s.handleCompleteMultipartUpload(w, r)
	case r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") != "":
		s.handleAbortMultipartUpload(w, r)
	case r.Method == http.MethodGet:
		s.handleGetObject(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *StubServer) handleGetObject(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	data, ok := s.objects[r.URL.Path]
	gate, waiting := s.readGate, s.readsStarted
	s.mu.Unlock()
	if gate != nil {
		waiting <- struct{}{}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
	}
	if !ok {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		writeXML(w, `<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
		return
	}
	end := len(data)
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if _, err := w.Write(data); err != nil {
			return
		}
		return
	}
	last, err := strconv.Atoi(strings.TrimPrefix(rangeHeader, "bytes=0-"))
	if err != nil || !strings.HasPrefix(rangeHeader, "bytes=0-") {
		http.Error(w, "unsupported range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if last+1 < end {
		end = last + 1
	}
	w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(end-1)+"/"+strconv.Itoa(len(data)))
	w.Header().Set("Content-Length", strconv.Itoa(end))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := w.Write(data[:end]); err != nil {
		return
	}
}

func (s *StubServer) handleCreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.createPath = r.URL.Path
	withoutUploadID := s.createWithoutUploadID
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/xml")
	if withoutUploadID {
		writeXML(w, `<CreateMultipartUploadResult></CreateMultipartUploadResult>`)
		return
	}
	writeXML(w, `<CreateMultipartUploadResult><UploadId>upload-id</UploadId></CreateMultipartUploadResult>`)
}

func (s *StubServer) handleCompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.completePath = r.URL.Path
	s.completeUploadID = r.URL.Query().Get("uploadId")
	s.ifNoneMatch = r.Header.Get("If-None-Match")
	s.completeBody = string(body)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/xml")
	writeXML(w, `<CompleteMultipartUploadResult></CompleteMultipartUploadResult>`)
}

func (s *StubServer) handleAbortMultipartUpload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.abortPath = r.URL.Path
	s.abortUploadID = r.URL.Query().Get("uploadId")
	status := s.abortStatus
	s.mu.Unlock()

	w.WriteHeader(status)
}

func writeXML(w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		return
	}
}
