package dynamicpdbapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_send_bearer_token_from_client_when_create_entry_called(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/entries", request.URL.Path)
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Accept"))
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer jwt-token", request.Header.Get("Authorization"))
		var body map[string]any
		err := json.NewDecoder(request.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"entry": map[string]any{
				"name": "5amf",
			},
			"model_operations": []any{
				map[string]any{
					"op": "add",
					"data": map[string]any{
						"model_id": "model-1",
						"name":     "model",
					},
				},
			},
		}, body)
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	err := client.CreateEntry(context.Background(), CreateEntryRequest{
		Entry: CreateEntryData{Name: "5amf"},
		ModelOperations: []AddModelOperation{
			{
				Op: "add",
				Data: AddModelData{
					ModelID: ptr("model-1"),
					Name:    "model",
				},
			},
		},
	})

	// then
	require.NoError(t, err)
}

func Test_should_send_pdb_id_filters_when_list_entries_called(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "/v1/entries", request.URL.Path)
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Accept"))
		assert.Equal(t, []string{"1YJO", "1YJP"}, request.URL.Query()["pdb_id"])
		assert.Equal(t, "Bearer jwt-token", request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", jsonAPIMediaType)
		err := json.NewEncoder(response).Encode(map[string]any{
			"data": []map[string]any{
				jsonAPIResourceData("entries", "entry-1", Entry{
					ID:   "entry-1",
					Name: "1YJO",
					Metadata: map[string]any{
						"external_refs": map[string]string{"pdb": "1YJO"},
					},
				}),
			},
		})
		require.NoError(t, err)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	entries, err := client.ListEntries(context.Background(), ListEntriesParams{PDBIDs: []string{"1YJO", "1YJP"}})

	// then
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "entry-1", entries[0].ID)
}

func Test_should_decode_file_upload_grant_when_create_file_upload_called(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/files", request.URL.Path)
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Accept"))
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer jwt-token", request.Header.Get("Authorization"))
		var body CreateFileUploadRequest
		err := json.NewDecoder(request.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "entry-1", body.EntryID)
		assert.Equal(t, "artifact-1", body.ArtifactID)
		response.Header().Set("Content-Type", jsonAPIMediaType)
		err = json.NewEncoder(response).Encode(jsonAPIResourceDocument("file_uploads", "files/key", FileUploadGrantResponse{
			Key:       "files/key",
			UploadID:  "upload-id",
			ObjectURL: "https://files.example/files/key",
			PartSize:  5,
			Parts: []FileUploadPart{
				{PartNumber: 1, URL: "https://upload.example/part-1"},
			},
		}))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	grant, err := client.CreateFileUpload(context.Background(), CreateFileUploadRequest{
		EntryID:    "entry-1",
		ArtifactID: "artifact-1",
		Filename:   "model.cif",
		Size:       5,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, "files/key", grant.Key)
	assert.Equal(t, "upload-id", grant.UploadID)
	assert.Equal(t, "https://files.example/files/key", grant.ObjectURL)
	require.Len(t, grant.Parts, 1)
	assert.Equal(t, 1, grant.Parts[0].PartNumber)
}

func Test_should_create_model_under_entry_when_create_model_called(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/entries/entry-1/models", request.URL.Path)
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Accept"))
		assert.Equal(t, jsonAPIMediaType, request.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer jwt-token", request.Header.Get("Authorization"))
		var body map[string]any
		err := json.NewDecoder(request.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"model": map[string]any{
				"name": "model",
			},
		}, body)
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	err := client.CreateModel(context.Background(), "entry-1", CreateModelRequest{Model: CreateModelData{Name: "model"}})

	// then
	require.NoError(t, err)
}

func Test_should_decode_s3_xml_error_when_upload_part_failed(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPut, request.Method)
		response.WriteHeader(http.StatusBadRequest)
		_, err := response.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>RequestTimeout</Code><Message>Your socket connection to the server was not read from or written to within the timeout period.</Message></Error>`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := NewClient("https://api.example.test", "jwt-token")

	// when
	_, err := client.PutUploadPart(context.Background(), server.URL, bytes.NewReader([]byte("MODEL\n")), int64(len("MODEL\n")))

	// then
	require.Error(t, err)
	var backendError *Error
	require.True(t, errors.As(err, &backendError))
	assert.Equal(t, http.StatusBadRequest, backendError.Status)
	assert.Equal(t, "RequestTimeout", backendError.Code)
	assert.Equal(t, "Your socket connection to the server was not read from or written to within the timeout period.", backendError.Message)
}

func ptr[T any](value T) *T {
	return &value
}

func jsonAPIResourceDocument(resourceType string, id string, attributes any) map[string]any {
	return map[string]any{
		"data": jsonAPIResourceData(resourceType, id, attributes),
	}
}

func jsonAPIResourceData(resourceType string, id string, attributes any) map[string]any {
	data := map[string]any{
		"type":       resourceType,
		"attributes": attributes,
	}
	if id != "" {
		data["id"] = id
	}
	return data
}
