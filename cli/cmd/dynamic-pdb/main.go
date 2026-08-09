package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"dynamic-pdb/cli/internal/frontend"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	code := 0
	root := newRootCommand(ctx, stdout, stderr, &code)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if _, writeErr := fmt.Fprintln(stderr, "dynamic-pdb:", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return code
}

func newRootCommand(ctx context.Context, stdout, stderr io.Writer, code *int) *cobra.Command {
	root := &cobra.Command{
		Use:           "dynamic-pdb <command> [args]",
		Short:         "Dynamic PDB console client",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(simpleCommand("login", "authenticate to Dynamic PDB", code, func() int {
		return frontend.Login(ctx, stdout, stderr)
	}))
	root.AddCommand(simpleCommand("logout", "clear saved authentication for Dynamic PDB", code, func() int {
		return frontend.Logout(stdout, stderr)
	}))
	root.AddCommand(passThroughCommand("manifest", "build upload manifests", code, func(args []string) int {
		return frontend.Manifest(ctx, args, stdout, stderr)
	}))

	return root
}

func passThroughCommand(use, short string, code *int, run func([]string) int) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		Run: func(_ *cobra.Command, args []string) {
			*code = run(args)
		},
	}
}

func simpleCommand(use, short string, code *int, run func() int) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			*code = run()
		},
	}
}
