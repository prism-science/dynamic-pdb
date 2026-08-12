package file

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_json_field_from_local_file(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "metadata/5amf.json", []byte(`{"entry":{"title":"Example","values":[{"score":12}]}}`))
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"metadata/{{ pdb_id }}.json"}},
		manifest.Extract{JSON: &manifest.ExtractRule{Field: "entry.values[0].score"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, float64(12), value)
}

func Test_should_extract_csv_field_from_matching_row(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "metrics.csv", []byte("pdb_id,r_free,r_work\n1abc,0.30,0.20\n5amf,0.24,0.19\n"))
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"metrics.csv"}},
		manifest.Extract{CSV: &manifest.ExtractRule{
			Column: "r_free",
			Where:  &manifest.ExtractRule{Column: "pdb_id", Equals: "5amf"},
		}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.24", value)
}

func Test_should_treat_delimited_missing_marker_as_no_value(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "metrics.csv", []byte("pdb_id,r_free,r_work\n2age,NA,0.1464\n"))
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "2AGE",
		manifest.Source{Files: []string{"metrics.csv"}},
		manifest.Extract{CSV: &manifest.ExtractRule{
			Column: "r_free",
			Where:  &manifest.ExtractRule{Column: "pdb_id", Equals: "{{ pdb_id }}"},
		}},
	)

	// then
	require.NoError(t, err)
	require.False(t, ok)
	assert.Nil(t, value)
}

func Test_should_extract_tsv_field_from_matching_row_with_template_value(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "structure.tsv", []byte("ID\tAtom Count\tLigands\n1abc\t123\tATP\n5AMF\t456\tCL;BME\n"))
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5amf",
		manifest.Source{Files: []string{"structure.tsv"}},
		manifest.Extract{TSV: &manifest.ExtractRule{
			Column: "Ligands",
			Where:  &manifest.ExtractRule{Column: "ID", Equals: "{{ pdb_id }}"},
		}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "CL;BME", value)
}

func Test_should_extract_pdb_field_from_zip_entry(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestZip(t, dataRoot, "models.zip", map[string][]byte{
		"models/5amf_model.pdb": []byte("REMARK   3   FREE R VALUE                     : 0.243\n"),
	})
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models.zip#models/{{ pdb_id }}_model.pdb"}},
		manifest.Extract{PDB: &manifest.ExtractRule{Field: "REMARK 3 FREE R VALUE"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 0.243, value)
}

func Test_should_extract_mmcif_field_from_local_file(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "models/5amf_model.cif", []byte("_refine.ls_R_factor_R_free 0.243\n_struct.title 'Example structure'\n"))
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.cif"}},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_struct.title"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "Example structure", value)
}
