package archive

import (
	"slices"
	"strings"

	"dynamic-pdb/lib/rcsb"
)

// ContentTypeReRefinement is the _pdbx_database_related.content_type of an entry that re-refines
// the related entry's data.
const ContentTypeReRefinement = "re-refinement"

// RelatedEntryContentTypes are the values the PDBx/mmCIF dictionary (mmcif_pdbx_v50) allows for
// _pdbx_database_related.content_type.
var RelatedEntryContentTypes = []string{
	"associated EM volume",
	"associated NMR restraints",
	"associated SAS data",
	"associated structure factors",
	"complete structure",
	"consensus EM volume",
	"derivative structure",
	"ensemble",
	"focused EM volume",
	"minimized average structure",
	"native structure",
	"other",
	"other EM volume",
	"protein target sequence and/or protocol data",
	ContentTypeReRefinement,
	"representative structure",
	"split",
	"unspecified",
}

// RelatedEntryDatabases are the values the PDBx/mmCIF dictionary (mmcif_pdbx_v50) allows for
// _pdbx_database_related.db_name.
var RelatedEntryDatabases = []string{
	"BIOISIS",
	"BMCD",
	"BMRB",
	"EMDB",
	"NDB",
	"PDB",
	"PDB-Dev",
	"SASBDB",
	"TargetDB",
	"TargetTrack",
}

// RelatedEntry is one _pdbx_database_related row, under the dictionary's item names.
type RelatedEntry struct {
	DBName      string
	DBID        string
	ContentType string
	Details     *string
}

// mapRelatedEntries keeps the rows in the archive's order. A row the dictionary's lists do not
// allow is dropped with a warning, so a new wwPDB term cannot fail a sync.
func (mapping *ModelMapping) mapRelatedEntries(rows []rcsb.EntryRelatedEntry) {
	mapping.from("related_entries", "pdbx_database_related[]")
	for _, row := range rows {
		related := RelatedEntry{
			DBName:      strings.TrimSpace(row.DBName),
			DBID:        strings.TrimSpace(row.DBID),
			ContentType: strings.TrimSpace(row.ContentType),
			Details:     optionalString(strings.TrimSpace(row.Details)),
		}
		if related.DBID == "" ||
			!slices.Contains(RelatedEntryDatabases, related.DBName) ||
			!slices.Contains(RelatedEntryContentTypes, related.ContentType) {
			mapping.warn(
				"related_entries",
				"dropped related entry %q in %q with content type %q: the mmCIF dictionary does not allow it",
				related.DBID, related.DBName, related.ContentType,
			)
			continue
		}
		mapping.RelatedEntries = append(mapping.RelatedEntries, related)
	}
}
