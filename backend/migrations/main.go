package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"migrations/internal"
)

func main() {
	if err := runSubcommand(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flag.Usage()
		} else {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}

func runSubcommand(args []string) error {
	cmds := []internal.Runner{
		internal.NewTemplateCmd(),
		internal.NewAddMigrationCmd(),
		internal.NewMigrateParamsCmd(),
		internal.NewDBTunnelCmd(),
	}

	subCommands := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		subCommands = append(subCommands, cmd.Name())
	}

	if len(args) < 1 {
		return fmt.Errorf("sub-command expected, one of: %+v", subCommands)
	}

	subcommand := os.Args[1]

	for _, cmd := range cmds {
		if cmd.Name() == subcommand {
			err := cmd.Init(os.Args[2:])
			if err != nil {
				return err
			}
			return cmd.Run()
		}
	}

	return fmt.Errorf("unknown subcommand: %q", subcommand)
}
