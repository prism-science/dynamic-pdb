package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/jobs"
)

type config struct {
	DB              db.Config                  `mapstructure:"db"`
	ArchiveRederive jobs.ArchiveRederiveConfig `mapstructure:"archive_rederive"`
}

func readConfig(filename string) (config, error) {
	settings := viper.NewWithOptions(viper.KeyDelimiter("::"))
	settings.SetEnvPrefix("DYNAMIC_PDB")
	settings.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	settings.AutomaticEnv()
	settings.SetDefault("archive_rederive::max_entries_per_run", 0)
	settings.SetDefault("archive_rederive::page_size", 200)
	settings.SetDefault("archive_rederive::download_concurrency", 4)
	settings.SetDefault("archive_rederive::run_for", 50*time.Minute)
	settings.SetDefault("archive_rederive::dry_run", false)
	settings.SetConfigType("yaml")
	settings.AddConfigPath("config")
	settings.SetConfigName(filename)

	if err := settings.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("archive re-derivation config: read: %w", err)
	}

	var cfg config
	if err := settings.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("archive re-derivation config: unmarshal: %w", err)
	}
	return cfg, nil
}
