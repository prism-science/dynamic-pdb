package frontend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
  dynamic-pdb upload start <manifest-path|data-folder> [flags]

Subcommands:
  manifest init  create a manifest draft from a data folder
  start          upload entries and files described by a manifest or recognized data folder

Init flags:
  --out <path>              manifest output path
  --include-rcsb-model      include the deposited RCSB model as the first model

Start flags:
  -j, --concurrency <n>              number of entries to upload in parallel (default 1)
      --upload-part-concurrency <n>  number of multipart upload parts per file to upload in parallel (default 1)
      --include <pdb-id>[,...]       PDB IDs to upload, overriding manifest filter.include
      --skip <pdb-id>[,...]          PDB IDs to skip, overriding manifest filter.skip`

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
	fs.BoolVar(&options.IncludeRCSBModel, "include-rcsb-model", false, "include the deposited RCSB model as the first model")

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
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload start <manifest-path|data-folder> [flags]")
		fs.PrintDefaults()
	}
	concurrency := 1
	uploadPartConcurrency := 1
	var includePDBIDs []string
	var skipPDBIDs []string
	fs.IntVarP(&concurrency, "concurrency", "j", concurrency, "number of entries to upload in parallel")
	fs.IntVar(&uploadPartConcurrency, "upload-part-concurrency", uploadPartConcurrency, "number of multipart upload parts per file to upload in parallel")
	fs.StringSliceVar(&includePDBIDs, "include", nil, "PDB IDs to upload, overriding manifest filter.include")
	fs.StringSliceVar(&skipPDBIDs, "skip", nil, "PDB IDs to skip, overriding manifest filter.skip")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: dynamic-pdb upload start <manifest-path|data-folder> [flags]")
		return 2
	}

	manifestPath, err := resolveUploadManifestPath(fs.Arg(0), stdout)
	if err != nil {
		fmt.Fprintln(stderr, "dynamic-pdb upload start:", err)
		return 1
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
	uploadOptions := upload.UploadOptions{}
	if fs.Changed("include") {
		uploadOptions.Include = includePDBIDs
		uploadOptions.OverrideInclude = true
	}
	if fs.Changed("skip") {
		uploadOptions.Skip = skipPDBIDs
		uploadOptions.OverrideSkip = true
	}
	summary, err := uploader.Upload(ctx, manifestPath, uploadOptions)
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

func resolveUploadManifestPath(inputPath string, stdout io.Writer) (string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return inputPath, nil
	}
	if !info.IsDir() {
		return inputPath, nil
	}
	outputPath := filepath.Join(inputPath, manifest.SampleWorksDefaultFilename)
	writtenPath, _, stats, err := manifest.InitSampleWorks(inputPath, outputPath)
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"Detected Sampleworks data folder; wrote manifest %s (PDB IDs: %d, local files: %d).\n",
		writtenPath,
		stats.PDBIDs,
		stats.LocalFiles,
	); err != nil {
		return "", fmt.Errorf("write Sampleworks manifest summary: %w", err)
	}
	return writtenPath, nil
}

func isHelpArgs(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")
}
