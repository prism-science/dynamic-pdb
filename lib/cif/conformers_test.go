package cif

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conformerSiteHeader is an _atom_site loop with the columns conformer counting reads.
const conformerSiteHeader = `data_TEST
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.pdbx_PDB_ins_code
_atom_site.occupancy
_atom_site.auth_seq_id
_atom_site.auth_asym_id
_atom_site.pdbx_PDB_model_num
`

// conformerAtom is one atom of conformerSiteHeader in chain A (label and author), entity 1, model 1.
type conformerAtom struct {
	group, element, atom, alt, compound, labelSeq, authSeq, occupancy string
	model                                                             int
}

func conformerText(atoms ...conformerAtom) string {
	var text strings.Builder
	text.WriteString(conformerSiteHeader)
	for id, atom := range atoms {
		group, model, authSeq := atom.group, atom.model, atom.authSeq
		if group == "" {
			group = "ATOM"
		}
		if model == 0 {
			model = 1
		}
		if authSeq == "" {
			authSeq = atom.labelSeq
		}
		fmt.Fprintf(&text, "%s %d %s %s %s %s A 1 %s ? %s %s A %d\n",
			group, id+1, atom.element, atom.atom, atom.alt, atom.compound, atom.labelSeq, atom.occupancy, authSeq, model)
	}
	return text.String()
}

// pdbConformerAtom writes one ATOM or HETATM record with an alternate location, occupancy and
// element (an empty element leaves columns 77-78 blank).
func pdbConformerAtom(record string, serial int, atom, alt, compound, chain string, residue int, occupancy float64, element string) string {
	return fmt.Sprintf("%-6s%5d %-4s%1s%3s %1s%4d%1s   %8.3f%8.3f%8.3f%6.2f%6.2f          %2s\n",
		record, serial, atom, alt, compound, chain, residue, "", 0.0, 0.0, 0.0, occupancy, 10.0, element)
}

func readConformers(t *testing.T, text string, format StructureFormat) ConformerCounts {
	t.Helper()
	return readIndex(t, text, format).Conformers
}

func conformerRows(counts ConformerCounts) []string {
	rows := make([]string, 0, len(counts.Rows))
	for _, row := range counts.Rows {
		rows = append(rows, fmt.Sprintf("%s %d x%d", row.ResidueType, row.ConformerCount, row.Residues))
	}
	return rows
}

func Test_should_count_a_letter_when_a_heavy_atom_with_it_reaches_a_tenth_occupancy(t *testing.T) {
	// given
	text := conformerText(
		// LYS 1: backbone without a letter, side chain A 0.6 / B 0.4: 2.
		conformerAtom{element: "N", atom: "N", alt: ".", compound: "LYS", labelSeq: "1", occupancy: "1.00"},
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "1", occupancy: "0.60"},
		conformerAtom{element: "C", atom: "CG", alt: "A", compound: "LYS", labelSeq: "1", occupancy: "0.60"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "1", occupancy: "0.40"},
		// LYS 2: B at 0.05 is below the floor: 1.
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "2", occupancy: "0.95"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "2", occupancy: "0.05"},
		// LYS 3: B exactly at the floor counts: 2.
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "3", occupancy: "0.90"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "3", occupancy: "0.10"},
		// LYS 4: B is 0.05 on CB but 0.2 on CG; the highest heavy atom decides: 2.
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "4", occupancy: "0.80"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "4", occupancy: "0.05"},
		conformerAtom{element: "C", atom: "CG", alt: "B", compound: "LYS", labelSeq: "4", occupancy: "0.20"},
		// LYS 5: a single letter is one position: 1.
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "5", occupancy: "0.70"},
		// LYS 6: three letters, both minor ones below the floor: 1.
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "6", occupancy: "0.90"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "6", occupancy: "0.05"},
		conformerAtom{element: "C", atom: "CB", alt: "C", compound: "LYS", labelSeq: "6", occupancy: "0.00"},
		// GLY 7: no letters: 1.
		conformerAtom{element: "C", atom: "CA", alt: ".", compound: "GLY", labelSeq: "7", occupancy: "1.00"},
	)

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"GLY 1 x1", "LYS 1 x3", "LYS 2 x3"}, conformerRows(counts))
	assert.Equal(t, 7, counts.Residues)
	assert.Equal(t, 3, counts.MultiConformerResidues)
	assert.Equal(t, ResidueKeyLabel, counts.Key)
	assert.False(t, counts.MultipleModelFrames)
}

