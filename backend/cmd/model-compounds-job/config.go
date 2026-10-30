package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/cmd/model-compounds-job/jobs"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/cdn"
)

type config struct {
	DB        db.Config   `mapstructure:"db"`
	CDN       cdn.Config  `mapstructure:"cdn"`
	Compounds jobs.Config `mapstructure:"compounds"`
}

func readConfig(filename string) (config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("compounds::max_files_per_run", 0)
	// A run starts every 5 minutes; it stops claiming before the next one starts.
	v.SetDefault("compounds::claim_for", 4*time.Minute)
	// A 1 GiB upload streams in well under a minute; this stops one that hangs.
	v.SetDefault("compounds::file_timeout", 10*time.Minute)
	v.SetDefault("compounds::recent_window", 48*time.Hour)
	v.SetDefault("compounds::historical_fill", false)
	v.SetDefault("compounds::fill_batch_size", 2000)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("model compounds config: read: %w", err)
	}

	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("model compounds config: unmarshal: %w", err)
	}
	return cfg, nil
}
