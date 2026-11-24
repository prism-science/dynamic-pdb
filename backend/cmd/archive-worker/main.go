// Command archive-worker plans bulk-download archives (POST /v1/downloads), recording each one's
// preview, and builds the ones asked for into the data bucket. It runs as its own Deployment, one
// job at a time in each of its two lanes (planning, building), until it is told to stop.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dynamic-pdb/backend/cmd/archive-worker/worker"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/integrations/s3"
	"dynamic-pdb/backend/internal/services/cdn"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := execute(); err != nil {
		slog.Error("archive worker failed", "err", err)
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

	store := worker.NewDatabaseStore(database)
	archiveWorker, err := worker.New(store, store, files, cfg.Worker, slog.Default())
	if err != nil {
		return fmt.Errorf("create archive worker: %w", err)
	}
	slog.Info("archive worker configured", "lease", cfg.Worker.Lease, "archive_ttl", cfg.Worker.ArchiveTTL,
		"archive_prefix", cfg.Worker.ArchivePrefix)
	return archiveWorker.Run(ctx)
}

func envFromEnvironment() string {
	env := os.Getenv("DYNAMIC_PDB_ENV")
	if strings.TrimSpace(env) == "" {
		return "local"
	}
	return env
}
