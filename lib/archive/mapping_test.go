package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/lib/rcsb"
)

func readFixture[T any](t *testing.T, name string) T {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var value T
	require.NoError(t, json.Unmarshal(contents, &value))
	return value
}

func Test_should_map_release_and_revision_fields_when_entry_is_released(t *testing.T) {
	// given
	details := readFixture[rcsb.EntryDetails](t, "entry_4HHB.json")

	// when
	mapping := MapEntry(details)

	// then
	require.NotNil(t, mapping.InitialReleaseDate)
	assert.Equal(t, time.Date(1984, 7, 17, 0, 0, 0, 0, time.UTC), mapping.InitialReleaseDate.UTC())
	require.NotNil(t, mapping.LatestRevisionDate)
	assert.True(t, mapping.LatestRevisionDate.After(*mapping.InitialReleaseDate))
	require.NotNil(t, mapping.LatestVersion)
	assert.Regexp(t, `^\d+\.\d+$`, *mapping.LatestVersion)
	assert.Equal(t, "REL", stringOrEmpty(mapping.StatusCode))
	assert.Contains(t, mapping.RevisionGroups, RevisionGroup{Group: "Advisory", RevisionOrdinal: 3})
	assert.Equal(t, []string{"1HHB"}, mapping.Supersedes)
	assert.Empty(t, mapping.SupersededBy)
	assert.Equal(t, "rcsb_accession_info.initial_release_date", mapping.Provenance["initial_release_date"])
	assert.Equal(t, "rcsb_accession_info.revision_date", mapping.Provenance["latest_revision_date"])
	assert.Equal(t, "pdbx_database_PDB_obs_spr", mapping.Provenance["supersedes"])
}

func Test_should_record_provenance_for_every_field_when_entry_is_mapped(t *testing.T) {
	// given
	details := readFixture[rcsb.EntryDetails](t, "entry_6LU7.json")

	// when
	mapping := MapEntry(details)

	// then
	assert.Equal(t, "struct.title", mapping.Provenance["title"])
	assert.Equal(t, "rcsb_entry_info.resolution_combined[0]", mapping.Provenance["resolution"])
	assert.Equal(t, "exptl[0].method", mapping.Provenance["method"])
	assert.Equal(t, "symmetry.space_group_name_H_M", mapping.Provenance["space_group"])
	assert.Equal(t, "exptl_crystal,exptl_crystal_grow,diffrn", mapping.Provenance["crystallography"])
	require.NotNil(t, mapping.Method)
	assert.Equal(t, MethodXRayCrystallography, *mapping.Method)
}

func Test_should_report_obsolete_entry_when_obsolete_record_names_replacement(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		AccessionInfo: rcsb.EntryAccessionInfo{StatusCode: "OBS"},
		ObsoleteRecords: []rcsb.EntryObsoleteRecord{
			{ID: "OBSLTE", PDBID: "1ABC", ReplacePDBID: "7XYZ", Date: "2020-01-02T00:00:00.000+00:00"},
		},
	}

	// when
	mapping := MapEntry(details)

	// then
	assert.Equal(t, []string{"7XYZ"}, mapping.SupersededBy)
	assert.Empty(t, mapping.Supersedes)
	assert.Contains(t, mapping.Warnings, Warning{Field: "status_code", Detail: `RCSB status is "OBS", not released`})
}

func Test_should_warn_when_obsolete_record_kind_is_unknown(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		ObsoleteRecords: []rcsb.EntryObsoleteRecord{{ID: "MYSTERY", PDBID: "1ABC", ReplacePDBID: "7XYZ"}},
	}

	// when
	mapping := MapEntry(details)

	// then
	assert.Empty(t, mapping.Supersedes)
	assert.Empty(t, mapping.SupersededBy)
	assert.Contains(t, mapping.Warnings, Warning{Field: "supersedes", Detail: `unknown obsolete record kind "MYSTERY"`})
}

func Test_should_warn_when_release_date_is_malformed(t *testing.T) {
	// given
	details := rcsb.EntryDetails{AccessionInfo: rcsb.EntryAccessionInfo{InitialReleaseDate: "yesterday"}}

	// when
	mapping := MapEntry(details)

	// then
	assert.Nil(t, mapping.InitialReleaseDate)
	require.Len(t, mapping.Warnings, 1)
	assert.Equal(t, "initial_release_date", mapping.Warnings[0].Field)
}

func Test_should_report_conflict_when_entry_has_several_methods(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		Experiments: []rcsb.EntryExperiment{{Method: "X-RAY DIFFRACTION"}, {Method: "ELECTRON MICROSCOPY"}},
		Info:        rcsb.EntryInfo{CombinedResolution: []float64{2.0, 3.5}},
	}

	// when
	mapping := MapEntry(details)

	// then
	require.NotNil(t, mapping.Method)
	assert.Equal(t, MethodXRayCrystallography, *mapping.Method)
	assert.Contains(t, mapping.Conflicts, Conflict{
		Field: "method", Chosen: "X-RAY DIFFRACTION", Others: []string{"ELECTRON MICROSCOPY"},
	})
	assert.Contains(t, mapping.Conflicts, Conflict{Field: "resolution", Chosen: "2", Others: []string{"3.5"}})
}

func Test_should_warn_when_method_is_not_recognized(t *testing.T) {
	// given
	details := rcsb.EntryDetails{Experiments: []rcsb.EntryExperiment{{Method: "NEUTRON DIFFRACTION"}}}

	// when
	mapping := MapEntry(details)

	// then
	require.NotNil(t, mapping.Method)
	assert.Equal(t, "NEUTRON DIFFRACTION", *mapping.Method)
	assert.Contains(t, mapping.Warnings, Warning{Field: "method", Detail: `unrecognized experimental method "NEUTRON DIFFRACTION" kept as reported`})
}

