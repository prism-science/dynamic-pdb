package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

func Test_should_create_data_sync_job_when_database_passed(t *testing.T) {
	// given
	database := &db.DB{}
	rcsbClient := rcsb.NewClient()
	siftsClient := sifts.NewClient()

	// when
	job, err := NewDataSyncJob(database, rcsbClient, siftsClient, nil)

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, database, job.database)
	assert.Equal(t, rcsbClient, job.rcsbClient)
	assert.Equal(t, siftsClient, job.siftsClient)
	assert.NotNil(t, job.logger)
}

func Test_should_reject_data_sync_job_when_database_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(nil, rcsb.NewClient(), sifts.NewClient(), nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "database is nil")
	assert.Nil(t, job)
}

func Test_should_reject_data_sync_job_when_rcsb_client_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, nil, sifts.NewClient(), nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "RCSB client is nil")
	assert.Nil(t, job)
}

func Test_should_reject_data_sync_job_when_sifts_client_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, rcsb.NewClient(), nil, nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "SIFTS client is nil")
	assert.Nil(t, job)
}

func Test_should_schedule_next_data_sync_between_seven_and_fourteen_days(t *testing.T) {
	// given
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)

	// when
	scheduledAt := nextDataSyncScheduledAt(now)

	// then
	assert.GreaterOrEqual(t, scheduledAt, now.Add(dataSyncMinimumInterval))
	assert.Less(t, scheduledAt, now.Add(dataSyncMinimumInterval+dataSyncScheduleJitter))
}

func Test_should_apply_rcsb_details_to_entry_revision_copy(t *testing.T) {
	// given
	originalTitle := "old title"
	original := models.EntryRevision{
		Title: &originalTitle,
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{models.EntrySourcePDB: "5AMF"},
		},
	}
	updated := original
	details := rcsb.EntryDetails{
		Structure:   rcsb.EntryStructure{Title: "Example structure"},
		Experiments: []rcsb.EntryExperiment{{Method: "X-RAY DIFFRACTION"}},
		Info:        rcsb.EntryInfo{CombinedResolution: []float64{1.5}},
		Symmetry:    rcsb.EntrySymmetry{SpaceGroup: "P 21 21 21"},
	}

	// when
	applyRCSBEntryDetails(&updated, details)

	// then
	require.NotNil(t, updated.Title)
	assert.Equal(t, "Example structure", *updated.Title)
	require.NotNil(t, updated.Metadata.Resolution)
	assert.Equal(t, 1.5, *updated.Metadata.Resolution)
	require.NotNil(t, updated.Metadata.Method)
	assert.Equal(t, models.StructureMethodXRayCrystallography, *updated.Metadata.Method)
	require.NotNil(t, updated.Metadata.SpaceGroup)
	assert.Equal(t, "P 21 21 21", *updated.Metadata.SpaceGroup)
	assert.Equal(t, "old title", *original.Title)
	assert.Equal(t, "5AMF", updated.Metadata.ExternalRefs[models.EntrySourcePDB])
	assert.False(t, original.HasSameData(updated))
}

func Test_should_clear_rcsb_fields_when_details_are_empty(t *testing.T) {
	// given
	title := "old title"
	resolution := 1.5
	method := models.StructureMethodCryoEM
	spaceGroup := "P 21 21 21"
	revision := models.EntryRevision{
		Title: &title,
		Metadata: models.EntryMetadata{
			Resolution: &resolution,
			Method:     &method,
			SpaceGroup: &spaceGroup,
		},
	}

	// when
	applyRCSBEntryDetails(&revision, rcsb.EntryDetails{})

	// then
	assert.Nil(t, revision.Title)
	assert.Nil(t, revision.Metadata.Resolution)
	assert.Nil(t, revision.Metadata.Method)
	assert.Nil(t, revision.Metadata.SpaceGroup)
}

