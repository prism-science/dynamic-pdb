package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	domainmodels "dynamic-pdb/backend/internal/models"
)

const archiveCoordinateURI = "https://files.rcsb.org/download/1ABC.cif"

func Test_should_need_model_sync_job_when_active_revision_is_archive_deposited_model(t *testing.T) {
	// given
	revision := archiveModelRevision(domainmodels.StructureModelTypeDeposited, "1ABC")
	linked := []domainmodels.Artifact{archiveModelArtifact(archiveCoordinateURI)}

	// when
	needed := needsArchiveModelSync(revision, linked, archiveCoordinateURI)

	// then
	assert.True(t, needed)
}

func Test_should_not_need_model_sync_job_when_model_is_not_deposited(t *testing.T) {
	// given
	revision := archiveModelRevision(domainmodels.StructureModelTypeSingleConformer, "1ABC")
	linked := []domainmodels.Artifact{archiveModelArtifact(archiveCoordinateURI)}

	// when
	needed := needsArchiveModelSync(revision, linked, archiveCoordinateURI)

	// then
	assert.False(t, needed)
}

func Test_should_not_need_model_sync_job_when_model_names_no_pdb_id(t *testing.T) {
	// given
	revision := archiveModelRevision(domainmodels.StructureModelTypeDeposited, " ")
	linked := []domainmodels.Artifact{archiveModelArtifact(archiveCoordinateURI)}

	// when
	needed := needsArchiveModelSync(revision, linked, archiveCoordinateURI)

	// then
	assert.False(t, needed)
}

func Test_should_not_need_model_sync_job_when_deposited_model_links_its_own_file_instead_of_rcsb(t *testing.T) {
	// given
	revision := archiveModelRevision(domainmodels.StructureModelTypeDeposited, "1ABC")
	linked := []domainmodels.Artifact{archiveModelArtifact("https://files.dynamicpdb.com/uploads/refined.cif")}

	// when
	needed := needsArchiveModelSync(revision, linked, archiveCoordinateURI)

	// then
	assert.False(t, needed)
}

func archiveModelRevision(modelType domainmodels.StructureModelType, pdbID string) domainmodels.ModelRevision {
	return domainmodels.ModelRevision{
		ID:      uuid.New(),
		EntryID: "dpdb_test",
		ModelID: "dpdb_test_m_001",
		Metadata: domainmodels.ModelMetadata{
			ModelType:    &modelType,
			ExternalRefs: map[domainmodels.ModelSource]string{domainmodels.ModelSourcePDB: pdbID},
		},
	}
}

func archiveModelArtifact(uri string) domainmodels.Artifact {
	return domainmodels.Artifact{ID: uuid.New(), Type: domainmodels.ArtifactTypeModel, URI: &uri}
}
