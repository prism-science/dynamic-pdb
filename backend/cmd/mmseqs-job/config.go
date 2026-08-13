package main

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/internal/db"
)

type config struct {
	DB db.Config `mapstructure:"db"`
}

func readConfig(filename string) (config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return config{}, fmt.Errorf("mmseqs config: read: %w", err)
	}

	var cfg config
	if err := v.Unmarshal(&cfg); err != nil {
		return config{}, fmt.Errorf("mmseqs config: unmarshal: %w", err)
	}
	return cfg, nil
}
