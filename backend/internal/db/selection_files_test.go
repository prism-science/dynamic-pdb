package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

// pinnedFile is a selection file as a test compares it.
type pinnedFile struct {
	id        uuid.UUID
	roles     []models.SelectionFileRole
	sha256    *string
	source    models.SelectionSHA256Source
	afterSave bool
	status    models.ChecksumStatus
}

// sha256Of is a valid SHA-256 hex string made from one hex digit, so tests can tell files apart.
func sha256Of(digit string) *string {
	value := strings.Repeat(digit, 64)
	return &value
}

// file stores an artifact with a declared SHA-256 (nil for none) and no checksum row.
func (f *modelSearchTestDB) file(
	t *testing.T,
	name string,
	level models.ArtifactLevel,
	artifactType models.ArtifactType,
	sha256 *string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `insert into artifacts (id, name, level, type, uri, sha256, format, size_bytes, created_by, created_at)
	           values ($1, $2, $3, $4, $5, $6, 'cif', 100, $7, $8)`,
		id, name, string(level), string(artifactType), "https://files.example.org/"+name, sha256, f.userID, f.tick())
	return id
}

// checksum stores the checksum job's row for a file. A verified or mismatch result was read at
// checkedAt; the bytes are in our storage, so nothing is copied.
func (f *modelSearchTestDB) checksum(
	t *testing.T,
	fileID uuid.UUID,
	status models.ChecksumStatus,
	sha256 *string,
	checkedAt time.Time,
) {
	t.Helper()
	switch status {
	case models.ChecksumStatusVerified, models.ChecksumStatusMismatch:
		f.exec(t, `insert into artifact_checksums (artifact_id, status, sha256, size_bytes, copy_status, checked_at)
		           values ($1, $2, $3, 100, 'own_storage', $4)`, fileID, string(status), sha256, checkedAt)
	case models.ChecksumStatusFailed:
		f.exec(t, `insert into artifact_checksums (artifact_id, status, last_error, checked_at)
		           values ($1, 'failed', 'not found', $2)`, fileID, checkedAt)
	case models.ChecksumStatusPending:
		f.exec(t, `insert into artifact_checksums (artifact_id) values ($1)`, fileID)
	default:
		require.FailNow(t, "unknown checksum status", string(status))
	}
}

// verifyChecksum records the checksum job verifying a pending file at checkedAt, as Record does.
func (f *modelSearchTestDB) verifyChecksum(t *testing.T, fileID uuid.UUID, sha256 *string, checkedAt time.Time) {
	t.Helper()
	f.exec(t, `update artifact_checksums
	           set status = 'verified', sha256 = $2, size_bytes = 100, copy_status = 'own_storage', checked_at = $3
	           where artifact_id = $1 and status = 'pending'`, fileID, sha256, checkedAt)
}

// testRun is a run of a model revision with its input and output files.
type testRun struct {
	inputs  []uuid.UUID
	outputs []uuid.UUID
}

// filedEntry makes an active entry whose revision has files, linked before it is published.
func (f *modelSearchTestDB) filedEntry(t *testing.T, metadata map[string]any, files ...uuid.UUID) searchEntry {
	t.Helper()
	return f.filedEntryInState(t, models.RevisionStateActive, metadata, files...)
}

// filedEntryInState is filedEntry with the revision left in state: files are linked while it is
// pending, then it moves to state.
func (f *modelSearchTestDB) filedEntryInState(
	t *testing.T,
	state models.RevisionState,
	metadata map[string]any,
	files ...uuid.UUID,
) searchEntry {
	t.Helper()
	entry := searchEntry{id: "dpdb_" + uuid.NewString()[:8], revisionID: uuid.New()}
	f.exec(t, `insert into entries (id, created_by, created_at, state) values ($1, $2, $3, 'active')`,
		entry.id, f.userID, f.tick())
	f.entryRevision(t, entry, entry.revisionID, 1, "pending", metadata)
	for _, file := range files {
		f.exec(t, `insert into entry_revision_artifacts (entry_revision_id, artifact_id) values ($1, $2)`,
			entry.revisionID, file)
	}
	f.exec(t, `update entry_revisions set state = $2 where id = $1`, entry.revisionID, string(state))
	return entry
}

