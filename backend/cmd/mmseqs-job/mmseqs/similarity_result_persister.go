package mmseqs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

type SimilarityResultPersister struct {
	database *db.DB
}

type SimilarityResultPersisterParams struct {
	Run              models.ProteinSequenceSimilarityRun
	SequenceFilePath string
	SearchResultPath string
}

const (
	similarityPersistBatchSize     = 5000
	processingStateUpdateBatchSize = 10000
)

func NewSimilarityResultPersister(database *db.DB) (*SimilarityResultPersister, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	return &SimilarityResultPersister{database: database}, nil
}

func (p *SimilarityResultPersister) Persist(ctx context.Context, params SimilarityResultPersisterParams) error {
	if params.Run.ID == uuid.Nil {
		return errors.New("similarity run id is empty")
	}
	if strings.TrimSpace(params.SequenceFilePath) == "" {
		return errors.New("sequence file path is empty")
	}
	if strings.TrimSpace(params.SearchResultPath) == "" {
		return errors.New("search result path is empty")
	}

	sequenceIDs, err := readSequenceIDs(params.SequenceFilePath)
	if err != nil {
		return fmt.Errorf("read sequence ids: %w", err)
	}
	hits, err := ParseOutput(params.SearchResultPath)
	if err != nil {
		return fmt.Errorf("parse similarity search result: %w", err)
	}
	similarities := similaritiesFromHits(params.Run.ID, hits)

	if err := p.persistSimilarities(ctx, similarities); err != nil {
		return fmt.Errorf("persist protein sequence similarities: %w", err)
	}
	if err := p.markSequencesProcessed(ctx, sequenceIDs); err != nil {
		return fmt.Errorf("mark protein sequences processed: %w", err)
	}
	return nil
}

func (p *SimilarityResultPersister) persistSimilarities(
	ctx context.Context,
	similarities []models.ProteinSequenceSimilarity,
) error {
	for start := 0; start < len(similarities); start += similarityPersistBatchSize {
		end := min(start+similarityPersistBatchSize, len(similarities))
		if err := p.database.ProteinSequenceSimilarities.Create(ctx, similarities[start:end]); err != nil {
			return fmt.Errorf("save protein sequence similarity batch: %w", err)
		}
	}
	return nil
}

func (p *SimilarityResultPersister) markSequencesProcessed(
	ctx context.Context,
	sequenceIDs []uuid.UUID,
) error {
	for start := 0; start < len(sequenceIDs); start += processingStateUpdateBatchSize {
		end := min(start+processingStateUpdateBatchSize, len(sequenceIDs))
		if err := p.database.ProteinSequences.UpdateProcessingState(
			ctx,
			sequenceIDs[start:end],
			models.ProteinSequenceProcessingStateProcessed,
		); err != nil {
			return fmt.Errorf("update protein sequence processing state batch: %w", err)
		}
	}
	return nil
}

func similaritiesFromHits(runID uuid.UUID, hits []Hit) []models.ProteinSequenceSimilarity {
	maxBits := 0.0
	for _, hit := range hits {
		maxBits = math.Max(maxBits, hit.Bits)
	}

	similarities := make([]models.ProteinSequenceSimilarity, 0, len(hits))
	now := time.Now().UTC()
	for _, hit := range hits {
		score := 0.0
		if maxBits > 0 {
			score = hit.Bits / maxBits
		}
		similarities = append(similarities, models.ProteinSequenceSimilarity{
			ID:                uuid.New(),
			RunID:             runID,
			SourceSequenceID:  hit.QuerySequenceID,
			SimilarSequenceID: hit.TargetSequenceID,
			Tool:              "mmseqs2",
			Score:             score,
			Metadata: map[string]any{
				"fident":              hit.Fident,
				"qcov":                hit.Qcov,
				"tcov":                hit.Tcov,
				"evalue":              hit.Evalue,
				"bits":                hit.Bits,
				"alignment_length":    hit.AlignmentLength,
				"qstart":              hit.Qstart,
				"qend":                hit.Qend,
				"tstart":              hit.Tstart,
				"tend":                hit.Tend,
				"qaln":                hit.Qaln,
				"taln":                hit.Taln,
				"score_source":        "bits",
				"score_normalization": "max_bits_per_run",
				"max_bits_in_run":     maxBits,
			},
			CreatedAt: now,
		})
	}
	return similarities
}

func readSequenceIDs(path string) (ids []uuid.UUID, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sequence file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close sequence file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 32*1024*1024)
	ids = make([]uuid.UUID, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, ">") {
			continue
		}
		idValue := strings.TrimSpace(strings.TrimPrefix(line, ">"))
		id, err := uuid.Parse(idValue)
		if err != nil {
			return nil, fmt.Errorf("parse FASTA sequence id %q: %w", idValue, err)
		}
		ids = append(ids, id)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan sequence file: %w", err)
	}
	return ids, nil
}
