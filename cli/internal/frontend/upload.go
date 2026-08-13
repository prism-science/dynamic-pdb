package frontend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"dynamic-pdb/cli/internal/config"
	"dynamic-pdb/cli/internal/dynamicpdbapi"
	"dynamic-pdb/cli/internal/paths"
	"dynamic-pdb/cli/internal/rcsb"
	"dynamic-pdb/cli/internal/upload"
	"dynamic-pdb/cli/internal/upload/manifest"

	"github.com/spf13/pflag"
)

const uploadHelp = `Upload Dynamic PDB datasets.

Usage:
  dynamic-pdb upload manifest init <data-folder> [flags]
  dynamic-pdb upload start <manifest-path>

Subcommands:
  manifest init  create a manifest draft from a data folder
  start          upload entries and files described by a manifest

Init flags:
  --out <path>              manifest output path

Start flags:
  -j, --concurrency <n>              number of entries to upload in parallel (default 1)
      --upload-part-concurrency <n>  number of multipart upload parts per file to upload in parallel (default 1)`

func Upload(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArgs(args) {
		fmt.Fprintln(stdout, uploadHelp)
		return 0
	}
	switch args[0] {
	case "manifest":
		return uploadManifest(args[1:], stdout, stderr)
	case "start":
		return uploadStart(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "dynamic-pdb upload: unknown subcommand %q\n", args[0])
		return 2
	}
}

func uploadManifest(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpArgs(args) {
		fmt.Fprintln(stdout, uploadHelp)
		return 0
	}
	switch args[0] {
	case "init":
		return uploadManifestInit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "dynamic-pdb upload manifest: unknown subcommand %q\n", args[0])
		return 2
	}
}

func uploadManifestInit(args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("upload manifest init", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload manifest init <data-folder> [flags]")
		fs.PrintDefaults()
	}

	options := manifest.Options{}
	fs.StringVar(&options.OutputPath, "out", "", "manifest output path")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload manifest init <data-folder> [flags]")
		return 2
	}

	options.DataRoot = fs.Arg(0)
	outputPath, _, stats, err := manifest.Init(options)
	if err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload manifest init:", err)
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "Wrote %s\nManifest written.\nPDB IDs: %d\nLocal files: %d\n", outputPath, stats.PDBIDs, stats.LocalFiles); err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload manifest init: write summary:", err)
		return 1
	}
	return 0
}

func uploadStart(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("upload start", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload start <manifest-path> [flags]")
		fs.PrintDefaults()
	}
	concurrency := 1
	uploadPartConcurrency := 1
	fs.IntVarP(&concurrency, "concurrency", "j", concurrency, "number of entries to upload in parallel")
	fs.IntVar(&uploadPartConcurrency, "upload-part-concurrency", uploadPartConcurrency, "number of multipart upload parts per file to upload in parallel")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload start <manifest-path> [flags]")
		return 2
	}

	dataHome, err := paths.DataHome()
	if err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload start:", err)
		return 1
	}
	cfg, err := config.Load(dataHome)
	if err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload start:", err)
		return 1
	}
	if cfg.Auth.AccessToken == "" {
		fmt.Fprintln(stderr, "dynamic-pdb upload start: authentication token is missing; run `dynamic-pdb login`")
		return 1
	}
	if !cfg.Auth.ExpiresAt.IsZero() && time.Now().After(cfg.Auth.ExpiresAt) {
		fmt.Fprintln(stderr, "dynamic-pdb upload start: authentication token is expired; run `dynamic-pdb login`")
		return 1
	}

	uploader := upload.New(
		dynamicpdbapi.NewClient(cfg.ServerURL(), cfg.Auth.AccessToken),
		rcsb.NewClient(),
		newUploadProgress(stdout),
		concurrency,
		uploadPartConcurrency,
	)
	summary, err := uploader.Upload(ctx, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload start:", err)
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "Uploaded %d entries, %d models, %d artifacts.\n", summary.Entries, summary.Models, summary.Artifacts); err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload start: write summary:", err)
		return 1
	}
	if summary.StatePath != "" {
		if _, err := fmt.Fprintf(stdout, "Upload state: %s\n", summary.StatePath); err != nil {
			fmt.Fprintln(stderr, "dynamic-pdb upload start: write summary:", err)
			return 1
		}
	}
	return 0
}

func isHelpArgs(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")
}
