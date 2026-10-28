package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

const coordinateURI = "https://files.rcsb.org/download/1ABC.cif"

func Test_should_find_atomic_model_change_when_group_is_in_a_version_after_the_applied_one(t *testing.T) {
	// given
	applied := models.ArchiveVersion{Ordinal: 3, Version: "4.1"}
	latest := models.ArchiveVersion{Ordinal: 5, Version: "4.3"}
	groups := []archive.RevisionGroup{
		{Group: "Atomic model", RevisionOrdinal: 2},
		{Group: "Atomic model", RevisionOrdinal: 4},
		{Group: "Data collection", RevisionOrdinal: 5},
	}

	// when
	changed := hasAtomicModelChange(applied, latest, groups)

	// then
	assert.True(t, changed)
}

func Test_should_find_atomic_model_change_when_group_is_in_the_latest_version(t *testing.T) {
	// given
	applied := models.ArchiveVersion{Ordinal: 1, Version: "1.0"}
	latest := models.ArchiveVersion{Ordinal: 2, Version: "1.1"}
	groups := []archive.RevisionGroup{{Group: "Atomic model", RevisionOrdinal: 2}}

	// when
	changed := hasAtomicModelChange(applied, latest, groups)

	// then
	assert.True(t, changed)
}

func Test_should_not_find_atomic_model_change_when_group_is_only_in_applied_or_older_versions(t *testing.T) {
	// given
	applied := models.ArchiveVersion{Ordinal: 3, Version: "4.1"}
	latest := models.ArchiveVersion{Ordinal: 5, Version: "4.3"}
	groups := []archive.RevisionGroup{
		{Group: "Atomic model", RevisionOrdinal: 1},
		{Group: "Atomic model", RevisionOrdinal: 3},
		{Group: "Atomic model", RevisionOrdinal: 6},
		{Group: "Refinement description", RevisionOrdinal: 4},
	}

	// when
	changed := hasAtomicModelChange(applied, latest, groups)

	// then
	assert.False(t, changed)
}

func Test_should_not_find_atomic_model_change_when_group_name_only_contains_it(t *testing.T) {
	// given
	applied := models.ArchiveVersion{Ordinal: 1, Version: "1.0"}
	latest := models.ArchiveVersion{Ordinal: 2, Version: "1.1"}
	groups := []archive.RevisionGroup{{Group: "Atomic model refinement", RevisionOrdinal: 2}}

	// when
	changed := hasAtomicModelChange(applied, latest, groups)

	// then
	assert.False(t, changed)
}

func Test_should_pick_linked_rcsb_coordinate_file_when_model_links_several_files(t *testing.T) {
	// given
	coordinateFile := coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/1ABC.cif"))
	linked := []models.Artifact{
		{ID: uuid.New(), Type: models.ArtifactTypeModel, URI: new("https://example.org/refined.cif")},
		coordinateFile,
		{ID: uuid.New(), Type: models.ArtifactTypeStructureFactors, URI: new("https://files.rcsb.org/download/1ABC-sf.cif")},
	}

	// when
	previous, reason := previousCoordinateArtifact(linked, coordinateURI)

	// then
	assert.Empty(t, reason)
	assert.Equal(t, coordinateFile.ID, previous.ID)
}

func Test_should_give_reason_when_model_links_no_rcsb_coordinate_file(t *testing.T) {
	// given
	linked := []models.Artifact{{ID: uuid.New(), Type: models.ArtifactTypeModel, URI: new("https://example.org/refined.cif")}}

	// when
	_, reason := previousCoordinateArtifact(linked, coordinateURI)

	// then
	assert.Equal(t, "the model links no RCSB coordinate file "+coordinateURI, reason)
}

func Test_should_give_reason_when_model_links_rcsb_coordinate_file_twice(t *testing.T) {
	// given
	linked := []models.Artifact{
		coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/first.cif")),
		coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/second.cif")),
	}

	// when
	_, reason := previousCoordinateArtifact(linked, coordinateURI)

	// then
	assert.Equal(t, "the model links 2 artifacts for RCSB coordinate file "+coordinateURI, reason)
}

func Test_should_allow_coordinate_change_when_switch_is_on_and_previous_file_is_verified_and_copied(t *testing.T) {
	// given
	coordinateFile := coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/1ABC.cif"))

	// when
	previous, reason, err := coordinateChangeBlock(true, []models.Artifact{coordinateFile}, coordinateURI)

	// then
	require.NoError(t, err)
	assert.Empty(t, reason)
	assert.Equal(t, coordinateFile.ID, previous.ID)
}

func Test_should_block_coordinate_change_when_switch_is_off(t *testing.T) {
	// given
	coordinateFile := coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/1ABC.cif"))

	// when
	_, reason, err := coordinateChangeBlock(false, []models.Artifact{coordinateFile}, coordinateURI)

	// then
	require.NoError(t, err)
	assert.Equal(t, "coordinate revisions are switched off", reason)
}

