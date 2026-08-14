package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"dynamic-pdb/cli/internal/frontend"
	"dynamic-pdb/cli/internal/version"

	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
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
		Version:       fmt.Sprintf("%s (commit %s, built %s)", version.Version, version.Commit, version.Date),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("dynamic-pdb {{.Version}}\n")

	root.AddCommand(simpleCommand("login", "authenticate to Dynamic PDB", code, func() int {
		return frontend.Login(ctx, stdout, stderr)
	}))
	root.AddCommand(simpleCommand("logout", "clear saved authentication for Dynamic PDB", code, func() int {
		return frontend.Logout(stdout, stderr)
	}))
	root.AddCommand(passThroughCommand("upload", "upload Dynamic PDB datasets", code, func(args []string) int {
		return frontend.Upload(ctx, args, stdout, stderr)
	}))
	root.AddCommand(passThroughCommand("update", "update dynamic-pdb to the latest release", code, func(args []string) int {
		return frontend.Update(ctx, args, stdout, stderr)
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
