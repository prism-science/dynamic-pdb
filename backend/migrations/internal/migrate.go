package internal

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
)

func NewMigrateParamsCmd() Runner {
	cmd := &migrateParamsCmd{}

	return cmd
}

type migrateParamsCmd struct {
	environment string
}

func (m *migrateParamsCmd) Init(args []string) error {
	if len(args) >= 1 {
		m.environment = args[0]
	}
	if !slices.Contains(targetEnvironments, m.environment) {
		return fmt.Errorf("invalid environment: %q, one of %+v expected", m.environment, targetEnvironments)
	}
	return nil
}

func (m *migrateParamsCmd) Run() error {
	envVars := map[string]string{}
	schemas, err := getSchemas()
	if err != nil {
		return err
	}
	locations, err := m.generateLocations(schemas)
	if err != nil {
		return err
	}
	envVars["SCHEMAS"] = strings.Join(schemas, ",")
	envVars["LOCATIONS"] = strings.Join(locations, ",")
	envFile, err := os.Create("flyway.env")
	if err != nil {
		return fmt.Errorf("could not create flyway.env: %w", err)
	}
	writer := bufio.NewWriter(envFile)
	for k, v := range envVars {
		_, err := fmt.Fprintf(writer, "FLYWAY_%s=%s\n", k, v)
		if err != nil {
			return fmt.Errorf("could not write: %w", err)
		}
	}
	return writer.Flush()
}

func (m *migrateParamsCmd) generateLocations(schemas []string) ([]string, error) {
	var locations []string
	for _, schema := range schemas {
		schemaLocations, err := m.generateLocationsForSchema(schema)
		if err != nil {
			return nil, err
		}
		for _, loc := range schemaLocations {
			locations = append(locations, "filesystem:sql/db/"+schema+"/"+loc)
		}
	}
	return locations, nil
}

func (m *migrateParamsCmd) generateLocationsForSchema(schema string) ([]string, error) {
	locations := []string{
		"structure",
		"functions",
		"data/common",
	}
	stat, err := os.Stat("db/" + schema + "/data/" + m.environment)
	if err != nil {
		return nil, fmt.Errorf(
			"could not get data for environment %q and schema %q: %w",
			m.environment,
			schema,
			err,
		)
	}
	if stat.IsDir() {
		locations = append(locations, "data/"+m.environment)
	}

	return locations, nil
}

func (m *migrateParamsCmd) Name() string {
	return "migrate-params"
}
