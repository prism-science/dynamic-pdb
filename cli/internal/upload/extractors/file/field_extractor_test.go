package file

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func Test_should_extract_mmcif_field_from_zip_entry_when_source_is_an_archive(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	archive := new(bytes.Buffer)
	writer := zip.NewWriter(archive)
	entry, err := writer.Create("5amf/model.cif")
	require.NoError(t, err)
	_, err = entry.Write([]byte("data_X\n_refine.ls_R_factor_R_free 0.243\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	writeTestFile(t, dataRoot, "models.zip", archive.Bytes())
	extractor := NewFieldExtractor(dataRoot)

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models.zip#5amf/model.cif"}},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 0.243, value)
}

func Test_should_report_not_found_when_mmcif_source_file_is_missing(t *testing.T) {
	// given
	extractor := NewFieldExtractor(t.TempDir())

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.cif"}},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_struct.title"}},
	)

	// then
	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_should_not_hold_the_file_in_memory_when_reading_one_value_from_a_large_mmcif(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "models", "5amf_model.cif")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	file, err := os.Create(path)
	require.NoError(t, err)
	_, err = file.WriteString("data_X\n_refine.ls_R_factor_R_free 0.243\nloop_\n_atom_site.id\n_atom_site.label_atom_id\n")
	require.NoError(t, err)
	row := []byte("1 CA\n")
	block := bytes.Repeat(row, 1<<16)
	const size = 40 << 20
	for written := 0; written < size; written += len(block) {
		_, err = file.Write(block)
		require.NoError(t, err)
	}
	require.NoError(t, file.Close())
	extractor := NewFieldExtractor(dataRoot)
	var before, after runtime.MemStats

	// when
	runtime.GC()
	runtime.ReadMemStats(&before)
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.cif"}},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)
	runtime.ReadMemStats(&after)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 0.243, value)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "bytes allocated reading one scalar from a 40 MiB file")
}

func Test_should_read_pdb_remark_without_loading_the_file_when_value_is_near_the_top(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	atoms := strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 1<<18)
	writeTestFile(t, dataRoot, "models/5amf_model.pdb", []byte("REMARK   3   FREE R VALUE                     : 0.243\n"+atoms))
	extractor := NewFieldExtractor(dataRoot)
	var before, after runtime.MemStats

	// when
	runtime.GC()
	runtime.ReadMemStats(&before)
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.pdb"}},
		manifest.Extract{PDB: &manifest.ExtractRule{Field: "REMARK 3 FREE R VALUE"}},
	)
	runtime.ReadMemStats(&after)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 0.243, value)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func Test_should_fail_loudly_when_pdb_field_is_not_one_the_extractor_reads(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "models/5amf_model.pdb", []byte("REMARK   3   FREE R VALUE                     : 0.243\n"))
	extractor := NewFieldExtractor(dataRoot)

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.pdb"}},
		manifest.Extract{PDB: &manifest.ExtractRule{Field: "REMARK 3 FREE R"}},
	)

	// then
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "unknown PDB field")
}

func Test_should_finish_quickly_when_a_mmcif_file_is_one_line_of_quoted_tokens(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	text := "data_X\nloop_\n_entity.id\n" + strings.Repeat("'a' ", 1<<20) + "\n_refine.ls_R_factor_R_free 0.243\n"
	writeTestFile(t, dataRoot, "models/5amf_model.cif", []byte(text))
	extractor := NewFieldExtractor(dataRoot)

	// when
	start := time.Now()
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Files: []string{"models/{{ pdb_id }}_model.cif"}},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)
	elapsed := time.Since(start)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 0.243, value)
	assert.Less(t, elapsed, 2*time.Second)
}
