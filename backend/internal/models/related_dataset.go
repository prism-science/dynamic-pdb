package models

import "strings"

// RelatedDataset is a dataset kept elsewhere that relates to the entry, referenced by DOI, as
// mmCIF _pdbx_related_exp_data_set records it (data_reference, data_set_type, db_source,
// details). We store no copy and no checksum of it.
type RelatedDataset struct {
	DOI        string  `json:"doi"`
	DataType   string  `json:"data_type"`
	Repository *string `json:"repository,omitempty"`
	Details    *string `json:"details,omitempty"`
}

// doiPrefixes are the ways people paste a DOI that are not the DOI itself.
var doiPrefixes = []string{
	"https://doi.org/",
	"http://doi.org/",
	"https://dx.doi.org/",
	"http://dx.doi.org/",
	"doi:",
}

// NormalizeDOI trims a DOI and strips a resolver URL or "doi:" in front of it, so
// "https://doi.org/10.15785/SBGRID/1303" is stored as "10.15785/SBGRID/1303".
func NormalizeDOI(value string) string {
	doi := strings.TrimSpace(value)
	for _, prefix := range doiPrefixes {
		if len(doi) >= len(prefix) && strings.EqualFold(doi[:len(prefix)], prefix) {
			return strings.TrimSpace(doi[len(prefix):])
		}
	}
	return doi
}

// NormalizeRelatedDatasets trims every value, normalizes DOIs and drops empty optional values.
// An empty list becomes nil: an entry stores related datasets only when it has some.
func NormalizeRelatedDatasets(datasets []RelatedDataset) []RelatedDataset {
	if len(datasets) == 0 {
		return nil
	}
	normalized := make([]RelatedDataset, 0, len(datasets))
	for _, dataset := range datasets {
		normalized = append(normalized, RelatedDataset{
			DOI:        NormalizeDOI(dataset.DOI),
			DataType:   strings.TrimSpace(dataset.DataType),
			Repository: nonEmptyTrimmed(dataset.Repository),
			Details:    nonEmptyTrimmed(dataset.Details),
		})
	}
	return normalized
}

func nonEmptyTrimmed(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
