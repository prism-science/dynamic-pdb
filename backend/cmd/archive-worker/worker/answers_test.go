package worker

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

// Tests for the researchers' answers of 17 Nov (patch 23): reuse terms (Q7), sizes never estimated
// (Q9), files not verified against the bytes at save time (Q10), original bytes gone (item 14).

func Test_should_say_the_terms_are_not_confirmed_when_someone_other_than_prism_deposited_the_file(t *testing.T) {
	// given the Single Conformer model's file deposited by another account
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	other := uuid.MustParse("7e57ab1e-0000-4000-8000-000000000002")
	f.store.updateFile(f.files["single"].ID, func(file *models.SelectionFile) { file.UploadedBy = other })

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then that file says so, and Prism's uploads and the PDB's files stay CC0
	names, files, rows := readArchive(t, f)
	single := fileRows(rows, f.files["single"].ID)[0]
	assert.Equal(t, "Reuse terms not yet confirmed by the depositor", single["license"])
	assert.Empty(t, single["license_url"])
	assert.Equal(t, "CC0 1.0", fileRows(rows, f.files["qfit"].ID)[0]["license"], "a Prism upload")
	assert.Equal(t, "CC0 1.0", fileRows(rows, f.files["coord1"].ID)[0]["license"], "a PDB archive file")
	require.NotNil(t, f.store.preview)
	assert.Equal(t, models.DownloadPreviewReuseTerms{CC0: 11, NotConfirmed: 1}, f.store.preview.ReuseTerms)
	readme := files[names[0]]
	assert.Contains(t, readme, "PDB archive files and Prism's own models are dedicated to the public domain under CC0 1.0.")
	assert.Contains(t, readme, "Files deposited by others: Reuse terms not yet confirmed by the depositor, until they confirm.")
	assert.NotContains(t, readme, "These data are dedicated", "no blanket CC0 any more")
}

func Test_should_give_each_file_its_reuse_terms_by_source_and_depositor(t *testing.T) {
	// given
	prism := uuid.New()
	depositors := map[uuid.UUID]bool{prism: true}
	cases := []struct {
		name       string
		source     fileSource
		uploadedBy uuid.UUID
		expected   models.DownloadReuseTerms
	}{
		{"PDB archive file", sourcePDBArchive, uuid.New(), models.DownloadReuseTermsCC0},
		{"Prism's upload", sourceDynamicPDBUpload, prism, models.DownloadReuseTermsCC0},
		{"Dynamic PDB's own upload", sourceDynamicPDBUpload, uuid.MustParse(models.SystemUserID), models.DownloadReuseTermsCC0},
		{"someone else's upload", sourceDynamicPDBUpload, uuid.New(), models.DownloadReuseTermsNotConfirmed},
		{"another repository's file", sourceExternal, prism, models.DownloadReuseTermsRepository},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			terms, err := reuseTermsOf(models.SelectionFile{ID: uuid.New(), UploadedBy: tc.uploadedBy}, tc.source, depositors)

			// then
			require.NoError(t, err)
			assert.Equal(t, tc.expected, terms)
		})
	}

	// and with no account listed, nothing deposited is called CC0
	terms, err := reuseTermsOf(models.SelectionFile{UploadedBy: prism}, sourceDynamicPDBUpload, nil)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadReuseTermsNotConfirmed, terms)
	_, err = reuseTermsOf(models.SelectionFile{}, "somewhere", nil)
	require.ErrorContains(t, err, "unknown file source")
}

func Test_should_take_a_size_only_from_the_bytes_the_record_or_the_source_when_it_is_known(t *testing.T) {
	// given
	read, declared, reported := int64(100), int64(90), int64(80)
	cases := []struct {
		name     string
		file     models.SelectionFile
		size     int64
		source   sizeSource
		knownNow bool
	}{
		{"read from matching bytes", models.SelectionFile{SizeBytes: &declared,
			Checksum: models.ArtifactChecksum{Status: models.ChecksumStatusVerified, SizeBytes: &read}}, read, sizeRead, true},
		{"a mismatch read measured other bytes", models.SelectionFile{SizeBytes: &declared,
			Checksum: models.ArtifactChecksum{Status: models.ChecksumStatusMismatch, SizeBytes: &read}}, declared, sizeDeclared, true},
		{"reported by the source", models.SelectionFile{
			Checksum: models.ArtifactChecksum{Status: models.ChecksumStatusFailed, SourceSizeBytes: &reported}}, reported,
			sizeReportedBySource, true},
		{"unknown", models.SelectionFile{Checksum: models.ArtifactChecksum{Status: models.ChecksumStatusPending}}, 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			size, source, known := knownSize(tc.file)

			// then
			assert.Equal(t, tc.knownNow, known)
			assert.Equal(t, tc.size, size)
			assert.Equal(t, tc.source, source)
		})
	}
}

