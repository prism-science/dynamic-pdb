package worker

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_record_each_file_the_archive_cannot_vouch_for_with_its_reason(t *testing.T) {
	// given an archive where one copy differs, one is missing, one cannot be read, and one file
	// changed at its source
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	differs, _ := f.bucket.StoredKey(*f.files["coord1"].Checksum.StoredURI)
	f.bucket.put(differs, []byte("other bytes"))
	missing, _ := f.bucket.StoredKey(*f.files["ensemble"].URI)
	delete(f.bucket.objects, missing)
	unreadable, _ := f.bucket.StoredKey(*f.files["log"].URI)
	f.bucket.openErrs[unreadable] = errors.New("connection reset")

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then each is a failure, once, with the first model that lists it; linked files are not
	_, _, rows := readArchive(t, f)
	byFile := map[uuid.UUID]models.DownloadFailure{}
	for _, failure := range f.store.result.Failures {
		assert.Equal(t, f.store.job.ID, failure.JobID)
		byFile[failure.FileID] = failure
	}
	require.Len(t, byFile, 4)
	assert.Equal(t, models.DownloadReasonCopyDiffers, byFile[f.files["coord1"].ID].ReasonCode)
	assert.Equal(t, models.DownloadReasonCopyMissing, byFile[f.files["ensemble"].ID].ReasonCode)
	assert.Equal(t, models.DownloadReasonReadFailed, byFile[f.files["log"].ID].ReasonCode)
	assert.Equal(t, models.DownloadReasonChangedAtSource, byFile[f.files["coord2"].ID].ReasonCode)
	assert.True(t, byFile[f.files["log"].ID].Retryable, "only a copy that could not be read is retryable")
	assert.False(t, byFile[f.files["coord1"].ID].Retryable)
	assert.Equal(t, models.DownloadFailure{
		JobID: f.store.job.ID, FileID: f.files["log"].ID, Ordinal: 5, EntryID: "dpdb_e2", ModelID: "dpdb_e2_m_002",
		FileName: "refine.log", ReasonCode: models.DownloadReasonReadFailed, Retryable: true,
	}, byFile[f.files["log"].ID])
	assert.Equal(t, f.store.result.Counts.Unavailable+f.store.result.Counts.ChangedAtSource, len(byFile))
	for fileID, failure := range byFile {
		assert.Equal(t, string(failure.ReasonCode), fileRows(rows, fileID)[0]["reason_code"])
	}
	assert.Equal(t, "over_copy_limit", fileRows(rows, f.files["sf2"].ID)[0]["reason_code"])
	assert.Empty(t, fileRows(rows, f.files["qfit"].ID)[0]["reason_code"], "an included file has no reason")
}

func Test_should_hold_only_the_files_asked_for_again_when_a_retry_is_built(t *testing.T) {
	// given a retry of an archive that could not read the log and the qFit model
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	parent := uuid.New()
	f.store.job.RetryOf = &parent
	f.store.retryFiles = map[uuid.UUID]bool{f.files["log"].ID: true, f.files["qfit"].ID: true}

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then the zip holds those two files under a top folder of its own, so unpacking it next to the
	// archive it completes replaces nothing there, and the manifest lists only them
	names, files, rows := readArchive(t, f)
	root := "dynamic-pdb-" + f.store.job.SelectionID.String() + "-retry-" + f.store.job.ID.String()
	assert.Equal(t, []string{
		root + "/README.txt",
		root + "/dpdb_e1/dpdb_e1_m_002/1abc_qFit.pdb",
		root + "/dpdb_e2/dpdb_e2_m_002/refine.log",
		root + "/manifest.tsv",
	}, names)
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Equal(t, "file", row["record_type"])
		assert.Equal(t, "included", row["status"])
	}
	assert.Contains(t, files[root+"/README.txt"], "This archive completes archive "+parent.String())
	assert.Contains(t, files[root+"/manifest.tsv"], "# This archive completes archive "+parent.String())
	assert.Equal(t, models.DownloadCounts{Included: 2}, f.store.result.Counts)
	require.NotNil(t, f.store.preview)
	assert.Equal(t, 2, f.store.preview.Files)
	assert.Equal(t, 2, f.store.preview.Models, "only the models listing a file asked for again")
	assert.Equal(t, 2, f.store.preview.Entries)
	assert.Zero(t, f.store.preview.RelatedDatasets.Total, "a retry lists no related datasets")
	assert.Zero(t, f.store.preview.ModelsWithoutMatchingFiles)
	assert.Empty(t, f.store.result.Failures)
}

func Test_should_account_for_every_selected_file_when_the_manifest_is_written(t *testing.T) {
	for _, choice := range models.DownloadFileChoices {
		t.Run(string(choice), func(t *testing.T) {
			// given
			f := newArchiveFixture(t, choice)

			// when
			runOnce(t, newTestWorker(t, f, testConfig(t)))

			// then every selected file of every model has a row with a status, and a model with
			// none has a row saying why
			_, _, rows := readArchive(t, f)
			for _, model := range f.store.models {
				details := f.store.details[model.Ordinal]
				selectedAny := false
				for _, file := range model.Files {
					selected, err := selectsFile(choice, file, details)
					require.NoError(t, err)
					if !selected {
						continue
					}
					selectedAny = true
					listed := fileRows(rows, file.ID)
					require.NotEmpty(t, listed, "model %s file %s", model.ModelID, file.Name)
					for _, row := range listed {
						assert.NotEmpty(t, row["status"])
					}
				}
				if !selectedAny {
					found := false
					for _, row := range rowsOfType(rows, "no_matching_file") {
						found = found || row["model_id"] == model.ModelID
					}
					assert.True(t, found, "model %s says why it has no file", model.ModelID)
				}
			}
			for _, row := range rowsOfType(rows, "file") {
				assert.False(t, strings.TrimSpace(row["status"]) == "")
			}
		})
	}
}
