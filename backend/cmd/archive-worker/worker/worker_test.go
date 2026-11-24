package worker

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

var (
	catalogDate = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	checkedAt   = time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
)

// archiveFixture is a selection of six models over two entries, one of each representation the
// researchers test (Deposited, Single Conformer, Multiconformer, Ensemble, re-refined), with files
// in every state the checksum job records.
type archiveFixture struct {
	bucket *fakeBucket
	store  *fakeStore
	// files by a short name, for assertions.
	files map[string]models.SelectionFile
	// content of each file by short name.
	content map[string]string
}

// fileSpec describes one pinned file of the fixture.
type fileSpec struct {
	name     string
	fileType models.ArtifactType
	level    models.ArtifactLevel
	roles    []models.SelectionFileRole
	content  string
	// where: "upload" (our bucket), "rcsb" (RCSB URI with our copy), "changed" (RCSB, changed at
	// source), "over_limit" (RCSB over 1 GiB), "pending" (RCSB, not checked yet).
	where string
	uri   string
}

func (f *archiveFixture) file(key string, spec fileSpec) models.SelectionFile {
	id := uuid.New()
	recorded := sha256Hex(spec.content)
	size := int64(len(spec.content))
	file := models.SelectionFile{
		ID:                   id,
		Roles:                spec.roles,
		Name:                 spec.name,
		Level:                spec.level,
		Type:                 spec.fileType,
		Format:               ptr("cif"),
		RecordedSHA256:       &recorded,
		RecordedSHA256Source: models.SelectionSHA256SourceDeclared,
		SizeBytes:            &size,
	}
	switch spec.where {
	case "upload":
		uploadKey := "dpdb/artifacts/" + id.String() + "/" + spec.name
		file.URI = ptr(bucketBase + uploadKey)
		f.bucket.put(uploadKey, []byte(spec.content))
		file.Checksum = models.ArtifactChecksum{
			Status: models.ChecksumStatusVerified, SHA256: &recorded, SizeBytes: &size,
			CopyStatus: ptr(models.CopyStatusOwnStorage), CheckedAt: &checkedAt,
		}
	case "rcsb":
		copyKey := "dpdb/copies/" + id.String() + "/" + spec.name
		file.URI = ptr(spec.uri)
		f.bucket.put(copyKey, []byte(spec.content))
		file.Checksum = models.ArtifactChecksum{
			Status: models.ChecksumStatusVerified, SHA256: &recorded, SizeBytes: &size,
			StoredURI: ptr(bucketBase + copyKey), CopyStatus: ptr(models.CopyStatusCopied), CheckedAt: &checkedAt,
		}
	case "changed":
		file.URI = ptr(spec.uri)
		file.Checksum = models.ArtifactChecksum{
			Status: models.ChecksumStatusMismatch, SHA256: ptr(sha256Hex("newer bytes")), SizeBytes: ptr(int64(11)),
			CopyStatus: ptr(models.CopyStatusMismatch), CheckedAt: &checkedAt,
		}
	case "over_limit":
		file.URI = ptr(spec.uri)
		big := int64(2 << 30)
		file.SizeBytes = &big
		file.Checksum = models.ArtifactChecksum{
			Status: models.ChecksumStatusVerified, SHA256: &recorded, SizeBytes: &big,
			CopyStatus: ptr(models.CopyStatusOverLimit), CheckedAt: &checkedAt,
		}
	case "pending":
		file.URI = ptr(spec.uri)
		file.Checksum = models.ArtifactChecksum{Status: models.ChecksumStatusPending}
	default:
		panic("unknown fixture file location: " + spec.where)
	}
	f.files[key] = file
	f.content[key] = spec.content
	return file
}

// withRoles is the same pinned file listed with other roles, as a second model lists a shared file.
func withRoles(file models.SelectionFile, roles ...models.SelectionFileRole) models.SelectionFile {
	file.Roles = roles
	return file
}

