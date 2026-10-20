package httpapi

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_round_trip_position_when_list_cursor_is_encoded_and_decoded(t *testing.T) {
	// given
	cursor := listCursor{
		Kind:                  listCursorKindEntries,
		CreatedAt:             time.Date(2026, 10, 20, 12, 30, 45, 123456000, time.UTC),
		ID:                    uuid.New(),
		ProteinSequenceSearch: true,
	}

	// when
	encoded, err := encodeListCursor(cursor)
	require.NoError(t, err)
	decoded, err := decodeListCursor(encoded, listCursorKindEntries)

	// then
	require.NoError(t, err)
	assert.True(t, cursor.CreatedAt.Equal(decoded.CreatedAt))
	assert.Equal(t, cursor.ID, decoded.ID)
	assert.True(t, decoded.ProteinSequenceSearch)
	assert.NotContains(t, encoded, "=", "cursors are unpadded so they need no URL escaping")
}

func Test_should_reject_list_cursor_when_value_is_invalid(t *testing.T) {
	validModelsCursor := mustEncodeListCursor(t, listCursor{
		Kind: listCursorKindModels, CreatedAt: time.Now().UTC(), ID: uuid.New(),
	})
	cases := map[string]string{
		"not base64":             "%%%",
		"not json":               base64.RawURLEncoding.EncodeToString([]byte("not json")),
		"unknown field":          rawCursor(`{"k":"entries","t":"2026-10-20T00:00:00Z","i":"` + uuid.NewString() + `","x":1}`),
		"unknown kind":           rawCursor(`{"k":"artifacts","t":"2026-10-20T00:00:00Z","i":"` + uuid.NewString() + `"}`),
		"missing id":             rawCursor(`{"k":"entries","t":"2026-10-20T00:00:00Z"}`),
		"missing created_at":     rawCursor(`{"k":"entries","i":"` + uuid.NewString() + `"}`),
		"issued by another list": validModelsCursor,
		"too long":               strings.Repeat("A", maxListCursorLength+1),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			// given / when
			_, err := decodeListCursor(value, listCursorKindEntries)

			// then
			require.ErrorIs(t, err, errInvalidListCursor)
		})
	}
}

func Test_should_reject_protein_sequence_flag_when_list_cursor_is_for_models(t *testing.T) {
	// given
	value := rawCursor(`{"k":"models","t":"2026-10-20T00:00:00Z","i":"` + uuid.NewString() + `","s":true}`)

	// when
	_, err := decodeListCursor(value, listCursorKindModels)

	// then
	require.ErrorIs(t, err, errInvalidListCursor)
}

func Test_should_reject_cursor_with_offset_when_list_page_is_built(t *testing.T) {
	// given
	cursor := mustEncodeListCursor(t, listCursor{Kind: listCursorKindEntries, CreatedAt: time.Now().UTC(), ID: uuid.New()})

	// when
	_, err := listPageFromParams(nil, ptr(0), &cursor, listCursorKindEntries)

	// then
	require.ErrorIs(t, err, errInvalidListCursor)
}

func Test_should_start_at_first_page_when_list_page_cursor_is_empty(t *testing.T) {
	// given
	empty := ""

	// when
	page, err := listPageFromParams(nil, nil, &empty, listCursorKindEntries)

	// then
	require.NoError(t, err)
	assert.Nil(t, page.Cursor)
	assert.Nil(t, page.after())
	assert.Equal(t, defaultListLimit, page.Limit)
}

func Test_should_report_next_page_only_when_probe_row_was_fetched(t *testing.T) {
	// given
	page := listPage{Limit: 2}

	// when
	fullPage, fullHasNext := trimToPage([]int{1, 2, 3}, page)
	lastPage, lastHasNext := trimToPage([]int{1, 2}, page)

	// then
	assert.Equal(t, []int{1, 2}, fullPage)
	assert.True(t, fullHasNext)
	assert.Equal(t, []int{1, 2}, lastPage)
	assert.False(t, lastHasNext)
}

func Test_should_keep_filters_and_replace_offset_when_next_page_links_are_built(t *testing.T) {
	// given
	server := &Server{publicBaseURL: "https://dynamicpdb.com/api"}
	request := httptest.NewRequest("GET", "/v1/entries?query=hemoglobin&pdb_id=1ABC&pdb_id=2DEF&limit=10&offset=20", nil)
	next := listCursor{Kind: listCursorKindEntries, CreatedAt: time.Now().UTC(), ID: uuid.New()}

	// when
	links, meta, err := server.nextPageLinks(request, next)

	// then
	require.NoError(t, err)
	require.NotNil(t, links)
	require.NotNil(t, meta)
	nextURL, err := url.Parse(links.Next)
	require.NoError(t, err)
	assert.Equal(t, "https://dynamicpdb.com/api/v1/entries", nextURL.Scheme+"://"+nextURL.Host+nextURL.Path)
	query := nextURL.Query()
	assert.Equal(t, "hemoglobin", query.Get("query"))
	assert.Equal(t, []string{"1ABC", "2DEF"}, query["pdb_id"])
	assert.Equal(t, "10", query.Get("limit"))
	assert.False(t, query.Has("offset"))
	assert.Equal(t, meta.NextCursor, query.Get("cursor"))
	decoded, err := decodeListCursor(meta.NextCursor, listCursorKindEntries)
	require.NoError(t, err)
	assert.Equal(t, next.ID, decoded.ID)
}

func mustEncodeListCursor(t *testing.T, cursor listCursor) string {
	t.Helper()
	encoded, err := encodeListCursor(cursor)
	require.NoError(t, err)
	return encoded
}

func rawCursor(json string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(json))
}
