package cif

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readMapping(t *testing.T, text string) ResidueMapping {
	t.Helper()
	mapping, err := ReadResidueMapping(context.Background(), strings.NewReader(text))
	require.NoError(t, err)
	return mapping
}

func Test_should_map_every_residue_of_the_pdb_format_file_when_the_deposited_mmcif_gives_its_label_numbering(t *testing.T) {
	// The same entries in both formats: the PDB-format file has author numbering only, the mmCIF's
	// _pdbx_poly_seq_scheme maps it.
	for _, entry := range []string{"1A62", "3CEB", "3CLN", "6OIM", "7LTB"} {
		// given
		mapping := readMapping(t, readFixture(t, entry+".cif"))
		pdb := readIndex(t, readFixture(t, entry+".pdb"), FormatPDB)
		mmcif := readIndex(t, readFixture(t, entry+".cif"), FormatMMCIF)

		// when
		mapped := pdb.ConformersMapped(mapping)

		// then: every residue is keyed by label numbering, and the counts are the mmCIF's.
		assert.Equal(t, ResidueKeyAuthor, pdb.Conformers.Key, entry)
		assert.Equal(t, ResidueKeyMapped, mapped.Key, entry)
		assert.Zero(t, mapped.AuthorKeyedResidues, entry)
		assert.Equal(t, pdb.Conformers.Rows, mapped.Rows, "%s: a mapping renames residues, never recounts them", entry)
		assert.Equal(t, mmcif.Conformers.Rows, mapped.Rows, entry)
		assert.Equal(t, mmcif.Conformers.Residues, mapped.Residues, entry)
	}
}

func Test_should_key_residues_the_mapping_does_not_know_by_author_numbering(t *testing.T) {
	// given: a scheme for chain A residues 1 and 2 only; the file also has residue 3 and chain B.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.entity_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.auth_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 1 1 LYS 10 10 LYS A .
A 1 2 ARG 11 11 ARG A .
`)
	pdb := pdbConformerAtom("ATOM", 1, "CB", "A", "LYS", "A", 10, 0.5, "C") +
		pdbConformerAtom("ATOM", 2, "CB", "B", "LYS", "A", 10, 0.5, "C") +
		pdbConformerAtom("ATOM", 3, "CB", "", "ARG", "A", 11, 1.0, "C") +
		pdbConformerAtom("ATOM", 4, "CB", "", "GLY", "A", 12, 1.0, "C") +
		pdbConformerAtom("ATOM", 5, "CB", "", "ALA", "B", 1, 1.0, "C")
	index := readIndex(t, pdb, FormatPDB)

	// when
	mapped := index.ConformersMapped(mapping)

	// then: 2 mapped, 2 still by author numbering; the counts stay.
	assert.Equal(t, ResidueKeyMixed, mapped.Key)
	assert.Equal(t, 2, mapped.AuthorKeyedResidues)
	assert.Equal(t, []string{"ALA 1 x1", "ARG 1 x1", "GLY 1 x1", "LYS 2 x1"}, conformerRows(mapped))
}

func Test_should_leave_every_residue_by_author_numbering_when_no_residue_matches(t *testing.T) {
	// given: the scheme names another chain.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
C 1 LYS 10 LYS C .
C 2 ARG 11 ARG C .
`)
	index := readIndex(t, pdbConformerAtom("ATOM", 1, "CB", "", "LYS", "A", 10, 1.0, "C"), FormatPDB)

	// when
	mapped := index.ConformersMapped(mapping)

	// then
	assert.Equal(t, ResidueKeyAuthor, mapped.Key)
	assert.Equal(t, 1, mapped.AuthorKeyedResidues)
}

func Test_should_map_each_residue_type_of_a_microheterogeneous_position_to_its_own_label_residue(t *testing.T) {
	// given: crambin-style position 22 with PRO and SER in the scheme (same seq_id, two mon_ids).
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 22 PRO 22 PRO A .
A 22 SER 22 SER A .
`)
	pdb := pdbConformerAtom("ATOM", 1, "CA", "A", "PRO", "A", 22, 0.6, "C") +
		pdbConformerAtom("ATOM", 2, "CA", "B", "SER", "A", 22, 0.2, "C") +
		pdbConformerAtom("ATOM", 3, "CA", "C", "SER", "A", 22, 0.2, "C")
	index := readIndex(t, pdb, FormatPDB)

	// when
	mapped := index.ConformersMapped(mapping)

	// then
	assert.Equal(t, ResidueKeyMapped, mapped.Key)
	assert.Equal(t, []string{"PRO 1 x1", "SER 2 x1"}, conformerRows(mapped))
}

func Test_should_merge_a_mapped_residue_with_the_label_residue_it_names_when_a_file_has_both(t *testing.T) {
	// given: an mmCIF file writes residue 5's backbone with label numbering and its side chain
	// without (as phenix does for some rows); the scheme maps the author residue to label A 5.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 5 LYS 5 LYS A .
`)
	text := conformerText(
		conformerAtom{element: "C", atom: "CA", alt: "A", compound: "LYS", labelSeq: "5", occupancy: "0.60"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: ".", authSeq: "5", occupancy: "0.40"},
	)
	index := readIndex(t, text, FormatMMCIF)

	// when
	mapped := index.ConformersMapped(mapping)

	// then: one residue with letters A and B.
	assert.Equal(t, 2, index.Conformers.Residues, "unmapped, the two kinds of rows are two residues")
	assert.Equal(t, ResidueKeyMapped, mapped.Key)
	assert.Equal(t, []string{"LYS 2 x1"}, conformerRows(mapped))
}

