package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"dynamic-pdb/backend/internal/models"
)

const (
	MaxRelatedDatasets           = 20
	MaxRelatedDatasetLabelChars  = 256
	MaxRelatedDatasetDetailChars = 2000
)

// doiPattern matches a bare DOI: "10.", a registrant code of dot-separated digits, "/", and a
// suffix without spaces. The registrant code has no minimum length: RCSB's own example is
// 10.000/10002/image_data/cif.
var doiPattern = regexp.MustCompile(`^10\.[0-9]+(\.[0-9]+)*/\S+$`)

// RelatedDatasets checks an entry's related datasets after models.NormalizeRelatedDatasets.
func RelatedDatasets(at Pointer, datasets []models.RelatedDataset) []FieldError {
	var fieldErrors []FieldError
	if len(datasets) > MaxRelatedDatasets {
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at,
			Code:    CodeOutOfRange,
			Detail:  fmt.Sprintf("must list at most %d datasets", MaxRelatedDatasets),
		})
	}
	seenDOIs := make(map[string]bool, len(datasets))
	for index, dataset := range datasets {
		item := at.Index(index)
		fieldErrors = append(fieldErrors, relatedDatasetDOI(item.Field("doi"), dataset.DOI, seenDOIs)...)
		fieldErrors = append(fieldErrors, RequiredText(item.Field("data_type"), &dataset.DataType)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("data_type"), &dataset.DataType, MaxRelatedDatasetLabelChars)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("repository"), dataset.Repository, MaxRelatedDatasetLabelChars)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("details"), dataset.Details, MaxRelatedDatasetDetailChars)...)
	}
	return fieldErrors
}

func relatedDatasetDOI(at Pointer, doi string, seen map[string]bool) []FieldError {
	if doi == "" {
		return []FieldError{required(at)}
	}
	if !doiPattern.MatchString(doi) {
		return []FieldError{{
			Pointer: at,
			Code:    CodeInvalidFormat,
			Detail:  "must be a DOI such as 10.15785/SBGRID/1303",
		}}
	}
	// DOIs are case-insensitive, so 10.15785/sbgrid/1303 is the same dataset.
	key := strings.ToLower(doi)
	if seen[key] {
		return []FieldError{{
			Pointer: at,
			Code:    CodeDuplicate,
			Detail:  "lists the same dataset twice",
		}}
	}
	seen[key] = true
	return nil
}

func maxChars(at Pointer, value *string, limit int) []FieldError {
	if value == nil || utf8.RuneCountInString(*value) <= limit {
		return nil
	}
	return []FieldError{{
		Pointer: at,
		Code:    CodeOutOfRange,
		Detail:  fmt.Sprintf("must be at most %d characters", limit),
	}}
}
