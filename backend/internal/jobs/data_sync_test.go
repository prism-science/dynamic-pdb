package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

func Test_should_create_data_sync_job_when_database_passed(t *testing.T) {
	// given
	database := &db.DB{}
	rcsbClient := rcsb.NewClient()

	// when
	job, err := NewDataSyncJob(database, rcsbClient, nil)

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, database, job.database)
	assert.Equal(t, rcsbClient, job.rcsbClient)
	assert.NotNil(t, job.logger)
}

func Test_should_reject_data_sync_job_when_database_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(nil, rcsb.NewClient(), nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "database is nil")
	assert.Nil(t, job)
}

func Test_should_reject_data_sync_job_when_rcsb_client_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, nil, nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "RCSB client is nil")
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
	originalDescription := "old description"
	original := models.EntryRevision{
		Name:        "5AMF",
		Description: &originalDescription,
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{models.EntrySourcePDB: "5AMF"},
		},
	}
	updated := original
	organism := "Homo sapiens; Escherichia coli"
	details := rcsb.EntryDetails{
		Structure:   rcsb.EntryStructure{Title: "Example structure"},
		Experiments: []rcsb.EntryExperiment{{Method: "X-RAY DIFFRACTION"}},
		Info:        rcsb.EntryInfo{CombinedResolution: []float64{1.5}},
		Symmetry:    rcsb.EntrySymmetry{SpaceGroup: "P 21 21 21"},
	}

	// when
	applyRCSBEntryDetails(&updated, details, &organism)

	// then
	require.NotNil(t, updated.Description)
	assert.Equal(t, "Example structure", *updated.Description)
	require.NotNil(t, updated.Metadata.Resolution)
	assert.Equal(t, 1.5, *updated.Metadata.Resolution)
	require.NotNil(t, updated.Metadata.Method)
	assert.Equal(t, models.StructureMethodXRayCrystallography, *updated.Metadata.Method)
	require.NotNil(t, updated.Metadata.SpaceGroup)
	assert.Equal(t, "P 21 21 21", *updated.Metadata.SpaceGroup)
	assert.Equal(t, &organism, updated.Metadata.Organism)
	assert.Equal(t, "old description", *original.Description)
	assert.Equal(t, "5AMF", updated.Metadata.ExternalRefs[models.EntrySourcePDB])
	assert.False(t, original.HasSameData(updated))
}

func Test_should_clear_rcsb_fields_when_details_are_empty(t *testing.T) {
	// given
	description := "old description"
	resolution := 1.5
	organism := "Homo sapiens"
	method := models.StructureMethodCryoEM
	spaceGroup := "P 21 21 21"
	revision := models.EntryRevision{
		Description: &description,
		Metadata: models.EntryMetadata{
			Resolution: &resolution,
			Organism:   &organism,
			Method:     &method,
			SpaceGroup: &spaceGroup,
		},
	}

	// when
	applyRCSBEntryDetails(&revision, rcsb.EntryDetails{}, nil)

	// then
	assert.Nil(t, revision.Description)
	assert.Nil(t, revision.Metadata.Resolution)
	assert.Nil(t, revision.Metadata.Organism)
	assert.Nil(t, revision.Metadata.Method)
	assert.Nil(t, revision.Metadata.SpaceGroup)
}
