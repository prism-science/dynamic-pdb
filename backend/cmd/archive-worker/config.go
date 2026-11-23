package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/cmd/archive-worker/worker"
	appconfig "dynamic-pdb/backend/internal/config"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/cdn"
)

type config struct {
	DB        db.Config                 `mapstructure:"db"`
	CDN       cdn.Config                `mapstructure:"cdn"`
	Downloads appconfig.DownloadsConfig `mapstructure:"downloads"`
	Worker    worker.Config             `mapstructure:"archive_worker"`
}

// readConfig reads the same profile as the server (config/<filename>.yml), so the worker and the
// API share the downloads section, and adds the worker's own archive_worker section.
func readConfig(filename string) (config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	appconfig.SetDownloadsDefaults(v)
	v.SetDefault("archive_worker::lease", 10*time.Minute)
	v.SetDefault("archive_worker::heartbeat", time.Minute)
	v.SetDefault("archive_worker::poll_interval", 5*time.Second)
	v.SetDefault("archive_worker::max_attempts", 3)
	v.SetDefault("archive_worker::page_size", 100)
	v.SetDefault("archive_worker::scratch_dir", "")
	v.SetDefault("archive_worker::archive_prefix", "downloads/")
	v.SetDefault("archive_worker::file_read_attempts", 3)
	v.SetDefault("archive_worker::exit_when_idle", false)
	v.SetDefault("archive_worker::file_timeout", 20*time.Minute)
	v.SetDefault("archive_worker::job_timeout", 4*time.Hour)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("archive worker config: read: %w", err)
	}
	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("archive worker config: unmarshal: %w", err)
	}
	if err := appconfig.ValidateDownloads(cfg.Downloads); err != nil {
		return config{}, fmt.Errorf("archive worker config: downloads.%w", err)
	}
	cfg.Worker.ArchiveTTL = cfg.Downloads.ArchiveTTL
	return cfg, nil
}