func Test_should_report_conflict_when_refinements_disagree(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		Refinements: []rcsb.EntryRefinement{
			{RFree: new(0.21), RWork: new(0.18)},
			{RFree: new(0.25), RWork: new(0.18)},
		},
	}

	// when
	mapping := MapModel(details)

	// then
	assert.Equal(t, []Metric{{Key: MetricKeyRFree, Value: 0.21}, {Key: MetricKeyRWork, Value: 0.18}}, mapping.Metrics)
	assert.Equal(t, []Conflict{{Field: "metrics.r_free", Chosen: "0.21", Others: []string{"0.25"}}}, mapping.Conflicts)
	assert.Equal(t, "refine[0].ls_R_factor_R_free", mapping.Provenance["metrics.r_free"])
}

func Test_should_map_model_fields_with_provenance_when_model_is_mapped(t *testing.T) {
	// given
	details := readFixture[rcsb.EntryDetails](t, "entry_4HHB.json")

	// when
	mapping := MapModel(details)

	// then
	assert.Equal(t, []string{"Fermi, G.", "Perutz, M.F."}, mapping.Authors)
	require.NotNil(t, mapping.AtomCount)
	assert.Equal(t, 4779, *mapping.AtomCount)
	assert.Equal(t, []string{"HEM"}, mapping.Ligands)
	assert.Equal(t, "audit_author[].name", mapping.Provenance["authors"])
	assert.Equal(t, "rcsb_entry_info.deposited_atom_count", mapping.Provenance["atom_count"])
}

func Test_should_map_alignment_uniprot_source_when_accession_has_no_reference_identifier(t *testing.T) {
	// given
	details := rcsb.PolymerEntityDetails{
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{
			ReferenceSequenceIdentifiers: []rcsb.ReferenceSequenceIdentifier{
				{DatabaseAccession: "P12345", DatabaseName: "UniProt", ProvenanceSource: "SIFTS"},
			},
		},
		Alignments: []rcsb.PolymerEntityAlignment{
			{ProvenanceSource: "SIFTS", ReferenceDatabaseName: "UniProt", ReferenceDatabaseAccession: "P12345"},
			{ProvenanceSource: "SIFTS", ReferenceDatabaseName: "UniProt", ReferenceDatabaseAccession: "Q99999"},
			{ProvenanceSource: "SIFTS", ReferenceDatabaseName: "GenBank", ReferenceDatabaseAccession: "X1"},
		},
	}

	// when
	mapping := MapPolymerEntity(details, "1", new("2026_03"))

	// then
	assert.Equal(t, []UniProtMapping{
		{Accession: "P12345", Source: UniProtSourceSIFTS, UniProtRelease: new("2026_03")},
	}, mapping.UniProtMappings)
	assert.Equal(t, []UniProtMapping{
		{Accession: "Q99999", Source: UniProtSourceAlignment, UniProtRelease: new("2026_03")},
	}, mapping.AlignmentMappings)
}

func Test_should_warn_when_alignment_mapping_has_no_uniprot_release(t *testing.T) {
	// given
	details := rcsb.PolymerEntityDetails{
		Alignments: []rcsb.PolymerEntityAlignment{
			{ProvenanceSource: "SIFTS", ReferenceDatabaseName: "UniProt", ReferenceDatabaseAccession: "Q99999"},
		},
	}

	// when
	mapping := MapPolymerEntity(details, "1", nil)

	// then
	require.Len(t, mapping.AlignmentMappings, 1)
	assert.Nil(t, mapping.AlignmentMappings[0].UniProtRelease)
	assert.Contains(t, mapping.Warnings, Warning{
		Field: "alignment_mappings", Detail: `UniProt release is unknown for alignment mapping "Q99999"`,
	})
}

func Test_should_warn_when_reference_provenance_is_not_supported(t *testing.T) {
	// given
	details := rcsb.PolymerEntityDetails{
		Identifiers: rcsb.PolymerEntityContainerIdentifiers{
			ReferenceSequenceIdentifiers: []rcsb.ReferenceSequenceIdentifier{
				{DatabaseAccession: "P12345", DatabaseName: "UniProt", ProvenanceSource: "OTHER"},
			},
		},
	}

	// when
	mapping := MapPolymerEntity(details, "1", nil)

	// then
	assert.Empty(t, mapping.UniProtMappings)
	assert.Contains(t, mapping.Warnings, Warning{
		Field: "uniprot_mappings", Detail: `unsupported UniProt provenance "OTHER" for accession "P12345"`,
	})
}

func Test_should_parse_insertion_code_when_author_sequence_mapping_has_one(t *testing.T) {
	// given
	mapping := []string{"?", "-2", "52A"}

	// when
	missingPosition, missingInsertionCode := authSequencePosition(mapping, 1)
	negativePosition, negativeInsertionCode := authSequencePosition(mapping, 2)
	insertedPosition, insertionCode := authSequencePosition(mapping, 3)

	// then
	assert.Nil(t, missingPosition)
	assert.Nil(t, missingInsertionCode)
	assert.Equal(t, -2, *negativePosition)
	assert.Nil(t, negativeInsertionCode)
	assert.Equal(t, 52, *insertedPosition)
	assert.Equal(t, "A", *insertionCode)
}

func Test_should_map_component_ids_when_polymer_sequence_has_modified_residue(t *testing.T) {
	// given
	polymer := rcsb.PolymerData{Sequence: "A(SEP)L", Type: "polypeptide(L)"}

	// when
	components := polymerComponentIDs(polymer)

	// then
	assert.Equal(t, map[int]string{1: "ALA", 2: "SEP", 3: "LEU"}, components)
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
