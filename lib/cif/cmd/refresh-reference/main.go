// Command refresh-reference checks the compound reference lists of lib/cif
// against the chemical component dictionary (CCD) on RCSB and regenerates the
// files made by rule (metal_ions.json, ccd_types.json). It needs the network,
// so it runs by hand, never in CI:
//
//	go run ./lib/cif/cmd/refresh-reference           # check, then write
//	go run ./lib/cif/cmd/refresh-reference --check   # report only; exit 1 on drift or a problem
//
// A reviewed ID is never rewritten. A missing, obsolete or replaced ID, or a
// reviewed metal the metal rule would drop, is reported and nothing is written.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("refresh-reference", flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("dir", "lib/cif", "directory holding the reference JSON files")
	check := flags.Bool("check", false, "report drift against the dictionary without writing; exit 1 on drift or a problem")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	root, err := os.OpenRoot(*directory)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open %s: %v\n", *directory, err)
		return 1
	}
	defer func() { _ = root.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	today := time.Now().UTC().Format(time.DateOnly)
	result, err := refresh(ctx, newRCSBDictionary(), root, today, *check)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "refresh-reference: %v\n", err)
		return 1
	}
	report(stdout, result, *check)
	switch {
	case len(result.Problems) > 0:
		return 1
	case *check && len(result.Drift) > 0:
		return 1
	default:
		return 0
	}
}

func report(out io.Writer, result refreshResult, check bool) {
	for _, problem := range result.Problems {
		_, _ = fmt.Fprintf(out, "PROBLEM %s\n", problem)
	}
	for _, name := range result.Drift {
		_, _ = fmt.Fprintf(out, "DRIFT   %s differs from what the dictionary gives\n", name)
	}
	for _, note := range result.Notes {
		_, _ = fmt.Fprintf(out, "NOTE    %s\n", note)
	}
	for _, name := range result.Written {
		_, _ = fmt.Fprintf(out, "WROTE   %s\n", name)
	}
	switch {
	case len(result.Problems) > 0:
		_, _ = fmt.Fprintln(out, "nothing written: fix the problems above first")
	case check && result.clean():
		_, _ = fmt.Fprintln(out, "reference lists match the dictionary")
	case check:
		_, _ = fmt.Fprintln(out, "run without --check to regenerate the files above")
	}
}