func Test_should_block_coordinate_change_when_previous_file_is_not_verified(t *testing.T) {
	testCases := []struct {
		status models.ChecksumStatus
		reason string
	}{
		{
			status: models.ChecksumStatusPending,
			reason: "the checksum job has not copied the previous coordinate file yet (checksum pending)",
		},
		{
			status: models.ChecksumStatusFailed,
			reason: "the checksum job could not read the previous coordinate file (checksum failed)",
		},
		{
			status: models.ChecksumStatusMismatch,
			reason: "RCSB replaced the previous coordinate file before the checksum job copied it (checksum mismatch)",
		},
	}
	for _, testCase := range testCases {
		t.Run(string(testCase.status), func(t *testing.T) {
			// given
			coordinateFile := coordinateArtifact(testCase.status, nil)

			// when
			_, reason, err := coordinateChangeBlock(true, []models.Artifact{coordinateFile}, coordinateURI)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.reason, reason)
		})
	}
}

func Test_should_block_coordinate_change_when_verified_previous_file_has_no_copy(t *testing.T) {
	// given
	coordinateFile := coordinateArtifact(models.ChecksumStatusVerified, nil)

	// when
	_, reason, err := coordinateChangeBlock(true, []models.Artifact{coordinateFile}, coordinateURI)

	// then
	require.NoError(t, err)
	assert.Equal(t, "the checksum job verified the previous coordinate file but kept no copy of it", reason)
}

func Test_should_block_coordinate_change_when_model_links_no_rcsb_coordinate_file(t *testing.T) {
	// when
	_, reason, err := coordinateChangeBlock(true, nil, coordinateURI)

	// then
	require.NoError(t, err)
	assert.Equal(t, "the model links no RCSB coordinate file "+coordinateURI, reason)
}

func Test_should_fail_when_previous_file_has_unknown_checksum_status(t *testing.T) {
	// given
	coordinateFile := coordinateArtifact(models.ChecksumStatus("copied"), new("s3://copies/1ABC.cif"))

	// when
	_, _, err := coordinateChangeBlock(true, []models.Artifact{coordinateFile}, coordinateURI)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown checksum status: copied")
}

func Test_should_link_new_coordinate_file_instead_of_previous_one_and_keep_other_links(t *testing.T) {
	// given
	structureFactors := uuid.New()
	previous := uuid.New()
	image := uuid.New()
	replacement := uuid.New()

	// when
	links := coordinateArtifactLinks([]uuid.UUID{structureFactors, previous, image}, previous, replacement)

	// then
	assert.Equal(t, []uuid.UUID{structureFactors, replacement, image}, links)
}

func Test_should_move_primary_artifact_when_it_was_the_previous_coordinate_file(t *testing.T) {
	// given
	previous := uuid.New()
	replacement := uuid.New()

	// when
	primary := coordinatePrimaryArtifact(new(previous), previous, replacement)

	// then
	assert.Equal(t, new(replacement), primary)
}

func Test_should_keep_primary_artifact_when_it_was_another_file(t *testing.T) {
	// given
	other := uuid.New()

	// when
	primary := coordinatePrimaryArtifact(new(other), uuid.New(), uuid.New())
	unset := coordinatePrimaryArtifact(nil, uuid.New(), uuid.New())

	// then
	assert.Equal(t, new(other), primary)
	assert.Nil(t, unset)
}

func Test_should_copy_previous_coordinate_file_with_archive_version_when_making_new_one(t *testing.T) {
	// given
	previous := coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/1ABC.cif"))
	previous.Metadata = map[string]any{"extensions": map[string]any{"note": "kept"}}
	createdAt := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	version := models.ArchiveVersion{Ordinal: 2, Version: "1.1"}

	// when
	artifact, err := newCoordinateArtifact(previous, version, createdAt)

	// then
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, artifact.ID)
	assert.NotEqual(t, previous.ID, artifact.ID)
	assert.Equal(t, previous.Name, artifact.Name)
	assert.Equal(t, previous.Level, artifact.Level)
	assert.Equal(t, previous.Type, artifact.Type)
	assert.Equal(t, previous.Format, artifact.Format)
	assert.Equal(t, previous.URI, artifact.URI)
	assert.Nil(t, artifact.SHA256)
	assert.Nil(t, artifact.SizeBytes)
	assert.Equal(t, map[string]any{
		"extensions":      map[string]any{"note": "kept"},
		"archive_version": "1.1",
	}, artifact.Metadata)
	assert.Equal(t, map[string]any{"extensions": map[string]any{"note": "kept"}}, previous.Metadata)
	assert.Equal(t, dataSyncUserID, artifact.CreatedBy)
	assert.Equal(t, createdAt, artifact.CreatedAt)
}

func Test_should_reject_new_coordinate_file_when_previous_metadata_is_not_an_object(t *testing.T) {
	// given
	previous := coordinateArtifact(models.ChecksumStatusVerified, new("s3://copies/1ABC.cif"))
	previous.Metadata = &models.FASTAMetadata{}

	// when
	_, err := newCoordinateArtifact(previous, models.ArchiveVersion{Ordinal: 2, Version: "1.1"}, time.Now())

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "metadata of previous coordinate file has type *models.FASTAMetadata")
}

func coordinateArtifact(status models.ChecksumStatus, storedURI *string) models.Artifact {
	return models.Artifact{
		ID:        uuid.New(),
		Name:      "1ABC.cif",
		Level:     models.ArtifactLevelL2,
		Type:      models.ArtifactTypeModel,
		URI:       new(coordinateURI),
		SHA256:    new("0000000000000000000000000000000000000000000000000000000000000000"),
		Format:    new("cif"),
		SizeBytes: new(int64(1024)),
		Metadata:  map[string]any{},
		Checksum:  models.ArtifactChecksum{Status: status, StoredURI: storedURI},
	}
}
