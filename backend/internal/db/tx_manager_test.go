package db_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
)

var (
	txTestOnce sync.Once
	txTestConn *sqlx.DB
)

// txManagerTestDB opens a dedicated connection and a scratch table the TxManager
// tests write to, so they exercise commit/rollback without depending on the
// application schema. The shared TestMain (suite_test.go) owns testDB; this
// keeps the TxManager tests self-contained on the same local Postgres.
func txManagerTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	txTestOnce.Do(func() {
		conn, err := sqlx.Connect("postgres", "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable")
		require.NoError(t, err)

		_, err = conn.Exec(`create table if not exists tx_manager_test (k text primary key)`)
		require.NoError(t, err)

		txTestConn = conn
	})

	return txTestConn
}

func insertTxTestKey(ctx context.Context, querier db.Querier, key string) error {
	_, err := querier.ExecContext(ctx, `insert into tx_manager_test (k) values ($1)`, key)
	return err
}

func txTestKeyExists(t *testing.T, key string) bool {
	t.Helper()

	var count int
	require.NoError(t, txManagerTestDB(t).Get(&count, `select count(*) from tx_manager_test where k = $1`, key))
	return count > 0
}

func Test_should_commit_inserted_row_when_fn_returns_nil(t *testing.T) {
	// given
	txManager := db.NewTxManager(txManagerTestDB(t))
	queriers := db.NewQuerierProvider()
	key := uuid.NewString()

	// when
	err := txManager.Do(context.Background(), func(ctx context.Context) error {
		return insertTxTestKey(ctx, queriers.Querier(ctx, txManagerTestDB(t)), key)
	})

	// then
	require.NoError(t, err)
	assert.True(t, txTestKeyExists(t, key))
}

func Test_should_roll_back_inserted_row_when_fn_returns_error(t *testing.T) {
	// given
	txManager := db.NewTxManager(txManagerTestDB(t))
	queriers := db.NewQuerierProvider()
	key := uuid.NewString()
	boom := errors.New("boom")

	// when
	err := txManager.Do(context.Background(), func(ctx context.Context) error {
		if insertErr := insertTxTestKey(ctx, queriers.Querier(ctx, txManagerTestDB(t)), key); insertErr != nil {
			return insertErr
		}
		return boom
	})

	// then
	require.ErrorIs(t, err, boom)
	assert.False(t, txTestKeyExists(t, key))
}

func Test_should_reuse_outer_transaction_when_do_is_nested(t *testing.T) {
	// given
	txManager := db.NewTxManager(txManagerTestDB(t))
	queriers := db.NewQuerierProvider()
	outerKey := uuid.NewString()
	innerKey := uuid.NewString()
	boom := errors.New("boom")

	// when
	err := txManager.Do(context.Background(), func(ctx context.Context) error {
		if insertErr := insertTxTestKey(ctx, queriers.Querier(ctx, txManagerTestDB(t)), outerKey); insertErr != nil {
			return insertErr
		}

		nestedErr := txManager.Do(ctx, func(ctx context.Context) error {
			return insertTxTestKey(ctx, queriers.Querier(ctx, txManagerTestDB(t)), innerKey)
		})
		require.NoError(t, nestedErr)

		return boom
	})

	// then
	require.ErrorIs(t, err, boom)
	// The inner Do joined the outer transaction rather than committing on its own,
	// so the outer rollback discards the inner insert too.
	assert.False(t, txTestKeyExists(t, outerKey))
	assert.False(t, txTestKeyExists(t, innerKey))
}

func Test_should_roll_back_and_repanic_when_fn_panics(t *testing.T) {
	// given
	txManager := db.NewTxManager(txManagerTestDB(t))
	queriers := db.NewQuerierProvider()
	key := uuid.NewString()

	// when / then
	assert.PanicsWithValue(t, "boom", func() {
		_ = txManager.Do(context.Background(), func(ctx context.Context) error {
			if insertErr := insertTxTestKey(ctx, queriers.Querier(ctx, txManagerTestDB(t)), key); insertErr != nil {
				return insertErr
			}
			panic("boom")
		})
	})

	// then
	assert.False(t, txTestKeyExists(t, key))
}

func Test_should_return_pool_when_no_transaction_is_active(t *testing.T) {
	// given
	queriers := db.NewQuerierProvider()
	pool := txManagerTestDB(t)

	// when
	querier := queriers.Querier(context.Background(), pool)

	// then
	assert.Same(t, pool, querier)
}
