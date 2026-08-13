package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"dynamic-pdb/backend/cmd/mmseqs-job/mmseqs"
	"dynamic-pdb/backend/internal/db"
)

type MMseqsJob struct {
	database *db.DB
	commands *mmseqs.Commands
	logger   *slog.Logger
	cacheDir string
}

func NewMMseqsJob(
	database *db.DB,
	commands *mmseqs.Commands,
	logger *slog.Logger,
	cacheDir string,
) (*MMseqsJob, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if commands == nil {
		return nil, errors.New("mmseqs commands is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cacheDir == "" {
		return nil, errors.New("mmseqs cache dir is empty")
	}

	return &MMseqsJob{
		database: database,
		commands: commands,
		logger:   logger,
		cacheDir: cacheDir,
	}, nil
}

func (j *MMseqsJob) Run(ctx context.Context) error {
	pipeline, err := mmseqs.NewPipeline(j.database, j.commands, j.logger, j.cacheDir)
	if err != nil {
		return fmt.Errorf("create mmseqs pipeline: %w", err)
	}
	if err := pipeline.Run(ctx); err != nil {
		return fmt.Errorf("run mmseqs pipeline: %w", err)
	}
	return nil
}
