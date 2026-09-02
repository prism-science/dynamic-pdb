package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"dynamic-pdb/backend/api"
)

const SpecMediaType = "application/yaml; charset=utf-8"
const SpecPath = "/openapi.yaml"

var specETag = func() string {
	sum := sha256.Sum256(api.Spec)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}()

func (s *Server) GetOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", SpecMediaType)
	w.Header().Set("ETag", specETag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(api.Spec))
}
