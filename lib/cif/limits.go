package cif

import "fmt"

// Limits that keep the memory of one parse independent of what a file contains.
// Each is far above what a real structure file needs inside the 4 MiB head the
// extractor reads (real _struct_conn rows are at least about 150 bytes, so a
// 4 MiB head holds under 30,000 of them; the largest fixtures have 28 rows).
// When a limit is hit the rest of the input is ignored and the result carries
// a warning; the parse never fails.
const (
	// maxTableRows and maxTableCells bound the rows kept per category.
	maxTableRows  = 50_000
	maxTableCells = 1_000_000
	// maxLoopColumns is the widest loop that is read; wider loops are skipped.
	maxLoopColumns = 1_000
	// maxScalarItems bounds the non-looped items kept (real files have under 700).
	maxScalarItems = 20_000
	// maxDistinctResidues, maxDistinctChains and maxDistinctInstances bound the
	// sets built while the coordinates stream by (a 4 MiB head holds under
	// 60,000 atoms).
	maxDistinctResidues  = 100_000
	maxDistinctChains    = 20_000
	maxDistinctInstances = 100_000
	// maxAltPositionsPerResidue bounds the alternate location letters tracked per residue.
	maxAltPositionsPerResidue = 16
	// maxCompoundsPerList bounds the distinct compound IDs returned in each of
	// ligands and cofactors (real entries have well under 200).
	maxCompoundsPerList = 2_000
	// maxMetalLinks bounds the distinct metalc partner pairs kept.
	maxMetalLinks = 50_000
	// maxSequenceNames bounds the distinct chain and residue name pairs kept
	// from SEQRES records (a real chain uses under 30 residue names).
	maxSequenceNames = 100_000
	// maxPDBFields bounds the REMARK fields kept from a PDB file.
	maxPDBFields = 5_000
	// maxScanLineBytes bounds one line read by the streaming scalar scan, and one
	// line or text field read by ReadModeledCompounds.
	maxScanLineBytes = 8 << 20
	// maxModeledCopies bounds the compound copies ReadModeledCompounds lists
	// from a whole file (6VXX, a glycosylated spike trimer, has 63; 5HAU has
	// 1,945).
	maxModeledCopies = 100_000
	// maxKeptValueBytes bounds the bytes of the category values
	// ReadModeledCompounds keeps from a whole file (_entity, _chem_comp and
	// single-row scheme categories; real files keep under 1 MB).
	maxKeptValueBytes = 64 << 20
	// maxCopyFieldBytes bounds each text field of a compound copy: CCD IDs have
	// up to 5 characters, chain IDs and residue numbers under 10.
	maxCopyFieldBytes = 64
	// maxReviewReasonBytes bounds a review reason that quotes a file's value.
	maxReviewReasonBytes = 240
)

// limitNotes collects each limit that was hit, once per reason.
type limitNotes struct {
	messages []string
}

func (n *limitNotes) add(format string, arguments ...any) {
	message := fmt.Sprintf(format, arguments...)
	for _, existing := range n.messages {
		if existing == message {
			return
		}
	}
	n.messages = append(n.messages, message)
}
