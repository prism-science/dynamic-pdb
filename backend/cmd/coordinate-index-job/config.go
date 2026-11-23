package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/cmd/coordinate-index-job/jobs"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/cdn"
)

type config struct {
	DB              db.Config   `mapstructure:"db"`
	CDN             cdn.Config  `mapstructure:"cdn"`
	CoordinateIndex jobs.Config `mapstructure:"coordinate_index"`
}

func readConfig(filename string) (config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("coordinate_index::max_files_per_run", 0)
	// A run starts every 5 minutes; it stops claiming before the next one starts.
	v.SetDefault("coordinate_index::claim_for", 4*time.Minute)
	// A 1 GiB upload streams in well under a minute; this stops one that hangs.
	v.SetDefault("coordinate_index::file_timeout", 10*time.Minute)
	v.SetDefault("coordinate_index::recent_window", 48*time.Hour)
	v.SetDefault("coordinate_index::fill", false)
	v.SetDefault("coordinate_index::fill_batch_size", 2000)
	// Archive model files are read only where config/<env>.yml turns them on.
	v.SetDefault("coordinate_index::archive_files", false)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("coordinate index config: read: %w", err)
	}

	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("coordinate index config: unmarshal: %w", err)
	}
	return cfg, nil
}