// filedModel makes an active model of entry whose revision has primary as its primary file,
// files and runs linked, all before it is published.
func (f *modelSearchTestDB) filedModel(
	t *testing.T,
	entry searchEntry,
	metadata map[string]any,
	primary uuid.UUID,
	files []uuid.UUID,
	runs ...testRun,
) searchModel {
	t.Helper()
	return f.filedModelInState(t, models.RevisionStateActive, entry, metadata, primary, files, runs...)
}

// filedModelInState is filedModel with the revision left in state: files and runs are linked
// while it is pending, then it moves to state.
func (f *modelSearchTestDB) filedModelInState(
	t *testing.T,
	state models.RevisionState,
	entry searchEntry,
	metadata map[string]any,
	primary uuid.UUID,
	files []uuid.UUID,
	runs ...testRun,
) searchModel {
	t.Helper()
	f.models++
	model := searchModel{
		id:             entry.id + "_m_" + uuid.NewString()[:8],
		revisionID:     uuid.New(),
		coordinateFile: primary,
	}
	createdAt := f.tick()
	f.exec(t, `insert into models (id, entry_id, created_by, created_at, state) values ($1, $2, $3, $4, 'active')`,
		model.id, entry.id, f.userID, createdAt)
	f.exec(t, `insert into model_revisions (id, model_id, primary_artifact_id, revision_number, state, model_state,
	                                         title, metadata, created_by, created_at, updated_at, published_at)
	           values ($1, $2, $3, 1, 'pending', 'active', 'pinned files model', $4, $5, $6, $6, $6)`,
		model.revisionID, model.id, primary, jsonText(t, metadata), f.userID, createdAt)
	f.linkModelFiles(t, model.revisionID, files, runs...)
	f.exec(t, `update model_revisions set state = $2 where id = $1`, model.revisionID, string(state))
	return model
}

func (f *modelSearchTestDB) linkModelFiles(t *testing.T, revisionID uuid.UUID, files []uuid.UUID, runs ...testRun) {
	t.Helper()
	ctx := context.Background()
	for _, file := range files {
		require.NoError(t, f.database.Artifacts.AttachToModelRevision(ctx, revisionID, file))
	}
	for _, run := range runs {
		runID := uuid.New()
		f.exec(t, `insert into runs (id, name, created_by) values ($1, 'refinement', $2)`, runID, f.userID)
		require.NoError(t, f.database.Runs.AttachToModelRevision(ctx, revisionID, runID))
		for _, input := range run.inputs {
			require.NoError(t, f.database.Runs.AttachArtifact(ctx, runID, input, models.RunArtifactDirectionInput))
		}
		for _, output := range run.outputs {
			require.NoError(t, f.database.Runs.AttachArtifact(ctx, runID, output, models.RunArtifactDirectionOutput))
		}
	}
}

// pinnedFiles reads a saved selection's models with their files, by model ID.
func (f *modelSearchTestDB) pinnedFiles(t *testing.T, selectionID uuid.UUID) map[string][]pinnedFile {
	t.Helper()
	items, err := f.database.Selections.ListModels(context.Background(), selectionID, 0, 100)
	require.NoError(t, err)
	byModel := make(map[string][]pinnedFile, len(items))
	for _, item := range items {
		files := make([]pinnedFile, 0, len(item.Files))
		for _, file := range item.Files {
			files = append(files, pinnedFile{
				id:        file.ID,
				roles:     file.Roles,
				sha256:    file.RecordedSHA256,
				source:    file.RecordedSHA256Source,
				afterSave: file.RecordedAfterSave,
				status:    file.Checksum.Status,
			})
		}
		byModel[item.ModelID] = files
	}
	return byModel
}

