package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

const (
	testSelectionMaxModels       = 1000
	testSelectionMaxStoredModels = 100000
	testSelectionTimeout         = 10 * time.Second
)

// freezeRequest is a selection freeze with the test limits.
func freezeRequest(modelIDs []string, searches []db.ModelSearch, excluded []string) db.SelectionFreeze {
	return db.SelectionFreeze{
		ID:               uuid.New(),
		ModelIDs:         modelIDs,
		Searches:         searches,
		ExcludeModelIDs:  excluded,
		Request:          models.SelectionRequest{ModelIDs: modelIDs, Searches: []string{}, ExcludeModelIDs: excluded},
		MaxModels:        testSelectionMaxModels,
		MaxStoredModels:  testSelectionMaxStoredModels,
		StatementTimeout: testSelectionTimeout,
	}
}

// frozenModel is one row of a frozen selection, as a test compares it.
type frozenModel struct {
	modelID             string
	modelRevisionID     uuid.UUID
	modelRevisionNumber int
	entryRevisionID     uuid.UUID
	entryRevisionNumber int
}

func (f *modelSearchTestDB) frozenModels(t *testing.T, selectionID uuid.UUID) []frozenModel {
	t.Helper()
	items, err := f.database.Selections.ListModels(context.Background(), selectionID, 0, 100)
	require.NoError(t, err)
	frozen := make([]frozenModel, 0, len(items))
	for index, item := range items {
		require.Equal(t, index+1, item.Ordinal)
		frozen = append(frozen, frozenModel{
			modelID:             item.ModelID,
			modelRevisionID:     item.ModelRevisionID,
			modelRevisionNumber: item.ModelRevisionNumber,
			entryRevisionID:     item.EntryRevisionID,
			entryRevisionNumber: item.EntryRevisionNumber,
		})
	}
	return frozen
}

func frozenModelIDs(frozen []frozenModel) []string {
	ids := make([]string, 0, len(frozen))
	for _, model := range frozen {
		ids = append(ids, model.modelID)
	}
	return ids
}

func (f *modelSearchTestDB) storedSelectionRows(t *testing.T) (selections int, rows int) {
	t.Helper()
	require.NoError(t, f.sql.Get(&selections, `select count(*) from selections`))
	require.NoError(t, f.sql.Get(&rows, `select count(*) from selection_models`))
	return selections, rows
}

func Test_should_freeze_named_models_and_search_matches_once_each_minus_excluded_when_a_selection_is_saved(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entryA := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	entryB := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	ensembleA := fixture.model(t, entryA, map[string]any{"model_type": "Ensemble"})
	multiconformerA := fixture.model(t, entryA, map[string]any{"model_type": "Multiconformer"})
	ensembleB := fixture.model(t, entryB, map[string]any{"model_type": "Ensemble"})
	fixture.model(t, entryB, map[string]any{"model_type": "Deposited"})
	request := freezeRequest(
		// ensembleA is named and matches the search: it is frozen once.
		[]string{multiconformerA.id, ensembleA.id},
		[]db.ModelSearch{{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}}},
		[]string{ensembleB.id, "dpdb_never_existed_m_001"},
	)

	// when
	selection, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then
	require.NoError(t, err)
	assert.Equal(t, 2, selection.ModelCount)
	assert.Equal(t, 1, selection.EntryCount)
	assert.Equal(t, []frozenModel{
		{modelID: ensembleA.id, modelRevisionID: ensembleA.revisionID, modelRevisionNumber: 1,
			entryRevisionID: entryA.revisionID, entryRevisionNumber: 2},
		{modelID: multiconformerA.id, modelRevisionID: multiconformerA.revisionID, modelRevisionNumber: 1,
			entryRevisionID: entryA.revisionID, entryRevisionNumber: 2},
	}, fixture.frozenModels(t, selection.ID))
	stored, err := fixture.database.Selections.Get(context.Background(), selection.ID)
	require.NoError(t, err)
	assert.Equal(t, selection.ModelCount, stored.ModelCount)
	assert.Equal(t, selection.EntryCount, stored.EntryCount)
	assert.Equal(t, request.Request, stored.Request)
	assert.True(t, selection.CreatedAt.Equal(stored.CreatedAt), "the saved and the read selection show the same time")
	// The save time is the freeze statement's start on the database clock, not the server's.
	assert.WithinDuration(t, time.Now(), stored.CreatedAt, time.Minute)
}

