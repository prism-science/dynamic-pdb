package validation

import (
	"fmt"
	"slices"
	"strings"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

const (
	// MaxRelatedEntries is well above what the archive lists for one entry.
	MaxRelatedEntries          = 200
	maxRelatedEntryIDChars     = 128
	maxRelatedEntryDetailChars = 10000
)

// RelatedEntries checks a model's _pdbx_database_related rows against the dictionary's lists.
func RelatedEntries(at Pointer, entries []models.RelatedEntry) []FieldError {
	var fieldErrors []FieldError
	if len(entries) > MaxRelatedEntries {
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at,
			Code:    CodeOutOfRange,
			Detail:  fmt.Sprintf("must list at most %d related entries", MaxRelatedEntries),
		})
	}
	seen := make(map[models.RelatedEntry]bool, len(entries))
	for index, entry := range entries {
		item := at.Index(index)
		fieldErrors = append(fieldErrors, dictionaryValue(item.Field("db_name"), entry.DBName, archive.RelatedEntryDatabases)...)
		fieldErrors = append(fieldErrors, RequiredText(item.Field("db_id"), &entry.DBID)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("db_id"), &entry.DBID, maxRelatedEntryIDChars)...)
		fieldErrors = append(fieldErrors, dictionaryValue(item.Field("content_type"), entry.ContentType, archive.RelatedEntryContentTypes)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("details"), entry.Details, maxRelatedEntryDetailChars)...)
		key := models.RelatedEntry{DBName: entry.DBName, DBID: strings.ToUpper(entry.DBID), ContentType: entry.ContentType}
		if seen[key] {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: item,
				Code:    CodeDuplicate,
				Detail:  "lists the same related entry twice",
			})
		}
		seen[key] = true
	}
	return fieldErrors
}

func dictionaryValue(at Pointer, value string, allowed []string) []FieldError {
	if value == "" {
		return []FieldError{required(at)}
	}
	if !slices.Contains(allowed, value) {
		return []FieldError{{
			Pointer: at,
			Code:    CodeInvalidValue,
			Detail:  fmt.Sprintf("%q is not one of the mmCIF dictionary's values", value),
		}}
	}
	return nil
}
