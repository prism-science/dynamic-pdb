package artifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/cif"
)

func Test_should_extract_field_from_artifact_payload(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {
			Filename: "model.cif",
			Contents: []byte("_refine.ls_R_factor_R_free 0.243\n"),
		},
	})

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}

func extractMMCIFField(t *testing.T, contents []byte, field string) (any, bool) {
	t.Helper()
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {Filename: "model.cif", Contents: contents},
	})
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: field}},
	)
	require.NoError(t, err)
	return value, ok
}

func readLibraryFixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "lib", "cif", "testdata", name))
	require.NoError(t, err)
	return contents
}

func Test_should_keep_heme_in_cofactors_not_ligands_when_extracting_from_mmcif(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "4HHB.cif")

	// when
	cofactors, cofactorsFound := extractMMCIFField(t, contents, "cofactors")
	_, ligandsFound := extractMMCIFField(t, contents, "ligands")

	// then
	require.True(t, cofactorsFound)
	assert.Equal(t, []string{"HEM"}, cofactors)
	assert.False(t, ligandsFound)
}

func Test_should_keep_catalytic_metal_in_cofactors_when_extracting_from_mmcif(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "3CLN.cif")

	// when
	cofactors, cofactorsFound := extractMMCIFField(t, contents, "cofactors")
	_, ligandsFound := extractMMCIFField(t, contents, "ligands")

	// then
	require.True(t, cofactorsFound)
	assert.Equal(t, []string{"CA"}, cofactors)
	assert.False(t, ligandsFound)
}

func Test_should_report_empty_cofactors_when_mmcif_has_no_cofactors(t *testing.T) {
	// given
	contents := readLibraryFixture(t, "1AKE.cif")

	// when
	cofactors, found := extractMMCIFField(t, contents, "cofactors")

	// then
	require.True(t, found)
	assert.Equal(t, []string{}, cofactors)
}

func Test_should_omit_atom_counts_when_read_limit_cuts_exactly_on_a_row_boundary(t *testing.T) {
	// given
	header := "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n_atom_site.pdbx_PDB_model_num\n"
	row := "ATOM C CA ALA A 1 1\n"
	remainder := (structureReadLimit - len(header)) % len(row)
	if remainder < 2 {
		remainder += len(row)
	}
	padding := "#" + strings.Repeat(" ", remainder-2) + "\n"
	body := padding + header
	rows := (structureReadLimit - len(body)) / len(row)
	text := body + strings.Repeat(row, rows)
	require.Equal(t, structureReadLimit, len(text))
	contents := []byte(text + row + row)
	untruncated, err := cif.MMCIFFields(text)
	require.NoError(t, err)

	// when
	_, atomCountFound := extractMMCIFField(t, contents, "atom_count")
	_, residuesFound := extractMMCIFField(t, contents, "modeled_residues")
	_, altlocFound := extractMMCIFField(t, contents, "altloc_fraction")

	// then
	assert.Contains(t, untruncated, "atom_count")
	assert.False(t, atomCountFound)
	assert.False(t, residuesFound)
	assert.False(t, altlocFound)
}
