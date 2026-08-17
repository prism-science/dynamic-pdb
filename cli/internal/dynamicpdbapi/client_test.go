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
		assert.Equal(t, []string{"1YJO", "1YJP"}, request.URL.Query()["pdb_id"])
		assert.Equal(t, "Bearer jwt-token", request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(response).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":   "entry-1",
					"name": "1YJO",
					"metadata": map[string]any{
						"external_refs": map[string]string{"pdb": "1YJO"},
					},
				},
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

func Test_should_create_model_under_entry_when_create_model_called(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/entries/entry-1/models", request.URL.Path)
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
