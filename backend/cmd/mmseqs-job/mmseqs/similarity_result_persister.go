package mmseqs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	similarityPersistLogEvery      = 20
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
	maxBits, err := MaxOutputBits(params.SearchResultPath)
	if err != nil {
		return fmt.Errorf("read max similarity search bits: %w", err)
	}
	slog.Info("persisting mmseqs similarity search result", "run_id", params.Run.ID, "max_bits", maxBits)
	if err := p.persistSearchResult(ctx, params.Run.ID, params.SearchResultPath, maxBits); err != nil {
		return fmt.Errorf("persist protein sequence similarities: %w", err)
	}
	if err := p.markSequencesProcessed(ctx, sequenceIDs); err != nil {
		return fmt.Errorf("mark protein sequences processed: %w", err)
	}
	slog.Info("mmseqs similarity result persisted", "run_id", params.Run.ID, "processed_sequences", len(sequenceIDs))
	return nil
}

func (p *SimilarityResultPersister) persistSearchResult(
	ctx context.Context,
	runID uuid.UUID,
	searchResultPath string,
	maxBits float64,
) error {
	now := time.Now().UTC()
	batch := make([]models.ProteinSequenceSimilarity, 0, similarityPersistBatchSize)
	persistedCount := 0
	batchCount := 0
	if err := ForEachOutputHit(searchResultPath, func(hit Hit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch = append(batch, similarityFromHit(runID, hit, maxBits, now))
		if len(batch) < similarityPersistBatchSize {
			return nil
		}
		if err := p.persistSimilarities(ctx, batch); err != nil {
			return err
		}
		persistedCount += len(batch)
		batchCount++
		if batchCount%similarityPersistLogEvery == 0 {
			slog.Info("persisted mmseqs similarity batch", "run_id", runID, "similarities", persistedCount)
		}
		batch = batch[:0]
		return nil
	}); err != nil {
		return fmt.Errorf("stream similarity search result: %w", err)
	}
	if err := p.persistSimilarities(ctx, batch); err != nil {
		return fmt.Errorf("save final protein sequence similarity batch: %w", err)
	}
	persistedCount += len(batch)
	slog.Info("finished streaming mmseqs similarity result", "run_id", runID, "similarities", persistedCount)
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
		similarities = append(similarities, similarityFromHit(runID, hit, maxBits, now))
	}
	return similarities
}

func similarityFromHit(
	runID uuid.UUID,
	hit Hit,
	maxBits float64,
	createdAt time.Time,
) models.ProteinSequenceSimilarity {
	score := 0.0
	if maxBits > 0 {
		score = hit.Bits / maxBits
	}
	return models.ProteinSequenceSimilarity{
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
		CreatedAt: createdAt,
	}
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