func Test_should_ignore_a_letter_found_only_on_hydrogens_when_counting_conformers(t *testing.T) {
	// given: SER 1 has letters A and B only on hydrogens; SER 2 has A on heavy atoms and B only on
	// a deuterium; THR 3 has A and B on heavy atoms and C only on a hydrogen.
	text := conformerText(
		conformerAtom{element: "C", atom: "CB", alt: ".", compound: "SER", labelSeq: "1", occupancy: "1.00"},
		conformerAtom{element: "H", atom: "HB2", alt: "A", compound: "SER", labelSeq: "1", occupancy: "0.60"},
		conformerAtom{element: "H", atom: "HB2", alt: "B", compound: "SER", labelSeq: "1", occupancy: "0.40"},
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "SER", labelSeq: "2", occupancy: "0.60"},
		conformerAtom{element: "D", atom: "DG", alt: "B", compound: "SER", labelSeq: "2", occupancy: "0.40"},
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "THR", labelSeq: "3", occupancy: "0.50"},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "THR", labelSeq: "3", occupancy: "0.30"},
		conformerAtom{element: "H", atom: "HG1", alt: "C", compound: "THR", labelSeq: "3", occupancy: "0.20"},
	)

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"SER 1 x2", "THR 2 x1"}, conformerRows(counts))
	assert.Equal(t, 1, counts.MultiConformerResidues)
}

func Test_should_tell_hydrogens_by_atom_name_when_a_pdb_file_has_no_element_column(t *testing.T) {
	// given: no element in columns 77-78. VAL 1 has A and B on CB; LEU 2 has A and B only on
	// hydrogens named "HB2" and "1HD1".
	text := pdbConformerAtom("ATOM", 1, "CB", "A", "VAL", "A", 1, 0.5, "") +
		pdbConformerAtom("ATOM", 2, "CB", "B", "VAL", "A", 1, 0.5, "") +
		pdbConformerAtom("ATOM", 3, "CB", "", "LEU", "A", 2, 1.0, "") +
		pdbConformerAtom("ATOM", 4, "HB2", "A", "LEU", "A", 2, 0.5, "") +
		pdbConformerAtom("ATOM", 5, "1HD1", "B", "LEU", "A", 2, 0.5, "")

	// when
	counts := readConformers(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"LEU 1 x1", "VAL 2 x1"}, conformerRows(counts))
	assert.Equal(t, ResidueKeyAuthor, counts.Key)
}

func Test_should_keep_two_residue_types_at_one_position_apart_when_the_position_is_microheterogeneous(t *testing.T) {
	// given: crambin-style microheterogeneity at position 22: PRO with letter A, SER with B and C.
	mmcif := conformerText(
		conformerAtom{element: "C", atom: "CA", alt: "A", compound: "PRO", labelSeq: "22", occupancy: "0.60"},
		conformerAtom{element: "C", atom: "CA", alt: "B", compound: "SER", labelSeq: "22", occupancy: "0.20"},
		conformerAtom{element: "C", atom: "CA", alt: "C", compound: "SER", labelSeq: "22", occupancy: "0.20"},
	)
	pdb := pdbConformerAtom("ATOM", 1, "CA", "A", "PRO", "A", 22, 0.6, "C") +
		pdbConformerAtom("ATOM", 2, "CA", "B", "SER", "A", 22, 0.2, "C") +
		pdbConformerAtom("ATOM", 3, "CA", "C", "SER", "A", 22, 0.2, "C")

	for _, file := range []struct {
		text   string
		format StructureFormat
	}{{mmcif, FormatMMCIF}, {pdb, FormatPDB}} {
		// when
		counts := readConformers(t, file.text, file.format)

		// then: two residues, PRO with 1 conformer and SER with 2; never one residue with 3.
		assert.Equal(t, []string{"PRO 1 x1", "SER 2 x1"}, conformerRows(counts))
		assert.Equal(t, 2, counts.Residues)
	}
}

func Test_should_count_the_first_model_and_report_later_frames_when_a_file_has_several_models(t *testing.T) {
	// given: model 2 gives LYS 1 a second letter; only model 1 is counted.
	text := conformerText(
		conformerAtom{element: "C", atom: "CB", alt: ".", compound: "LYS", labelSeq: "1", occupancy: "1.00"},
		conformerAtom{element: "C", atom: "CB", alt: "A", compound: "LYS", labelSeq: "1", occupancy: "0.50", model: 2},
		conformerAtom{element: "C", atom: "CB", alt: "B", compound: "LYS", labelSeq: "1", occupancy: "0.50", model: 2},
	)

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"LYS 1 x1"}, conformerRows(counts))
	assert.True(t, counts.MultipleModelFrames)
}

