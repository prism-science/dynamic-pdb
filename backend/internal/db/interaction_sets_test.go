package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

func createDBTestRun(t *testing.T, createdBy uuid.UUID) *models.Run {
	t.Helper()
	now := time.Now().UTC()
	run, err := testDB.Runs.Create(context.Background(), models.Run{
		ID:        uuid.New(),
		Name:      "interaction run " + uuid.NewString(),
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	return run
}

func Test_should_create_set_and_records_when_interaction_sets_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction entry revision", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction model revision", now)
	run := createDBTestRun(t, modelRevision.CreatedBy)
	seqID := 87
	distance := 2.143
	set, err := testDB.InteractionSets.Create(ctx, models.InteractionSet{
		ID:              uuid.New(),
		ModelRevisionID: modelRevision.ID,
		RunID:           run.ID,
		Source:          models.InteractionSourceMMCIFStructConn,
		CreatedAt:       now,
	})
	require.NoError(t, err)
	records := []models.InteractionRecord{
		{
			ID:                 uuid.New(),
			InteractionSetID:   set.ID,
			Ordinal:            0,
			SourceConnectionID: "metalc1",
			ConnectionType:     "metalc",
			PartnerA: models.InteractionPartner{
				LabelAsymID: "A", LabelCompID: "HIS", LabelSeqID: &seqID, LabelAtomID: "NE2",
				AuthAsymID: "A", AuthCompID: "HIS", AuthSeqID: "87",
			},
			PartnerB: models.InteractionPartner{
				LabelAsymID: "E", LabelCompID: "HEM", LabelAtomID: "FE", LabelAltID: "A",
				AuthAsymID: "A", AuthCompID: "HEM", AuthSeqID: "142", InsCode: "B",
			},
			SymmetryA:         "1_555",
			SymmetryB:         "1_555",
			DistanceAngstroms: &distance,
		},
		{
			ID:                 uuid.New(),
			InteractionSetID:   set.ID,
			Ordinal:            1,
			SourceConnectionID: "disulf1",
			ConnectionType:     "disulf",
		},
	}

	// when
	require.NoError(t, testDB.InteractionSets.AddRecords(ctx, records))
	gotSet, err := testDB.InteractionSets.Get(ctx, set.ID)
	require.NoError(t, err)
	gotRecords, err := testDB.InteractionSets.ListRecords(ctx, set.ID)
	require.NoError(t, err)

	// then
	assert.Equal(t, modelRevision.ID, gotSet.ModelRevisionID)
	assert.Equal(t, run.ID, gotSet.RunID)
	assert.Equal(t, models.InteractionSourceMMCIFStructConn, gotSet.Source)
	require.Len(t, gotRecords, 2)
	assert.Equal(t, records[0], gotRecords[0])
	assert.Equal(t, "metalc", gotRecords[0].ConnectionType)
	assert.Equal(t, "metalc1", gotRecords[0].SourceConnectionID)
	assert.Nil(t, gotRecords[1].PartnerA.LabelSeqID)
	assert.Nil(t, gotRecords[1].DistanceAngstroms)
	assert.Empty(t, gotRecords[1].PartnerA.LabelAtomID)
}

func Test_should_list_sets_by_model_revision_when_filters_given(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction list entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction list model", now)
	otherRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction other model", now)
	first, err := testDB.InteractionSets.Create(ctx, models.InteractionSet{
		ID: uuid.New(), ModelRevisionID: modelRevision.ID, RunID: createDBTestRun(t, modelRevision.CreatedBy).ID,
		Source: models.InteractionSourceMMCIFStructConn, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = testDB.InteractionSets.Create(ctx, models.InteractionSet{
		ID: uuid.New(), ModelRevisionID: otherRevision.ID, RunID: createDBTestRun(t, modelRevision.CreatedBy).ID,
		Source: models.InteractionSourceMMCIFStructConn, CreatedAt: now,
	})
	require.NoError(t, err)

	// when
	got, err := testDB.InteractionSets.List(ctx, db.InteractionSetFilters{ModelRevisionID: &modelRevision.ID})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, first.ID, got[0].ID)
}

func Test_should_return_not_found_when_interaction_set_does_not_exist(t *testing.T) {
	// when
	_, err := testDB.InteractionSets.Get(context.Background(), uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrInteractionSetNotFound)
}

func Test_should_reject_set_when_source_is_unknown(t *testing.T) {
	// given
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction bad source", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction bad source model", now)

	// when
	_, err := testDB.InteractionSets.Create(context.Background(), models.InteractionSet{
		ID: uuid.New(), ModelRevisionID: modelRevision.ID, RunID: createDBTestRun(t, modelRevision.CreatedBy).ID,
		Source: models.InteractionSource("pdb_remark"), CreatedAt: now,
	})

	// then
	require.Error(t, err)
}

