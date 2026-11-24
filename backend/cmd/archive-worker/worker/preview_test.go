package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_count_what_the_archive_holds_when_the_preview_of_each_file_choice_is_planned(t *testing.T) {
	for _, choice := range models.DownloadFileChoices {
		t.Run(string(choice), func(t *testing.T) {
			// given
			f := newArchiveFixture(t, choice)

			// when
			runOnce(t, newTestWorker(t, f, testConfig(t)))

			// then the preview the planning lane recorded counts what the archive holds
			_, _, rows := readArchive(t, f)
			preview := f.store.preview
			require.NotNil(t, preview)
			assert.False(t, f.store.planUpdated, "nothing changed between the preview and the build")
			assert.Equal(t, f.store.result.Counts, preview.Availability)
			distinct := map[string]bool{}
			for _, row := range rowsOfType(rows, "file") {
				distinct[row["file_id"]] = true
			}
			assert.Equal(t, len(distinct), preview.Files)
			assert.Len(t, rowsOfType(rows, "no_matching_file"), preview.ModelsWithoutMatchingFiles)
			assert.Len(t, rowsOfType(rows, "related_dataset"), preview.RelatedDatasets.Total)
			assert.Equal(t, f.store.plannedBytes, preview.IncludedBytes)
			assert.GreaterOrEqual(t, preview.ArchiveBytesUpperBound, f.store.result.ArchiveBytes,
				"the zip is never larger than its bound")
			notIncluded := 0
			for _, reason := range preview.NotIncluded {
				notIncluded += reason.Files
			}
			assert.Equal(t, preview.Files-preview.Availability.Included, notIncluded, "every file left out has its reason")
			assert.Equal(t, preview.Files,
				preview.Verification.Verified+preview.Verification.Mismatch+preview.Verification.NotVerified)
			assert.Equal(t, 6, preview.Models)
			assert.Equal(t, 2, preview.Entries)
		})
	}
}

func Test_should_list_every_count_of_the_five_representations_when_all_files_are_previewed(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	preview := f.store.preview
	require.NotNil(t, preview)
	assert.Equal(t, 12, preview.Files)
	assert.Zero(t, preview.ModelsWithoutMatchingFiles)
	assert.Equal(t, models.DownloadCounts{Included: 9, Linked: 2, ChangedAtSource: 1}, preview.Availability)
	assert.Equal(t, []models.DownloadPreviewReason{
		{Code: models.DownloadReasonOverCopyLimit, Files: 1},
		{Code: models.DownloadReasonNotCheckedYet, Files: 1},
		{Code: models.DownloadReasonChangedAtSource, Files: 1},
	}, preview.NotIncluded)
	assert.Equal(t, models.DownloadPreviewVerification{Verified: 10, Mismatch: 1, NotVerified: 1}, preview.Verification)
	byType := map[models.ArtifactType]models.DownloadPreviewFileType{}
	for _, counted := range preview.FileTypes {
		byType[counted.Type] = counted
	}
	assert.Equal(t, []models.ArtifactType{models.ArtifactTypeModel, models.ArtifactTypeStructureFactors,
		models.ArtifactTypeFASTA, models.ArtifactTypeOther}, []models.ArtifactType{preview.FileTypes[0].Type,
		preview.FileTypes[1].Type, preview.FileTypes[2].Type, preview.FileTypes[3].Type})
	assert.Equal(t, 7, byType[models.ArtifactTypeModel].Files)
	assert.Equal(t, 2, byType[models.ArtifactTypeStructureFactors].Files)
	assert.Equal(t, int64(len(f.content["sf1"]))+int64(2<<30), byType[models.ArtifactTypeStructureFactors].BytesKnown)
	assert.Equal(t, int64(2<<30)+int64(len(f.content["fasta2"])), preview.LinkedBytesKnown)
	assert.Zero(t, preview.LinkedFilesSizeUnknown)
	assert.Equal(t, models.DownloadPreviewRelatedDatasets{Total: 1, WithDOI: 1}, preview.RelatedDatasets)
}

func Test_should_stop_after_planning_and_build_once_asked_when_the_archive_was_not_asked_for(t *testing.T) {
	// given a request that only asked for the preview
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	f.store.job.PrepareRequested = false
	worker := newTestWorker(t, f, testConfig(t))

	// when
	runOnce(t, worker)

	// then the job rests at planned with its preview, and nothing is written
	assert.Equal(t, models.DownloadStatusPlanned, f.store.job.Status)
	require.NotNil(t, f.store.preview)
	assert.Equal(t, 2, f.store.preview.Availability.Included)
	assert.Equal(t, 3, f.store.preview.ModelsWithoutMatchingFiles)
	assert.Empty(t, f.bucket.writers, "nothing is written to the bucket")
	worked, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, worked, "no lane takes a planned job that was not asked for")

	// and once it is asked for, the building lane builds it
	f.store.job.PrepareRequested = true
	runOnce(t, worker)
	_, _, _ = readArchive(t, f)
	assert.Equal(t, models.DownloadCounts{Included: 2, ChangedAtSource: 1}, f.store.result.Counts)
}

