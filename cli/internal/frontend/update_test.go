package frontend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"dynamic-pdb/cli/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_refuse_dev_build_when_update_called_without_version(t *testing.T) {
	// given
	withVersion(t, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Update(context.Background(), nil, &stdout, &stderr)

	// then
	assert.Equal(t, 1, exitCode)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "dev build")
	assert.Contains(t, stderr.String(), "pass --version")
}

func Test_should_run_installer_with_requested_version_when_update_called(t *testing.T) {
	// given
	withVersion(t, "v0.0.1")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var gotDir string
	var gotVersion string
	withInstallDir(t, "/tmp/dynamic-pdb-bin")
	withInstaller(t, func(_ context.Context, dir string, targetVersion string, stdout io.Writer, _ io.Writer) error {
		gotDir = dir
		gotVersion = targetVersion
		_, err := stdout.Write([]byte("installer ran\n"))
		return err
	})

	// when
	exitCode := Update(context.Background(), []string{"--version", "v1.2.3"}, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "/tmp/dynamic-pdb-bin", gotDir)
	assert.Equal(t, "v1.2.3", gotVersion)
	assert.Contains(t, stdout.String(), "Updating dynamic-pdb to v1.2.3")
	assert.Contains(t, stdout.String(), "installer ran")
	assert.Empty(t, stderr.String())
}

func Test_should_resolve_latest_version_when_update_called_without_version(t *testing.T) {
	// given
	withVersion(t, "v0.0.1")
	withInstallDir(t, "/tmp/dynamic-pdb-bin")
	withLatestVersion(t, "v1.2.4")
	var gotVersion string
	withInstaller(t, func(_ context.Context, _ string, targetVersion string, _ io.Writer, _ io.Writer) error {
		gotVersion = targetVersion
		return nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Update(context.Background(), nil, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "v1.2.4", gotVersion)
	assert.Empty(t, stderr.String())
}

func Test_should_skip_installer_when_current_release_is_already_target(t *testing.T) {
	// given
	withVersion(t, "v1.2.3")
	withLatestVersion(t, "v1.2.3")
	withInstaller(t, func(context.Context, string, string, io.Writer, io.Writer) error {
		return errors.New("installer should not run")
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Update(context.Background(), nil, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "dynamic-pdb is already at v1.2.3\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func Test_should_run_installer_when_current_release_is_target_but_force_is_set(t *testing.T) {
	// given
	withVersion(t, "v1.2.3")
	withInstallDir(t, "/tmp/dynamic-pdb-bin")
	var ran bool
	withInstaller(t, func(context.Context, string, string, io.Writer, io.Writer) error {
		ran = true
		return nil
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Update(context.Background(), []string{"--version", "v1.2.3", "--force"}, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.True(t, ran)
	assert.Empty(t, stderr.String())
}

func withVersion(t *testing.T, value string) {
	t.Helper()
	original := version.Version
	version.Version = value
	t.Cleanup(func() {
		version.Version = original
	})
}

func withLatestVersion(t *testing.T, value string) {
	t.Helper()
	original := fetchLatestVersion
	fetchLatestVersion = func(context.Context) (string, error) {
		return value, nil
	}
	t.Cleanup(func() {
		fetchLatestVersion = original
	})
}

func withInstallDir(t *testing.T, value string) {
	t.Helper()
	original := installDir
	installDir = func() (string, error) {
		return value, nil
	}
	t.Cleanup(func() {
		installDir = original
	})
}

func withInstaller(t *testing.T, installer func(context.Context, string, string, io.Writer, io.Writer) error) {
	t.Helper()
	require.NotNil(t, installer)
	original := runInstaller
	runInstaller = installer
	t.Cleanup(func() {
		runInstaller = original
	})
}