func Test_should_list_the_size_the_source_reported_when_a_linked_file_could_not_be_read(t *testing.T) {
	// given the pending FASTA file: no recorded size, and its source answered a HEAD request
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	reported := int64(4321)
	f.store.updateFile(f.files["fasta2"].ID, func(file *models.SelectionFile) {
		file.SizeBytes = nil
		file.Checksum = models.ArtifactChecksum{Status: models.ChecksumStatusFailed, SourceSizeBytes: &reported}
	})
	f.store.updateFile(f.files["sf2"].ID, func(file *models.SelectionFile) {
		file.SizeBytes = nil
		file.Checksum.SizeBytes = nil
	})

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then the manifest says where each size comes from, and an unknown size is left empty
	_, _, rows := readArchive(t, f)
	fasta := fileRows(rows, f.files["fasta2"].ID)[0]
	assert.Equal(t, "4321", fasta["size_bytes"])
	assert.Equal(t, "source", fasta["size_source"])
	sf := fileRows(rows, f.files["sf2"].ID)[0]
	assert.Empty(t, sf["size_bytes"])
	assert.Empty(t, sf["size_source"])
	assert.Equal(t, "read", fileRows(rows, f.files["qfit"].ID)[0]["size_source"])
	assert.Equal(t, "declared", fileRows(rows, f.files["coord2"].ID)[0]["size_source"],
		"original bytes gone: the recorded size, not the new bytes' size")
	assert.Equal(t, 1, f.store.preview.LinkedFilesSizeUnknown, "the structure factors over 1 GiB")
	for _, counted := range f.store.preview.FileTypes {
		if counted.Type == models.ArtifactTypeFASTA {
			assert.Equal(t, int64(len(f.content["fasta1"]))+reported, counted.BytesKnown,
				"the unreadable FASTA file counts with the size its source reported")
		}
	}
}

func Test_should_mark_files_not_verified_against_the_bytes_at_save_time(t *testing.T) {
	// given a file whose SHA-256 was computed after the save, and one with none recorded
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.updateFile(f.files["qfit"].ID, func(file *models.SelectionFile) {
		file.RecordedSHA256Source = models.SelectionSHA256SourceComputed
		file.RecordedAfterSave = true
	})
	f.store.updateFile(f.files["fasta2"].ID, func(file *models.SelectionFile) {
		file.RecordedSHA256 = nil
		file.RecordedSHA256Source = models.SelectionSHA256SourceNotRecorded
	})

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	names, files, rows := readArchive(t, f)
	assert.Equal(t, "no", fileRows(rows, f.files["qfit"].ID)[0]["verified_at_save"])
	assert.Equal(t, "no", fileRows(rows, f.files["fasta2"].ID)[0]["verified_at_save"])
	assert.Equal(t, "yes", fileRows(rows, f.files["coord1"].ID)[0]["verified_at_save"])
	assert.Equal(t, 2, f.store.preview.NotVerifiedAtSave)
	assert.Contains(t, files[names[0]], "not verified against the bytes at save time")
	_, err := verifiedAtSave(models.SelectionFile{RecordedSHA256Source: "guessed"})
	require.ErrorContains(t, err, "unknown sha256 source")
}

func Test_should_mark_a_file_unavailable_when_its_original_bytes_are_gone(t *testing.T) {
	// given entry 2's coordinate file: the source now serves other bytes, and no copy was kept
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then it is unavailable, its record and SHA-256 kept, and the failure is not retryable
	_, _, rows := readArchive(t, f)
	row := fileRows(rows, f.files["coord2"].ID)[0]
	assert.Equal(t, "unavailable", row["status"])
	assert.Equal(t, "original_bytes_unavailable", row["reason_code"])
	assert.Equal(t, "original bytes unavailable: the source now serves other bytes, and no copy was kept", row["reason"])
	assert.Equal(t, "mismatch", row["verification"])
	assert.Equal(t, sha256Hex(f.content["coord2"]), row["recorded_sha256"])
	for _, failure := range f.store.result.Failures {
		if failure.FileID == f.files["coord2"].ID {
			assert.Equal(t, models.DownloadReasonOriginalBytesUnavailable, failure.ReasonCode)
			assert.False(t, failure.Retryable)
		}
	}
	for _, manifestRow := range rows {
		assert.NotEqual(t, "changed-at-source", manifestRow["status"])
	}
}
