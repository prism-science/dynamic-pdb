package internal

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func NewAddMigrationCmd() Runner {
	cmd := &addMigrationCmd{}

	return cmd
}

type addMigrationCmd struct {
	schema        string
	migrationType string
	description   string
}

func (a *addMigrationCmd) Name() string {
	return "add-migration"
}

func (a *addMigrationCmd) Init(args []string) error {
	return nil
}

func (a *addMigrationCmd) Run() error {
	err := a.setSchema()
	if err != nil {
		return err
	}
	a.setMigrationType()
	a.setDescription()
	migrationDir := "db/" + a.schema + "/" + a.migrationType
	if a.migrationType == "data" {
		dataDir, err := a.getMigrationDataEnvironment()
		if err != nil {
			return err
		}
		migrationDir += "/" + dataDir
	}
	migrationPrefix := a.getMigrationPrefix()
	migrationFile := migrationPrefix + a.description + ".sql"
	// create migration file
	fmt.Println("Creating migration file: ", migrationDir+"/"+migrationFile)
	file, err := os.Create(migrationDir + "/" + migrationFile)
	if err != nil {
		return fmt.Errorf("could not create migration file: %w", err)
	}
	return file.Close()
}

func (a *addMigrationCmd) setDescription() {
	if a.description == "" {
		// ask user to provide description in interactive mode
		for {
			fmt.Println("Enter migration description:")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			a.description = scanner.Text()
			if a.description != "" {
				break
			}
			fmt.Println("Description should not be empty")
		}
	}
	a.description = strings.ReplaceAll(a.description, " ", "_")
}

func (a *addMigrationCmd) setMigrationType() {
	if a.migrationType == "" {
		// ask user to provide migration type in interactive mode
		migrationTypes := []string{"data", "functions", "structure"}
		fmt.Println("Enter migration type, one of: ", migrationTypes, " (default: structure)")
		for {
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			a.migrationType = scanner.Text()
			if a.migrationType == "" {
				a.migrationType = "structure"
				fmt.Println("Using default migration type: ", a.migrationType)
			}
			if slices.Contains(migrationTypes, a.migrationType) {
				break
			}
			fmt.Println("Invalid migration type, should be one of: ", migrationTypes)
		}
	}
}

func (a *addMigrationCmd) getMigrationPrefix() string {
	// set migration prefix based on migration type
	switch a.migrationType {
	case "structure":
		// structure migrations are versioned migrations with prefix V{timestamp}__
		// where timestamp is UTC time in format year-month-day-hour-minute-second
		// it's used to order migrations in the correct order
		return "V" + time.Now().UTC().Format("20060102150405") + "__"
	case "functions":
		// functions migrations are repeatable migrations with prefix R__{priority}__
		// where priority is a number from 0 to 499, used to prioritize migrations
		// to apply them in the order of priority
		return fmt.Sprintf("R__%03d__", a.getMigrationsPriority(100, 0, 499))
	case "data":
		// data migrations are repeatable migrations with prefix R__{priority}__
		// where priority is a number from 500 to 999, the priorities range for data
		// migrations is higher than for functions migrations because data migrations
		// should be applied after functions migrations as they may call functions
		return fmt.Sprintf("R__%03d__", a.getMigrationsPriority(600, 500, 999))
	default:
		panic("unknown migration type: " + a.migrationType)
	}
}

func (a *addMigrationCmd) getMigrationDataEnvironment() (string, error) {
	dataDirs, err := getSubdirNames("db/" + a.schema + "/data")
	if err != nil {
		return "", fmt.Errorf("could not get data directories: %w", err)
	}
	fmt.Println("Available data directories: ", dataDirs, " (default: "+commonDataDir+")")
	for {
		fmt.Println("Enter data directory:")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		dataDir := scanner.Text()
		if dataDir == "" {
			dataDir = commonDataDir
			fmt.Println("Using default data directory: ", dataDir)
			return dataDir, nil
		}
		if slices.Contains(dataDirs, dataDir) {
			return dataDir, nil
		}
		fmt.Println("Invalid data directory, should be one of: ", dataDirs)
	}
}

func (a *addMigrationCmd) getMigrationsPriority(def, min, max int) int {
	fmt.Printf("Enter migration priority (%d-%d), default [%d]:", min, max, def)
	fmt.Println()
	for {
		fmt.Println()
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		priorityStr := scanner.Text()
		if priorityStr != "" {
			// check if priority is a number
			priority, err := strconv.Atoi(priorityStr)
			if err == nil {
				if priority >= min && priority <= max {
					return priority
				}
			}
		} else {
			fmt.Println("Using default priority: ", def)
			return def
		}
		fmt.Printf("Invalid priority, should be a number from %d to %d, default [%d]", min, max, def)
		fmt.Println()
	}
}

func (a *addMigrationCmd) setSchema() error {
	if a.schema == "" {
		// ask user to provide schema in interactive mode
		schemas, err := getSchemas()
		if err != nil {
			return fmt.Errorf("could not get schemas: %w", err)
		}
		fmt.Println("Available schemas: ", schemas)
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Println("Enter schema name:")
			scanner.Scan()
			a.schema = scanner.Text()
			if slices.Contains(schemas, a.schema) {
				break
			}
			fmt.Println("Invalid schema, should be one of: ", schemas)
		}
	}
	return nil
}
