package cif

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// ResidueMapping gives residues written with author numbering their label numbering, as a deposited
// mmCIF's _pdbx_poly_seq_scheme states it: (pdb_strand_id, pdb_seq_num, pdb_ins_code, pdb_mon_id) →
// (asym_id, seq_id). The researchers asked for it on 17 Nov (Q2): residues of PDB-format files are
// keyed by label numbering through the entry's deposited mmCIF, by author numbering only where that
// fails. A residue name is part of the match, so two residue types at one position
// (microheterogeneity) map to their own label residues.
type ResidueMapping struct {
	byAuthor map[authorResidue]labelResidue
}

// labelResidue is a residue's label chain and label number.
type labelResidue struct {
	asymID, seqID string
}

// Len is the number of author residues the mapping knows.
func (m ResidueMapping) Len() int {
	return len(m.byAuthor)
}

// label returns the label numbering of an author residue; false when the mapping does not know it
// or knows it twice.
func (m ResidueMapping) label(residue authorResidue) (labelResidue, bool) {
	label, known := m.byAuthor[residue]
	return label, known && label.asymID != ""
}

// ErrNoResidueMapping marks a file without a usable _pdbx_poly_seq_scheme.
var ErrNoResidueMapping = errors.New("no _pdbx_poly_seq_scheme")

// ReadResidueMapping reads the _pdbx_poly_seq_scheme of an mmCIF file in one streamed pass. Rows
// without a strand, number, residue name, label chain or label number are skipped; an author residue
// that two rows give different label residues is ambiguous and left out (label returns false). It
// fails with ErrNoResidueMapping when the file has no usable rows, and with ErrCoordinateIndexLimit
// past maxSequencePositions rows.
func ReadResidueMapping(ctx context.Context, source io.Reader) (ResidueMapping, error) {
	sink := &polySeqSchemeSink{mapping: ResidueMapping{byAuthor: map[authorResidue]labelResidue{}}}
	started := false
	sinks := map[string]func(columns []string) rowSink{
		polySeqSchemeCategory: func(columns []string) rowSink {
			started = true
			sink.columns(columns)
			return sink
		},
	}
	document, err := parseStream(ctx, source, parseOptions{
		onlyTables:      map[string]struct{}{},
		rowSinks:        sinks,
		keptValueBudget: maxKeptValueBytes,
	})
	if err != nil {
		return ResidueMapping{}, fmt.Errorf("read mmCIF: %w", err)
	}
	if table := document.Table(polySeqSchemeCategory); table != nil {
		if !table.Complete {
			return ResidueMapping{}, fmt.Errorf(cutOffFileError, polySeqSchemeCategory)
		}
		if !started {
			// A one-residue polymer is written as pairs, not a loop.
			sinks[polySeqSchemeCategory](table.Columns)
			for _, row := range table.Rows {
				sink.add(row)
			}
		}
	}
	if sink.overflow {
		return ResidueMapping{}, fmt.Errorf("more than %d %s rows: %w", maxSequencePositions, polySeqSchemeCategory, ErrCoordinateIndexLimit)
	}
	if sink.mapping.Len() == 0 {
		return ResidueMapping{}, ErrNoResidueMapping
	}
	return sink.mapping, nil
}

const polySeqSchemeCategory = "_pdbx_poly_seq_scheme"

// polySeqSchemeSink turns _pdbx_poly_seq_scheme rows into the mapping as they are read.
type polySeqSchemeSink struct {
	column struct {
		asymID, seqID, strand, pdbSeq, insCode, pdbCompound int
	}
	mapping  ResidueMapping
	rows     int
	overflow bool
}

func (s *polySeqSchemeSink) columns(columns []string) {
	s.column.asymID = slices.Index(columns, "asym_id")
	s.column.seqID = slices.Index(columns, "seq_id")
	s.column.strand = slices.Index(columns, "pdb_strand_id")
	s.column.pdbSeq = slices.Index(columns, "pdb_seq_num")
	s.column.insCode = slices.Index(columns, "pdb_ins_code")
	s.column.pdbCompound = slices.Index(columns, "pdb_mon_id")
}

func (s *polySeqSchemeSink) add(row []string) {
	if s.overflow {
		return
	}
	author := authorResidue{
		chain:    cell(row, s.column.strand),
		seq:      cell(row, s.column.pdbSeq),
		insCode:  cell(row, s.column.insCode),
		compound: strings.ToUpper(cell(row, s.column.pdbCompound)),
	}
	label := labelResidue{asymID: cell(row, s.column.asymID), seqID: cell(row, s.column.seqID)}
	if author.chain == "" || author.seq == "" || author.compound == "" || label.asymID == "" || label.seqID == "" ||
		!plainCopyText(author.chain, author.seq, author.insCode, author.compound, label.asymID, label.seqID) {
		return
	}
	if s.rows >= maxSequencePositions {
		s.overflow = true
		return
	}
	s.rows++
	known, seen := s.mapping.byAuthor[author]
	switch {
	case !seen:
		author = authorResidue{
			chain: strings.Clone(author.chain), seq: strings.Clone(author.seq),
			insCode: strings.Clone(author.insCode), compound: author.compound,
		}
		s.mapping.byAuthor[author] = labelResidue{asymID: strings.Clone(label.asymID), seqID: strings.Clone(label.seqID)}
	case known != label:
		// Two label residues for one author residue: ambiguous, never mapped.
		s.mapping.byAuthor[author] = labelResidue{}
	default:
		// The same row again.
	}
}
