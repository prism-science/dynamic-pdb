package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
)

func Test_should_search_the_pdb_id_values_and_a_one_word_query_when_matched_models_are_found(t *testing.T) {
	cases := []struct {
		name    string
		filters db.EntryRevisionFilters
		want    []string
	}{
		{name: "nothing searched", filters: db.EntryRevisionFilters{}, want: nil},
		{name: "pdb_id values", filters: db.EntryRevisionFilters{EntryOrModelPDBIDs: []string{"7KR0", "44ps"}}, want: []string{"7KR0", "44ps"}},
		{name: "a one-word query", filters: db.EntryRevisionFilters{Query: "44PS"}, want: []string{"44PS"}},
		{name: "a query of several words is no PDB ID", filters: db.EntryRevisionFilters{Query: "44PS kinase"}, want: nil},
		{name: "both", filters: db.EntryRevisionFilters{EntryOrModelPDBIDs: []string{"7KR0"}, Query: "44PS"}, want: []string{"7KR0", "44PS"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := searchedModelPDBIDs(tc.filters)

			// then
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_should_not_write_into_the_filters_when_searched_pdb_ids_are_read(t *testing.T) {
	// given: spare capacity, which a plain append of the query would write into
	pdbIDs := make([]string, 1, 4)
	pdbIDs[0] = "7KR0"
	filters := db.EntryRevisionFilters{EntryOrModelPDBIDs: pdbIDs, Query: "44PS"}

	// when
	got := searchedModelPDBIDs(filters)

	// then
	assert.Equal(t, []string{"7KR0", "44PS"}, got)
	assert.Empty(t, pdbIDs[:2][1])
}

func Test_should_copy_every_entry_field_when_entry_list_attributes_are_built(t *testing.T) {
	// given
	info := EntryInfoAttributes{
		Id: "dpdb_abcd1234", CreatedBy: uuid.New(), Title: ptr("entry"), RevisionNumber: ptr(2),
		ThumbnailImageUrl: ptr("https://example.com/t.png"), Ligands: &[]string{"ADP"},
		ExternalRefs: &map[string]string{"pdb": "7KR0"},
		CreatedAt:    time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC),
	}

	// when
	attributes, err := entryListAttributesFromInfo(info)

	// then
	require.NoError(t, err)
	assert.Equal(t, info.Id, attributes.Id)
	assert.Equal(t, info.CreatedBy, attributes.CreatedBy)
	assert.Equal(t, info.Title, attributes.Title)
	assert.Equal(t, info.RevisionNumber, attributes.RevisionNumber)
	assert.Equal(t, info.ThumbnailImageUrl, attributes.ThumbnailImageUrl)
	assert.Equal(t, info.Ligands, attributes.Ligands)
	assert.Equal(t, info.ExternalRefs, attributes.ExternalRefs)
	assert.Equal(t, info.CreatedAt, attributes.CreatedAt)
	assert.Equal(t, info.UpdatedAt, attributes.UpdatedAt)
	assert.Nil(t, attributes.MatchedModels)
}