func Test_should_freeze_the_union_of_several_searches_in_list_order_when_a_selection_is_saved(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	ensemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	deposited := fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	request := freezeRequest(nil, []db.ModelSearch{
		{ModelTypes: []models.StructureModelType{models.StructureModelTypeDeposited}},
		{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}},
	}, nil)

	// when
	selection, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{ensemble.id, deposited.id}, frozenModelIDs(fixture.frozenModels(t, selection.ID)))
}

func Test_should_freeze_what_the_model_search_lists_when_a_selection_is_saved_from_a_search(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	highResolution := fixture.entry(t, map[string]any{"method": "X-ray crystallography", "resolution": 1.5},
		map[string]any{"source_organisms": []map[string]any{{"scientific_name": "Homo sapiens", "ncbi_taxonomy_id": 9606}}})
	lowResolution := fixture.entry(t, map[string]any{"method": "X-ray crystallography", "resolution": 3.1},
		map[string]any{"source_organisms": []map[string]any{{"scientific_name": "Homo sapiens", "ncbi_taxonomy_id": 9606}}})
	matching := fixture.model(t, highResolution, map[string]any{"model_type": "Ensemble"})
	fixture.metric(t, matching, models.MetricKeyRFree, 0.18)
	tooHighRFree := fixture.model(t, highResolution, map[string]any{"model_type": "Ensemble"})
	fixture.metric(t, tooHighRFree, models.MetricKeyRFree, 0.31)
	lowResolutionModel := fixture.model(t, lowResolution, map[string]any{"model_type": "Ensemble"})
	fixture.metric(t, lowResolutionModel, models.MetricKeyRFree, 0.18)
	search := db.ModelSearch{
		Organisms:     []string{"homo sapiens"},
		ResolutionMax: ptr(2.0),
		ModelTypes:    []models.StructureModelType{models.StructureModelTypeEnsemble},
		MetricBounds:  []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(0.2)}},
	}
	listed := fixture.search(t, search)

	// when
	selection, err := fixture.database.Selections.Freeze(context.Background(), freezeRequest(nil, []db.ModelSearch{search}, nil))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{matching.id}, listed)
	assert.Equal(t, listed, frozenModelIDs(fixture.frozenModels(t, selection.ID)))
}

func Test_should_freeze_every_listed_model_when_a_selection_is_saved_from_a_search_without_filters(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	deletedEntry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.model(t, deletedEntry, map[string]any{"model_type": "Ensemble"})
	fixture.exec(t, `update entries set state = 'deleted' where id = $1`, deletedEntry.id)
	listed := fixture.search(t, db.ModelSearch{})

	// when
	selection, err := fixture.database.Selections.Freeze(context.Background(), freezeRequest(nil, []db.ModelSearch{{}}, nil))

	// then
	require.NoError(t, err)
	assert.Len(t, listed, 2)
	assert.Equal(t, listed, frozenModelIDs(fixture.frozenModels(t, selection.ID)))
}

