package mmseqs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

type DataLoader struct {
	database *db.DB
	cacheDir string
	runID    uuid.UUID
}

type DataLoaderCheckpoint struct {
	LastSequenceID *uuid.UUID `json:"last_sequence_id,omitempty"`
	LoadedCount    int        `json:"loaded_count"`
	FileSize       int64      `json:"file_size"`
	Completed      bool       `json:"completed"`
}

func NewDataLoader(database *db.DB, cacheDir string, runID uuid.UUID) (*DataLoader, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if strings.TrimSpace(cacheDir) == "" {
		return nil, errors.New("mmseqs cache dir is empty")
	}
	if runID == uuid.Nil {
		return nil, errors.New("run id is empty")
	}
	return &DataLoader{
		database: database,
		cacheDir: cacheDir,
		runID:    runID,
	}, nil
}

func (l *DataLoader) LoadHistoricalProteinSequences(ctx context.Context) (string, error) {
	return l.loadProteinSequences(ctx, "historical", models.ProteinSequenceProcessingStateProcessed)
}

func (l *DataLoader) LoadPendingProteinSequences(ctx context.Context) (string, error) {
	return l.loadProteinSequences(ctx, "pending", models.ProteinSequenceProcessingStatePending)
}

func (l *DataLoader) loadProteinSequences(
	ctx context.Context,
	name string,
	processingState models.ProteinSequenceProcessingState,
) (path string, err error) {
	paths := l.proteinSequenceDataPaths(name)
	if err := prepareDirectories(paths); err != nil {
		return "", fmt.Errorf("prepare %s protein sequence load: %w", name, err)
	}

	checkpoint, err := readCheckpoint(paths.checkpoint)
	if err != nil {
		return "", fmt.Errorf("read %s protein sequence checkpoint: %w", name, err)
	}
	if checkpoint.Completed {
		if exists, err := fileExists(paths.output); err != nil {
			return "", fmt.Errorf("check completed %s protein sequence output file: %w", name, err)
		} else if exists {
			return paths.output, nil
		}
		checkpoint = DataLoaderCheckpoint{}
	}
	if err := recoverOutputFromCheckpoint(paths.output, checkpoint); err != nil {
		return "", fmt.Errorf("restore %s protein sequence output file: %w", name, err)
	}

	outputFile, err := openOutputFile(paths.output)
	if err != nil {
		return "", fmt.Errorf("open %s protein sequence output file: %w", name, err)
	}
	defer func() {
		if closeErr := outputFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s protein sequence output file: %w", name, closeErr)
		}
	}()

	writer := bufio.NewWriter(outputFile)
	for {
		sequences, err := l.loadNextBatch(ctx, processingState, checkpoint.LastSequenceID)
		if err != nil {
			return "", fmt.Errorf("load next %s protein sequence batch: %w", name, err)
		}
		if len(sequences) == 0 {
			checkpoint.Completed = true
			if err := writeCheckpoint(paths.checkpoint, checkpoint); err != nil {
				return "", fmt.Errorf("write completed %s protein sequence checkpoint: %w", name, err)
			}
			return paths.output, nil
		}

		if err := appendSequenceBatch(writer, sequences); err != nil {
			return "", fmt.Errorf("append %s protein sequence batch: %w", name, err)
		}
		if err := writer.Flush(); err != nil {
			return "", fmt.Errorf("flush %s protein sequence output file: %w", name, err)
		}
		if err := outputFile.Sync(); err != nil {
			return "", fmt.Errorf("sync %s protein sequence output file: %w", name, err)
		}

		fileInfo, err := outputFile.Stat()
		if err != nil {
			return "", fmt.Errorf("stat %s protein sequence output file: %w", name, err)
		}
		lastSequenceID := sequences[len(sequences)-1].ID
		checkpoint = DataLoaderCheckpoint{
			LastSequenceID: &lastSequenceID,
			LoadedCount:    checkpoint.LoadedCount + len(sequences),
			FileSize:       fileInfo.Size(),
			Completed:      false,
		}
		if err := writeCheckpoint(paths.checkpoint, checkpoint); err != nil {
			return "", fmt.Errorf("write %s protein sequence checkpoint: %w", name, err)
		}
	}
}

func (l *DataLoader) loadNextBatch(
	ctx context.Context,
	processingState models.ProteinSequenceProcessingState,
	lastSequenceID *uuid.UUID,
) ([]models.ProteinSequence, error) {
	limit := 50000
	sequences, err := l.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		ProcessingState: &processingState,
		AfterID:         lastSequenceID,
		OrderByID:       true,
		Limit:           &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list protein sequences after id: %w", err)
	}
	return sequences, nil
}
func (l *DataLoader) proteinSequenceDataPaths(name string) dataLoaderPaths {
	dir := filepath.Join(l.cacheDir, "runs", l.runID.String(), "data", name)
	return dataLoaderPaths{
		dir:        dir,
		output:     filepath.Join(dir, "sequences.fasta"),
		checkpoint: filepath.Join(dir, "checkpoint.json"),
	}
}

type dataLoaderPaths struct {
	dir        string
	output     string
	checkpoint string
}

func prepareDirectories(paths dataLoaderPaths) error {
	if err := os.MkdirAll(paths.dir, 0o755); err != nil {
		return fmt.Errorf("create data loader dir: %w", err)
	}
	if err := removeIfExists(paths.output + ".tmp"); err != nil {
		return fmt.Errorf("remove stale output temp file: %w", err)
	}
	if err := removeIfExists(paths.checkpoint + ".tmp"); err != nil {
		return fmt.Errorf("remove stale checkpoint temp file: %w", err)
	}
	return nil
}

func readCheckpoint(path string) (DataLoaderCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DataLoaderCheckpoint{}, nil
		}
		return DataLoaderCheckpoint{}, fmt.Errorf("read checkpoint file: %w", err)
	}

	var checkpoint DataLoaderCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return DataLoaderCheckpoint{}, fmt.Errorf("decode checkpoint file: %w", err)
	}
	return checkpoint, nil
}

func writeCheckpoint(path string, checkpoint DataLoaderCheckpoint) error {
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

func recoverOutputFromCheckpoint(path string, checkpoint DataLoaderCheckpoint) error {
	if err := ensureFile(path); err != nil {
		return fmt.Errorf("ensure output file exists: %w", err)
	}
	if err := os.Truncate(path, checkpoint.FileSize); err != nil {
		return fmt.Errorf("truncate output file to checkpoint: %w", err)
	}
	return nil
}

func openOutputFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open output file for append: %w", err)
	}
	return file, nil
}

func appendSequenceBatch(writer *bufio.Writer, sequences []models.ProteinSequence) error {
	for _, sequence := range sequences {
		if err := writeProteinSequenceRecord(writer, sequence.ID, sequence.Sequence); err != nil {
			return fmt.Errorf("write protein sequence record: %w", err)
		}
	}
	return nil
}

func writeProteinSequenceRecord(writer *bufio.Writer, id uuid.UUID, sequence string) error {
	if _, err := fmt.Fprintf(writer, ">%s\n", id.String()); err != nil {
		return fmt.Errorf("write FASTA header: %w", err)
	}

	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		default:
			return r
		}
	}, sequence)
	for len(cleaned) > 80 {
		if _, err := fmt.Fprintln(writer, cleaned[:80]); err != nil {
			return fmt.Errorf("write FASTA sequence line: %w", err)
		}
		cleaned = cleaned[80:]
	}
	if _, err := fmt.Fprintln(writer, cleaned); err != nil {
		return fmt.Errorf("write FASTA sequence line: %w", err)
	}
	return nil
}

func ensureFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