func Test_should_keep_a_mapped_residue_apart_when_the_file_gives_its_label_residue_to_another_author_residue(t *testing.T) {
	// given: the deposited scheme gives author residue 10 label A 1, but this file's own label
	// numbering is shifted: its label A 1 is author residue 20, and residue 10 has no label number.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 1 LYS 10 LYS A .
`)
	text := conformerText(
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "1", authSeq: "20", occupancy: "0.60"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: ".", authSeq: "10", occupancy: "0.40"},
	)
	index := readIndex(t, text, FormatMMCIF)

	// when
	mapped := index.ConformersMapped(mapping)

	// then: two residues of one conformer each, not one residue of two; residue 10 keeps its author
	// key, and the warning says why.
	assert.Equal(t, 2, mapped.Residues)
	assert.Equal(t, 0, mapped.MultiConformerResidues)
	assert.Equal(t, []string{"LYS 1 x2"}, conformerRows(mapped))
	assert.Equal(t, ResidueKeyMixed, mapped.Key)
	assert.Equal(t, 1, mapped.AuthorKeyedResidues)
	assert.Equal(t, 1, mapped.MappingConflicts)
	assert.Equal(t, []string{"1 residues kept author numbering: the deposited mmCIF gives each the label numbering of a different residue in this file"}, mapped.MappingWarnings())
}

func Test_should_map_only_the_first_author_residue_when_the_scheme_gives_two_the_same_label_residue(t *testing.T) {
	// given: a scheme that gives author residues 5 and 6 the same label residue A 5.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 5 LYS 5 LYS A .
A 5 LYS 6 LYS A .
`)
	pdb := pdbConformerAtom("ATOM", 1, "CB", "A", "LYS", "A", 5, 0.5, "C") +
		pdbConformerAtom("ATOM", 2, "CB", "B", "LYS", "A", 5, 0.5, "C") +
		pdbConformerAtom("ATOM", 3, "CB", "", "LYS", "A", 6, 1.0, "C")
	index := readIndex(t, pdb, FormatPDB)

	// when
	mapped := index.ConformersMapped(mapping)

	// then: residue 5 (first in key order) is mapped, residue 6 stays by author numbering; the two
	// are never merged into one residue.
	assert.Equal(t, 2, mapped.Residues)
	assert.Equal(t, []string{"LYS 1 x1", "LYS 2 x1"}, conformerRows(mapped))
	assert.Equal(t, ResidueKeyMixed, mapped.Key)
	assert.Equal(t, 1, mapped.MappingConflicts)
}