func Test_should_record_the_new_plan_when_a_file_was_copied_after_the_preview(t *testing.T) {
	// given a planned preview, and then the checksum job copies the pending FASTA file
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.job.PrepareRequested = false
	worker := newTestWorker(t, f, testConfig(t))
	runOnce(t, worker)
	require.Equal(t, 9, f.store.preview.Availability.Included)
	copied := f.files["fasta2"]
	size := int64(len(f.content["fasta2"]))
	copyKey := "dpdb/copies/" + copied.ID.String() + "/3ghi.fasta"
	f.bucket.put(copyKey, []byte(f.content["fasta2"]))
	f.store.setChecksum(copied.ID, models.ArtifactChecksum{
		Status: models.ChecksumStatusVerified, SHA256: copied.RecordedSHA256, SizeBytes: &size,
		StoredURI: ptr(bucketBase + copyKey), CopyStatus: ptr(models.CopyStatusCopied), CheckedAt: &checkedAt,
	})

	// when it is asked for
	f.store.job.PrepareRequested = true
	runOnce(t, worker)

	// then the new plan replaces the preview, and the archive holds what it says
	_, _, rows := readArchive(t, f)
	assert.True(t, f.store.planUpdated)
	assert.Equal(t, 10, f.store.preview.Availability.Included)
	assert.Equal(t, f.store.result.Counts, f.store.preview.Availability)
	assert.Equal(t, "included", fileRows(rows, copied.ID)[0]["status"])
}

func Test_should_count_the_size_as_unknown_when_a_linked_file_has_none_recorded(t *testing.T) {
	// given structure factors over 1 GiB whose size nobody recorded
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.job.PrepareRequested = false
	f.store.updateFile(f.files["sf2"].ID, func(file *models.SelectionFile) {
		file.SizeBytes = nil
		file.Checksum.SizeBytes = nil
	})

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	preview := f.store.preview
	require.NotNil(t, preview)
	assert.Equal(t, 1, preview.LinkedFilesSizeUnknown)
	assert.Equal(t, int64(len(f.content["fasta2"])), preview.LinkedBytesKnown)
	for _, counted := range preview.FileTypes {
		if counted.Type == models.ArtifactTypeStructureFactors {
			assert.Equal(t, 1, counted.FilesSizeUnknown)
			assert.Equal(t, int64(len(f.content["sf1"])), counted.BytesKnown)
		}
	}
}

func Test_should_keep_the_preview_when_the_archive_is_blocked(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.job.MaxArchiveBytes = 40

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then the whole selection was counted, not only up to the limit
	require.NotNil(t, f.store.preview)
	assert.Equal(t, models.DownloadStatusBlocked, f.store.job.Status)
	assert.Equal(t, 9, f.store.preview.Availability.Included)
	assert.Equal(t, f.store.plannedBytes, f.store.preview.IncludedBytes)
	assert.Contains(t, f.store.blockedReason, "add up to "+formatBytes(f.store.plannedBytes)+", more than")
}

func Test_should_count_a_model_without_files_as_the_manifest_lists_it(t *testing.T) {
	for choice, expected := range map[models.DownloadFileChoice]int{
		models.DownloadFileChoiceAllFiles:      0, // every model is listed with its files: none here
		models.DownloadFileChoiceOriginalMMCIF: 4, // the three computed models, and this one
	} {
		t.Run(string(choice), func(t *testing.T) {
			// given a seventh model with no files at all
			f := newArchiveFixture(t, choice)
			f.store.models = append(f.store.models, models.SelectionModel{
				Ordinal: 7, ModelID: "dpdb_e2_m_004", EntryID: "dpdb_e2", ModelRevisionNumber: 1, EntryRevisionNumber: 1})
			f.store.details[7] = models.DownloadSelectionModel{Ordinal: 7, ModelType: ptr("Deposited"), EntryPDBID: ptr("3GHI")}

			// when
			runOnce(t, newTestWorker(t, f, testConfig(t)))

			// then
			_, _, rows := readArchive(t, f)
			assert.Equal(t, expected, f.store.preview.ModelsWithoutMatchingFiles)
			assert.Len(t, rowsOfType(rows, "no_matching_file"), expected)
			assert.Equal(t, 7, f.store.preview.Models)
		})
	}
}

func Test_should_keep_a_file_out_of_a_model_folders_path_when_an_entry_file_is_named_like_a_model(t *testing.T) {
	// given an entry file of entry 1 named exactly like its second model, listed by the first
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	clash := f.file("clash", fileSpec{name: "dpdb_e1_m_002", fileType: models.ArtifactTypeOther, level: models.ArtifactLevelL1,
		roles: []models.SelectionFileRole{models.SelectionFileRoleEntry}, content: "entry notes\n", where: "upload"})
	f.store.models[0].Files = append(f.store.models[0].Files, clash)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then the file takes another name, and no file's path is a folder of another
	names, files, rows := readArchive(t, f)
	root := "dynamic-pdb-" + f.store.job.SelectionID.String()
	renamed := root + "/dpdb_e1/" + clash.ID.String()[:8] + "_dpdb_e1_m_002"
	assert.Equal(t, "entry notes\n", files[renamed])
	assert.Equal(t, renamed, fileRows(rows, clash.ID)[0]["path_in_archive"])
	for _, name := range names {
		for _, other := range names {
			assert.False(t, strings.HasPrefix(other, name+"/"), "%s is both a file and the folder of %s", name, other)
		}
	}
}
