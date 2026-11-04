package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"dynamic-pdb/backend/internal/validation"
)

// fieldsWithOwnKeyRule are request fields whose keys a rule of their own checks, so
// requestKeyErrors does not enter them and every key gets one error:
//   - metadata: model, artifact and run metadata are checked against their domain shapes
//     (decodeModelMetadataFields, artifactRequestErrors, runRequestErrors).
//   - external_refs: each key must name a known source (validation.EntryMetadata).
var fieldsWithOwnKeyRule = []string{"metadata", "external_refs"}

// requestKeyErrors reports the keys of a write request body that its request type does not
// define, and keys starting with "__", at every depth. request is the zero value of the
// generated request type, for example CreateEntryRequest{}. Pointers address the body.
func requestKeyErrors(raw any, request any) []validation.FieldError {
	return validation.RequestKeys(validation.Root, raw, request, fieldsWithOwnKeyRule...)
}

// decodeRequestBody reads a JSON request body once and decodes it into request. It also
// returns the body's bytes and the body as plain JSON values: decoding into the request type
// drops keys the type does not define, so requestKeyErrors walks the plain values.
func decodeRequestBody(r *http.Request, request any) ([]byte, any, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read request body: %w", err)
	}
	if err := json.Unmarshal(data, request); err != nil {
		return nil, nil, fmt.Errorf("decode request body: %w", err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode request body keys: %w", err)
	}
	return data, raw, nil
}