func newArchiveFixture(t *testing.T, choice models.DownloadFileChoice) *archiveFixture {
	t.Helper()
	f := &archiveFixture{bucket: newFakeBucket(), files: map[string]models.SelectionFile{}, content: map[string]string{}}
	primary := []models.SelectionFileRole{models.SelectionFileRolePrimary}
	model := []models.SelectionFileRole{models.SelectionFileRoleModel}
	entry := []models.SelectionFileRole{models.SelectionFileRoleEntry}

	fasta1 := f.file("fasta1", fileSpec{name: "1abc.fasta", fileType: models.ArtifactTypeFASTA, level: models.ArtifactLevelL1,
		roles: entry, content: ">1ABC\nMKV\n", where: "rcsb", uri: "https://www.rcsb.org/fasta/entry/1ABC"})
	coord1 := f.file("coord1", fileSpec{name: "1abc.cif", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: primary, content: "data_1ABC\n_atom_site.id 1\n", where: "rcsb", uri: "https://files.rcsb.org/download/1ABC.cif"})
	sf1 := f.file("sf1", fileSpec{name: "1abc-sf.cif", fileType: models.ArtifactTypeStructureFactors, level: models.ArtifactLevelL1,
		roles: model, content: "data_r1abcsf\n", where: "rcsb", uri: "https://files.rcsb.org/download/1ABC-sf.cif"})
	qfit := f.file("qfit", fileSpec{name: "1abc_qFit.pdb", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles:   []models.SelectionFileRole{models.SelectionFileRolePrimary, models.SelectionFileRoleRunOutput},
		content: "ATOM      1  N   MET A   1\n", where: "upload"})
	rerefined := f.file("rerefined", fileSpec{name: "2def.cif", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: primary, content: "data_2DEF\n", where: "rcsb", uri: "https://files.rcsb.org/download/2DEF.cif"})

	fasta2 := f.file("fasta2", fileSpec{name: "3ghi.fasta", fileType: models.ArtifactTypeFASTA, level: models.ArtifactLevelL1,
		roles: entry, content: ">3GHI\nGGG\n", where: "pending", uri: "https://www.rcsb.org/fasta/entry/3GHI"})
	coord2 := f.file("coord2", fileSpec{name: "3ghi.cif", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: primary, content: "data_3GHI old\n", where: "changed", uri: "https://files.rcsb.org/download/3GHI.cif"})
	sf2 := f.file("sf2", fileSpec{name: "3ghi-sf.cif", fileType: models.ArtifactTypeStructureFactors, level: models.ArtifactLevelL1,
		roles: model, content: "huge", where: "over_limit", uri: "https://files.rcsb.org/download/3GHI-sf.cif"})
	single := f.file("single", fileSpec{name: "model.cif", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: primary, content: "data_single\n", where: "upload"})
	singleInput := f.file("single_input", fileSpec{name: "model.cif", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: []models.SelectionFileRole{models.SelectionFileRoleRunInput}, content: "data_single_input\n", where: "upload"})
	log := f.file("log", fileSpec{name: "refine.log", fileType: models.ArtifactTypeOther, level: models.ArtifactLevelL3,
		roles: []models.SelectionFileRole{models.SelectionFileRoleRunOutput}, content: "refinement done\n", where: "upload"})
	ensemble := f.file("ensemble", fileSpec{name: "ens.pdb", fileType: models.ArtifactTypeModel, level: models.ArtifactLevelL2,
		roles: primary, content: "MODEL 1\nENDMDL\nMODEL 2\nENDMDL\n", where: "upload"})

	selectionID := uuid.New()
	f.store = &fakeStore{
		job: models.DownloadJob{
			ID: uuid.New(), SelectionID: selectionID, FileChoice: choice,
			Status: models.DownloadStatusQueued, MaxArchiveBytes: 10_000_000_000, PrepareRequested: true,
		},
		selection: models.Selection{ID: selectionID, CreatedAt: catalogDate},
		models: []models.SelectionModel{
			{Ordinal: 1, ModelID: "dpdb_e1_m_001", EntryID: "dpdb_e1", ModelRevisionNumber: 3, EntryRevisionNumber: 2,
				Files: []models.SelectionFile{coord1, sf1, fasta1}},
			{Ordinal: 2, ModelID: "dpdb_e1_m_002", EntryID: "dpdb_e1", ModelRevisionNumber: 1, EntryRevisionNumber: 2,
				Files: []models.SelectionFile{qfit, withRoles(sf1, models.SelectionFileRoleRunInput), fasta1}},
			{Ordinal: 3, ModelID: "dpdb_e1_m_003", EntryID: "dpdb_e1", ModelRevisionNumber: 1, EntryRevisionNumber: 2,
				Files: []models.SelectionFile{rerefined, fasta1}},
			{Ordinal: 4, ModelID: "dpdb_e2_m_001", EntryID: "dpdb_e2", ModelRevisionNumber: 1, EntryRevisionNumber: 1,
				Files: []models.SelectionFile{coord2, sf2, fasta2}},
			{Ordinal: 5, ModelID: "dpdb_e2_m_002", EntryID: "dpdb_e2", ModelRevisionNumber: 1, EntryRevisionNumber: 1,
				Files: []models.SelectionFile{single, singleInput, log, fasta2}},
			{Ordinal: 6, ModelID: "dpdb_e2_m_003", EntryID: "dpdb_e2", ModelRevisionNumber: 2, EntryRevisionNumber: 1,
				Files: []models.SelectionFile{ensemble, fasta2}},
		},
		details: map[int]models.DownloadSelectionModel{
			1: {Ordinal: 1, ModelType: ptr("Deposited"), ModelPDBID: ptr("1ABC"), EntryPDBID: ptr("1ABC"),
				Authors: []string{"Smith, R.D.", "Olson, J.S."},
				RelatedDatasets: []models.RelatedDataset{{DOI: "10.15785/SBGRID/1", DataType: "diffraction image data",
					Repository: ptr("SBGrid Data Bank")}}},
			2: {Ordinal: 2, ModelType: ptr("Multiconformer"), EntryPDBID: ptr("1ABC")},
			3: {Ordinal: 3, ModelType: ptr("Ensemble"), Origin: ptr("archive"), ModelPDBID: ptr("2DEF"), EntryPDBID: ptr("1ABC")},
			4: {Ordinal: 4, ModelType: ptr("Deposited"), ModelPDBID: ptr("3GHI"), EntryPDBID: ptr("3GHI")},
			5: {Ordinal: 5, ModelType: ptr("Single Conformer"), EntryPDBID: ptr("3GHI"), Authors: []string{"Doe, J."},
				Affiliation: ptr("Lab\twith a tab")},
			6: {Ordinal: 6, ModelType: ptr("Ensemble"), EntryPDBID: ptr("3GHI")},
		},
	}
	return f
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Lease: time.Minute, Heartbeat: 30 * time.Second, PollInterval: 10 * time.Millisecond, MaxAttempts: 3,
		PageSize: 2, ScratchDir: t.TempDir(), ArchivePrefix: "downloads/", FileReadAttempts: 2, MaxConsecutiveReadFailures: 5,
		FileTimeout: time.Minute, JobTimeout: time.Hour, ArchiveTTL: 7 * 24 * time.Hour,
	}
}

