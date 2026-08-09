package dynamicpdbapi

import (
	"context"
	"encoding/json"
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
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	err := client.CreateEntry(context.Background(), CreateEntryRequest{Name: "5amf"})

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
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := NewClient(server.URL, "jwt-token")

	// when
	err := client.CreateModel(context.Background(), "entry-1", CreateModelRequest{Name: "model"})

	// then
	require.NoError(t, err)
}
