package internal

import (
	"fmt"
	"os"
)

type Runner interface {
	Init(args []string) error
	Run() error
	Name() string
}

var targetEnvironments = []string{"local", "dev", "prod"}

const commonDataDir = "common"

var allDataEnvironments = func() []string {
	allEnvs := make([]string, 0, len(targetEnvironments)+1)
	allEnvs = append(allEnvs, targetEnvironments...)
	return append(allEnvs, commonDataDir)
}()

func getSchemas() ([]string, error) {
	return getSubdirNames("db")
}

func getSubdirNames(dir string) ([]string, error) {
	// read schemas from db directory, return list of directories
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("could not read db directory: %w", err)
	}
	// public is the default schema and must be the first in the list
	schemas := []string{"public"}
	for _, entry := range entries {
		if entry.Name() == "public" {
			continue
		}
		if entry.IsDir() {
			schemas = append(schemas, entry.Name())
		}
	}
	return schemas, nil
}