func newTestWorker(t *testing.T, f *archiveFixture, cfg Config) *Worker {
	t.Helper()
	worker, err := New(f.store, f.store, f.bucket, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	worker.firstReadPause = time.Millisecond
	return worker
}

// runOnce plans and builds the fixture's job, and requires that there was one.
func runOnce(t *testing.T, worker *Worker) {
	t.Helper()
	worked, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)
}

// readArchive opens the ready archive and returns its entries by name, in order, and the
// manifest's rows.
func readArchive(t *testing.T, f *archiveFixture) ([]string, map[string]string, []map[string]string) {
	t.Helper()
	require.NotNil(t, f.store.result, "the job did not finish: failed=%q requeued=%q blocked=%q",
		f.store.failed, f.store.requeued, f.store.blockedReason)
	content, found := f.bucket.object(f.store.result.ArchiveKey)
	require.True(t, found)
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	require.NoError(t, err)
	names := []string{}
	files := map[string]string{}
	for _, entry := range reader.File {
		opened, err := entry.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(opened)
		require.NoError(t, err)
		require.NoError(t, opened.Close())
		names = append(names, entry.Name)
		files[entry.Name] = string(data)
		assert.True(t, entry.Modified.Equal(catalogDate), "entry %s keeps the catalog date", entry.Name)
	}
	manifest, found := f.bucket.object(f.store.result.ManifestKey)
	require.True(t, found)
	assert.Equal(t, string(manifest), files[archiveRoot(f.store.job)+"/manifest.tsv"], "the stored manifest equals the zip's")
	assert.Equal(t, int64(len(content)), f.store.result.ArchiveBytes)
	assert.Equal(t, sha256Hex(string(content)), f.store.result.ArchiveSHA256)
	return names, files, parseManifest(t, string(manifest))
}

func parseManifest(t *testing.T, text string) []map[string]string {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(text))
	var header []string
	var rows []map[string]string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if header == nil {
			header = cells
			continue
		}
		require.Len(t, cells, len(header), "row %q", line)
		row := map[string]string{}
		for index, column := range header {
			row[column] = cells[index]
		}
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}

// fileRows returns the manifest's file rows of a file ID, in order.
func fileRows(rows []map[string]string, fileID uuid.UUID) []map[string]string {
	var found []map[string]string
	for _, row := range rows {
		if row["record_type"] == "file" && row["file_id"] == fileID.String() {
			found = append(found, row)
		}
	}
	return found
}

