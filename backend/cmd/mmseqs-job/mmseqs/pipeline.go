package mmseqs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/utils"
)

type PipelineMode string

const (
	PipelineModeBootstrap PipelineMode = "bootstrap"
	PipelineModeProcess   PipelineMode = "process"
)

type Pipeline struct {
	database *db.DB
	commands *Commands
	logger   *slog.Logger
	cacheDir string
}

func NewPipeline(
	database *db.DB,
	commands *Commands,
	logger *slog.Logger,
	cacheDir string,
) (*Pipeline, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if commands == nil {
		return nil, errors.New("mmseqs commands is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cacheDir) == "" {
		return nil, errors.New("mmseqs cache dir is empty")
	}
	return &Pipeline{
		database: database,
		commands: commands,
		logger:   logger,
		cacheDir: cacheDir,
	}, nil
}

func (p *Pipeline) Run(ctx context.Context) error {
	if err := os.MkdirAll(p.cacheDir, 0o755); err != nil {
		return fmt.Errorf("create mmseqs cache dir: %w", err)
	}

	lock := utils.NewFileLock(filepath.Join(p.cacheDir, "mmseqs-job.lock"))
	if err := lock.Locked(func() error {
		mode, err := p.mode()
		if err != nil {
			return fmt.Errorf("resolve mmseqs pipeline mode: %w", err)
		}
		if err := p.run(ctx, mode); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("run mmseqs pipeline with file lock: %w", err)
	}
	return nil
}

func (p *Pipeline) run(ctx context.Context, mode PipelineMode) error {
	if mode != PipelineModeBootstrap && mode != PipelineModeProcess {
		return fmt.Errorf("unknown mmseqs pipeline mode %q", mode)
	}

	run, err := p.findActiveRun(ctx, mode)
	if err != nil {
		return fmt.Errorf("find running mmseqs similarity run: %w", err)
	}
	if run == nil {
		if mode == PipelineModeProcess {
			hasPendingSequences, err := p.hasPendingSequences(ctx)
			if err != nil {
				return fmt.Errorf("check pending protein sequences: %w", err)
			}
			if !hasPendingSequences {
				p.logger.Info("mmseqs pipeline has no pending sequences")
				return nil
			}
		}

		run, err = p.createRun(ctx, mode)
		if err != nil {
			return fmt.Errorf("create mmseqs similarity run: %w", err)
		}
		p.logger.Info("mmseqs pipeline run created", "run_id", run.ID, "mode", string(mode))
	} else {
		p.logger.Info("mmseqs pipeline run resumed", "run_id", run.ID, "mode", string(mode))
	}

	runErr := p.execute(ctx, mode, *run)
	if runErr != nil {
		finishedAt := time.Now().UTC()
		errorMessage := runErr.Error()
		if err := p.database.ProteinSequenceSimilarities.UpdateRunState(
			ctx,
			run.ID,
			models.ProteinSequenceSimilarityRunStateFailed,
			&errorMessage,
			&finishedAt,
		); err != nil {
			return fmt.Errorf("mark mmseqs similarity run failed after error %q: %w", runErr.Error(), err)
		}
		return fmt.Errorf("run mmseqs pipeline: %w", runErr)
	}
	return nil
}

func (p *Pipeline) execute(
	ctx context.Context,
	mode PipelineMode,
	run models.ProteinSequenceSimilarityRun,
) error {
	loader, err := NewDataLoader(p.database, p.cacheDir, run.ID)
	if err != nil {
		return fmt.Errorf("create mmseqs data loader: %w", err)
	}

	sequenceFilePath, err := p.loadSequences(ctx, loader, mode)
	if err != nil {
		return fmt.Errorf("load mmseqs pipeline sequences: %w", err)
	}
	sequenceCount, err := countFASTARecords(sequenceFilePath)
	if err != nil {
		return fmt.Errorf("count loaded protein sequences: %w", err)
	}
	if sequenceCount == 0 {
		if mode == PipelineModeBootstrap {
			return p.execute(ctx, PipelineModeProcess, run)
		}
		if err := p.markRunSucceeded(ctx, run.ID); err != nil {
			return fmt.Errorf("mark empty process run succeeded: %w", err)
		}
		return nil
	}

	existingSimilarityIndexPath, err := p.existingSimilarityIndexPath()
	if err != nil {
		return fmt.Errorf("resolve existing similarity index: %w", err)
	}

	builder, err := NewSimilarityIndexBuilder(p.commands, p.cacheDir, run.ID)
	if err != nil {
		return fmt.Errorf("create mmseqs similarity index builder: %w", err)
	}
	buildResult, err := builder.BuildSimilarityIndex(ctx, sequenceFilePath, existingSimilarityIndexPath)
	if err != nil {
		return fmt.Errorf("build mmseqs similarity index: %w", err)
	}

	if mode == PipelineModeProcess {
		persister, err := NewSimilarityResultPersister(p.database)
		if err != nil {
			return fmt.Errorf("create similarity result persister: %w", err)
		}
		if err := persister.Persist(ctx, SimilarityResultPersisterParams{
			Run:              run,
			SequenceFilePath: sequenceFilePath,
			SearchResultPath: buildResult.SearchResultPath,
		}); err != nil {
			return fmt.Errorf("persist similarity result: %w", err)
		}
	}

	if err := p.publishLatestIndexState(run.ID); err != nil {
		return fmt.Errorf("publish latest similarity index state: %w", err)
	}
	if err := p.deleteOldRuns(run.ID); err != nil {
		return fmt.Errorf("delete old mmseqs runs: %w", err)
	}
	if err := p.markRunSucceeded(ctx, run.ID); err != nil {
		return fmt.Errorf("mark mmseqs similarity run succeeded: %w", err)
	}
	return nil
}

func (p *Pipeline) publishLatestIndexState(runID uuid.UUID) error {
	if runID == uuid.Nil {
		return errors.New("run id is empty")
	}
	now := time.Now().UTC()
	state := MMseqsIndexState{
		LatestCompletedRunID: &runID,
		UpdatedAt:            now,
	}
	if err := WriteIndexState(p.cacheDir, state); err != nil {
		return fmt.Errorf("write mmseqs index state: %w", err)
	}
	return nil
}

func (p *Pipeline) deleteOldRuns(currentRunID uuid.UUID) error {
	if currentRunID == uuid.Nil {
		return errors.New("current run id is empty")
	}

	runsDir := filepath.Join(p.cacheDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read mmseqs runs dir: %w", err)
	}

	currentRunDirName := currentRunID.String()
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == currentRunDirName {
			continue
		}
		path := filepath.Join(runsDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove old mmseqs run dir %s: %w", path, err)
		}
	}
	return nil
}

func (p *Pipeline) markRunSucceeded(ctx context.Context, runID uuid.UUID) error {
	if runID == uuid.Nil {
		return errors.New("run id is empty")
	}
	finishedAt := time.Now().UTC()
	if err := p.database.ProteinSequenceSimilarities.UpdateRunState(
		ctx,
		runID,
		models.ProteinSequenceSimilarityRunStateSucceeded,
		nil,
		&finishedAt,
	); err != nil {
		return fmt.Errorf("update protein sequence similarity run state: %w", err)
	}
	return nil
}

func (p *Pipeline) findActiveRun(ctx context.Context, mode PipelineMode) (*models.ProteinSequenceSimilarityRun, error) {
	state := models.ProteinSequenceSimilarityRunStateRunning
	limit := 100
	runs, err := p.database.ProteinSequenceSimilarities.ListRuns(ctx, db.ProteinSequenceSimilarityRunFilters{
		State: &state,
		Limit: &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list running protein sequence similarity runs: %w", err)
	}
	for _, run := range runs {
		if runMode(run) == mode {
			return &run, nil
		}
	}
	return nil, nil
}

func (p *Pipeline) createRun(ctx context.Context, mode PipelineMode) (*models.ProteinSequenceSimilarityRun, error) {
	version, err := p.commands.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("read mmseqs version: %w", err)
	}

	now := time.Now().UTC()
	startedAt := now
	run, err := p.database.ProteinSequenceSimilarities.CreateRun(ctx, models.ProteinSequenceSimilarityRun{
		ID:    uuid.New(),
		Tool:  "mmseqs2",
		State: models.ProteinSequenceSimilarityRunStateRunning,
		Parameters: map[string]any{
			"mode":          string(mode),
			"format_output": FormatOutput,
			"tool_version":  version,
		},
		StartedAt: &startedAt,
		CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("create protein sequence similarity run: %w", err)
	}
	return run, nil
}

func (p *Pipeline) hasPendingSequences(ctx context.Context) (bool, error) {
	processingState := models.ProteinSequenceProcessingStatePending
	return p.hasProteinSequences(ctx, processingState)
}

func (p *Pipeline) hasProteinSequences(ctx context.Context, processingState models.ProteinSequenceProcessingState) (bool, error) {
	limit := 1
	sequences, err := p.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		ProcessingState: &processingState,
		OrderByID:       true,
		Limit:           &limit,
	})
	if err != nil {
		return false, fmt.Errorf("list protein sequences for pipeline input: %w", err)
	}
	return len(sequences) > 0, nil
}

