package frontend

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dynamic-pdb/cli/internal/version"
)

const defaultReleaseBaseURL = "https://dynamicpdb.com"

var (
	fetchLatestVersion = fetchLatestVersionFromWeb
	installDir         = currentInstallDir
	runInstaller       = runInstallScript
)

func Update(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dynamic-pdb update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetVersion := fs.String("version", "", "install a specific tag (default: latest)")
	force := fs.Bool("force", false, "install even if already on the target version")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: dynamic-pdb update [--version vX.Y.Z] [--force]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "dynamic-pdb update: unexpected argument: %s\n", fs.Arg(0))
		fs.Usage()
		return 1
	}

	current := version.Version
	isDev := !version.IsRelease()
	if isDev && *targetVersion == "" && !*force {
		fmt.Fprintf(stderr, "dynamic-pdb update: dev build (version=%q); pass --version <tag> to install a release\n", current)
		return 1
	}

	target := *targetVersion
	if target == "" {
		latest, err := fetchLatestVersion(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "dynamic-pdb update: check latest version: %v\n", err)
			return 1
		}
		target = latest
	}
	target = strings.TrimSpace(target)
	if target == "" {
		fmt.Fprintln(stderr, "dynamic-pdb update: latest version is empty")
		return 1
	}
	if !*force && !isDev && target == current {
		fmt.Fprintf(stdout, "dynamic-pdb is already at %s\n", current)
		return 0
	}

	dir, err := installDir()
	if err != nil {
		fmt.Fprintf(stderr, "dynamic-pdb update: find install directory: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Updating dynamic-pdb to %s...\n", target)
	if err := runInstaller(ctx, dir, target, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "dynamic-pdb update: install %s: %v\n", target, err)
		return 1
	}
	return 0
}

func releaseBaseURL() string {
	if baseURL := strings.TrimRight(os.Getenv("DYNAMIC_PDB_BASE_URL"), "/"); baseURL != "" {
		return baseURL
	}
	return defaultReleaseBaseURL
}

func fetchLatestVersionFromWeb(ctx context.Context) (string, error) {
	url := releaseBaseURL() + "/releases/latest.txt"
	httpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(httpCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func currentInstallDir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return filepath.Dir(executable), nil
}

func runInstallScript(ctx context.Context, dir string, targetVersion string, stdout, stderr io.Writer) error {
	script, err := fetchInstallScript(ctx)
	if err != nil {
		return err
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("find bash: %w", err)
	}
	command := exec.CommandContext(ctx, bash, "-s", "--", "--dir", dir, "--version", targetVersion)
	command.Stdin = strings.NewReader(script)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = append(os.Environ(), "DYNAMIC_PDB_BASE_URL="+releaseBaseURL())
	if err := command.Run(); err != nil {
		return fmt.Errorf("run install.sh: %w", err)
	}
	return nil
}

func fetchInstallScript(ctx context.Context) (string, error) {
	url := releaseBaseURL() + "/install.sh"
	httpCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(httpCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return string(data), nil
}
