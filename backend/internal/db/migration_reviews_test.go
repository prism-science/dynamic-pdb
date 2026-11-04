package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_resolve_only_open_rows_of_given_objects_when_review_rows_are_resolved(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).MigrationReviews
	ctx := context.Background()
	resolution := models.MigrationReviewResolution{
		ObjectType: "entry",
		Issue:      "ligands_cofactors_rederive_from_archive",
		ObjectIDs:  []string{"e_pdb", "e_missing"},
		ResolvedBy: "archive-rederive-job",
	}
	resolvedAt := func() *time.Time {
		var at *time.Time
		require.NoError(t, fixture.database.Get(&at,
			`select resolved_at from schema_migration_review
			 where object_type = 'entry' and object_id = 'e_pdb' and issue = 'ligands_cofactors_rederive_from_archive'`))
		return at
	}

	// when
	require.NoError(t, repository.Resolve(ctx, resolution))
	first := resolvedAt()
	require.NoError(t, repository.Resolve(ctx, resolution))

	// then
	require.NotNil(t, first)
	assert.Equal(t, first, resolvedAt(), "a resolved row keeps when it was resolved")
	var resolvedBy string
	require.NoError(t, fixture.database.Get(&resolvedBy,
		`select resolved_by from schema_migration_review
		 where object_type = 'entry' and object_id = 'e_pdb' and issue = 'ligands_cofactors_rederive_from_archive'`))
	assert.Equal(t, "archive-rederive-job", resolvedBy)
	var otherResolved int
	require.NoError(t, fixture.database.Get(&otherResolved,
		`select count(*) from schema_migration_review where resolved_at is not null and object_id <> 'e_pdb'`))
	assert.Zero(t, otherResolved)
}

func Test_should_store_open_row_with_details_when_review_is_opened(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).MigrationReviews
	review := models.MigrationReview{
		ObjectType: "model",
		ObjectID:   "m_review",
		Issue:      "sync_validation_failed",
		Details:    map[string]any{"archive_version": "2.0"},
	}

	// when
	require.NoError(t, repository.Open(context.Background(), review))

	// then
	row := fixture.migrationReviewRow(t, "model", "m_review", "sync_validation_failed")
	assert.JSONEq(t, `{"archive_version": "2.0"}`, row.Details)
	assert.Nil(t, row.ResolvedAt)
	assert.Nil(t, row.ResolvedBy)
}

func Test_should_reopen_row_with_new_details_when_resolved_review_is_opened_again(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).MigrationReviews
	ctx := context.Background()
	review := models.MigrationReview{
		ObjectType: "model",
		ObjectID:   "m_review",
		Issue:      "sync_validation_failed",
		Details:    map[string]any{"archive_version": "2.0"},
	}
	require.NoError(t, repository.Open(ctx, review))
	first := fixture.migrationReviewRow(t, "model", "m_review", "sync_validation_failed")
	require.NoError(t, repository.Resolve(ctx, models.MigrationReviewResolution{
		ObjectType: "model",
		Issue:      "sync_validation_failed",
		ObjectIDs:  []string{"m_review"},
		ResolvedBy: "data-sync",
	}))
	review.Details = map[string]any{"archive_version": "3.0"}

	// when
	require.NoError(t, repository.Open(ctx, review))

	// then
	row := fixture.migrationReviewRow(t, "model", "m_review", "sync_validation_failed")
	assert.JSONEq(t, `{"archive_version": "3.0"}`, row.Details)
	assert.Nil(t, row.ResolvedAt, "the row is open again")
	assert.Nil(t, row.ResolvedBy)
	assert.Equal(t, first.CreatedAt, row.CreatedAt, "the row keeps when it was first made")
}

type migrationReviewRow struct {
	Details    string     `db:"details"`
	CreatedAt  time.Time  `db:"created_at"`
	ResolvedAt *time.Time `db:"resolved_at"`
	ResolvedBy *string    `db:"resolved_by"`
}

func (f *migratedFixture) migrationReviewRow(t *testing.T, objectType, objectID, issue string) migrationReviewRow {
	t.Helper()
	var row migrationReviewRow
	require.NoError(t, f.database.Get(&row,
		`select details, created_at, resolved_at, resolved_by from schema_migration_review
		 where object_type = $1 and object_id = $2 and issue = $3`,
		objectType, objectID, issue))
	return row
}
