package mmseqs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

type SimilarityResultPersisterCheckpoint struct {
	SearchResultOffset      int64 `json:"search_result_offset"`
	ProcessedSimilarityRows int64 `json:"processed_similarity_rows"`
	InsertedSimilarityRows  int64 `json:"inserted_similarity_rows"`
	Completed               bool  `json:"completed"`
}

const (
	similarityPersistBatchSize      = 5000
	similarityPersistLogEvery       = 20
	similarityPersistCheckpointRows = 1000000
	processingStateUpdateBatchSize  = 10000
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

	checkpointPath := similarityResultPersisterCheckpointPath(params.SearchResultPath)
	checkpoint, err := readSimilarityResultPersisterCheckpoint(checkpointPath)
	if err != nil {
		return fmt.Errorf("read similarity result persister checkpoint: %w", err)
	}
	if checkpoint.Completed {
		slog.Info("mmseqs similarity result already persisted", "run_id", params.Run.ID)
		return nil
	}

	slog.Info(
		"persisting mmseqs similarity search result",
		"run_id",
		params.Run.ID,
		"search_result_offset",
		checkpoint.SearchResultOffset,
		"processed_similarity_rows",
		checkpoint.ProcessedSimilarityRows,
	)
	checkpoint, err = p.persistSearchResult(ctx, params.Run.ID, params.SearchResultPath, checkpointPath, checkpoint)
	if err != nil {
		return fmt.Errorf("persist protein sequence similarities: %w", err)
	}
	if err := p.markSequencesProcessed(ctx, sequenceIDs); err != nil {
		return fmt.Errorf("mark protein sequences processed: %w", err)
	}
	checkpoint.Completed = true
	if err := writeSimilarityResultPersisterCheckpoint(checkpointPath, checkpoint); err != nil {
		return fmt.Errorf("write completed similarity result persister checkpoint: %w", err)
	}
	slog.Info("mmseqs similarity result persisted", "run_id", params.Run.ID, "processed_sequences", len(sequenceIDs))
	return nil
}

func (p *SimilarityResultPersister) persistSearchResult(
	ctx context.Context,
	runID uuid.UUID,
	searchResultPath string,
	checkpointPath string,
	checkpoint SimilarityResultPersisterCheckpoint,
) (SimilarityResultPersisterCheckpoint, error) {
	for {
		nextCheckpoint, reachedEOF, err := p.persistNextSearchResultChunk(ctx, runID, searchResultPath, checkpoint)
		if err != nil {
			return SimilarityResultPersisterCheckpoint{}, err
		}
		checkpoint = nextCheckpoint
		if err := writeSimilarityResultPersisterCheckpoint(checkpointPath, checkpoint); err != nil {
			return SimilarityResultPersisterCheckpoint{}, fmt.Errorf("write similarity result persister checkpoint: %w", err)
		}
		if reachedEOF {
			return checkpoint, nil
		}
	}
}