func declaredFile(id uuid.UUID, sha256 *string, roles ...models.SelectionFileRole) pinnedFile {
	return pinnedFile{id: id, roles: roles, sha256: sha256, source: models.SelectionSHA256SourceDeclared,
		status: models.ChecksumStatusPending}
}

func Test_should_list_each_pinned_file_once_with_every_role_when_a_selection_is_read(t *testing.T) {
	// given an entry file, a primary file the run wrote, and structure factors the run read
	fixture := newModelSearchTestDB(t)
	fasta := fixture.file(t, "1abc.fasta", models.ArtifactLevelL0, models.ArtifactTypeFASTA, sha256Of("1"))
	entry := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"}, fasta)
	coordinates := fixture.file(t, "1abc_qfit.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("2"))
	structureFactors := fixture.file(t, "1abc-sf.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, sha256Of("3"))
	log := fixture.file(t, "qfit.log", models.ArtifactLevelL3, models.ArtifactTypeOther, sha256Of("4"))
	model := fixture.filedModel(t, entry, map[string]any{"model_type": "Multiconformer"}, coordinates,
		[]uuid.UUID{coordinates, structureFactors},
		testRun{inputs: []uuid.UUID{structureFactors}, outputs: []uuid.UUID{coordinates, log}})

	// when
	saved, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{model.id}, nil, nil))
	require.NoError(t, err)

	// then
	assert.Equal(t, map[string][]pinnedFile{model.id: {
		declaredFile(coordinates, sha256Of("2"), models.SelectionFileRolePrimary, models.SelectionFileRoleRunOutput),
		declaredFile(structureFactors, sha256Of("3"), models.SelectionFileRoleModel, models.SelectionFileRoleRunInput),
		declaredFile(log, sha256Of("4"), models.SelectionFileRoleRunOutput),
		declaredFile(fasta, sha256Of("1"), models.SelectionFileRoleEntry),
	}}, fixture.pinnedFiles(t, saved.ID))
}

