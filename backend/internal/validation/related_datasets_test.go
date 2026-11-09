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
		{Pointer: "/related_datasets/0/doi", Code: CodeRequired, Detail: "needs a DOI or an accession"},
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

func Test_should_accept_related_datasets_when_named_by_accession_with_or_without_doi(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{
			Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs", Repository: new("EMPIAR"),
			LandingURL: new("https://www.ebi.ac.uk/empiar/EMPIAR-10028/"),
		},
		{Accession: new("1303"), DataType: "diffraction image data"},
		{DOI: "10.15785/SBGRID/1303", Accession: new("1303"), DataType: "diffraction image data", Repository: new("SBGrid Data Bank")},
		{DOI: "10.5281/zenodo.1234", DataType: "other data", LandingURL: new("http://example.org:8080/data?id=1#top")},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_duplicate_accession_only_within_the_same_repository(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs", Repository: new("EMPIAR")},
		{Accession: new("empiar-10028"), DataType: "EM raw micrographs", Repository: new("empiar")},
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs", Repository: new("Zenodo")},
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs"},
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs"},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Equal(t, []FieldError{
		{Pointer: "/related_datasets/1/accession", Code: CodeDuplicate, Detail: "lists the same dataset twice"},
		{Pointer: "/related_datasets/4/accession", Code: CodeDuplicate, Detail: "lists the same dataset twice"},
	}, fieldErrors)
}

func Test_should_report_duplicate_when_the_same_accession_is_listed_twice_without_a_repository(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs"},
		{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs", Repository: new("EMPIAR")},
		{Accession: new("empiar-10028"), DataType: "EM raw micrographs"},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then: the row with a repository is a different dataset; the second row without one is not.
	assert.Equal(t, []FieldError{
		{Pointer: "/related_datasets/2/accession", Code: CodeDuplicate, Detail: "lists the same dataset twice"},
	}, fieldErrors)
}

func Test_should_report_accession_longer_than_the_label_limit(t *testing.T) {
	// given
	datasets := []models.RelatedDataset{
		{Accession: new(strings.Repeat("x", MaxRelatedDatasetLabelChars+1)), DataType: "other data"},
	}

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), datasets)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/related_datasets/0/accession", Code: CodeOutOfRange, Detail: "must be at most 256 characters",
	}}, fieldErrors)
}

func Test_should_refuse_landing_url_when_it_is_not_an_http_url_with_a_host(t *testing.T) {
	// given
	invalidURLs := []string{
		"ftp://ftp.ebi.ac.uk/empiar/world_availability/10028/",
		"javascript:alert(1)",
		"www.ebi.ac.uk/empiar/EMPIAR-10028/",
		"https://",
		"https://:443/path",
		"https:example.org",
		"https://www.ebi.ac.uk/empiar/EMPIAR 10028/",
		"https://user:secret@example.org/data",
		"https://example.org:port/",
	}

	for _, landingURL := range invalidURLs {
		// when
		fieldErrors := RelatedDatasets(Root.Field("related_datasets"), []models.RelatedDataset{
			{DOI: "10.15785/SBGRID/1303", DataType: "diffraction image data", LandingURL: new(landingURL)},
		})

		// then
		assert.Equal(t, []FieldError{{
			Pointer: "/related_datasets/0/landing_url",
			Code:    CodeInvalidFormat,
			Detail:  "must be an http or https URL such as https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
		}}, fieldErrors, "landing_url %q", landingURL)
	}
}

func Test_should_report_landing_url_longer_than_the_url_limit(t *testing.T) {
	// given
	landingURL := "https://example.org/" + strings.Repeat("a", MaxRelatedDatasetURLChars)

	// when
	fieldErrors := RelatedDatasets(Root.Field("related_datasets"), []models.RelatedDataset{
		{Accession: new("1303"), DataType: "diffraction image data", LandingURL: &landingURL},
	})

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/related_datasets/0/landing_url", Code: CodeOutOfRange, Detail: "must be at most 2048 characters",
	}}, fieldErrors)
}
