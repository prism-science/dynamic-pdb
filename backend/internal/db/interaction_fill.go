package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

type FillInteractionSetParams struct {
	ModelRevisionID uuid.UUID
	// CreatedBy owns the run that records the parser.
	CreatedBy uuid.UUID
	Result    cif.Result
}

// FillInteractionSet stores the connections of a parsed mmCIF result as a new
// interaction set of a model revision. The set points to a new run naming the
// parser and its version. The run, the set and its records are stored together
// or not at all.
func (d *DB) FillInteractionSet(
	ctx context.Context,
	params FillInteractionSetParams,
) (*models.InteractionSet, error) {
	now := time.Now().UTC()
	setID := uuid.New()
	records, err := interactionRecordsFromConnections(setID, params.Result.Connections)
	if err != nil {
		return nil, fmt.Errorf("build interaction records: %w", err)
	}

	var created *models.InteractionSet
	err = d.Do(ctx, func(ctx context.Context) error {
		run, err := d.Runs.Create(ctx, models.Run{
			ID:              uuid.New(),
			Name:            "mmCIF interaction extraction",
			SoftwareName:    ptrTo(cif.ParserName),
			SoftwareVersion: ptrTo(cif.ParserVersion),
			Parameters:      map[string]any{"reference_version": params.Result.ReferenceVersion},
			Metadata:        map[string]any{"source": string(models.InteractionSourceMMCIFStructConn)},
			StartedAt:       &now,
			FinishedAt:      &now,
			CreatedBy:       params.CreatedBy,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		if err != nil {
			return fmt.Errorf("create parser run: %w", err)
		}
		set, err := d.InteractionSets.Create(ctx, models.InteractionSet{
			ID:              setID,
			ModelRevisionID: params.ModelRevisionID,
			RunID:           run.ID,
			Source:          models.InteractionSourceMMCIFStructConn,
			CreatedAt:       now,
		})
		if err != nil {
			return fmt.Errorf("create interaction set: %w", err)
		}
		if err := d.InteractionSets.AddRecords(ctx, records); err != nil {
			return fmt.Errorf("add interaction records: %w", err)
		}
		created = set
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fill interaction set: %w", err)
	}
	return created, nil
}

func interactionRecordsFromConnections(
	setID uuid.UUID,
	connections []cif.Connection,
) ([]models.InteractionRecord, error) {
	records := make([]models.InteractionRecord, 0, len(connections))
	for ordinal, connection := range connections {
		partnerA, err := interactionPartner(connection.Partner1)
		if err != nil {
			return nil, fmt.Errorf("connection %q partner 1: %w", connection.ID, err)
		}
		partnerB, err := interactionPartner(connection.Partner2)
		if err != nil {
			return nil, fmt.Errorf("connection %q partner 2: %w", connection.ID, err)
		}
		records = append(records, models.InteractionRecord{
			ID:                 uuid.New(),
			InteractionSetID:   setID,
			Ordinal:            ordinal,
			SourceConnectionID: connection.ID,
			ConnectionType:     connection.ConnTypeID,
			PartnerA:           partnerA,
			PartnerB:           partnerB,
			SymmetryA:          connection.Symmetry1,
			SymmetryB:          connection.Symmetry2,
			DistanceAngstroms:  connection.DistanceAngstroms,
			ValueOrder:         connection.ValueOrder,
		})
	}
	return records, nil
}

func interactionPartner(partner cif.Partner) (models.InteractionPartner, error) {
	converted := models.InteractionPartner{
		LabelAsymID: partner.LabelAsymID,
		LabelCompID: partner.LabelCompID,
		LabelAtomID: partner.LabelAtomID,
		LabelAltID:  partner.LabelAltID,
		AuthAsymID:  partner.AuthAsymID,
		AuthCompID:  partner.AuthCompID,
		AuthSeqID:   partner.AuthSeqID,
		InsCode:     partner.InsCode,
	}
	if partner.LabelSeqID != "" {
		sequenceID, err := strconv.Atoi(partner.LabelSeqID)
		if err != nil {
			return models.InteractionPartner{}, fmt.Errorf("label_seq_id %q is not a number: %w", partner.LabelSeqID, err)
		}
		converted.LabelSeqID = &sequenceID
	}
	return converted, nil
}

func ptrTo[T any](value T) *T {
	return &value
}