func rowsOfType(rows []map[string]string, record string) []map[string]string {
	var found []map[string]string
	for _, row := range rows {
		if row["record_type"] == record {
			found = append(found, row)
		}
	}
	return found
}

func Test_should_include_every_held_file_and_list_the_rest_when_all_files_of_five_representations_are_chosen(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	names, files, rows := readArchive(t, f)
	root := "dynamic-pdb-" + f.store.job.SelectionID.String()
	assert.Equal(t, root+"/README.txt", names[0], "README comes first")
	assert.Equal(t, root+"/manifest.tsv", names[len(names)-1], "the manifest comes last")

	included := map[string]string{
		"fasta1":    root + "/dpdb_e1/1abc.fasta",
		"coord1":    root + "/dpdb_e1/dpdb_e1_m_001/1abc.cif",
		"sf1":       root + "/dpdb_e1/dpdb_e1_m_001/1abc-sf.cif",
		"qfit":      root + "/dpdb_e1/dpdb_e1_m_002/1abc_qFit.pdb",
		"rerefined": root + "/dpdb_e1/dpdb_e1_m_003/2def.cif",
		"single":    root + "/dpdb_e2/dpdb_e2_m_002/model.cif",
		"log":       root + "/dpdb_e2/dpdb_e2_m_002/refine.log",
		"ensemble":  root + "/dpdb_e2/dpdb_e2_m_003/ens.pdb",
	}
	clashing := root + "/dpdb_e2/dpdb_e2_m_002/" + f.files["single_input"].ID.String()[:8] + "_model.cif"
	included["single_input"] = clashing
	for key, path := range included {
		assert.Equal(t, f.content[key], files[path], "%s holds its recorded bytes at %s", key, path)
		for _, row := range fileRows(rows, f.files[key].ID) {
			assert.Equal(t, "included", row["status"], key)
			assert.Equal(t, "verified", row["verification"], key)
			assert.Equal(t, path, row["path_in_archive"], key)
			assert.Equal(t, sha256Hex(f.content[key]), row["recorded_sha256"], key)
			assert.Equal(t, "CC0 1.0", row["license"], key)
		}
	}
	assert.Len(t, names, len(included)+2, "README, the included files once each, and the manifest")

	sharedRows := fileRows(rows, f.files["sf1"].ID)
	require.Len(t, sharedRows, 2, "the shared structure factors are listed for both models")
	assert.Equal(t, "dpdb_e1_m_001", sharedRows[0]["model_id"])
	assert.Equal(t, "dpdb_e1_m_002", sharedRows[1]["model_id"])
	assert.Equal(t, "run_input", sharedRows[1]["roles"])

	entryRows := fileRows(rows, f.files["fasta1"].ID)
	require.Len(t, entryRows, 1, "an entry file is listed once per entry")
	assert.Empty(t, entryRows[0]["model_id"])
	assert.Equal(t, "2", entryRows[0]["entry_revision"])
	assert.Equal(t, "pdb_archive", entryRows[0]["source"])

	assert.Equal(t, "changed-at-source", fileRows(rows, f.files["coord2"].ID)[0]["status"])
	assert.Equal(t, "mismatch", fileRows(rows, f.files["coord2"].ID)[0]["verification"])
	assert.Empty(t, fileRows(rows, f.files["coord2"].ID)[0]["path_in_archive"])
	overLimit := fileRows(rows, f.files["sf2"].ID)[0]
	assert.Equal(t, "linked", overLimit["status"])
	assert.Equal(t, "verified", overLimit["verification"])
	assert.Equal(t, checkedAt.Format(time.RFC3339), overLimit["verified_at"])
	assert.Equal(t, "https://files.rcsb.org/download/3GHI-sf.cif", overLimit["source_url"])
	pending := fileRows(rows, f.files["fasta2"].ID)[0]
	assert.Equal(t, "linked", pending["status"])
	assert.Equal(t, "not_verified", pending["verification"])

	assert.Equal(t, models.DownloadCounts{Included: 9, Linked: 2, Unavailable: 0, ChangedAtSource: 1}, f.store.result.Counts)
	var plannedBytes int64
	for key := range included {
		plannedBytes += int64(len(f.content[key]))
	}
	assert.Equal(t, plannedBytes, f.store.plannedBytes)

	datasets := rowsOfType(rows, "related_dataset")
	require.Len(t, datasets, 1, "the entry's related dataset is listed once")
	assert.Equal(t, "10.15785/SBGRID/1", datasets[0]["dataset_id"])
	assert.Equal(t, "https://doi.org/10.15785/SBGRID/1", datasets[0]["source_url"])
	assert.Equal(t, "See the repository's terms", datasets[0]["license"])

	deposited := fileRows(rows, f.files["coord1"].ID)[0]
	assert.Equal(t, "Smith, R.D.; Olson, J.S.", deposited["authors"])
	assert.Equal(t, "1ABC", deposited["pdb_id"])
	assert.Equal(t, "https://www.rcsb.org/structure/1ABC", deposited["citation_link"])
	assert.Equal(t, "3", deposited["model_revision"])
	rerefinedRow := fileRows(rows, f.files["rerefined"].ID)[0]
	assert.Equal(t, "2DEF", rerefinedRow["pdb_id"], "a re-refinement shows its own PDB ID")
	assert.Equal(t, `Lab\twith a tab`, fileRows(rows, f.files["single"].ID)[0]["affiliation"])

	// Claim 1 planned the job, claim 2 builds it.
	assert.Equal(t, "downloads/"+f.store.job.ID.String()+"/2/dynamic-pdb-"+f.store.job.SelectionID.String()+"-all_files.zip",
		f.store.result.ArchiveKey, "each attempt writes under a folder of its own")
	assert.Equal(t, "downloads/"+f.store.job.ID.String()+"/2/manifest.tsv", f.store.result.ManifestKey)
	assert.Equal(t, catalogDate.Add(0), f.store.selection.CreatedAt)
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), f.store.expiresAt, time.Minute)
	assert.Empty(t, f.bucket.aborted)
}

