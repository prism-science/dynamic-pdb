package worker

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

// checksumFile is a pinned file with a declared SHA-256 and the checksum result given.
func checksumFile(uri string, checksum models.ArtifactChecksum) models.SelectionFile {
	return models.SelectionFile{
		ID:             uuid.New(),
		Name:           "file.cif",
		Type:           models.ArtifactTypeModel,
		URI:            &uri,
		RecordedSHA256: ptr(strings.Repeat("a", 64)),
		Checksum:       checksum,
	}
}

func Test_should_give_each_file_its_availability_and_verification_when_its_checksum_result_is_read(t *testing.T) {
	// given
	size := int64(10)
	copied := bucketBase + "copies/x/file.cif"
	cases := []struct {
		name         string
		uri          string
		checksum     models.ArtifactChecksum
		availability availability
		verification verification
		canInclude   bool
		key          string
	}{
		{"upload verified", bucketBase + "uploads/file.cif", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusOwnStorage)},
			availabilityIncluded, verificationVerified, true, "uploads/file.cif"},
		{"external copied", "https://files.rcsb.org/download/1ABC.cif", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusCopied), StoredURI: &copied},
			availabilityIncluded, verificationVerified, true, "copies/x/file.cif"},
		{"over the copy limit", "https://files.rcsb.org/download/1ABC-sf.cif", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusOverLimit), CheckedAt: &checkedAt},
			availabilityLinked, verificationVerified, false, ""},
		{"copy failed", "https://example.org/f", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusCopyFailed)},
			availabilityLinked, verificationVerified, false, ""},
		{"copying off", "https://example.org/f", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusCopyOff)},
			availabilityLinked, verificationVerified, false, ""},
		{"changed at the source", "https://files.rcsb.org/download/1ABC.cif", models.ArtifactChecksum{Status: models.ChecksumStatusMismatch,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusMismatch)},
			availabilityChangedAtSource, verificationMismatch, false, ""},
		{"own upload differs from its record", bucketBase + "uploads/file.cif", models.ArtifactChecksum{Status: models.ChecksumStatusMismatch,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusOwnStorage)},
			availabilityUnavailable, verificationMismatch, false, ""},
		{"not checked yet", "https://files.rcsb.org/download/1ABC.cif", models.ArtifactChecksum{Status: models.ChecksumStatusPending},
			availabilityLinked, verificationNotVerified, false, ""},
		{"unreadable", "https://www.rcsb.org/fasta/entry/1ABC", models.ArtifactChecksum{Status: models.ChecksumStatusFailed},
			availabilityUnavailable, verificationNotVerified, false, ""},
		{"copy outside our bucket", "https://files.rcsb.org/download/1ABC.cif", models.ArtifactChecksum{Status: models.ChecksumStatusVerified,
			SizeBytes: &size, CopyStatus: ptr(models.CopyStatusCopied), StoredURI: ptr("https://elsewhere.org/x")},
			availabilityUnavailable, verificationNotVerified, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			state, err := stateOf(checksumFile(tc.uri, tc.checksum), newFakeBucket())

			// then
			require.NoError(t, err)
			assert.Equal(t, tc.availability, state.availability)
			assert.Equal(t, tc.verification, state.verification)
			assert.Equal(t, tc.canInclude, state.canInclude)
			assert.Equal(t, tc.key, state.key)
			if !tc.canInclude {
				assert.NotEmpty(t, state.reason, "a file left out says why")
			}
		})
	}
}

func Test_should_fail_when_a_checksum_result_is_impossible_or_unknown(t *testing.T) {
	// given
	cases := map[string]models.ArtifactChecksum{
		"verified without copy status": {Status: models.ChecksumStatusVerified},
		"verified with copy status mismatch": {Status: models.ChecksumStatusVerified,
			CopyStatus: ptr(models.CopyStatusMismatch)},
		"mismatch that was copied":  {Status: models.ChecksumStatusMismatch, CopyStatus: ptr(models.CopyStatusCopied)},
		"unknown status":            {Status: "lost"},
		"unknown copy status":       {Status: models.ChecksumStatusVerified, CopyStatus: ptr(models.CopyStatus("moved"))},
		"copied without stored uri": {Status: models.ChecksumStatusVerified, CopyStatus: ptr(models.CopyStatusCopied)},
	}
	for name, checksum := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := stateOf(checksumFile("https://files.rcsb.org/download/1ABC.cif", checksum), newFakeBucket())

			// then
			require.Error(t, err)
		})
	}
}

