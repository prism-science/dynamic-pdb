package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_accept_related_datasets_when_doi_and_data_type_are_given(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{DOI: "10.15785/SBGRID/1303", DataType: "diffraction image data", Repository: new("SBGrid Data Bank")},
		// RCSB's own example: a three-digit registrant code.
		{DOI: "10.000/10002/image_data/cif", DataType: "diffraction image data"},
		{DOI: "10.1000.10/abc", DataType: "other data"},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_each_broken_rule_with_its_pointer_when_related_datasets_are_malformed(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{DOI: "", DataType: ""},
		{DOI: "https://zenodo.org/records/1234", DataType: "raw data"},
		{DOI: "10.15785/SBGRID/1303", DataType: "diffraction image data"},
		{DOI: "10.15785/sbgrid/1303", DataType: "diffraction image data", Details: new(strings.Repeat("x", 2001))},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Equal(t, []FieldError{
		{Pointer: "/related_datasets/0/doi", Code: CodeRequired, Detail: "is required"},
		{Pointer: "/related_datasets/0/data_type", Code: CodeRequired, Detail: "must not be blank"},
		{Pointer: "/related_datasets/1/doi", Code: CodeInvalidFormat, Detail: "must be a DOI such as 10.15785/SBGRID/1303"},
		{Pointer: "/related_datasets/3/doi", Code: CodeDuplicate, Detail: "lists the same dataset twice"},
		{Pointer: "/related_datasets/3/details", Code: CodeOutOfRange, Detail: "must be at most 2000 characters"},
	}, fieldErrors)
}

func Test_should_report_list_length_when_more_than_twenty_related_datasets_are_given(t *testing.T) {
	// given
	datasets := make([]models.RelatedDataset, 0, MaxRelatedDatasets+1)
	for index := range MaxRelatedDatasets + 1 {
		datasets = append(datasets, models.RelatedDataset{
			DOI: "10.5281/zenodo." + strings.Repeat("1", index+1), DataType: "other data",
		})
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/related_datasets", Code: CodeOutOfRange, Detail: "must list at most 20 datasets",
	}}, fieldErrors)
}
