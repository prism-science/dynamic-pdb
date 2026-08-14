package jobs

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/cmd/mmseqs-job/mmseqs"
	"dynamic-pdb/backend/internal/db"
)

func Test_should_create_mmseqs_job_when_required_dependencies_passed(t *testing.T) {
	// given
	database := &db.DB{}
	commands, err := mmseqs.NewCommands("mmseqs")
	require.NoError(t, err)
	logger := slog.Default()

	// when
	job, err := NewMMseqsJob(database, commands, logger, ".tmp/mmseqs")

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, database, job.database)
	assert.Equal(t, commands, job.commands)
	assert.Equal(t, logger, job.logger)
	assert.Equal(t, ".tmp/mmseqs", job.cacheDir)
}

func Test_should_default_logger_when_mmseqs_job_created_without_logger(t *testing.T) {
	// given
	database := &db.DB{}
	commands, err := mmseqs.NewCommands("mmseqs")
	require.NoError(t, err)

	// when
	job, err := NewMMseqsJob(database, commands, nil, ".tmp/mmseqs")

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.NotNil(t, job.logger)
}

func Test_should_return_error_when_mmseqs_job_dependencies_are_invalid(t *testing.T) {
	tests := []struct {
		name     string
		database *db.DB
		commands *mmseqs.Commands
		cacheDir string
		error    string
	}{
		{
			name:     "database nil",
			commands: mustCommandsForTest(t),
			cacheDir: ".tmp/mmseqs",
			error:    "database is nil",
		},
		{
			name:     "commands nil",
			database: &db.DB{},
			cacheDir: ".tmp/mmseqs",
			error:    "mmseqs commands is nil",
		},
		{
			name:     "cache dir empty",
			database: &db.DB{},
			commands: mustCommandsForTest(t),
			error:    "mmseqs cache dir is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			job, err := NewMMseqsJob(tt.database, tt.commands, nil, tt.cacheDir)

			// then
			require.Error(t, err)
			assert.Nil(t, job)
			assert.Contains(t, err.Error(), tt.error)
		})
	}
}

func mustCommandsForTest(t *testing.T) *mmseqs.Commands {
	t.Helper()
	commands, err := mmseqs.NewCommands("mmseqs")
	require.NoError(t, err)
	return commands
}
