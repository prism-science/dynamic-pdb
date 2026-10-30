package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"dynamic-pdb/lib/cif"
	"dynamic-pdb/lib/rcsb"
)

// archiveFileValues are what the entry sync and the archive re-derivation job read from the head of an
// entry's RCSB mmCIF. When the head could not be read completely, composition is nil, mutations
// is incomplete and incompleteReason says why: the callers then keep the stored values.
type archiveFileValues struct {
	mutations        archiveMutations
	composition      *archiveComposition
	referenceVersion string
	incompleteReason string
}

// readArchiveFile reads the head of pdbID's RCSB mmCIF, cif.HeadReadLimit bytes, so the sync and
// the job derive the same values from the same bytes. A failed download, or a compound rule the
// backend does not know, is an error. A head that cannot be parsed, or ends before the
// coordinates, is logged and gives incomplete values: archive files write the mutation
// rows, the compound entities and the metal links before the coordinates.
func readArchiveFile(ctx context.Context, client *rcsb.RemoteClient, logger *slog.Logger, pdbID string) (archiveFileValues, error) {
	head, err := client.DownloadFileHead(ctx, pdbID, rcsb.CoordinateFile(pdbID), cif.HeadReadLimit)
	if err != nil {
		return archiveFileValues{}, fmt.Errorf("download RCSB mmCIF of %s: %w", pdbID, err)
	}
	parseCtx, cancel := context.WithTimeout(ctx, cif.TimeBudget)
	defer cancel()
	document, result, err := cif.ExtractTextContext(parseCtx, string(head.Contents), head.Truncated)
	if err != nil {
		logger.Warn("kept stored mutations and compounds: RCSB mmCIF could not be parsed", "pdb_id", pdbID, "err", err)
		return archiveFileValues{incompleteReason: "the RCSB mmCIF could not be parsed: " + err.Error()}, nil
	}
	if !document.MutationsComplete() {
		logger.Warn(
			"kept stored mutations and compounds: RCSB mmCIF was not read up to its coordinates",
			"pdb_id", pdbID,
			"warnings", result.Warnings,
		)
		return archiveFileValues{
			referenceVersion: result.ReferenceVersion,
			incompleteReason: "the head of the RCSB mmCIF ends before its coordinates",
		}, nil
	}
	composition, err := compositionFromResult(result)
	if err != nil {
		return archiveFileValues{}, fmt.Errorf("derive composition of %s: %w", pdbID, err)
	}
	for _, set := range result.Mutations {
		if set.EntityID == "" {
			logger.Warn(
				"ignored RCSB engineered mutation rows that name no entity",
				"pdb_id", pdbID,
				"unmapped", set.Unmapped,
			)
		}
	}
	return archiveFileValues{
		mutations:        archiveMutations{sets: result.Mutations, complete: true},
		composition:      &composition,
		referenceVersion: result.ReferenceVersion,
	}, nil
}