func (p *SimilarityResultPersister) persistNextSearchResultChunk(
	ctx context.Context,
	runID uuid.UUID,
	searchResultPath string,
	checkpoint SimilarityResultPersisterCheckpoint,
) (nextCheckpoint SimilarityResultPersisterCheckpoint, reachedEOF bool, err error) {
	file, err := os.Open(searchResultPath)
	if err != nil {
		return SimilarityResultPersisterCheckpoint{}, false, fmt.Errorf("open similarity search result: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close similarity search result: %w", closeErr)
		}
	}()

	if _, err := file.Seek(checkpoint.SearchResultOffset, io.SeekStart); err != nil {
		return SimilarityResultPersisterCheckpoint{}, false, fmt.Errorf("seek similarity search result checkpoint: %w", err)
	}

	now := time.Now().UTC()
	batch := make([]models.ProteinSequenceSimilarity, 0, similarityPersistBatchSize)
	stagedCount := int64(0)
	batchCount := 0
	nextOffset := checkpoint.SearchResultOffset
	reachedEOF = false
	reader := bufio.NewReader(file)
	var insertedCount int64
	if err := p.database.Do(ctx, func(ctx context.Context) error {
		if err := p.database.ProteinSequenceSimilarities.CreateSimilarityStagingTable(ctx); err != nil {
			return fmt.Errorf("create protein sequence similarities staging: %w", err)
		}

		for stagedCount < similarityPersistCheckpointRows {
			if err := ctx.Err(); err != nil {
				return err
			}

			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					return fmt.Errorf("read similarity search result line: %w", readErr)
				}
				reachedEOF = true
				if line == "" {
					break
				}
			}

			nextOffset += int64(len(line))
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine != "" {
				hit, err := parseHit(trimmedLine)
				if err != nil {
					return fmt.Errorf("parse similarity search result at offset %d: %w", nextOffset, err)
				}
				if hit.QuerySequenceID != hit.TargetSequenceID {
					batch = append(batch, similarityFromHit(runID, *hit, now))
					if len(batch) == similarityPersistBatchSize {
						if err := p.copySimilaritiesToStaging(ctx, batch); err != nil {
							return err
						}
						stagedCount += int64(len(batch))
						batchCount++
						if batchCount%similarityPersistLogEvery == 0 {
							slog.Info("staged mmseqs similarity batch", "run_id", runID, "similarities", checkpoint.ProcessedSimilarityRows+stagedCount)
						}
						batch = batch[:0]
					}
				}
			}

			if reachedEOF {
				break
			}
		}

		if err := p.copySimilaritiesToStaging(ctx, batch); err != nil {
			return fmt.Errorf("copy final protein sequence similarity batch to staging: %w", err)
		}
		stagedCount += int64(len(batch))

		insertedCount, err = p.database.ProteinSequenceSimilarities.CreateFromSimilarityStaging(ctx)
		if err != nil {
			return fmt.Errorf("insert protein sequence similarities from staging: %w", err)
		}
		return nil
	}); err != nil {
		return SimilarityResultPersisterCheckpoint{}, false, fmt.Errorf("persist similarity result chunk: %w", err)
	}

	checkpoint.SearchResultOffset = nextOffset
	checkpoint.ProcessedSimilarityRows += stagedCount
	checkpoint.InsertedSimilarityRows += insertedCount
	slog.Info(
		"persisted mmseqs similarity result chunk",
		"run_id",
		runID,
		"staged_similarity_rows",
		stagedCount,
		"inserted_similarity_rows",
		insertedCount,
		"processed_similarity_rows",
		checkpoint.ProcessedSimilarityRows,
		"search_result_offset",
		checkpoint.SearchResultOffset,
	)
	return checkpoint, reachedEOF, nil
}

func (p *SimilarityResultPersister) copySimilaritiesToStaging(
	ctx context.Context,
	similarities []models.ProteinSequenceSimilarity,
) error {
	for start := 0; start < len(similarities); start += similarityPersistBatchSize {
		end := min(start+similarityPersistBatchSize, len(similarities))
		if err := p.database.ProteinSequenceSimilarities.CopyToSimilarityStaging(ctx, similarities[start:end]); err != nil {
			return fmt.Errorf("copy protein sequence similarity batch to staging: %w", err)
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
	similarities := make([]models.ProteinSequenceSimilarity, 0, len(hits))
	now := time.Now().UTC()
	for _, hit := range hits {
		similarities = append(similarities, similarityFromHit(runID, hit, now))
	}
	return similarities
}

func similarityFromHit(
	runID uuid.UUID,
	hit Hit,
	createdAt time.Time,
) models.ProteinSequenceSimilarity {
	return models.ProteinSequenceSimilarity{
		ID:                uuid.New(),
		RunID:             runID,
		SourceSequenceID:  hit.QuerySequenceID,
		SimilarSequenceID: hit.TargetSequenceID,
		Tool:              "mmseqs2",
		Score:             hit.Fident * min(hit.Qcov, hit.Tcov),
		Metadata: map[string]any{
			"fident":           hit.Fident,
			"qcov":             hit.Qcov,
			"tcov":             hit.Tcov,
			"evalue":           hit.Evalue,
			"bits":             hit.Bits,
			"alignment_length": hit.AlignmentLength,
			"qstart":           hit.Qstart,
			"qend":             hit.Qend,
			"tstart":           hit.Tstart,
			"tend":             hit.Tend,
			"qaln":             hit.Qaln,
			"taln":             hit.Taln,
			"score_source":     "fident_min_coverage",
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

func similarityResultPersisterCheckpointPath(searchResultPath string) string {
	return searchResultPath + ".persist-checkpoint.json"
}

func readSimilarityResultPersisterCheckpoint(path string) (SimilarityResultPersisterCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SimilarityResultPersisterCheckpoint{}, nil
		}
		return SimilarityResultPersisterCheckpoint{}, fmt.Errorf("read checkpoint file: %w", err)
	}

	var checkpoint SimilarityResultPersisterCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return SimilarityResultPersisterCheckpoint{}, fmt.Errorf("decode checkpoint file: %w", err)
	}
	return checkpoint, nil
}

func writeSimilarityResultPersisterCheckpoint(path string, checkpoint SimilarityResultPersisterCheckpoint) error {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint file: %w", err)
	}
	data = append(data, '\n')

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("write checkpoint temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish checkpoint file: %w", err)
	}
	return nil
}
