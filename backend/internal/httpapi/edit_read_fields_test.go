package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_return_empty_list_when_entry_has_no_typed_polymer_entities(t *testing.T) {
	// given
	var entities []domainmodels.DepositorPolymerEntity

	// when
	response := depositorPolymerEntitiesFromModels(entities)
	encoded, err := json.Marshal(response)

	// then
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(encoded))
}

func Test_should_return_typed_values_when_entry_has_typed_polymer_entities(t *testing.T) {
	// given
	taxonomyID := 562
	entities := []domainmodels.DepositorPolymerEntity{
		{
			LabelEntityID: "1",
			Construct:     new("residues 1-164"),
			Mutations:     []string{"C54T"},
			SourceOrganisms: []domainmodels.PolymerEntityOrganism{
				{ScientificName: "Escherichia coli", NCBITaxonomyID: &taxonomyID},
			},
		},
		{LabelEntityID: "2"},
	}

	// when
	response := depositorPolymerEntitiesFromModels(entities)
	encoded, err := json.Marshal(response)

	// then
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{
			"label_entity_id": "1",
			"construct": "residues 1-164",
			"mutations": ["C54T"],
			"source_organisms": [{"scientific_name": "Escherichia coli", "ncbi_taxonomy_id": 562}]
		},
		{"label_entity_id": "2"}
	]`, string(encoded))
}

func Test_should_return_null_field_sources_when_model_detail_predates_sources(t *testing.T) {
	// given
	revision := domainmodels.ModelRevision{ID: uuid.New(), ModelID: "e_m_001", EntryID: "e"}

	// when
	response, err := modelDetailAttributesFromRevision(revision, nil)
	encoded, marshalErr := json.Marshal(response)

	// then
	require.NoError(t, err)
	require.NoError(t, marshalErr)
	var attributes map[string]any
	require.NoError(t, json.Unmarshal(encoded, &attributes))
	assert.Contains(t, attributes, "field_sources")
	assert.Nil(t, attributes["field_sources"])
}

func Test_should_return_sources_and_residue_data_when_model_detail_is_built(t *testing.T) {
	// given
	revision := domainmodels.ModelRevision{
		ID:      uuid.New(),
		ModelID: "e_m_001",
		EntryID: "e",
		Metadata: domainmodels.ModelMetadata{
			Details:     new("Refined"),
			ResidueData: []domainmodels.ResidueData{{LabelAsymID: "A", LabelSeqID: 1}},
		},
		FieldSources: domainmodels.FieldSources{
			domainmodels.ModelFieldDetails: {Source: domainmodels.FieldSourceDepositor},
		},
	}

	// when
	response, err := modelDetailAttributesFromRevision(revision, nil)

	// then
	require.NoError(t, err)
	require.NotNil(t, response.FieldSources)
	assert.Equal(t, FieldSources{"/metadata/details": {Source: FieldSourceSourceDepositor}}, *response.FieldSources)
	require.NotNil(t, response.ResidueData)
	assert.Len(t, *response.ResidueData, 1)
	assert.NotContains(t, response.Metadata, "residue_data", "residue data sits beside metadata, never inside it")
}