func Test_should_name_the_recorded_sha256_by_its_source_when_a_selection_is_read(t *testing.T) {
	// given one file per checksum case, all linked to one model
	fixture := newModelSearchTestDB(t)
	anHourAgo := time.Now().Add(-time.Hour)
	entry := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"})
	declared := fixture.file(t, "declared.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("a"))
	changedAtSource := fixture.file(t, "changed.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, sha256Of("b"))
	fixture.checksum(t, changedAtSource, models.ChecksumStatusMismatch, sha256Of("c"), anHourAgo)
	checkedBefore := fixture.file(t, "checked-before.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, checkedBefore, models.ChecksumStatusVerified, sha256Of("d"), anHourAgo)
	checkedAfter := fixture.file(t, "checked-after.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, checkedAfter, models.ChecksumStatusPending, nil, time.Time{})
	checkedAtSave := fixture.file(t, "checked-at-save.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, checkedAtSave, models.ChecksumStatusPending, nil, time.Time{})
	waiting := fixture.file(t, "waiting.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, waiting, models.ChecksumStatusPending, nil, time.Time{})
	unreadable := fixture.file(t, "unreadable.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, unreadable, models.ChecksumStatusFailed, nil, anHourAgo)
	noRow := fixture.file(t, "no-row.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	sizeDiffers := fixture.file(t, "size-differs.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, sizeDiffers, models.ChecksumStatusMismatch, sha256Of("f"), anHourAgo)
	model := fixture.filedModel(t, entry, map[string]any{"model_type": "Deposited"}, declared,
		[]uuid.UUID{changedAtSource, checkedBefore, checkedAfter, checkedAtSave, waiting, unreadable, noRow, sizeDiffers})

	// when the selection is saved, then the checksum job verifies two pending files: one a
	// second after the save, one at the very moment of the save
	saved, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{model.id}, nil, nil))
	require.NoError(t, err)
	fixture.verifyChecksum(t, checkedAfter, sha256Of("e"), saved.CreatedAt.Add(time.Second))
	fixture.verifyChecksum(t, checkedAtSave, sha256Of("7"), saved.CreatedAt)

	// then
	files := fixture.pinnedFiles(t, saved.ID)[model.id]
	byID := make(map[uuid.UUID]pinnedFile, len(files))
	for _, file := range files {
		byID[file.id] = file
	}
	modelFile := models.SelectionFileRoleModel
	assert.Equal(t, map[uuid.UUID]pinnedFile{
		declared: declaredFile(declared, sha256Of("a"), models.SelectionFileRolePrimary),
		changedAtSource: {id: changedAtSource, roles: []models.SelectionFileRole{modelFile}, sha256: sha256Of("b"),
			source: models.SelectionSHA256SourceDeclared, status: models.ChecksumStatusMismatch},
		checkedBefore: {id: checkedBefore, roles: []models.SelectionFileRole{modelFile}, sha256: sha256Of("d"),
			source: models.SelectionSHA256SourceComputed, status: models.ChecksumStatusVerified},
		checkedAfter: {id: checkedAfter, roles: []models.SelectionFileRole{modelFile}, sha256: sha256Of("e"),
			source: models.SelectionSHA256SourceComputed, afterSave: true, status: models.ChecksumStatusVerified},
		checkedAtSave: {id: checkedAtSave, roles: []models.SelectionFileRole{modelFile}, sha256: sha256Of("7"),
			source: models.SelectionSHA256SourceComputed, status: models.ChecksumStatusVerified},
		waiting: {id: waiting, roles: []models.SelectionFileRole{modelFile},
			source: models.SelectionSHA256SourceNotRecorded, status: models.ChecksumStatusPending},
		unreadable: {id: unreadable, roles: []models.SelectionFileRole{modelFile},
			source: models.SelectionSHA256SourceNotRecorded, status: models.ChecksumStatusFailed},
		noRow: {id: noRow, roles: []models.SelectionFileRole{modelFile},
			source: models.SelectionSHA256SourceNotRecorded, status: models.ChecksumStatusPending},
		sizeDiffers: {id: sizeDiffers, roles: []models.SelectionFileRole{modelFile},
			source: models.SelectionSHA256SourceNotRecorded, status: models.ChecksumStatusMismatch},
	}, byID)
}

// representation is one of the model representations every item is tested on (researchers'
// global rule 4), with the files such a model has.
type representation struct {
	name     string
	metadata map[string]any
	// files makes the representation's primary file, its other files and its runs.
	files func(t *testing.T, fixture *modelSearchTestDB, label string) (uuid.UUID, []uuid.UUID, []testRun)
}

func depositedFiles(t *testing.T, fixture *modelSearchTestDB, label string) (uuid.UUID, []uuid.UUID, []testRun) {
	t.Helper()
	coordinates := fixture.file(t, label+".cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("1"))
	structureFactors := fixture.file(t, label+"-sf.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, structureFactors, models.ChecksumStatusVerified, sha256Of("2"), time.Now().Add(-time.Hour))
	return coordinates, []uuid.UUID{coordinates, structureFactors}, nil
}

func refinedFiles(t *testing.T, fixture *modelSearchTestDB, label string) (uuid.UUID, []uuid.UUID, []testRun) {
	t.Helper()
	coordinates := fixture.file(t, label+".cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("3"))
	structureFactors := fixture.file(t, label+"-sf.mtz", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, sha256Of("4"))
	return coordinates, []uuid.UUID{coordinates},
		[]testRun{{inputs: []uuid.UUID{structureFactors}, outputs: []uuid.UUID{coordinates}}}
}

var representations = []representation{
	{name: "Deposited", metadata: map[string]any{"model_type": "Deposited", "origin": "archive"}, files: depositedFiles},
	{name: "Single Conformer", metadata: map[string]any{"model_type": "Single Conformer"}, files: refinedFiles},
	{name: "Multiconformer", metadata: map[string]any{"model_type": "Multiconformer"}, files: refinedFiles},
	{name: "Ensemble", metadata: map[string]any{"model_type": "Ensemble", "member_count": 8}, files: refinedFiles},
	{name: "re-refined", metadata: map[string]any{"model_type": "Single Conformer", "purpose": "Refinement",
		"external_refs": map[string]any{"pdb": "7ABC"}}, files: refinedFiles},
}

// replaceCoordinateFile gives model a new revision as the model sync makes one: a revision in
// review with the parent's runs and files, the coordinate file replaced, then activated.
func (f *modelSearchTestDB) replaceCoordinateFile(t *testing.T, model searchModel, replacement uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	revisionID := uuid.New()
	f.exec(t, `insert into model_revisions (id, model_id, parent_revision_id, primary_artifact_id, state, model_state,
	                                         title, metadata, created_by, created_at, updated_at)
	           select $1, model_id, id, $2, 'in_review', 'active', title, metadata, created_by, $3, $3
	           from model_revisions where id = $4`, revisionID, replacement, f.tick(), model.revisionID)
	parentFiles, err := f.database.Artifacts.ModelRevisionArtifactIDs(ctx, model.revisionID)
	require.NoError(t, err)
	for _, file := range parentFiles {
		if file != model.coordinateFile {
			require.NoError(t, f.database.Artifacts.AttachToModelRevision(ctx, revisionID, file))
		}
	}
	require.NoError(t, f.database.Artifacts.AttachToModelRevision(ctx, revisionID, replacement))
	require.NoError(t, f.database.Runs.CopyModelRevisionLinks(ctx, model.revisionID, revisionID))
	_, err = f.database.Models.ActivateRevision(ctx, revisionID)
	require.NoError(t, err)
	return revisionID
}

func Test_should_keep_its_files_when_a_sync_replaces_the_coordinate_file_after_a_selection_is_saved(t *testing.T) {
	for _, shape := range representations {
		t.Run(shape.name, func(t *testing.T) {
			// given a saved selection of a model
			fixture := newModelSearchTestDB(t)
			fasta := fixture.file(t, "entry.fasta", models.ArtifactLevelL0, models.ArtifactTypeFASTA, sha256Of("0"))
			entry := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"}, fasta)
			primary, files, runs := shape.files(t, fixture, "original")
			model := fixture.filedModel(t, entry, shape.metadata, primary, files, runs...)
			saved, err := fixture.database.Selections.Freeze(context.Background(),
				freezeRequest([]string{model.id}, nil, nil))
			require.NoError(t, err)
			before := fixture.pinnedFiles(t, saved.ID)

			// when a sync replaces the coordinate file with a new revision
			replacement := fixture.file(t, "revised.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("9"))
			fixture.replaceCoordinateFile(t, model, replacement)
			resaved, err := fixture.database.Selections.Freeze(context.Background(),
				freezeRequest([]string{model.id}, nil, nil))
			require.NoError(t, err)

			// then the saved selection still names the original files and SHA-256 values
			assert.ElementsMatch(t, distinctFiles(append(append([]uuid.UUID{primary, fasta}, files...), runFiles(runs)...)),
				pinnedFileIDs(before[model.id]))
			assert.Equal(t, primary, before[model.id][0].id)
			assert.Equal(t, before, fixture.pinnedFiles(t, saved.ID))
			// and a new save names the replacement instead of the original coordinate file
			after := fixture.pinnedFiles(t, resaved.ID)[model.id]
			assert.Equal(t, replacement, after[0].id)
			assert.Equal(t, sha256Of("9"), after[0].sha256)
			for _, file := range after {
				if file.id == primary {
					// The sync copies the parent's runs, so the old file stays the output of the run
					// that made it.
					assert.Equal(t, []models.SelectionFileRole{models.SelectionFileRoleRunOutput}, file.roles)
				}
			}
		})
	}
}

// runFiles is every input and output file of runs.
func runFiles(runs []testRun) []uuid.UUID {
	files := make([]uuid.UUID, 0)
	for _, run := range runs {
		files = append(files, run.inputs...)
		files = append(files, run.outputs...)
	}
	return files
}

// distinctFiles is files with each ID once.
func distinctFiles(files []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(files))
	distinct := make([]uuid.UUID, 0, len(files))
	for _, file := range files {
		if !seen[file] {
			seen[file] = true
			distinct = append(distinct, file)
		}
	}
	return distinct
}

func pinnedFileIDs(files []pinnedFile) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(files))
	for _, file := range files {
		ids = append(ids, file.id)
	}
	return ids
}

func Test_should_list_the_primary_file_when_no_link_names_it_and_a_selection_is_read(t *testing.T) {
	// given a model whose primary file is its only file and is not linked
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	model := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})

	// when
	saved, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{model.id}, nil, nil))
	require.NoError(t, err)

	// then the primary file is listed even though no link names it
	files := fixture.pinnedFiles(t, saved.ID)[model.id]
	require.Len(t, files, 1)
	assert.Equal(t, model.coordinateFile, files[0].id)
	assert.Equal(t, []models.SelectionFileRole{models.SelectionFileRolePrimary}, files[0].roles)
}

func Test_should_page_models_with_their_own_files_when_selection_models_are_listed(t *testing.T) {
	// given three models with a file each, two of them sharing an entry and its sequence file
	fixture := newModelSearchTestDB(t)
	fasta := fixture.file(t, "shared.fasta", models.ArtifactLevelL0, models.ArtifactTypeFASTA, sha256Of("0"))
	shared := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"}, fasta)
	other := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"})
	modelFiles := make([]uuid.UUID, 0, 3)
	modelIDs := make([]string, 0, 3)
	for index, entry := range []searchEntry{shared, shared, other} {
		file := fixture.file(t, "model.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of(string(rune('1'+index))))
		model := fixture.filedModel(t, entry, map[string]any{"model_type": "Ensemble"}, file, []uuid.UUID{file})
		modelFiles = append(modelFiles, file)
		modelIDs = append(modelIDs, model.id)
	}
	saved, err := fixture.database.Selections.Freeze(context.Background(), freezeRequest(modelIDs, nil, nil))
	require.NoError(t, err)

	// when
	first, err := fixture.database.Selections.ListModels(context.Background(), saved.ID, 0, 2)
	require.NoError(t, err)
	second, err := fixture.database.Selections.ListModels(context.Background(), saved.ID, 2, 2)
	require.NoError(t, err)

	// then each model lists its own file, and the shared entry's file on both of its models
	page := slices.Concat(first, second)
	require.Len(t, page, 3)
	for index, item := range page {
		require.Equal(t, modelIDs[index], item.ModelID)
		ids := make([]uuid.UUID, 0, len(item.Files))
		for _, file := range item.Files {
			ids = append(ids, file.ID)
		}
		if index < 2 {
			assert.Equal(t, []uuid.UUID{modelFiles[index], fasta}, ids)
		} else {
			assert.Equal(t, []uuid.UUID{modelFiles[index]}, ids)
		}
	}
}

func Test_should_list_a_file_once_when_two_runs_of_a_model_wrote_it(t *testing.T) {
	// given a coordinate file that two runs of the model both list as output
	fixture := newModelSearchTestDB(t)
	entry := fixture.filedEntry(t, map[string]any{"method": "X-ray crystallography"})
	coordinates := fixture.file(t, "refined.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("1"))
	model := fixture.filedModel(t, entry, map[string]any{"model_type": "Single Conformer"}, coordinates, nil,
		testRun{outputs: []uuid.UUID{coordinates}}, testRun{outputs: []uuid.UUID{coordinates}})

	// when
	saved, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{model.id}, nil, nil))
	require.NoError(t, err)

	// then
	assert.Equal(t, map[string][]pinnedFile{model.id: {
		declaredFile(coordinates, sha256Of("1"), models.SelectionFileRolePrimary, models.SelectionFileRoleRunOutput),
	}}, fixture.pinnedFiles(t, saved.ID))
}
