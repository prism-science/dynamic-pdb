package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/cmd/checksum-job/jobs"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/cdn"
)

type config struct {
	DB       db.Config   `mapstructure:"db"`
	CDN      cdn.Config  `mapstructure:"cdn"`
	Checksum jobs.Config `mapstructure:"checksum"`
}

func readConfig(filename string) (config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("checksum::concurrency", 4)
	v.SetDefault("checksum::per_host_concurrency", 2)
	v.SetDefault("checksum::batch_size", 50)
	v.SetDefault("checksum::max_files_per_run", 0)
	v.SetDefault("checksum::claim_for", 50*time.Minute)
	v.SetDefault("checksum::file_timeout", 20*time.Minute)
	v.SetDefault("checksum::copy_external", true)
	v.SetDefault("checksum::copy_limit_bytes", int64(1<<30))
	v.SetDefault("checksum::fill_batch_size", 2000)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("checksum config: read: %w", err)
	}

	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("checksum config: unmarshal: %w", err)
	}
	return cfg, nil
}
