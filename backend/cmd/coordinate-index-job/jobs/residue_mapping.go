package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/modelfiles"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// noDepositedFileWarning is stored with a file whose residues without label numbering stay keyed by
// author numbering because its entry has no deposited mmCIF to map them through.
const noDepositedFileWarning = "residue numbering not mapped to label numbering: the entry of the file's models has no active Deposited model (or the file's models belong to several entries)"

// residueMapping is one deposited mmCIF's mapping, or why it could not be read. The job keeps them
// for the run (residueMappings), so the models of one entry read it once.
type residueMapping struct {
	mapping cif.ResidueMapping
	problem string
}

// maxCachedMappedResidues bounds the author residues the run's mappings hold together: one mapping
// may hold up to maxSequencePositions (200,000) residues, about 150 bytes each, and a run reads up to
// 500 files. Past it the cache starts over, so a run keeps under about 30 MB of mappings.
const maxCachedMappedResidues = 200_000

// residueMappings caches the run's mappings by deposited file, within maxCachedMappedResidues.
type residueMappings struct {
	byFile   map[uuid.UUID]residueMapping
	residues int
}

func newResidueMappings() *residueMappings {
	return &residueMappings{byFile: map[uuid.UUID]residueMapping{}}
}

func (c *residueMappings) get(artifactID uuid.UUID) (residueMapping, bool) {
	mapping, known := c.byFile[artifactID]
	return mapping, known
}

func (c *residueMappings) put(artifactID uuid.UUID, mapping residueMapping) {
	size := mapping.mapping.Len()
	if c.residues+size > maxCachedMappedResidues {
		c.byFile = map[uuid.UUID]residueMapping{}
		c.residues = 0
	}
	if size > maxCachedMappedResidues {
		return
	}
	c.byFile[artifactID] = mapping
	c.residues += size
}

// mapResidues gives the residues a read keys by author numbering (PDB format, rows without
// label_seq_id) label numbering through the entry's deposited mmCIF, as the researchers asked on 17
// Nov (Q2), and records the key it ends with. Only while conformer counts are stored, and not for
// ensembles (more than one frame), which get no counts. When no mapping can be had, the residues stay
// keyed by author numbering, with a warning saying why. It fails when the database lookup does, or when
// ctx (the claim's read deadline) ends during the deposited file's read: the claim is then tried again,
// rather than saved without its mapping.
func (j *CoordinateIndexJob) mapResidues(ctx context.Context, claim models.CoordinateIndexClaim, read cif.CoordinateIndex) (cif.CoordinateIndex, error) {
	if !j.cfg.ResidueCounts || read.Conformers.MultipleModelFrames || read.Conformers.AuthorKeyedResidues == 0 {
		return read, nil
	}
	deposited, err := j.queue.DepositedFileOf(ctx, claim.ArtifactID)
	if err != nil {
		return read, fmt.Errorf("find the entry's deposited mmCIF: %w", err)
	}
	if deposited == nil {
		read.Warnings = append(read.Warnings, noDepositedFileWarning)
		return read, nil
	}
	mapping, known := j.mappings.get(deposited.ArtifactID)
	if !known {
		mapping = j.readMapping(ctx, *deposited)
		if err := ctx.Err(); err != nil {
			return read, fmt.Errorf("read the entry's deposited mmCIF %s: %w", deposited.ArtifactID, err)
		}
		j.mappings.put(deposited.ArtifactID, mapping)
	}
	if mapping.problem != "" {
		read.Warnings = append(read.Warnings, fmt.Sprintf("residue numbering not mapped to label numbering: the deposited mmCIF (artifact %s) %s", deposited.ArtifactID, mapping.problem))
		return read, nil
	}
	read.Conformers = read.ConformersMapped(mapping.mapping)
	read.Warnings = append(read.Warnings, read.Conformers.MappingWarnings()...)
	return read, nil
}

// readMapping reads the _pdbx_poly_seq_scheme of a deposited mmCIF from our bucket (or the checksum
// job's copy of it), within ctx's deadline.
func (j *CoordinateIndexJob) readMapping(ctx context.Context, deposited models.DepositedFile) residueMapping {
	key, _, err := modelfiles.StoredKey(j.files, modelfiles.Location{
		URI:        deposited.URI,
		CopyStatus: deposited.CopyStatus,
		StoredURI:  deposited.StoredURI,
	})
	if err != nil {
		return residueMapping{problem: fmt.Sprintf("could not be located: %v", err)}
	}
	file, err := j.files.Open(ctx, key)
	if err != nil {
		return residueMapping{problem: fmt.Sprintf("could not be opened: %v", err)}
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			j.logger.Warn("close deposited file failed", "artifact_id", deposited.ArtifactID, "err", closeErr)
		}
	}()
	body, format, err := modelfiles.OpenStructure(file, deposited.Format)
	if err != nil {
		return residueMapping{problem: fmt.Sprintf("could not be opened: %v", err)}
	}
	if format != cif.FormatMMCIF {
		return residueMapping{problem: "is not an mmCIF file"}
	}
	mapping, err := cif.ReadResidueMapping(ctx, body)
	switch {
	case errors.Is(err, cif.ErrNoResidueMapping):
		return residueMapping{problem: "has no _pdbx_poly_seq_scheme"}
	case err != nil:
		return residueMapping{problem: fmt.Sprintf("could not be read: %v", err)}
	default:
		return residueMapping{mapping: mapping}
	}
}