func Test_should_take_only_the_pdb_coordinate_file_when_original_mmcif_is_chosen(t *testing.T) {
	// given
	cases := map[string]bool{
		"https://files.rcsb.org/download/1ABC.cif":    true,
		"https://FILES.rcsb.org/download/1abc.CIF":    true,
		"https://files.rcsb.org/download/1ABC-sf.cif": false,
		"https://files.rcsb.org/download/1ABC.pdb":    false,
		"https://files.rcsb.org/other/1ABC.cif":       false,
		bucketBase + "dpdb/1abc.cif":                  false,
	}
	structureFactors := models.SelectionFile{Type: models.ArtifactTypeStructureFactors,
		URI: ptr("https://files.rcsb.org/download/1ABC.cif")}

	// when
	taken := map[string]bool{}
	for uri := range cases {
		taken[uri] = isPDBCoordinateFile(models.SelectionFile{Type: models.ArtifactTypeModel, URI: ptr(uri)})
	}
	structureFactorsTaken := isPDBCoordinateFile(structureFactors)

	// then
	assert.Equal(t, cases, taken)
	assert.False(t, structureFactorsTaken, "only model files are coordinate files")
}

func Test_should_fail_when_the_file_choice_or_model_type_is_unknown(t *testing.T) {
	// given
	unknownChoice := models.DownloadFileChoice("everything")
	unknownType := models.DownloadSelectionModel{ModelType: ptr("Hologram")}

	// when
	_, selectErr := selectsFile(unknownChoice, models.SelectionFile{}, models.DownloadSelectionModel{})
	_, reasonErr := noMatchingFileReason(unknownChoice)
	_, typeErr := selectsFile(models.DownloadFileChoiceModifiedCoordinates, models.SelectionFile{}, unknownType)

	// then
	require.ErrorContains(t, selectErr, "unknown download file choice")
	require.ErrorContains(t, reasonErr, "unknown download file choice")
	require.Error(t, typeErr)
}

func Test_should_write_the_cc0_text_and_keep_each_value_in_its_cell_when_a_manifest_is_written(t *testing.T) {
	// given
	var out bytes.Buffer
	writer, err := newManifestWriter(&out, manifestHeader{
		SelectionID: uuid.MustParse("6f1c4a2e-0000-4000-8000-000000000001"),
		CatalogDate: catalogDate, FileChoice: models.DownloadFileChoiceAllFiles, PreparedAt: catalogDate,
	})
	require.NoError(t, err)

	// when
	require.NoError(t, writer.write(manifestRow{"record_type": "file", "file_name": "a\tb\nc\\d"}))
	require.ErrorContains(t, writer.write(manifestRow{"filename": "x"}), `unknown column "filename"`)
	require.NoError(t, writer.flush())

	// then
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 5)
	assert.Equal(t, "# These data are dedicated to the public domain under CC0 1.0. We ask that you acknowledge "+
		"Dynamic PDB and cite the depositors of the entries you use.", lines[1])
	assert.Contains(t, lines[2], "CC0 1.0 https://creativecommons.org/publicdomain/zero/1.0/")
	assert.Equal(t, strings.Join(manifestColumns, "\t"), lines[3])
	cells := strings.Split(lines[4], "\t")
	require.Len(t, cells, len(manifestColumns), "an escaped tab stays inside its cell")
	assert.Equal(t, `a\tb\nc\\d`, cells[indexOf(manifestColumns, "file_name")])
	assert.Contains(t, readmeText(manifestHeader{SelectionID: uuid.Nil, CatalogDate: catalogDate}),
		"add the Deposited model of the entry to your selection")
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return -1
}

func Test_should_give_the_repositorys_terms_when_a_file_is_held_elsewhere(t *testing.T) {
	// given
	file := checksumFile("https://zenodo.org/record/1/files/model.cif", models.ArtifactChecksum{})

	// when
	row, err := fileRowValues(file, sourceOf(file, newFakeBucket()))

	// then
	require.NoError(t, err)
	assert.Equal(t, "external", row["source"])
	assert.Equal(t, "See the repository's terms", row["license"])
	assert.Equal(t, "https://zenodo.org/record/1/files/model.cif", row["license_url"])
	_, err = fileRowValues(file, "somewhere")
	require.ErrorContains(t, err, "unknown file source")
}

func Test_should_keep_the_file_in_its_folder_when_its_name_has_a_path(t *testing.T) {
	// given
	id := uuid.MustParse("12345678-0000-4000-8000-000000000001")
	names := []string{"../../etc/model.cif", `C:\data\model.cif`, "a\x00b.cif", "..", "  "}

	// when
	safe := make([]string, 0, len(names))
	for _, name := range names {
		safe = append(safe, safeFileName(name, id))
	}

	// then
	assert.Equal(t, []string{"model.cif", "model.cif", "ab.cif", id.String(), id.String()}, safe)
}

