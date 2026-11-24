package models

import "strings"

// RelatedDataset is a dataset kept elsewhere that relates to the entry, as mmCIF
// _pdbx_related_exp_data_set records it (data_reference, data_set_type, db_source, details).
// It is named by a DOI, by the repository's accession, or by both; a dataset without a DOI
// has an empty DOI, which is left out of the stored JSON. We store no copy and no checksum
// of it. Relation and LinkEvidence are Dynamic PDB extensions with no mmCIF item; absent
// means not stated.
type RelatedDataset struct {
	DOI          string                      `json:"doi,omitempty"`
	Accession    *string                     `json:"accession,omitempty"`
	LandingURL   *string                     `json:"landing_url,omitempty"`
	DataType     string                      `json:"data_type"`
	Repository   *string                     `json:"repository,omitempty"`
	Details      *string                     `json:"details,omitempty"`
	Relation     *RelatedDatasetRelation     `json:"relation,omitempty"`
	LinkEvidence *RelatedDatasetLinkEvidence `json:"link_evidence,omitempty"`
}

// RelatedDatasetRelation is how a related dataset relates to the experiment the entry
// describes.
type RelatedDatasetRelation string

const (
	// RelatedDatasetRelationSourceData: the deposited coordinates were refined against data
	// reduced from this dataset.
	RelatedDatasetRelationSourceData RelatedDatasetRelation = "source_data"
	// RelatedDatasetRelationSimilarSample: collected from comparable crystals or samples, not
	// the deposited one.
	RelatedDatasetRelationSimilarSample RelatedDatasetRelation = "similar_sample"
	// RelatedDatasetRelationOther: any other relationship, which the dataset's details state.
	RelatedDatasetRelationOther RelatedDatasetRelation = "other"
)

// RelatedDatasetLinkEvidence is what shows that a related dataset relates to the entry.
type RelatedDatasetLinkEvidence string

const (
	// RelatedDatasetLinkEvidencePDBRecord: the PDB entry cites the dataset.
	RelatedDatasetLinkEvidencePDBRecord RelatedDatasetLinkEvidence = "pdb_record"
	// RelatedDatasetLinkEvidenceRepositoryRecord: the repository's metadata names the PDB ID.
	RelatedDatasetLinkEvidenceRepositoryRecord RelatedDatasetLinkEvidence = "repository_record"
	// RelatedDatasetLinkEvidenceRepositoryText: the repository's page text names the PDB ID.
	RelatedDatasetLinkEvidenceRepositoryText RelatedDatasetLinkEvidence = "repository_text"
	// RelatedDatasetLinkEvidencePublication: a paper's data availability statement names both.
	RelatedDatasetLinkEvidencePublication RelatedDatasetLinkEvidence = "publication"
	// RelatedDatasetLinkEvidenceDepositorStatement: the depositor states it.
	RelatedDatasetLinkEvidenceDepositorStatement RelatedDatasetLinkEvidence = "depositor_statement"
)

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
			DOI:          NormalizeDOI(dataset.DOI),
			Accession:    nonEmptyTrimmed(dataset.Accession),
			LandingURL:   nonEmptyTrimmed(dataset.LandingURL),
			DataType:     strings.TrimSpace(dataset.DataType),
			Repository:   nonEmptyTrimmed(dataset.Repository),
			Details:      nonEmptyTrimmed(dataset.Details),
			Relation:     nonEmptyTrimmedValue(dataset.Relation),
			LinkEvidence: nonEmptyTrimmedValue(dataset.LinkEvidence),
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

// nonEmptyTrimmedValue is nonEmptyTrimmed for a vocabulary value.
func nonEmptyTrimmedValue[T ~string](value *T) *T {
	if value == nil {
		return nil
	}
	trimmed := T(strings.TrimSpace(string(*value)))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
