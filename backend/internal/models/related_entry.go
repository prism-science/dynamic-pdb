package models

import "strings"

// RelatedEntry is an entry in another database that relates to this model, as mmCIF
// _pdbx_database_related records it, under the dictionary's item names. For a re-refinement,
// db_id names the deposition whose data the model re-refines.
type RelatedEntry struct {
	DBName      string  `json:"db_name"`
	DBID        string  `json:"db_id"`
	ContentType string  `json:"content_type"`
	Details     *string `json:"details,omitempty"`
}

// NormalizeRelatedEntries trims every value and drops empty optional values. An empty list
// becomes nil: a model stores related entries only when it has some.
func NormalizeRelatedEntries(entries []RelatedEntry) []RelatedEntry {
	if len(entries) == 0 {
		return nil
	}
	normalized := make([]RelatedEntry, 0, len(entries))
	for _, entry := range entries {
		normalized = append(normalized, RelatedEntry{
			DBName:      strings.TrimSpace(entry.DBName),
			DBID:        strings.TrimSpace(entry.DBID),
			ContentType: strings.TrimSpace(entry.ContentType),
			Details:     nonEmptyTrimmed(entry.Details),
		})
	}
	return normalized
}
