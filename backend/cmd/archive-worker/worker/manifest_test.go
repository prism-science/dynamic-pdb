package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_leave_relation_cells_empty_when_related_dataset_states_none(t *testing.T) {
	// given
	dataset := models.RelatedDataset{DOI: "10.15785/SBGRID/1303", DataType: "diffraction image data"}

	// when
	row := relatedDatasetRow(dataset)

	// then
	assert.Empty(t, row["relation"])
	assert.Empty(t, row["link_evidence"])
	assert.Equal(t, "10.15785/SBGRID/1303", row["dataset_id"])
}

func Test_should_end_with_the_relation_columns_so_earlier_columns_keep_their_positions(t *testing.T) {
	// given: the columns before Schema v0.3.0, in their positions.
	before := []string{
		"record_type", "entry_id", "entry_revision", "model_id", "model_revision", "model_type", "pdb_id",
		"file_id", "file_name", "path_in_archive", "roles", "type", "level", "format", "size_bytes", "size_source",
		"recorded_sha256", "sha256_source", "recorded_after_save", "verified_at_save", "status", "reason",
		"reason_code", "verification", "verified_at", "source", "source_url", "license", "license_url", "authors",
		"affiliation", "citation_link", "dataset_id",
	}

	// when
	columns := manifestColumns

	// then
	assert.Equal(t, before, columns[:len(before)])
	assert.Equal(t, []string{"relation", "link_evidence"}, columns[len(before):])
}