func (p *Pipeline) loadSequences(ctx context.Context, loader *DataLoader, mode PipelineMode) (string, error) {
	switch mode {
	case PipelineModeBootstrap:
		path, err := loader.LoadHistoricalProteinSequences(ctx)
		if err != nil {
			return "", fmt.Errorf("load historical protein sequences: %w", err)
		}
		return path, nil
	case PipelineModeProcess:
		path, err := loader.LoadPendingProteinSequences(ctx)
		if err != nil {
			return "", fmt.Errorf("load pending protein sequences: %w", err)
		}
		return path, nil
	default:
		return "", fmt.Errorf("unknown mmseqs pipeline mode %q", mode)
	}
}

func (p *Pipeline) mode() (PipelineMode, error) {
	existingSimilarityIndexPath, err := p.existingSimilarityIndexPath()
	if err != nil {
		return "", fmt.Errorf("resolve existing similarity index: %w", err)
	}
	if existingSimilarityIndexPath == "" {
		return PipelineModeBootstrap, nil
	}
	return PipelineModeProcess, nil
}

func (p *Pipeline) existingSimilarityIndexPath() (string, error) {
	state, err := ReadIndexState(p.cacheDir)
	if err != nil {
		return "", fmt.Errorf("read mmseqs index state: %w", err)
	}
	if state == nil || state.LatestCompletedRunID == nil || *state.LatestCompletedRunID == uuid.Nil {
		return "", nil
	}

	path := filepath.Join(p.cacheDir, "runs", state.LatestCompletedRunID.String(), "similarity-index", "index")
	if exists, err := regularFileExists(path); err != nil {
		return "", fmt.Errorf("check existing similarity index file: %w", err)
	} else if !exists {
		return "", nil
	}
	return path, nil
}

func runMode(run models.ProteinSequenceSimilarityRun) PipelineMode {
	if run.Parameters == nil {
		return ""
	}
	value, ok := run.Parameters["mode"]
	if !ok {
		return ""
	}
	mode, ok := value.(string)
	if !ok {
		return ""
	}
	return PipelineMode(mode)
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}
