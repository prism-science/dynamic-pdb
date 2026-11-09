package validation

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"dynamic-pdb/backend/internal/models"
)

const (
	MaxRelatedDatasets           = 20
	MaxRelatedDatasetLabelChars  = 256
	MaxRelatedDatasetDetailChars = 2000
	MaxRelatedDatasetURLChars    = 2048
)

// doiPattern matches a bare DOI: "10.", a registrant code of dot-separated digits, "/", and a
// suffix without spaces. The registrant code has no minimum length: RCSB's own example is
// 10.000/10002/image_data/cif.
var doiPattern = regexp.MustCompile(`^10\.[0-9]+(\.[0-9]+)*/\S+$`)

// relatedDatasetAccessionKey names a dataset by its accession. Accessions are compared
// case-insensitively and only within one repository: two repositories may use the same ID.
// No repository counts as a repository of its own, so the same accession twice with no
// repository is a duplicate, but once with a repository and once without is not.
type relatedDatasetAccessionKey struct {
	repository string
	accession  string
}

// relatedDatasetsSeen holds the names of the datasets checked so far, to find duplicates.
type relatedDatasetsSeen struct {
	dois       map[string]bool
	accessions map[relatedDatasetAccessionKey]bool
}

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
	seen := relatedDatasetsSeen{
		dois:       make(map[string]bool, len(datasets)),
		accessions: make(map[relatedDatasetAccessionKey]bool, len(datasets)),
	}
	for index, dataset := range datasets {
		item := at.Index(index)
		fieldErrors = append(fieldErrors, relatedDatasetName(item, dataset, seen)...)
		fieldErrors = append(fieldErrors, relatedDatasetLandingURL(item.Field("landing_url"), dataset.LandingURL)...)
		fieldErrors = append(fieldErrors, RequiredText(item.Field("data_type"), &dataset.DataType)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("data_type"), &dataset.DataType, MaxRelatedDatasetLabelChars)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("repository"), dataset.Repository, MaxRelatedDatasetLabelChars)...)
		fieldErrors = append(fieldErrors, maxChars(item.Field("details"), dataset.Details, MaxRelatedDatasetDetailChars)...)
	}
	return fieldErrors
}

// relatedDatasetName checks that a dataset is named by a DOI, an accession or both, and that
// no earlier dataset of the entry has the same DOI or the same accession in its repository.
func relatedDatasetName(item Pointer, dataset models.RelatedDataset, seen relatedDatasetsSeen) []FieldError {
	if dataset.DOI == "" && dataset.Accession == nil {
		return []FieldError{{
			Pointer: item.Field("doi"),
			Code:    CodeRequired,
			Detail:  "needs a DOI or an accession",
		}}
	}
	var fieldErrors []FieldError
	if dataset.DOI != "" {
		fieldErrors = append(fieldErrors, relatedDatasetDOI(item.Field("doi"), dataset.DOI, seen.dois)...)
	}
	if dataset.Accession != nil {
		fieldErrors = append(fieldErrors, relatedDatasetAccession(
			item.Field("accession"), *dataset.Accession, dataset.Repository, seen.accessions,
		)...)
	}
	return fieldErrors
}

func relatedDatasetDOI(at Pointer, doi string, seen map[string]bool) []FieldError {
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
		return []FieldError{duplicateRelatedDataset(at)}
	}
	seen[key] = true
	return nil
}

func relatedDatasetAccession(
	at Pointer,
	accession string,
	repository *string,
	seen map[relatedDatasetAccessionKey]bool,
) []FieldError {
	if tooLong := maxChars(at, &accession, MaxRelatedDatasetLabelChars); tooLong != nil {
		return tooLong
	}
	key := relatedDatasetAccessionKey{accession: strings.ToLower(accession)}
	if repository != nil {
		key.repository = strings.ToLower(*repository)
	}
	if seen[key] {
		return []FieldError{duplicateRelatedDataset(at)}
	}
	seen[key] = true
	return nil
}

func duplicateRelatedDataset(at Pointer) FieldError {
	return FieldError{Pointer: at, Code: CodeDuplicate, Detail: "lists the same dataset twice"}
}

func relatedDatasetLandingURL(at Pointer, landingURL *string) []FieldError {
	if landingURL == nil {
		return nil
	}
	if tooLong := maxChars(at, landingURL, MaxRelatedDatasetURLChars); tooLong != nil {
		return tooLong
	}
	if !isLandingURL(*landingURL) {
		return []FieldError{{
			Pointer: at,
			Code:    CodeInvalidFormat,
			Detail:  "must be an http or https URL such as https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
		}}
	}
	return nil
}

// isLandingURL accepts an absolute http or https URL with a host. It refuses spaces, which a
// pasted URL should not hold, and a user name or password, which the entry page would show.
func isLandingURL(value string) bool {
	if strings.ContainsFunc(value, unicode.IsSpace) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Hostname() != "" &&
		parsed.User == nil
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
