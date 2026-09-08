package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_default_artifact_type_to_other_when_type_is_missing(t *testing.T) {
	// given
	// when
	artifactType, err := artifactTypeFromRequest(nil)

	// then
	require.NoError(t, err)
	assert.Equal(t, domainmodels.ArtifactTypeOther, artifactType)
}

func Test_should_reject_duplicate_structure_factors_when_model_artifacts_are_validated(t *testing.T) {
	// given
	artifactType := ArtifactTypeStructureFactors
	requests := []CreateArtifactRequest{
		{Id: uuid.New(), Type: &artifactType},
		{Id: uuid.New(), Type: &artifactType},
	}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeModel)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_assembly_artifact_type(t *testing.T) {
	// given
	artifactType := ArtifactType("assembly")

	// when
	_, err := artifactTypeFromRequest(&artifactType)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_model_artifact_type_when_entry_artifacts_are_validated(t *testing.T) {
	// given
	artifactType := ArtifactTypeModel
	requests := []CreateArtifactRequest{{Id: uuid.New(), Type: &artifactType}}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeEntry)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_fasta_type_when_artifact_format_is_not_fasta(t *testing.T) {
	// given
	artifactType := ArtifactTypeFASTA
	format := "cif"
	requests := []CreateArtifactRequest{
		{Id: uuid.New(), Type: &artifactType, Format: &format},
	}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeEntry)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_apply_default_limit_when_list_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := entryFiltersFromParams(ListEntriesParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_keep_explicit_limit_when_list_entries_limit_is_provided(t *testing.T) {
	// given
	limit := 7

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, limit, *got.Limit)
}

func Test_should_cap_limit_when_list_entries_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, maxListLimit, *got.Limit)
}

func Test_should_truncate_search_query_when_list_entries_query_is_too_long(t *testing.T) {
	// given
	query := strings.Repeat("Ж", maxEntrySearchQueryLength+1)

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Query: &query})

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("Ж", maxEntrySearchQueryLength), got.Query)
}

func Test_should_bound_pdb_filters_when_list_entries_has_too_many_long_values(t *testing.T) {
	// given
	pdbIDs := make([]string, maxListFilterValues+1)
	for index := range pdbIDs {
		pdbIDs[index] = strings.Repeat("A", maxEntryPDBIDLength+1)
	}

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{PdbId: &pdbIDs})

	// then
	require.NoError(t, err)
	require.Len(t, got.PDBIDs, maxListFilterValues)
	assert.Equal(t, strings.Repeat("A", maxEntryPDBIDLength), got.PDBIDs[0])
}

func Test_should_bound_levels_when_list_artifacts_has_too_many_values(t *testing.T) {
	// given
	levels := make([]ArtifactLevel, maxListFilterValues+1)
	for index := range levels {
		levels[index] = L0
	}

	// when
	got, err := artifactFiltersFromParams(ListArtifactsParams{Levels: &levels})

	// then
	require.NoError(t, err)
	require.Len(t, got.Levels, maxListFilterValues)
}

func Test_should_cap_limit_when_revision_pagination_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	err := validatePagination(&limit, nil)

	// then
	require.NoError(t, err)
	assert.Equal(t, maxListLimit, *limitOrDefault(&limit))
}

func Test_should_apply_default_limit_when_list_models_across_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_reject_negative_offset_when_list_models_across_entries_offset_is_provided(t *testing.T) {
	// given
	offset := -1

	// when
	_, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{Offset: &offset})

	// then
	require.Error(t, err)
}

func Test_should_apply_default_limit_when_list_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := artifactFiltersFromParams(ListArtifactsParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_apply_default_limit_when_list_model_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := modelArtifactFiltersFromParams(ListModelArtifactsParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_reject_state_field_when_entry_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, entryRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_apply_flat_properties_when_entry_revision_is_created(t *testing.T) {
	// given
	details := "crystal structure"
	resolution := 1.8
	method := "X-ray crystallography"
	spaceGroup := "P 21 21 21"
	ph := 7.4
	crystals := []EntryCrystal{{
		Id: "1",
		Growth: &EntryCrystalGrowth{
			Ph: &ph,
		},
	}}
	change := EntryRevisionChange{
		ExternalRefs:    &map[string]string{"pdb": "1ABC"},
		Details:         &details,
		Resolution:      &resolution,
		Method:          &method,
		SpaceGroup:      &spaceGroup,
		Crystallography: &EntryCrystallography{Crystals: &crystals},
	}
	fields := map[string]json.RawMessage{
		"external_refs":   json.RawMessage(`{"pdb":"1ABC"}`),
		"details":         json.RawMessage(`"crystal structure"`),
		"resolution":      json.RawMessage(`1.8`),
		"method":          json.RawMessage(`"X-ray crystallography"`),
		"space_group":     json.RawMessage(`"P 21 21 21"`),
		"crystallography": json.RawMessage(`{"crystals":[{"id":"1","growth":{"ph":7.4}}]}`),
	}
	revision := domainmodels.EntryRevision{}

	// when
	err := applyEntryRevisionChange(&revision, change, fields)

	// then
	require.NoError(t, err)
	assert.Equal(t, "1ABC", revision.Metadata.ExternalRefs[domainmodels.EntrySourcePDB])
	require.NotNil(t, revision.Metadata.Details)
	assert.Equal(t, details, *revision.Metadata.Details)
	require.NotNil(t, revision.Metadata.Resolution)
	assert.Equal(t, resolution, *revision.Metadata.Resolution)
	require.NotNil(t, revision.Metadata.Method)
	assert.Equal(t, domainmodels.StructureMethodXRayCrystallography, *revision.Metadata.Method)
	require.NotNil(t, revision.Metadata.SpaceGroup)
	assert.Equal(t, spaceGroup, *revision.Metadata.SpaceGroup)
	require.NotNil(t, revision.Metadata.Crystallography)
	require.Len(t, revision.Metadata.Crystallography.Crystals, 1)
	assert.Equal(t, "1", revision.Metadata.Crystallography.Crystals[0].ID)
}

func Test_should_reject_metadata_field_when_entry_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"metadata": json.RawMessage(`{}`)}

	// when
	err := validateRevisionFields(fields, entryRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_state_field_when_model_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, modelRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}
