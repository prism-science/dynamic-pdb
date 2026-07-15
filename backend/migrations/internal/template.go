package internal

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
)

func NewTemplateCmd() Runner {
	cmd := &templateCmd{
		fs: flag.NewFlagSet("template", flag.ContinueOnError),
	}

	cmd.fs.StringVar(&cmd.schema, "schema", "", "schema to create")

	return cmd
}

type templateCmd struct {
	fs *flag.FlagSet

	schema string
}

func (t *templateCmd) Name() string {
	return t.fs.Name()
}

func (t *templateCmd) Init(args []string) error {
	return t.fs.Parse(args)
}

func (t *templateCmd) Run() error {
	if t.schema == "" {
		// ask user for schema name
		for {
			fmt.Print("Enter schema name: ")
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			t.schema = scanner.Text()
			if t.schema != "" {
				matched, err := regexp.MatchString("^[a-z_-]+$", t.schema)
				if err != nil {
					return fmt.Errorf("failed to validate schema name: %w", err)
				}
				if !matched {
					fmt.Println("Schema name must contain only lowercase letters, underscores, and hyphens")
				} else {
					break
				}
			}
		}
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get working directory: %w", err)
	}
	// create directories for schema layout
	// ./db/{schema}/data
	//     	  /common
	//     	  /local
	//     	  /dev
	//     	  /prod
	// ./db/{schema}/functions
	// ./db/{schema}/structure

	// create files for schema layout
	commonPrefix := "db/" + t.schema
	dataDirs := allDataEnvironments
	dirs := []string{
		"functions",
		"structure",
	}
	for _, dir := range dataDirs {
		dirs = append(dirs, "data/"+dir)
	}

	for _, dir := range dirs {
		dirName := commonPrefix + "/" + dir
		if err := os.MkdirAll(dirName, 0755); err != nil {
			return err
		}
		// create empty .gitignore file to keep directories in git
		if _, err := os.Create(dirName + "/.gitignore"); err != nil {
			return err
		}
	}
	println("schema layout created successfully in " + workingDirectory + "/" + commonPrefix)
	return nil
}
