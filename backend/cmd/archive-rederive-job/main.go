package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/jobs"
	"dynamic-pdb/lib/rcsb"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := execute(); err != nil {
		slog.Error("archive re-derivation job failed", "err", err)
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

	// Heads skip the response cache, so the client keeps no cache of its own.
	job, err := jobs.NewArchiveRederiveJob(database, rcsb.NewClient(rcsb.WithCacheEntries(0)), cfg.ArchiveRederive, slog.Default())
	if err != nil {
		return fmt.Errorf("create archive re-derivation job: %w", err)
	}
	if err := job.Run(ctx); err != nil {
		return fmt.Errorf("run archive re-derivation job: %w", err)
	}
	return nil
}

func envFromEnvironment() string {
	env := os.Getenv("DYNAMIC_PDB_ENV")
	if strings.TrimSpace(env) == "" {
		return "local"
	}
	return env
}