func Test_should_keep_its_frozen_revisions_when_the_catalog_changes_after_a_selection_is_saved(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entryA := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	entryB := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	revisedModel := fixture.model(t, entryA, map[string]any{"model_type": "Ensemble"})
	withdrawnModel := fixture.model(t, entryB, map[string]any{"model_type": "Ensemble"})
	ensembles := []db.ModelSearch{{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}}}
	saved, err := fixture.database.Selections.Freeze(context.Background(), freezeRequest(nil, ensembles, nil))
	require.NoError(t, err)
	before := fixture.frozenModels(t, saved.ID)

	// when the catalog changes: the model gets a new revision, its entry gets a new revision, a
	// new model starts matching the search, and a model is withdrawn
	newModelRevisionID := uuid.New()
	fixture.exec(t, `update model_revisions set state = 'archived' where id = $1`, revisedModel.revisionID)
	fixture.exec(t, `insert into model_revisions (id, model_id, primary_artifact_id, revision_number, state, model_state,
	                                               title, metadata, created_by, created_at, updated_at, published_at)
	                 values ($1, $2, $3, 2, 'active', 'active', 'revised', '{"model_type": "Ensemble"}', $4, $5, $5, $5)`,
		newModelRevisionID, revisedModel.id, revisedModel.coordinateFile, fixture.userID, fixture.tick())
	newEntryRevisionID := uuid.New()
	fixture.exec(t, `update entry_revisions set state = 'archived' where id = $1`, entryA.revisionID)
	fixture.entryRevision(t, entryA, newEntryRevisionID, 3, "active", map[string]any{"method": "X-ray crystallography"})
	newModel := fixture.model(t, entryA, map[string]any{"model_type": "Ensemble"})
	fixture.exec(t, `update models set state = 'deleted' where id = $1`, withdrawnModel.id)
	resaved, err := fixture.database.Selections.Freeze(context.Background(), freezeRequest(nil, ensembles, nil))
	require.NoError(t, err)

	// then the saved selection still names the revisions that were active when it was saved
	assert.Equal(t, []frozenModel{
		{modelID: revisedModel.id, modelRevisionID: revisedModel.revisionID, modelRevisionNumber: 1,
			entryRevisionID: entryA.revisionID, entryRevisionNumber: 2},
		{modelID: withdrawnModel.id, modelRevisionID: withdrawnModel.revisionID, modelRevisionNumber: 1,
			entryRevisionID: entryB.revisionID, entryRevisionNumber: 2},
	}, before)
	assert.Equal(t, before, fixture.frozenModels(t, saved.ID))
	stored, err := fixture.database.Selections.Get(context.Background(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.ModelCount)
	assert.Equal(t, 2, stored.EntryCount)
	// and a new save takes the catalog as it is now
	assert.Equal(t, []frozenModel{
		{modelID: revisedModel.id, modelRevisionID: newModelRevisionID, modelRevisionNumber: 2,
			entryRevisionID: newEntryRevisionID, entryRevisionNumber: 3},
		{modelID: newModel.id, modelRevisionID: newModel.revisionID, modelRevisionNumber: 1,
			entryRevisionID: newEntryRevisionID, entryRevisionNumber: 3},
	}, fixture.frozenModels(t, resaved.ID))
}

func Test_should_save_nothing_when_a_selection_would_be_larger_than_its_cap(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	for range 3 {
		fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	}
	request := freezeRequest(nil, []db.ModelSearch{{}}, nil)
	request.MaxModels = 2

	// when
	_, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then
	require.ErrorIs(t, err, db.ErrSelectionTooLarge)
	selections, rows := fixture.storedSelectionRows(t)
	assert.Zero(t, selections)
	assert.Zero(t, rows)
}

func Test_should_save_nothing_when_stored_selections_would_pass_their_budget(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	first := freezeRequest(nil, []db.ModelSearch{{}}, nil)
	first.MaxStoredModels = 3
	_, err := fixture.database.Selections.Freeze(context.Background(), first)
	require.NoError(t, err)
	second := freezeRequest(nil, []db.ModelSearch{{}}, nil)
	second.MaxStoredModels = 3

	// when
	_, err = fixture.database.Selections.Freeze(context.Background(), second)

	// then
	require.ErrorIs(t, err, db.ErrSelectionsFull)
	selections, rows := fixture.storedSelectionRows(t)
	assert.Equal(t, 1, selections)
	assert.Equal(t, 2, rows)
}

