// Package downloadfiles holds the rules a bulk download applies to each file of a saved
// selection that both the archive worker and the API need, so an archive's manifest and the
// selection's model list never disagree: where a file's recorded location points, and the terms
// it may be reused under (researchers' Q7).
package downloadfiles

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

// Source is where a file's recorded location points.
type Source string

const (
	// SourceDynamicPDBUpload: a file in Dynamic PDB's own storage.
	SourceDynamicPDBUpload Source = "dynamic_pdb_upload"
	// SourcePDBArchive: a file the PDB archive serves.
	SourcePDBArchive Source = "pdb_archive"
	// SourceExternal: a file another repository holds.
	SourceExternal Source = "external"
)

// pdbArchiveHosts serve the PDB archive's files: coordinates and structure factors from
// files.rcsb.org, sequences from www.rcsb.org.
var pdbArchiveHosts = map[string]bool{"files.rcsb.org": true, "www.rcsb.org": true}

// KeyLocator finds the bucket key of a URL that points into Dynamic PDB's storage.
type KeyLocator interface {
	StoredKey(uri string) (string, bool)
}

// SourceOf says where a file's recorded location points.
func SourceOf(file models.SelectionFile, storage KeyLocator) Source {
	if file.URI == nil {
		return SourceExternal
	}
	if _, inStorage := storage.StoredKey(*file.URI); inStorage {
		return SourceDynamicPDBUpload
	}
	parsed, err := url.Parse(*file.URI)
	if err == nil && pdbArchiveHosts[strings.ToLower(parsed.Host)] {
		return SourcePDBArchive
	}
	return SourceExternal
}

// ReuseTermsOf is what a file may be reused under (researchers' Q7): PDB archive files are CC0; a
// file in Dynamic PDB's storage is CC0 when Dynamic PDB itself or one of Prism's accounts
// (cc0Depositors) added it, and otherwise its depositor has not confirmed terms yet; a file another
// repository holds is under that repository's terms.
func ReuseTermsOf(
	file models.SelectionFile,
	source Source,
	cc0Depositors map[uuid.UUID]bool,
) (models.DownloadReuseTerms, error) {
	switch source {
	case SourcePDBArchive:
		return models.DownloadReuseTermsCC0, nil
	case SourceDynamicPDBUpload:
		if file.UploadedBy.String() == models.SystemUserID || cc0Depositors[file.UploadedBy] {
			return models.DownloadReuseTermsCC0, nil
		}
		return models.DownloadReuseTermsNotConfirmed, nil
	case SourceExternal:
		return models.DownloadReuseTermsRepository, nil
	default:
		return "", fmt.Errorf("file %s: unknown file source: %s", file.ID, source)
	}
}

// DepositorSet is a list of Prism's accounts as a set, for ReuseTermsOf.
func DepositorSet(ids []uuid.UUID) map[uuid.UUID]bool {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}
