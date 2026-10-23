package cif

import "strconv"

const connTypeMetalCoordination = "metalc"

func readConnections(document *Document) []Connection {
	table := document.Table("_struct_conn")
	if table == nil {
		return nil
	}
	connections := make([]Connection, 0, len(table.Rows))
	for _, row := range table.Rows {
		connection := Connection{
			ID:              table.Value(row, "id"),
			ConnTypeID:      table.Value(row, "conn_type_id"),
			LeavingAtomFlag: table.Value(row, "pdbx_leaving_atom_flag"),
			PDBID:           table.Value(row, "pdbx_pdb_id"),
			Partner1:        readPartner(table, row, "ptnr1", "pdbx_ptnr1"),
			Partner2:        readPartner(table, row, "ptnr2", "pdbx_ptnr2"),
			Symmetry1:       table.Value(row, "ptnr1_symmetry"),
			Symmetry2:       table.Value(row, "ptnr2_symmetry"),
			ValueOrder:      table.Value(row, "pdbx_value_order"),
			Details:         table.Value(row, "details"),
		}
		if raw := table.Value(row, "pdbx_dist_value"); raw != "" {
			if distance, err := strconv.ParseFloat(raw, 64); err == nil {
				connection.DistanceAngstroms = &distance
			}
		}
		connections = append(connections, connection)
	}
	return connections
}

func readPartner(table *Table, row []string, prefix string, extraPrefix string) Partner {
	return Partner{
		LabelAsymID: table.Value(row, prefix+"_label_asym_id"),
		LabelCompID: table.Value(row, prefix+"_label_comp_id"),
		LabelSeqID:  table.Value(row, prefix+"_label_seq_id"),
		LabelAtomID: table.Value(row, prefix+"_label_atom_id"),
		LabelAltID:  table.Value(row, extraPrefix+"_label_alt_id"),
		AuthAsymID:  table.Value(row, prefix+"_auth_asym_id"),
		AuthCompID:  table.Value(row, prefix+"_auth_comp_id"),
		AuthSeqID:   table.Value(row, prefix+"_auth_seq_id"),
		InsCode:     table.Value(row, extraPrefix+"_PDB_ins_code"),
	}
}

func readSequenceDifferences(document *Document) []SequenceDifference {
	table := document.Table("_struct_ref_seq_dif")
	if table == nil {
		return nil
	}
	differences := make([]SequenceDifference, 0, len(table.Rows))
	for _, row := range table.Rows {
		differences = append(differences, SequenceDifference{
			AlignID:        table.Value(row, "align_id"),
			PDBIDCode:      table.Value(row, "pdbx_pdb_id_code"),
			MonID:          table.Value(row, "mon_id"),
			StrandID:       table.Value(row, "pdbx_pdb_strand_id"),
			SeqNum:         table.Value(row, "seq_num"),
			InsCode:        table.Value(row, "pdbx_pdb_ins_code"),
			SeqDBName:      table.Value(row, "pdbx_seq_db_name"),
			SeqDBAccession: table.Value(row, "pdbx_seq_db_accession_code"),
			DBMonID:        table.Value(row, "db_mon_id"),
			SeqDBSeqNum:    table.Value(row, "pdbx_seq_db_seq_num"),
			Details:        table.Value(row, "details"),
			AuthSeqNum:     table.Value(row, "pdbx_auth_seq_num"),
			Ordinal:        table.Value(row, "pdbx_ordinal"),
		})
	}
	return differences
}