func Test_should_count_a_letter_at_full_occupancy_when_the_file_has_no_occupancy_column(t *testing.T) {
	// given
	text := `data_TEST
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.auth_asym_id
ATOM 1 C CB A LYS A 1 1 A
ATOM 2 C CB B LYS A 1 1 A
`

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"LYS 2 x1"}, conformerRows(counts))
}

func Test_should_warn_and_stop_adding_letters_when_a_residue_has_more_than_the_letter_limit(t *testing.T) {
	// given: one residue with maxAltPositionsPerResidue + 2 letters, each at 0.05 except the
	// first two.
	atoms := []conformerAtom{}
	for index := range maxAltPositionsPerResidue + 2 {
		occupancy := "0.05"
		if index < 2 {
			occupancy = "0.40"
		}
		atoms = append(atoms, conformerAtom{element: "C", atom: "CB", alt: fmt.Sprintf("L%d", index), compound: "LYS", labelSeq: "1", occupancy: occupancy})
	}

	// when
	index := readIndex(t, conformerText(atoms...), FormatMMCIF)

	// then
	assert.Equal(t, []string{"LYS 2 x1"}, conformerRows(index.Conformers))
	assert.Contains(t, index.Warnings, fmt.Sprintf(tooManyLettersWarning, 2, maxAltPositionsPerResidue))
}

func Test_should_have_no_counts_when_the_first_model_has_no_polymer_residues(t *testing.T) {
	// given: only a ligand and water.
	text := conformerText(
		conformerAtom{group: "HETATM", element: "C", atom: "C1", alt: "A", compound: "GOL", labelSeq: ".", authSeq: "101", occupancy: "0.50"},
		conformerAtom{group: "HETATM", element: "O", atom: "O", alt: ".", compound: "HOH", labelSeq: ".", authSeq: "102", occupancy: "1.00"},
	)

	// when
	counts := readConformers(t, text, FormatMMCIF)

	// then
	assert.Empty(t, counts.Rows)
	assert.Equal(t, ResidueKey(""), counts.Key)
	assert.Zero(t, counts.Residues)
}

// conformerFixture is a real file cut to a few residues (testdata/conformers, README).
type conformerFixture struct {
	file          string
	format        StructureFormat
	rows          []string
	key           ResidueKey
	multiple      int
	severalFrames bool
}

