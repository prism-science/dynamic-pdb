package s3

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type StubServer struct {
	server *httptest.Server
	mu     sync.Mutex

	createPath       string
	completePath     string
	completeUploadID string
	completeBody     string
	abortPath        string
	abortUploadID    string

	createWithoutUploadID bool
	abortStatus           int
}

type Requests struct {
	CreatePath       string
	CompletePath     string
	CompleteUploadID string
	CompleteBody     string
	AbortPath        string
	AbortUploadID    string
}

func NewStubServer() *StubServer {
	server := &StubServer{}
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
	s.completeBody = ""
	s.abortPath = ""
	s.abortUploadID = ""
	s.createWithoutUploadID = false
	s.abortStatus = http.StatusNoContent
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
	default:
		http.NotFound(w, r)
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