func Test_should_include_only_pdb_coordinate_files_when_original_mmcif_is_chosen(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	names, _, rows := readArchive(t, f)
	root := "dynamic-pdb-" + f.store.job.SelectionID.String()
	assert.Equal(t, []string{
		root + "/README.txt",
		root + "/dpdb_e1/dpdb_e1_m_001/1abc.cif",
		root + "/dpdb_e1/dpdb_e1_m_003/2def.cif",
		root + "/manifest.tsv",
	}, names)
	assert.Equal(t, "changed-at-source", fileRows(rows, f.files["coord2"].ID)[0]["status"])
	noFile := rowsOfType(rows, "no_matching_file")
	require.Len(t, noFile, 3)
	assert.Equal(t, []string{"dpdb_e1_m_002", "dpdb_e2_m_002", "dpdb_e2_m_003"},
		[]string{noFile[0]["model_id"], noFile[1]["model_id"], noFile[2]["model_id"]})
	assert.Contains(t, noFile[0]["reason"], "select that model")
	assert.Empty(t, fileRows(rows, f.files["sf1"].ID), "structure factors are not original mmCIF")
	assert.Equal(t, models.DownloadCounts{Included: 2, ChangedAtSource: 1}, f.store.result.Counts)
}

func Test_should_include_only_dynamic_pdb_coordinate_files_when_modified_coordinates_are_chosen(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceModifiedCoordinates)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	names, _, rows := readArchive(t, f)
	root := "dynamic-pdb-" + f.store.job.SelectionID.String()
	assert.Equal(t, []string{
		root + "/README.txt",
		root + "/dpdb_e1/dpdb_e1_m_002/1abc_qFit.pdb",
		root + "/dpdb_e2/dpdb_e2_m_002/model.cif",
		root + "/dpdb_e2/dpdb_e2_m_002/" + f.files["single_input"].ID.String()[:8] + "_model.cif",
		root + "/dpdb_e2/dpdb_e2_m_003/ens.pdb",
		root + "/manifest.tsv",
	}, names)
	noFile := rowsOfType(rows, "no_matching_file")
	require.Len(t, noFile, 3, "the Deposited models and the re-refinement are PDB archive models")
	assert.Equal(t, []string{"dpdb_e1_m_001", "dpdb_e1_m_003", "dpdb_e2_m_001"},
		[]string{noFile[0]["model_id"], noFile[1]["model_id"], noFile[2]["model_id"]})
	assert.Empty(t, fileRows(rows, f.files["log"].ID), "a log is not a coordinate file")
	assert.Equal(t, models.DownloadCounts{Included: 4}, f.store.result.Counts)
}