func Test_should_keep_at_most_16_letters_when_a_mapped_residue_merges_with_its_label_rows(t *testing.T) {
	// given: residue 5's label rows have 16 letters (A to P) and its rows without label numbering a
	// 17th (Q); the scheme maps author residue 5 to label A 5.
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 5 LYS 5 LYS A .
`)
	atoms := make([]conformerAtom, 0, 17)
	for _, letter := range "ABCDEFGHIJKLMNOP" {
		atoms = append(atoms, conformerAtom{element: "C", atom: "CB", alt: string(letter), compound: "LYS", labelSeq: "5", occupancy: "0.20"})
	}
	atoms = append(atoms, conformerAtom{element: "C", atom: "CG", alt: "Q", compound: "LYS", labelSeq: ".", authSeq: "5", occupancy: "0.20"})
	index := readIndex(t, conformerText(atoms...), FormatMMCIF)

	// when
	mapped := index.ConformersMapped(mapping)

	// then: one residue of 16 conformers (the table allows 1 to 16); the 17th letter is left out
	// with a warning.
	assert.Equal(t, ResidueKeyMapped, mapped.Key)
	assert.Equal(t, []string{"LYS 16 x1"}, conformerRows(mapped))
	assert.Equal(t, 1, mapped.MergedLettersDropped)
	assert.Equal(t, []string{"1 alternate location letters were left out of conformer counts: a residue mapped to label numbering has more than 16 with its label rows"}, mapped.MappingWarnings())
}

func Test_should_give_no_mapping_warnings_when_counts_were_read_without_a_mapping(t *testing.T) {
	// given
	text := conformerText(conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "1", occupancy: "0.60"})

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Empty(t, counts.MappingWarnings())
}

func Test_should_leave_an_author_residue_unmapped_when_the_scheme_gives_it_two_label_residues(t *testing.T) {
	// given
	mapping := readMapping(t, `data_X
loop_
_pdbx_poly_seq_scheme.asym_id
_pdbx_poly_seq_scheme.seq_id
_pdbx_poly_seq_scheme.mon_id
_pdbx_poly_seq_scheme.pdb_seq_num
_pdbx_poly_seq_scheme.pdb_mon_id
_pdbx_poly_seq_scheme.pdb_strand_id
_pdbx_poly_seq_scheme.pdb_ins_code
A 1 LYS 10 LYS A .
B 1 LYS 10 LYS A .
`)
	index := readIndex(t, pdbConformerAtom("ATOM", 1, "CB", "", "LYS", "A", 10, 1.0, "C"), FormatPDB)

	// when
	mapped := index.ConformersMapped(mapping)

	// then
	assert.Equal(t, ResidueKeyAuthor, mapped.Key)
}

func Test_should_read_a_one_residue_scheme_when_it_is_written_as_pairs(t *testing.T) {
	// given
	text := `data_X
_pdbx_poly_seq_scheme.asym_id A
_pdbx_poly_seq_scheme.seq_id 1
_pdbx_poly_seq_scheme.mon_id GLY
_pdbx_poly_seq_scheme.pdb_seq_num 7
_pdbx_poly_seq_scheme.pdb_mon_id GLY
_pdbx_poly_seq_scheme.pdb_strand_id P
_pdbx_poly_seq_scheme.pdb_ins_code ?
`

	// when
	mapping := readMapping(t, text)

	// then
	label, known := mapping.label(authorResidue{chain: "P", seq: "7", compound: "GLY"})
	assert.True(t, known)
	assert.Equal(t, labelResidue{asymID: "A", seqID: "1"}, label)
}

func Test_should_fail_with_no_residue_mapping_when_the_file_has_no_scheme(t *testing.T) {
	// when
	_, err := ReadResidueMapping(context.Background(), strings.NewReader(readFixture(t, "conformers/deposited_2CO3.cif")))

	// then: the cut fixture keeps only _atom_site.
	require.ErrorIs(t, err, ErrNoResidueMapping)
}

func Test_should_take_altloc_fraction_from_the_conformer_counts_when_a_file_is_extracted(t *testing.T) {
	// One rule for conformer_count and altloc_fraction (researchers, 17 Nov, Q1): the extraction's
	// altloc_fraction is the coordinate index's residues with 2+ conformers over its residues.
	for _, name := range parityFiles {
		// given
		text := readFixture(t, name)
		format := FormatMMCIF
		if strings.HasSuffix(name, ".pdb") {
			format = FormatPDB
		}
		index, err := ReadCoordinateIndex(context.Background(), strings.NewReader(text), format)
		if err != nil {
			continue // a file the coordinate index refuses (two data blocks) has no counts to compare
		}

		// when
		extracted := parityExtraction(t, name)

		// then
		want, counted := index.Conformers.AltlocFraction()
		if extracted.Atoms == nil || !counted {
			continue
		}
		assert.InDelta(t, want, extracted.Atoms.AltlocFraction, 1e-12, name)
	}
}

func Test_should_count_a_modified_residue_inside_its_chain_when_extracting_altloc_fraction(t *testing.T) {
	// given: ALA 1 without letters, MSE 2 (HETATM with a label_seq_id) with A 0.5 / B 0.5.
	text := conformerText(
		conformerAtom{element: "C", atom: "CA", alt: ".", compound: "ALA", labelSeq: "1", occupancy: "1.00"},
		conformerAtom{group: "HETATM", element: "SE", atom: "SE", alt: "A", compound: "MSE", labelSeq: "2", occupancy: "0.50"},
		conformerAtom{group: "HETATM", element: "SE", atom: "SE", alt: "B", compound: "MSE", labelSeq: "2", occupancy: "0.50"},
	)

	// when
	result, err := Extract(Parse(text))

	// then: the old ATOM-only rule gave 0; MSE is a polymer residue with two conformers.
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	assert.InDelta(t, 0.5, result.Atoms.AltlocFraction, 1e-12)
}