func Test_should_reject_duplicate_connection_id_when_records_are_added_to_one_set(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction duplicate", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction duplicate model", now)
	set, err := testDB.InteractionSets.Create(ctx, models.InteractionSet{
		ID: uuid.New(), ModelRevisionID: modelRevision.ID, RunID: createDBTestRun(t, modelRevision.CreatedBy).ID,
		Source: models.InteractionSourceMMCIFStructConn, CreatedAt: now,
	})
	require.NoError(t, err)
	record := func(ordinal int) models.InteractionRecord {
		return models.InteractionRecord{
			ID: uuid.New(), InteractionSetID: set.ID, Ordinal: ordinal,
			SourceConnectionID: "metalc1", ConnectionType: "metalc",
		}
	}

	// when
	err = testDB.InteractionSets.AddRecords(ctx, []models.InteractionRecord{record(0), record(1)})

	// then
	require.Error(t, err)
}

func Test_should_fill_set_from_parsed_result_when_connections_exist(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction fill", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction fill model", now)
	result, err := cif.Extract(cif.Parse(readCIFFixture(t, "6OIM.cif")))
	require.NoError(t, err)

	// when
	set, err := testDB.FillInteractionSet(ctx, db.FillInteractionSetParams{
		ModelRevisionID: modelRevision.ID,
		CreatedBy:       modelRevision.CreatedBy,
		Result:          result,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.InteractionSourceMMCIFStructConn, set.Source)
	records, err := testDB.InteractionSets.ListRecords(ctx, set.ID)
	require.NoError(t, err)
	require.Len(t, records, 7)
	assert.Equal(t, "covale1", records[0].SourceConnectionID)
	assert.Equal(t, "covale", records[0].ConnectionType)
	assert.Equal(t, "CYS", records[0].PartnerA.LabelCompID)
	assert.Equal(t, "MOV", records[0].PartnerB.LabelCompID)
	assert.Equal(t, "12", records[0].PartnerA.AuthSeqID)
	assert.Equal(t, "metalc1", records[1].SourceConnectionID)
	assert.Equal(t, "metalc", records[1].ConnectionType)
	assert.Equal(t, "SER", records[1].PartnerA.LabelCompID)
	require.NotNil(t, records[1].PartnerA.LabelSeqID)
	assert.Equal(t, "MG", records[1].PartnerB.LabelCompID)
	assert.Equal(t, "17", records[1].PartnerA.AuthSeqID)
	require.NotNil(t, records[1].DistanceAngstroms)
	assert.InDelta(t, 2.164, *records[1].DistanceAngstroms, 1e-9)

	runs, err := testDB.Runs.List(ctx, db.RunFilters{ID: &set.RunID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.NotNil(t, runs[0].SoftwareName)
	assert.Equal(t, cif.ParserName, *runs[0].SoftwareName)
	require.NotNil(t, runs[0].SoftwareVersion)
	assert.Equal(t, cif.ParserVersion, *runs[0].SoftwareVersion)
}

func Test_should_roll_back_set_and_run_when_a_record_is_invalid(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "interaction rollback", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "interaction rollback model", now)
	result := cif.Result{Connections: []cif.Connection{
		{ID: "a", ConnTypeID: "metalc", Partner1: cif.Partner{LabelSeqID: "not-a-number"}},
	}}

	// when
	_, err := testDB.FillInteractionSet(ctx, db.FillInteractionSetParams{
		ModelRevisionID: modelRevision.ID,
		CreatedBy:       modelRevision.CreatedBy,
		Result:          result,
	})

	// then
	require.Error(t, err)
	sets, listErr := testDB.InteractionSets.List(ctx, db.InteractionSetFilters{ModelRevisionID: &modelRevision.ID})
	require.NoError(t, listErr)
	assert.Empty(t, sets)
}
