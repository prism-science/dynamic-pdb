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
)

func Test_should_return_model_revision_with_metadata_when_models_create_and_get_called(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "model-entry", time.Now().UTC())
	now := time.Now().UTC()
	title := "Refined coordinate model with electron-density support " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	affiliation := "Department of Chemistry, Boston University"
	details := "Multiconformer refinement"
	purpose := models.ModelPurposeRefinement
	modelType := models.StructureModelTypeMulticonformer
	altLocFraction := 0.31
	unmodeledFraction := 0.04
	idempotencyKey := uuid.NewString()
	revision := models.ModelRevision{
		ID:                uuid.New(),
		ModelID:           "model-primary",
		State:             models.RevisionStatePending,
		Title:             &title,
		ThumbnailImageURL: &thumbnailImageURL,
		Metadata: models.ModelMetadata{
			ExternalRefs: map[models.ModelSource]string{
				models.ModelSourcePDB: "5AMF",
			},
			Details:           &details,
			Authors:           []string{"Hendrickson, W.A.", "Teeter, M.M."},
			Affiliation:       &affiliation,
			Purpose:           &purpose,
			ModelType:         &modelType,
			AltLocFraction:    &altLocFraction,
			UnmodeledFraction: &unmodeledFraction,
			Ligands:           []string{"ATP"},
			Cofactors:         []string{"HEM"},
			ResidueData: []models.ResidueData{{
				LabelAsymID: "A", LabelSeqID: 52, LabelCompID: "ILE", AuthAsymID: ptr("X"),
				AuthSeqID: ptr(52), UniProtPosition: ptr("P69441:52"), RSCC: ptr(0.97),
			}},
		},
		IdempotencyKey: &idempotencyKey,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// when
	created, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, revision)
	require.NoError(t, err)
	got, err := testDB.Models.Get(context.Background(), db.ModelRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, revision.ID, got.ID)
	assert.Equal(t, entryRevision.EntryID, got.EntryID)
	assert.Equal(t, revision.ModelID, got.ModelID)
	assert.Equal(t, models.ModelStateActive, got.ModelState)
	assert.Equal(t, revision.Metadata.ExternalRefs, got.Metadata.ExternalRefs)
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, revision.Title, got.Title)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, []string{"Hendrickson, W.A.", "Teeter, M.M."}, got.Metadata.Authors)
	require.NotNil(t, got.Metadata.Details)
	assert.Equal(t, details, *got.Metadata.Details)
	require.NotNil(t, got.Metadata.Affiliation)
	assert.Equal(t, affiliation, *got.Metadata.Affiliation)
	require.NotNil(t, got.Metadata.Purpose)
	assert.Equal(t, purpose, *got.Metadata.Purpose)
	require.NotNil(t, got.Metadata.ModelType)
	assert.Equal(t, modelType, *got.Metadata.ModelType)
	require.NotNil(t, got.Metadata.AltLocFraction)
	assert.Equal(t, altLocFraction, *got.Metadata.AltLocFraction)
	require.NotNil(t, got.Metadata.UnmodeledFraction)
	assert.Equal(t, unmodeledFraction, *got.Metadata.UnmodeledFraction)
	assert.Equal(t, []string{"ATP"}, got.Metadata.Ligands)
	assert.Equal(t, []string{"HEM"}, got.Metadata.Cofactors)
	assert.Equal(t, revision.Metadata.ResidueData, got.Metadata.ResidueData)
	require.NotNil(t, got.IdempotencyKey)
	assert.Equal(t, idempotencyKey, *got.IdempotencyKey)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
	newModelState := models.ModelStateNew
	_, err = testDB.Models.Get(context.Background(), db.ModelRevisionFilters{
		ID:         &created.ID,
		ModelState: &newModelState,
	})
	require.NoError(t, err)
	activeModelState := models.ModelStateActive
	_, err = testDB.Models.Get(context.Background(), db.ModelRevisionFilters{
		ID:         &created.ID,
		ModelState: &activeModelState,
	})
	require.ErrorIs(t, err, db.ErrModelRevisionNotFound)
}

func Test_should_return_not_found_when_models_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Models.Get(context.Background(), db.ModelRevisionFilters{ID: ptr(uuid.New())})

	// then
	require.ErrorIs(t, err, db.ErrModelRevisionNotFound)
}

func Test_should_allow_duplicate_model_revision_idempotency_key_when_revision_is_not_active(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "idempotent-model-entry", time.Now().UTC())
	createdBy := createDBTestUser(t)
	now := time.Now().UTC()
	modelID := "model-" + uuid.NewString()
	idempotencyKey := uuid.NewString()
	first := models.ModelRevision{
		ID:             uuid.New(),
		ModelID:        modelID,
		State:          models.RevisionStatePending,
		Title:          ptr("first idempotent model revision"),
		IdempotencyKey: &idempotencyKey,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	second := first
	second.ID = uuid.New()
	second.Title = ptr("second idempotent model revision")

	// when
	_, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, first)
	require.NoError(t, err)
	createdDuplicate, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, second)

	// then
	require.NoError(t, err)
	assert.Equal(t, second.ID, createdDuplicate.ID)
}

func Test_should_reject_duplicate_active_model_revision_idempotency_key(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "active-idempotent-model-entry", time.Now().UTC())
	createdBy := createDBTestUser(t)
	now := time.Now().UTC()
	idempotencyKey := uuid.NewString()
	first := models.ModelRevision{
		ID:             uuid.New(),
		ModelID:        "model-" + uuid.NewString(),
		State:          models.RevisionStateActive,
		Title:          ptr("first active idempotent model revision"),
		IdempotencyKey: &idempotencyKey,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	second := first
	second.ID = uuid.New()
	second.ModelID = "model-" + uuid.NewString()
	second.Title = ptr("second active idempotent model revision")

	// when
	_, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, first)
	require.NoError(t, err)
	_, err = testDB.Models.Create(context.Background(), entryRevision.EntryID, second)

	// then
	require.Error(t, err)
}