func Test_should_map_supported_rcsb_polymer_entity_fields(t *testing.T) {
	// given
	release := "2026_03"
	humanTaxonomyID := 9606
	eColiTaxonomyID := 562
	details := rcsb.PolymerEntityDetails{
		Entity: rcsb.PolymerEntityData{
			Description: " Spike glycoprotein ",
			Fragment:    " receptor-binding domain ",
			Mutation:    " N501Y ",
		},
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{
			EntityID: " 1 ",
			ReferenceSequenceIdentifiers: []rcsb.ReferenceSequenceIdentifier{
				{DatabaseAccession: " P0DTC2 ", DatabaseName: "UniProt", ProvenanceSource: "SIFTS"},
				{DatabaseAccession: " Q9BYF1 ", DatabaseName: "UniProt", ProvenanceSource: "PDB"},
				{DatabaseAccession: "ignored", DatabaseName: "UniProt", ProvenanceSource: "RCSB"},
				{DatabaseAccession: "ignored", DatabaseName: "GenBank", ProvenanceSource: "PDB"},
			},
		},
		SourceOrganisms: []rcsb.SourceOrganism{
			{ScientificName: "Homo sapiens", NCBITaxonomyID: &humanTaxonomyID},
			{ScientificName: "Escherichia coli", NCBITaxonomyID: &eColiTaxonomyID},
		},
	}

	// when
	metadata := polymerEntityMetadataFromRCSB(details, "fallback", &release)

	// then
	assert.Equal(t, optionalString("1"), metadata.LabelEntityID)
	assert.Equal(t, optionalString("Spike glycoprotein"), metadata.Description)
	assert.Equal(t, optionalString("receptor-binding domain"), metadata.Construct)
	assert.Equal(t, optionalString("N501Y"), metadata.Mutations)
	assert.Equal(t, []models.PolymerEntityOrganism{
		{ScientificName: "Escherichia coli", NCBITaxonomyID: &eColiTaxonomyID},
		{ScientificName: "Homo sapiens", NCBITaxonomyID: &humanTaxonomyID},
	}, metadata.SourceOrganisms)
	assert.Equal(t, []models.PolymerEntityUniProtReference{
		{Accession: "P0DTC2", Source: models.UniProtReferenceSourceSIFTS, UniProtRelease: &release},
		{Accession: "Q9BYF1", Source: models.UniProtReferenceSourceStructRef},
	}, metadata.UniProtMappings)
}

func Test_should_match_polymer_entity_to_rcsb_fasta_record(t *testing.T) {
	// given
	expectedID := uuid.New()
	sequences := []models.ProteinSequence{
		{ID: uuid.New(), Header: "6M0J_2|Chain B", Sequence: "TTTT"},
		{ID: expectedID, Header: "6M0J_1|Chain A", Sequence: "AC DE"},
	}

	// when
	sequence, err := proteinSequenceForPolymerEntity("6m0j", "1", "acde", sequences)

	// then
	require.NoError(t, err)
	assert.Equal(t, expectedID, sequence.ID)
}

func Test_should_reject_polymer_entity_when_rcsb_sequence_changed(t *testing.T) {
	// given
	sequences := []models.ProteinSequence{
		{ID: uuid.New(), Header: "6M0J_1|Chain A", Sequence: "ACDE"},
	}

	// when
	sequence, err := proteinSequenceForPolymerEntity("6M0J", "1", "TTTT", sequences)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "does not match RCSB")
	assert.Nil(t, sequence)
}

func Test_should_detect_polymer_entity_metadata_change(t *testing.T) {
	// given
	sequenceID := uuid.New()
	entityID := "1"
	oldDescription := "old description"
	newDescription := "new description"
	stored := []models.PolymerEntity{{
		ProteinSequenceID: sequenceID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID: &entityID,
			Description:   &oldDescription,
		},
	}}
	desired := []polymerEntitySnapshot{{
		ProteinSequenceID: sequenceID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID: &entityID,
			Description:   &newDescription,
		},
	}}

	// when
	same := polymerEntitiesHaveSameData(stored, desired)

	// then
	assert.False(t, same)
}