func Test_should_count_conformers_of_every_model_representation_when_reading_real_files(t *testing.T) {
	fixtures := []conformerFixture{
		// Deposited, 2CO3: ASP 14 has A 0.99 and B 0.01 on every heavy atom: 1 conformer.
		{"deposited_2CO3.cif", FormatMMCIF, []string{"ASP 1 x1", "ILE 2 x1", "SER 1 x1", "VAL 1 x1", "VAL 2 x1"}, ResidueKeyLabel, 2, false},
		// Deposited, 7TDY: GLU 86 has A and B only on hydrogens: 1 conformer.
		{"deposited_7TDY.cif", FormatMMCIF, []string{"GLU 1 x2", "ILE 1 x1", "MET 2 x1", "VAL 1 x1"}, ResidueKeyLabel, 1, false},
		// Single Conformer, 5RGD: SER 5 and LEU 9 have letters only on hydrogens; SER 234 and
		// ILE 235 a B at 0.01 and 0.02.
		{"single_conformer_5RGD.cif", FormatMMCIF, []string{"ASP 1 x1", "GLN 2 x1", "ILE 1 x2", "LEU 1 x1", "SER 1 x2"}, ResidueKeyLabel, 1, false},
		// Multiconformer, qFit mmCIF 4HXP: hydrogen-only letters (SER 51), a heavy letter at 0.07
		// (ASN 61), several residues with 2.
		{"qfit_4hxp.cif", FormatMMCIF, []string{"ARG 2 x1", "ASN 1 x1", "ASN 2 x2", "GLN 1 x1", "LYS 1 x1", "LYS 2 x1", "PRO 2 x2", "SER 1 x1", "THR 2 x1"}, ResidueKeyLabel, 7, false},
		// Multiconformer, qFit mmCIF 3SJM: its DNA chain C has label_seq_id "?", so those residues
		// are keyed by author numbering.
		{"qfit_3sjm_dna.cif", FormatMMCIF, []string{"DC 1 x1", "DT 1 x2", "GLN 2 x1", "LYS 1 x2"}, ResidueKeyMixed, 1, false},
		// Multiconformer, qFit PDB 6LLX: SER 177 has A 0.08 and B 0.92: 1 conformer.
		{"qfit_6llx.pdb", FormatPDB, []string{"ALA 1 x1", "LEU 1 x2", "SER 1 x1", "TYR 1 x1"}, ResidueKeyAuthor, 0, false},
		// Ensemble as model frames, 5AI3: counts of the first frame, and the frames reported.
		{"ensemble_5ai3.pdb", FormatPDB, []string{"ALA 1 x1", "LYS 1 x1", "MET 1 x1", "TRP 1 x1"}, ResidueKeyAuthor, 0, true},
		// Ensemble as whole-chain altloc letters, 44PS: one frame, six letters on every residue.
		{"altloc_ensemble_44PS.cif", FormatMMCIF, []string{"GLY 6 x1", "PHE 6 x1", "SER 6 x1"}, ResidueKeyLabel, 3, false},
	}
	for _, fixture := range fixtures {
		// given
		text := readFixture(t, "conformers/"+fixture.file)

		// when
		index := readIndex(t, text, fixture.format)

		// then
		counts := index.Conformers
		assert.Equal(t, fixture.rows, conformerRows(counts), fixture.file)
		assert.Equal(t, fixture.key, counts.Key, fixture.file)
		assert.Equal(t, fixture.multiple, counts.MultiConformerResidues, fixture.file)
		assert.Equal(t, fixture.severalFrames, counts.MultipleModelFrames, fixture.file)
		residues := 0
		for _, row := range counts.Rows {
			residues += row.Residues
		}
		assert.Equal(t, counts.Residues, residues, fixture.file)
		assert.Empty(t, index.Warnings, fixture.file)
	}
}

func Test_should_read_the_same_conformers_when_one_reader_streams_both_counts(t *testing.T) {
	// given: the archive fixtures with alternate locations (193L, 1AKE) and the PDB ones.
	for _, name := range []string{"193L.cif", "1AKE.cif", "1A62.cif", "1A62.pdb", "3CEB.pdb"} {
		format := FormatMMCIF
		if strings.HasSuffix(name, ".pdb") {
			format = FormatPDB
		}
		text := readFixture(t, name)

		// when: the conformer counts and the chain rows come from one read.
		index, err := ReadCoordinateIndex(context.Background(), strings.NewReader(text), format)

		// then: every chain residue (distinct author position) is counted once per residue type.
		require.NoError(t, err, name)
		modeled := 0
		for _, chain := range index.Chains {
			modeled += chain.ModeledResidues
		}
		assert.Equal(t, modeled, index.Conformers.Residues, name)
	}
}

func Test_should_report_later_frames_when_a_later_model_holds_only_water_or_ligands(t *testing.T) {
	// given: model 1 has a lysine; model 2 only a water (mmCIF) or a glycerol (PDB format).
	mmcif := conformerText(
		conformerAtom{element: "C", atom: "CB", alt: ".", compound: "LYS", labelSeq: "1", occupancy: "1.00"},
		conformerAtom{group: "HETATM", element: "O", atom: "O", alt: ".", compound: "HOH", labelSeq: ".", authSeq: "101", occupancy: "1.00", model: 2},
	)
	pdb := "MODEL        1\n" + pdbConformerAtom("ATOM", 1, "CB", "", "LYS", "A", 1, 1.0, "C") + "ENDMDL\n" +
		"MODEL        2\n" + pdbConformerAtom("HETATM", 1, "C1", "", "GOL", "A", 301, 1.0, "C") + "ENDMDL\nEND\n"
	onlyOneModel := "MODEL        1\n" + pdbConformerAtom("ATOM", 1, "CB", "", "LYS", "A", 1, 1.0, "C") + "ENDMDL\nEND\n"

	for _, file := range []struct {
		text   string
		format StructureFormat
		want   bool
	}{{mmcif, FormatMMCIF, true}, {pdb, FormatPDB, true}, {onlyOneModel, FormatPDB, false}} {
		// when
		counts := readConformers(t, file.text, file.format)

		// then: the lysine is counted from model 1 either way.
		assert.Equal(t, file.want, counts.MultipleModelFrames)
		assert.Equal(t, []string{"LYS 1 x1"}, conformerRows(counts))
	}
}