func Test_should_charge_the_stored_request_to_the_budget_when_a_selection_is_saved(t *testing.T) {
	// given one model and a request that names many excluded models
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	only := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	excluded := make([]string, 300)
	for index := range excluded {
		excluded[index] = fmt.Sprintf("dpdb_never_existed_m_%03d", index)
	}
	request := freezeRequest([]string{only.id}, nil, excluded)
	request.MaxStoredModels = 50

	// when
	_, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then the request's ~8 kB count as about 70 rows, past the budget of 50
	require.ErrorIs(t, err, db.ErrSelectionsFull)
	selections, _ := fixture.storedSelectionRows(t)
	assert.Zero(t, selections)
}

func Test_should_name_requested_models_that_cannot_be_selected_and_save_nothing_when_a_selection_is_saved(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	active := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	deleted := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.exec(t, `update models set state = 'deleted' where id = $1`, deleted.id)
	request := freezeRequest([]string{"dpdb_never_existed_m_001", active.id, deleted.id}, nil, nil)

	// when
	_, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then
	var unknown *db.UnknownSelectionModelsError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, []string{"dpdb_never_existed_m_001", deleted.id}, unknown.ModelIDs)
	selections, rows := fixture.storedSelectionRows(t)
	assert.Zero(t, selections)
	assert.Zero(t, rows)
}

func Test_should_save_nothing_when_a_selection_matches_no_model(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	only := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	request := freezeRequest([]string{only.id}, nil, []string{only.id})

	// when
	_, err := fixture.database.Selections.Freeze(context.Background(), request)

	// then
	require.ErrorIs(t, err, db.ErrSelectionEmpty)
	selections, _ := fixture.storedSelectionRows(t)
	assert.Zero(t, selections)
}

func Test_should_stop_with_a_timeout_error_when_a_selection_freeze_runs_past_its_statement_timeout(t *testing.T) {
	// given a lock that keeps the freeze from reading the stored selections
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	lock, err := fixture.sql.Beginx()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, lock.Rollback()) })
	_, err = lock.Exec(`lock table selections in access exclusive mode`)
	require.NoError(t, err)
	request := freezeRequest(nil, []db.ModelSearch{{}}, nil)
	request.StatementTimeout = 200 * time.Millisecond

	// when
	_, err = fixture.database.Selections.Freeze(context.Background(), request)

	// then
	require.ErrorIs(t, err, db.ErrSelectionFreezeTimeout)
}

func Test_should_stop_with_a_timeout_error_when_a_selection_freeze_waits_too_long_for_a_connection(t *testing.T) {
	// given a pool of one connection, held by another transaction
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	request := freezeRequest(nil, []db.ModelSearch{{}}, nil)
	request.StatementTimeout = 100 * time.Millisecond
	var freezeErr error

	// when
	err := fixture.database.Do(context.Background(), func(context.Context) error {
		_, freezeErr = fixture.database.Selections.Freeze(context.Background(), request)
		return nil
	})

	// then
	require.NoError(t, err)
	require.ErrorIs(t, freezeErr, db.ErrSelectionFreezeTimeout)
	selections, _ := fixture.storedSelectionRows(t)
	assert.Zero(t, selections)
}

func Test_should_page_frozen_models_by_ordinal_when_selection_models_are_listed(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	first := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	second := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	third := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	selection, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{third.id, first.id, second.id}, nil, nil))
	require.NoError(t, err)

	// when
	page, err := fixture.database.Selections.ListModels(context.Background(), selection.ID, 1, 1)

	// then
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, 2, page[0].Ordinal)
	assert.Equal(t, second.id, page[0].ModelID)
	assert.Equal(t, entry.id, page[0].EntryID)
}

func Test_should_return_not_found_when_an_unknown_selection_is_read(t *testing.T) {
	// when
	_, err := testDB.Selections.Get(context.Background(), uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrSelectionNotFound)
}