func Test_should_block_before_reading_any_file_when_an_archive_is_over_its_limit(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.job.MaxArchiveBytes = 40

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	assert.Contains(t, f.store.blockedReason, "more than the 40 bytes one archive may hold")
	assert.Greater(t, f.store.plannedBytes, int64(40))
	assert.Nil(t, f.store.result)
	assert.False(t, f.store.preparing)
	assert.Empty(t, f.bucket.writers, "nothing is written to the bucket")
}

func Test_should_build_the_archive_when_its_files_add_up_to_exactly_the_limit(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	f.store.job.MaxArchiveBytes = int64(len(f.content["coord1"]) + len(f.content["rerefined"]))

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	require.NotNil(t, f.store.result)
	assert.Equal(t, f.store.job.MaxArchiveBytes, f.store.plannedBytes)

	// and one byte less blocks it
	over := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	over.store.job.MaxArchiveBytes = int64(len(over.content["coord1"])+len(over.content["rerefined"])) - 1
	runOnce(t, newTestWorker(t, over, testConfig(t)))
	assert.NotEmpty(t, over.store.blockedReason, "one byte over the limit blocks it")
}

func Test_should_leave_the_file_out_when_our_copy_differs_from_its_record(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	copyKey, _ := f.bucket.StoredKey(*f.files["coord1"].Checksum.StoredURI)
	f.bucket.put(copyKey, []byte("data_1ABC\n_atom_site.id 2\n"))

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	names, _, rows := readArchive(t, f)
	assert.NotContains(t, strings.Join(names, "\n"), "1abc.cif")
	row := fileRows(rows, f.files["coord1"].ID)[0]
	assert.Equal(t, "unavailable", row["status"])
	assert.Equal(t, "mismatch", row["verification"])
	assert.Empty(t, row["path_in_archive"])
	assert.Equal(t, 8, f.store.result.Counts.Included, "the other files are still included")
	assert.Equal(t, 1, f.store.result.Counts.Unavailable)
}

func Test_should_report_the_file_unavailable_when_our_copy_is_missing(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	copyKey, _ := f.bucket.StoredKey(*f.files["ensemble"].URI)
	delete(f.bucket.objects, copyKey)

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	_, _, rows := readArchive(t, f)
	row := fileRows(rows, f.files["ensemble"].ID)[0]
	assert.Equal(t, "unavailable", row["status"])
	assert.Equal(t, "not_verified", row["verification"])
	assert.Contains(t, row["reason"], "missing")
}

func Test_should_list_the_file_as_linked_when_it_was_copied_after_planning(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	copied := f.files["fasta2"]
	size := int64(len(f.content["fasta2"]))
	copyKey := "dpdb/copies/" + copied.ID.String() + "/3ghi.fasta"
	f.bucket.put(copyKey, []byte(f.content["fasta2"]))
	pages := 0
	f.store.beforeList = func(_ context.Context, call int, store *fakeStore) error {
		pages++
		// Each walk reads 4 pages: 3 of 2 models and an empty one. The planning lane walks once, the
		// building lane twice (plan again, then write). The copy appears on the building lane's
		// planning read of the last page, after it passed the file, so only the writing walk sees it.
		if call == 8 {
			store.setChecksum(copied.ID, models.ArtifactChecksum{
				Status: models.ChecksumStatusVerified, SHA256: copied.RecordedSHA256, SizeBytes: &size,
				StoredURI: ptr(bucketBase + copyKey), CopyStatus: ptr(models.CopyStatusCopied), CheckedAt: &checkedAt,
			})
		}
		return nil
	}

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	_, _, rows := readArchive(t, f)
	row := fileRows(rows, copied.ID)[0]
	assert.Equal(t, "linked", row["status"])
	assert.Contains(t, row["reason"], "after the archive was planned")
	assert.Equal(t, 12, pages)
	assert.False(t, f.store.planUpdated, "the plan the building lane checked equals the preview")
}

func Test_should_abandon_the_job_when_another_attempt_took_its_lease(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.heartbeat = func() error { return db.ErrDownloadLeaseLost }
	f.store.beforeList = func(ctx context.Context, _ int, _ *fakeStore) error {
		<-ctx.Done()
		return ctx.Err()
	}
	cfg := testConfig(t)
	cfg.Heartbeat = 5 * time.Millisecond

	// when
	runOnce(t, newTestWorker(t, f, cfg))

	// then
	assert.Nil(t, f.store.result)
	assert.Empty(t, f.store.failed)
	assert.Empty(t, f.store.requeued)
	assert.False(t, f.store.released)
}

