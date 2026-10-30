package httpapi

import (
	"context"
	"errors"
	"fmt"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// modeledCompoundsForRevision returns what the model-compounds job read from a revision's model
// file. A revision without a file, or whose file is not in our bucket (it has no set), is
// not_stored: its typed metadata ligands and cofactors stand.
func (s *Server) modeledCompoundsForRevision(ctx context.Context, revision domainmodels.ModelRevision) (ModeledCompounds, error) {
	if revision.PrimaryArtifactID == nil {
		return notStoredCompounds(), nil
	}
	set, err := s.database.ModeledCompounds.Get(ctx, *revision.PrimaryArtifactID)
	if errors.Is(err, db.ErrModeledCompoundSetNotFound) {
		return notStoredCompounds(), nil
	}
	if err != nil {
		return ModeledCompounds{}, fmt.Errorf("get the compounds of the model file: %w", err)
	}
	return modeledCompoundsFromModel(*set)
}

func notStoredCompounds() ModeledCompounds {
	return ModeledCompounds{Status: NotStored, Warnings: []string{}, Copies: []ModeledCompoundCopy{}}
}

func modeledCompoundsFromModel(set domainmodels.ModeledCompoundSet) (ModeledCompounds, error) {
	status, err := modeledCompoundsStatusFromModel(set.Status)
	if err != nil {
		return ModeledCompounds{}, err
	}
	compounds := ModeledCompounds{
		Status:        status,
		ParserVersion: set.ParserVersion,
		Complete:      set.Complete,
		Warnings:      append([]string{}, set.Warnings...),
		LastError:     set.LastError,
		ReadAt:        set.ReadAt,
		Copies:        []ModeledCompoundCopy{},
	}
	// A set that is read again keeps its old copies until the new ones are saved; they are not
	// the file's record until then.
	if set.Status != domainmodels.ModeledCompoundStatusDone {
		return compounds, nil
	}
	for _, copied := range set.Copies {
		source, err := modeledCompoundSourceFromModel(copied.Source)
		if err != nil {
			return ModeledCompounds{}, err
		}
		compounds.Copies = append(compounds.Copies, ModeledCompoundCopy{
			CcdId:        copied.CCDID,
			LabelAsymId:  optionalText(copied.LabelAsymID),
			AuthAsymId:   optionalText(copied.AuthAsymID),
			AuthSeqId:    optionalText(copied.AuthSeqID),
			PdbInsCode:   optionalText(copied.InsCode),
			EntityId:     optionalText(copied.EntityID),
			Source:       source,
			NeedsReview:  copied.NeedsReview,
			ReviewReason: optionalText(copied.ReviewReason),
		})
	}
	return compounds, nil
}

func modeledCompoundsStatusFromModel(status domainmodels.ModeledCompoundStatus) (ModeledCompoundsStatus, error) {
	switch status {
	case domainmodels.ModeledCompoundStatusPending:
		return Pending, nil
	case domainmodels.ModeledCompoundStatusDone:
		return Done, nil
	case domainmodels.ModeledCompoundStatusFailed:
		return Failed, nil
	default:
		return "", fmt.Errorf("unknown modeled compound status: %s", status)
	}
}

func modeledCompoundSourceFromModel(source domainmodels.ModeledCompoundSource) (ModeledCompoundCopySource, error) {
	switch source {
	case domainmodels.ModeledCompoundSourceNonpolyScheme:
		return NonpolyScheme, nil
	case domainmodels.ModeledCompoundSourceBranchScheme:
		return BranchScheme, nil
	case domainmodels.ModeledCompoundSourceAtomSite:
		return AtomSite, nil
	case domainmodels.ModeledCompoundSourcePDBHetatm:
		return PdbHetatm, nil
	default:
		return "", fmt.Errorf("unknown modeled compound source: %s", source)
	}
}

// optionalText is nil for an empty value, which the API returns as null.
func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
