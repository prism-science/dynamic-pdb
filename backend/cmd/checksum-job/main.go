package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dynamic-pdb/backend/cmd/checksum-job/jobs"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/integrations/s3"
	"dynamic-pdb/backend/internal/services/cdn"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := execute(); err != nil {
		slog.Error("checksum job failed", "err", err)
		return 1
	}
	return 0
}

func execute() error {
	cfg, err := readConfig(envFromEnvironment())
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := db.NewDB(cfg.DB)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer func() {
		if closeErr := database.Close(); closeErr != nil {
			slog.Error("close db failed", "err", closeErr)
		}
	}()

	bucket, err := s3.NewBucket(ctx, cfg.CDN.S3)
	if err != nil {
		return fmt.Errorf("create s3 bucket: %w", err)
	}
	files, err := cdn.NewArtifactStore(bucket, cfg.CDN)
	if err != nil {
		return fmt.Errorf("create artifact store: %w", err)
	}

	job, err := jobs.NewChecksumJob(
		jobs.NewDatabaseQueue(database),
		files,
		jobs.NewHTTPSource(jobs.NewGuardedHTTPClient()),
		cfg.Checksum,
		slog.Default(),
	)
	if err != nil {
		return fmt.Errorf("create checksum job: %w", err)
	}
	return job.Run(ctx)
}

func envFromEnvironment() string {
	env := os.Getenv("DYNAMIC_PDB_ENV")
	if strings.TrimSpace(env) == "" {
		return "local"
	}
	return env
}
