package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dynamic-pdb/backend/cmd/mmseqs-job/jobs"
	"dynamic-pdb/backend/cmd/mmseqs-job/mmseqs"
	"dynamic-pdb/backend/internal/db"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := execute(); err != nil {
		slog.Error("mmseqs job failed", "err", err)
		return 1
	}
	return 0
}

func execute() error {
	mmseqsCacheDir := mmseqsCacheDirFromEnvironment()
	cfg, err := readConfig(envFromEnvironment())
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	database, err := db.NewDB(cfg.DB)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer func() {
		if closeErr := database.Close(); closeErr != nil {
			slog.Error("close db failed", "err", closeErr)
		}
	}()

	commands, err := mmseqs.NewCommands("mmseqs")
	if err != nil {
		return fmt.Errorf("create mmseqs commands: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	job, err := jobs.NewMMseqsJob(database, commands, slog.Default(), mmseqsCacheDir)
	if err != nil {
		return fmt.Errorf("create mmseqs job: %w", err)
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

func mmseqsCacheDirFromEnvironment() string {
	cacheDir := os.Getenv("DYNAMIC_PDB_MMSEQS_CACHE_DIR")
	if strings.TrimSpace(cacheDir) == "" {
		return ".tmp/mmseqs"
	}
	return cacheDir
}
