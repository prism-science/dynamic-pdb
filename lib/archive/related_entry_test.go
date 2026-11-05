package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/lib/rcsb"
)

func Test_should_map_related_entries_when_rows_use_the_dictionary_values(t *testing.T) {
	// given
	details := rcsb.EntryDetails{RelatedEntries: []rcsb.EntryRelatedEntry{
		{DBName: "PDB", DBID: "7KR0", ContentType: "re-refinement", Details: "uses the dataset of entry 7KR0"},
		{DBName: "PDB", DBID: "4PSS", ContentType: "re-refinement"},
	}}

	// when
	mapping := MapModel(details)

	// then
	require.Len(t, mapping.RelatedEntries, 2)
	assert.Equal(t, "7KR0", mapping.RelatedEntries[0].DBID)
	assert.Equal(t, new("uses the dataset of entry 7KR0"), mapping.RelatedEntries[0].Details)
	assert.Nil(t, mapping.RelatedEntries[1].Details)
	assert.Empty(t, mapping.Warnings)
}

func Test_should_drop_related_entry_with_a_warning_when_the_dictionary_does_not_allow_it(t *testing.T) {
	// given
	details := rcsb.EntryDetails{RelatedEntries: []rcsb.EntryRelatedEntry{
		{DBName: "PDB", DBID: "7KR0", ContentType: "a term wwPDB adds later"},
	}}

	// when
	mapping := MapModel(details)

	// then
	assert.Empty(t, mapping.RelatedEntries)
	require.Len(t, mapping.Warnings, 1)
	assert.Equal(t, "related_entries", mapping.Warnings[0].Field)
}