func Test_should_put_the_job_back_in_the_queue_when_the_worker_stops(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	ctx, cancel := context.WithCancel(context.Background())
	f.store.beforeList = func(_ context.Context, call int, _ *fakeStore) error {
		// The writing walk's first read: planning (1-4) and planning again (5-8) are done, and the
		// upload is open.
		if call == 9 {
			cancel()
			return context.Canceled
		}
		return nil
	}

	// when
	worker := newTestWorker(t, f, testConfig(t))
	worked, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)

	// then
	assert.True(t, f.store.released)
	assert.Equal(t, models.DownloadStatusPlanned, f.store.job.Status, "back in the building lane's queue")
	assert.Zero(t, f.store.job.FailedAttempts, "a stopped worker's attempt does not count")
	assert.Nil(t, f.store.result)
	assert.Equal(t, []string{f.bucket.writers[0]}, f.bucket.aborted, "the archive upload is aborted")
}

func Test_should_requeue_the_job_when_an_attempt_fails_and_fail_it_when_the_last_one_does(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.beforeList = func(context.Context, int, *fakeStore) error { return errors.New("database went away") }

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	assert.Equal(t, "the archive could not be written to storage", f.store.requeued)
	assert.Empty(t, f.store.failed)

	assert.Equal(t, 1, f.store.job.FailedAttempts)
	assert.Equal(t, models.DownloadStatusQueued, f.store.job.Status, "back in the planning lane's queue")

	// and the last attempt fails the job
	last := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	last.store.job.FailedAttempts = 2
	last.store.beforeList = f.store.beforeList
	runOnce(t, newTestWorker(t, last, testConfig(t)))
	assert.Equal(t, "the archive could not be written to storage", last.store.failed)
	assert.Empty(t, last.store.requeued)
}

func Test_should_fail_the_job_without_building_it_when_it_was_claimed_too_often(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.job.FailedAttempts = 3
	f.store.job.Error = ptr("the archive could not be written to storage")

	// when
	runOnce(t, newTestWorker(t, f, testConfig(t)))

	// then
	assert.Equal(t, "the archive could not be planned in 3 attempts; the last attempt failed: "+
		"the archive could not be written to storage", f.store.failed)
	assert.Zero(t, f.store.listCalls)
}

func Test_should_report_no_job_when_none_is_waiting(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.claimed = true

	// when
	worked, err := newTestWorker(t, f, testConfig(t)).RunOnce(context.Background())

	// then
	require.NoError(t, err)
	assert.False(t, worked)
}

func Test_should_include_the_file_when_a_read_fails_and_the_retry_succeeds(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	copyKey, _ := f.bucket.StoredKey(*f.files["coord1"].Checksum.StoredURI)
	f.bucket.openErrs[copyKey] = errors.New("connection reset")
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.bucket.mu.Lock()
		delete(f.bucket.openErrs, copyKey)
		f.bucket.mu.Unlock()
	}()
	cfg := testConfig(t)
	cfg.FileReadAttempts = 10
	worker := newTestWorker(t, f, cfg)
	worker.firstReadPause = 10 * time.Millisecond

	// when
	runOnce(t, worker)

	// then
	_, _, rows := readArchive(t, f)
	assert.Equal(t, "included", fileRows(rows, f.files["coord1"].ID)[0]["status"])
}

func Test_should_refuse_the_config_when_the_heartbeat_is_not_shorter_than_the_lease(t *testing.T) {
	// given
	cfg := testConfig(t)
	cfg.Heartbeat = cfg.Lease

	// when
	_, err := New(&fakeStore{}, &fakeStore{}, newFakeBucket(), cfg, nil)

	// then
	require.ErrorContains(t, err, "heartbeat must be positive and shorter than the lease")
}

func Test_should_stop_when_idle_only_when_asked_to(t *testing.T) {
	// given
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	cfg := testConfig(t)
	cfg.ExitWhenIdle = true

	// when
	require.NoError(t, newTestWorker(t, f, cfg).Run(context.Background()))

	// then
	require.NotNil(t, f.store.result, "the waiting job was built before the worker stopped")

	// and without it the worker keeps polling
	idle := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	idle.store.claimed = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	require.NoError(t, newTestWorker(t, idle, testConfig(t)).Run(ctx))
	assert.GreaterOrEqual(t, time.Since(started), 40*time.Millisecond, "without it the worker keeps polling until stopped")
}

