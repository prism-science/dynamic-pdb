package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_should_strip_resolver_url_or_doi_prefix_when_doi_is_normalized(t *testing.T) {
	// given
	inputs := map[string]string{
		"  10.15785/SBGRID/1303 ":                "10.15785/SBGRID/1303",
		"https://doi.org/10.15785/SBGRID/1303":   "10.15785/SBGRID/1303",
		"HTTP://DX.DOI.ORG/10.15785/SBGRID/1303": "10.15785/SBGRID/1303",
		"doi: 10.15785/SBGRID/1303":              "10.15785/SBGRID/1303",
		"https://zenodo.org/records/1234":        "https://zenodo.org/records/1234",
		"":                                       "",
	}

	for input, expected := range inputs {
		// when
		normalized := NormalizeDOI(input)

		// then
		assert.Equal(t, expected, normalized, "input %q", input)
	}
}

func Test_should_trim_values_and_drop_empty_optional_ones_when_related_datasets_are_normalized(t *testing.T) {
	// given
	datasets := []RelatedDataset{{
		DOI:        " https://doi.org/10.15785/SBGRID/1303 ",
		Accession:  new(" "),
		LandingURL: new(""),
		DataType:   " diffraction image data ",
		Repository: new("  "),
		Details:    new(" Similar crystals. "),
	}, {
		Accession:  new(" EMPIAR-10028 "),
		LandingURL: new(" https://www.ebi.ac.uk/empiar/EMPIAR-10028/ "),
		DataType:   "EM raw micrographs",
	}}

	// when
	normalized := NormalizeRelatedDatasets(datasets)

	// then
	assert.Equal(t, []RelatedDataset{{
		DOI:      "10.15785/SBGRID/1303",
		DataType: "diffraction image data",
		Details:  new("Similar crystals."),
	}, {
		Accession:  new("EMPIAR-10028"),
		LandingURL: new("https://www.ebi.ac.uk/empiar/EMPIAR-10028/"),
		DataType:   "EM raw micrographs",
	}}, normalized)
}

func Test_should_leave_doi_out_of_the_stored_json_when_dataset_is_named_only_by_accession(t *testing.T) {
	// given
	dataset := RelatedDataset{Accession: new("EMPIAR-10028"), DataType: "EM raw micrographs"}

	// when
	encoded, err := json.Marshal(dataset)

	// then
	assert.NoError(t, err)
	assert.JSONEq(t, `{"accession": "EMPIAR-10028", "data_type": "EM raw micrographs"}`, string(encoded))
}

func Test_should_return_nil_when_related_dataset_list_is_empty(t *testing.T) {
	// when
	normalized := NormalizeRelatedDatasets([]RelatedDataset{})

	// then
	assert.Nil(t, normalized)
}

func Test_should_detect_related_dataset_change_when_entry_revisions_are_compared(t *testing.T) {
	// given
	revision := EntryRevision{Metadata: EntryMetadata{
		RelatedDatasets: []RelatedDataset{{DOI: "10.15785/SBGRID/1303", DataType: "diffraction image data"}},
	}}
	other := revision
	other.Metadata.RelatedDatasets = []RelatedDataset{{DOI: "10.15785/SBGRID/1304", DataType: "diffraction image data"}}

	// when
	equal := revision.HasSameData(other)

	// then
	assert.False(t, equal)
}