func Test_should_store_without_compressing_when_a_file_is_compressed_already(t *testing.T) {
	// given
	names := []string{"a/1abc.cif", "a/model.pdb", "a/data.mtz.gz", "a/image.PNG"}

	// when
	methods := make([]uint16, 0, len(names))
	for _, name := range names {
		methods = append(methods, compressionMethod(name))
	}

	// then
	assert.Equal(t, []uint16{zip.Deflate, zip.Deflate, zip.Store, zip.Store}, methods)
}

func Test_should_write_zip64_records_when_an_archive_has_more_than_65535_entries(t *testing.T) {
	// given
	var out bytes.Buffer
	archive := newZipArchive(&out, catalogDate)
	const entries = 70_000

	// when
	for index := range entries {
		require.NoError(t, archive.add(fmt.Sprintf("root/f%05d.txt", index), strings.NewReader("x")))
	}
	size, _, err := archive.close()
	require.NoError(t, err)

	// then
	assert.Equal(t, int64(out.Len()), size)

	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, entries)
	last, err := reader.File[entries-1].Open()
	require.NoError(t, err)
	content, err := io.ReadAll(last)
	require.NoError(t, err)
	assert.Equal(t, "x", string(content))
	assert.True(t, bytes.Contains(out.Bytes(), zip64EndSignature), "zip64 end of central directory record")
}

// zip64EndSignature starts a zip64 end of central directory record.
var zip64EndSignature = binary.LittleEndian.AppendUint32(nil, 0x06064b50)

// tailWriter keeps only the last bytes written to it.
type tailWriter struct {
	keep int
	tail []byte
}

func (w *tailWriter) Write(data []byte) (int, error) {
	w.tail = append(w.tail, data...)
	if len(w.tail) > w.keep {
		w.tail = w.tail[len(w.tail)-w.keep:]
	}
	return len(data), nil
}

// zeroReader gives size zero bytes.
type zeroReader struct {
	left int64
}

func (r *zeroReader) Read(data []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(data)), r.left)
	clear(data[:n])
	r.left -= n
	return int(n), nil
}

func Test_should_write_zip64_offsets_when_an_archive_passes_4_GiB(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 4.3 GB through the zip writer")
	}

	// given
	started := time.Now()
	sink := &tailWriter{keep: 1 << 16}
	archive := newZipArchive(sink, catalogDate)

	// when
	// Stored, not deflated (.gz), so the offsets really pass 4 GiB: five 900 MB entries and a
	// small one after them.
	for index := range 5 {
		require.NoError(t, archive.add(fmt.Sprintf("root/part%d.gz", index), &zeroReader{left: 900_000_000}))
	}
	require.NoError(t, archive.add("root/manifest.tsv", strings.NewReader("last\n")))
	size, _, err := archive.close()
	require.NoError(t, err)

	// then
	assert.Greater(t, size, int64(4_500_000_000))

	end := bytes.LastIndex(sink.tail, zip64EndSignature)
	require.GreaterOrEqual(t, end, 0, "zip64 end of central directory record")
	// The zip64 end record holds the central directory's offset at byte 48.
	directoryOffset := binary.LittleEndian.Uint64(sink.tail[end+48 : end+56])
	assert.Greater(t, directoryOffset, uint64(1)<<32, "the central directory starts past 4 GiB")
	t.Logf("4.5 GB zip written in %s", time.Since(started).Round(time.Millisecond))
}

func Test_should_give_every_file_a_path_of_its_own_when_names_clash(t *testing.T) {
	// given four files of one model folder: two named model.cif, one named as the second one's
	// short fallback, and one sharing the second one's short ID prefix
	layout := newArchiveLayout(uuid.MustParse("6f1c4a2e-0000-4000-8000-000000000001"))
	model := models.SelectionModel{EntryID: "dpdb_e1", ModelID: "dpdb_e1_m_001"}
	first := models.SelectionFile{ID: uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001"), Name: "model.cif"}
	squatter := models.SelectionFile{ID: uuid.MustParse("cccccccc-0000-4000-8000-000000000003"), Name: "bbbbbbbb_model.cif"}
	clashing := models.SelectionFile{ID: uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002"), Name: "model.cif"}
	samePrefix := models.SelectionFile{ID: uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000004"), Name: "model.cif"}

	// when each gets its path, and the first asks again
	paths := []string{}
	for _, file := range []models.SelectionFile{first, squatter, clashing, samePrefix, first} {
		paths = append(paths, layout.path(file, model, false))
	}

	// then no two files share a path, and a file keeps its path
	folder := "dynamic-pdb-6f1c4a2e-0000-4000-8000-000000000001/dpdb_e1/dpdb_e1_m_001/"
	assert.Equal(t, []string{
		folder + "model.cif",
		folder + "bbbbbbbb_model.cif",
		folder + "bbbbbbbb-0000-4000-8000-000000000002_model.cif",
		folder + "bbbbbbbb-0000-4000-8000-000000000004_model.cif",
		folder + "model.cif",
	}, paths)
}
