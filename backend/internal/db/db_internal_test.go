package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_cancel_statement_when_it_runs_longer_than_statement_timeout(t *testing.T) {
	// given
	cfg := localDockerTestConfig()
	cfg.StatementTimeout = 100 * time.Millisecond
	database := newInternalTestDB(t, cfg)
	started := time.Now()

	// when
	_, err := database.sqlx.ExecContext(context.Background(), "select pg_sleep(5)")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "statement timeout")
	assert.Less(t, time.Since(started), 2*time.Second)
}

func Test_should_ping_when_every_main_pool_connection_is_busy(t *testing.T) {
	// given
	cfg := localDockerTestConfig()
	cfg.MaxOpenConnections = 1
	database := newInternalTestDB(t, cfg)
	busyConnection, err := database.sqlx.Connx(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, busyConnection.Close())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// when
	err = database.Ping(ctx)

	// then
	assert.NoError(t, err)
}

func Test_should_add_statement_timeout_in_milliseconds_when_building_dsn(t *testing.T) {
	// given
	cfg := localDockerTestConfig()
	cfg.StatementTimeout = 10 * time.Second

	// when
	dsn := dsnFrom(cfg)

	// then
	assert.Contains(t, dsn, " sslmode=disable statement_timeout=10000")
}

func Test_should_leave_out_statement_timeout_when_building_dsn_without_one(t *testing.T) {
	// given
	cfg := localDockerTestConfig()

	// when
	dsn := dsnFrom(cfg)

	// then
	assert.NotContains(t, dsn, "statement_timeout")
}

func localDockerTestConfig() Config {
	return Config{
		Host:             "localhost",
		Port:             35432,
		Name:             "dynamic_pdb_local",
		Username:         "postgres",
		Password:         "password",
		ConnectionParams: "sslmode=disable",
	}
}

func newInternalTestDB(t *testing.T, cfg Config) *DB {
	t.Helper()

	database, err := NewDB(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, database.Close())
	})
	return database
}