func Test_should_list_model_revisions_for_entry_with_pagination_when_models_list_called(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "list-models-entry", time.Now().UTC())
	otherEntryRevision := createDBTestEntryRevision(t, "list-models-other-entry", time.Now().UTC())
	now := time.Now().UTC()
	first := createDBTestModelRevision(t, entryRevision.EntryID, "first model", now)
	second := createDBTestModelRevision(t, entryRevision.EntryID, "second model", now.Add(time.Second))
	_ = createDBTestModelRevision(t, otherEntryRevision.EntryID, "other model", now.Add(2*time.Second))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Models.List(context.Background(), db.ModelRevisionFilters{
		EntryID: &entryRevision.EntryID,
		Limit:   &limit,
		Offset:  &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.NotEqual(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[0].ID)
}

func Test_should_list_active_model_revisions_across_active_entries_when_models_list_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	firstEntryRevision := createDBTestActiveEntryRevision(t, "rolling-models-entry-first", now)
	secondEntryRevision := createDBTestActiveEntryRevision(t, "rolling-models-entry-second", now.Add(time.Second))
	inactiveEntryRevision := createDBTestEntryRevision(t, "rolling-models-inactive-entry", now.Add(2*time.Second))
	firstModelRevision := createDBTestActiveModelRevision(t, firstEntryRevision.EntryID, "first active model", now)
	secondModelRevision := createDBTestActiveModelRevision(t, secondEntryRevision.EntryID, "second active model", now.Add(time.Second))
	_ = createDBTestModelRevision(t, firstEntryRevision.EntryID, "pending model", now.Add(2*time.Second))
	_ = createDBTestActiveModelRevision(t, inactiveEntryRevision.EntryID, "inactive entry model", now.Add(3*time.Second))
	activeState := models.RevisionStateActive
	activeEntryState := models.EntryStateActive
	activeModelState := models.ModelStateActive

	// when
	got, err := testDB.Models.List(context.Background(), db.ModelRevisionFilters{
		State:      &activeState,
		EntryState: &activeEntryState,
		ModelState: &activeModelState,
	})

	// then
	require.NoError(t, err)
	gotIDs := make([]uuid.UUID, 0, len(got))
	for _, revision := range got {
		gotIDs = append(gotIDs, revision.ID)
	}
	assert.ElementsMatch(t, []uuid.UUID{firstModelRevision.ID, secondModelRevision.ID}, gotIDs)
}

func Test_should_return_error_when_models_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Models.List(context.Background(), db.ModelRevisionFilters{Limit: &limit})

	// then
	require.Error(t, err)
}

func Test_should_activate_only_target_model_revision_when_models_activate_revision_called(t *testing.T) {
	// given
	ctx := context.Background()
	entryRevision := createDBTestEntryRevision(t, "submission-entry", time.Now().UTC())
	createdBy := createDBTestUser(t)
	now := time.Now().UTC()
	modelID := "model-" + uuid.NewString()
	revisionNumber := 1
	active, err := testDB.Models.Create(ctx, entryRevision.EntryID, models.ModelRevision{
		ID:             uuid.New(),
		ModelID:        modelID,
		RevisionNumber: &revisionNumber,
		State:          models.RevisionStateActive,
		ModelState:     models.ModelStateActive,
		Title:          ptr("active model revision"),
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	require.NoError(t, err)
	target, err := testDB.Models.Create(ctx, entryRevision.EntryID, models.ModelRevision{
		ID:               uuid.New(),
		ModelID:          modelID,
		ParentRevisionID: &active.ID,
		State:            models.RevisionStateInReview,
		ModelState:       models.ModelStateActive,
		Title:            ptr("replacement model revision"),
		CreatedBy:        createdBy,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	})
	require.NoError(t, err)
	unrelated, err := testDB.Models.Create(ctx, entryRevision.EntryID, models.ModelRevision{
		ID:               uuid.New(),
		ModelID:          modelID,
		ParentRevisionID: &active.ID,
		State:            models.RevisionStatePending,
		ModelState:       models.ModelStateActive,
		Title:            ptr("unrelated model revision"),
		CreatedBy:        createdBy,
		CreatedAt:        now.Add(2 * time.Second),
		UpdatedAt:        now.Add(2 * time.Second),
	})
	require.NoError(t, err)

	// when
	err = testDB.Do(ctx, func(ctx context.Context) error {
		_, err := testDB.Models.ActivateRevision(ctx, target.ID)
		return err
	})
	require.NoError(t, err)
	archived, err := testDB.Models.Get(ctx, db.ModelRevisionFilters{ID: &active.ID})
	require.NoError(t, err)
	activated, err := testDB.Models.Get(ctx, db.ModelRevisionFilters{ID: &target.ID})
	require.NoError(t, err)
	untouched, err := testDB.Models.Get(ctx, db.ModelRevisionFilters{ID: &unrelated.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.RevisionStateArchived, archived.State)
	assert.Equal(t, models.RevisionStateActive, activated.State)
	assert.Equal(t, models.ModelStateActive, activated.ModelState)
	activeModelState := models.ModelStateActive
	_, err = testDB.Models.Get(ctx, db.ModelRevisionFilters{
		ID:         &activated.ID,
		ModelState: &activeModelState,
	})
	require.NoError(t, err)
	require.NotNil(t, activated.RevisionNumber)
	assert.Equal(t, 2, *activated.RevisionNumber)
	assert.Equal(t, models.RevisionStatePending, untouched.State)
}