func Test_should_try_the_read_again_and_then_list_the_file_as_read_failed_when_a_read_stalls(t *testing.T) {
	// given a copy whose read answers and then sends nothing
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	copyKey, _ := f.bucket.StoredKey(*f.files["coord1"].Checksum.StoredURI)
	f.bucket.stalled[copyKey] = true
	cfg := testConfig(t)
	cfg.FileTimeout = 20 * time.Millisecond
	started := time.Now()

	// when
	runOnce(t, newTestWorker(t, f, cfg))

	// then each read gave up after its timeout, and the archive was built without that file
	assert.Less(t, time.Since(started), 5*time.Second)
	_, _, rows := readArchive(t, f)
	row := fileRows(rows, f.files["coord1"].ID)[0]
	assert.Equal(t, "unavailable", row["status"])
	assert.Equal(t, "read_failed", row["reason_code"])
	assert.Equal(t, 1, f.store.result.Counts.Included, "the other files are still included")
}

func Test_should_requeue_the_job_when_files_fail_to_read_too_many_times_in_a_row(t *testing.T) {
	// given a bucket whose reads all stall, and a worker that takes 1 failure in a row for down
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	copyKey, _ := f.bucket.StoredKey(*f.files["coord1"].Checksum.StoredURI)
	f.bucket.stalled[copyKey] = true
	cfg := testConfig(t)
	cfg.FileTimeout = 20 * time.Millisecond
	cfg.MaxConsecutiveReadFailures = 1

	// when
	runOnce(t, newTestWorker(t, f, cfg))

	// then the whole attempt is tried again later
	assert.Equal(t, "the archive's files could not be read from Dynamic PDB's storage", f.store.requeued)
	assert.Nil(t, f.store.result)
}

func Test_should_requeue_the_job_saying_so_when_an_attempt_runs_past_its_time(t *testing.T) {
	// given a selection whose models take forever to read
	f := newArchiveFixture(t, models.DownloadFileChoiceAllFiles)
	f.store.beforeList = func(ctx context.Context, _ int, _ *fakeStore) error {
		<-ctx.Done()
		return ctx.Err()
	}
	cfg := testConfig(t)
	cfg.FileTimeout = 10 * time.Millisecond
	cfg.JobTimeout = 30 * time.Millisecond

	// when
	runOnce(t, newTestWorker(t, f, cfg))

	// then
	assert.Equal(t, "the archive took longer than one attempt may take", f.store.requeued)
	assert.False(t, f.store.released, "a timed-out attempt counts, unlike a stopped worker's")
}

// fullDisk is a scratch file whose disk takes no more bytes: every write fails as a full disk does.
type fullDisk struct {
	scratchStore
}

func (fullDisk) Write([]byte) (int, error) {
	return 0, syscall.ENOSPC
}

// countingBucket serves the same bytes at every key and counts the reads.
type countingBucket struct {
	content []byte
	opens   int
}

func (b *countingBucket) Open(context.Context, string) (io.ReadCloser, error) {
	b.opens++
	return io.NopCloser(bytes.NewReader(b.content)), nil
}

func Test_should_fail_staging_at_once_without_reading_again_when_the_scratch_disk_is_full(t *testing.T) {
	// given a scratch file on a full disk, and a bucket that serves the file
	scratch, err := newScratchFile(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, scratch.close()) })
	scratch.file = fullDisk{scratchStore: scratch.file}
	bucket := &countingBucket{content: []byte("coordinates")}

	// when
	_, err = scratch.stage(context.Background(), bucket, "models/1abc.cif", 3, time.Minute, time.Millisecond)

	// then the error is the disk's, and the bucket was read once
	require.ErrorIs(t, err, errScratchDisk)
	require.ErrorIs(t, err, syscall.ENOSPC)
	assert.Equal(t, 1, bucket.opens, "a full disk is not the read's fault, so the read is not tried again")
}

func Test_should_requeue_the_job_and_list_no_file_as_read_failed_when_the_scratch_disk_is_full(t *testing.T) {
	// given a bucket that serves every file, and a worker whose scratch disk is full
	f := newArchiveFixture(t, models.DownloadFileChoiceOriginalMMCIF)
	worker := newTestWorker(t, f, testConfig(t))
	worker.newScratch = func(dir string) (*scratchFile, error) {
		scratch, err := newScratchFile(dir)
		if err != nil {
			return nil, err
		}
		scratch.file = fullDisk{scratchStore: scratch.file}
		return scratch, nil
	}

	// when
	runOnce(t, worker)

	// then the attempt is tried again later, saying why; no archive is ready with a read_failed file
	assert.Equal(t, "the archive worker could not hold a file on its disk", f.store.requeued)
	assert.Nil(t, f.store.result)
	assert.Equal(t, 1, f.store.job.FailedAttempts)
}
