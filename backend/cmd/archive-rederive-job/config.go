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
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("archive_rederive::max_entries_per_run", 0)
	v.SetDefault("archive_rederive::page_size", 200)
	v.SetDefault("archive_rederive::download_concurrency", 4)
	v.SetDefault("archive_rederive::run_for", 50*time.Minute)
	v.SetDefault("archive_rederive::dry_run", false)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("archive re-derivation config: read: %w", err)
	}

	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("archive re-derivation config: unmarshal: %w", err)
	}
	return cfg, nil
}
