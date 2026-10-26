package cif

import (
	"fmt"
	"strings"
)

const syntheticAtomSiteHeader = "loop_\n_atom_site.group_PDB\n_atom_site.id\n_atom_site.type_symbol\n" +
	"_atom_site.label_atom_id\n_atom_site.label_alt_id\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
	"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.pdbx_PDB_ins_code\n_atom_site.Cartn_x\n" +
	"_atom_site.Cartn_y\n_atom_site.Cartn_z\n_atom_site.occupancy\n_atom_site.B_iso_or_equiv\n" +
	"_atom_site.pdbx_formal_charge\n_atom_site.auth_seq_id\n_atom_site.auth_comp_id\n_atom_site.auth_asym_id\n" +
	"_atom_site.auth_atom_id\n_atom_site.pdbx_PDB_model_num\n"

// syntheticMMCIF builds an archive-style mmCIF of about targetBytes bytes: a
// small header with refinement scalars, then one large atom_site loop.
func syntheticMMCIF(targetBytes int) string {
	var builder strings.Builder
	builder.Grow(targetBytes + 4096)
	builder.WriteString("data_SYNTH\n#\n_entry.id   SYNTH\n#\n_refine.ls_R_factor_R_free   0.243\n#\n")
	builder.WriteString("_software.name PROLSQ\n#\n")
	builder.WriteString(syntheticAtomSiteHeader)
	atomNames := []string{"N", "CA", "C", "O", "CB", "CG", "CD", "NE"}
	atomID := 0
	for builder.Len() < targetBytes {
		residue := atomID/len(atomNames) + 1
		name := atomNames[atomID%len(atomNames)]
		altID := "."
		if residue%7 == 0 {
			altID = "A"
		}
		atomID++
		fmt.Fprintf(&builder, "ATOM   %-6d %-2s %-3s %s ARG %s 1 %-4d ? %.3f  %.3f  %.3f 1.00 %.2f ? %-4d ARG %s %-3s 1\n",
			atomID, name[:1], name, altID, string(rune('A'+residue/5000%26)), residue, 20.123, 30.456, 40.789, 43.14, residue,
			string(rune('A'+residue/5000%26)), name)
	}
	builder.WriteString("HETATM 99999 FE FE . HEM A 2 1 ? 1.000 2.000 3.000 1.00 10.00 ? 500 HEM A FE 1\n#\n")
	return builder.String()
}
