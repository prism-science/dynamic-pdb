package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// TxManager is a minimal transaction manager: it runs a function inside a single
// database transaction and propagates that transaction through context.Context
// so repository calls automatically join it.

// Querier is the common subset of *sqlx.DB and *sqlx.Tx that repositories use,
// so a statement can run either on the pool or on an active transaction without
// caring which. Stmtx, StmtxContext, NamedStmt and NamedStmtContext are not
// included.
//
//nolint:interfacebloat
type Querier interface {
	sqlx.ExtContext

	sqlx.Preparer
	Preparex(query string) (*sqlx.Stmt, error)
	PreparexContext(ctx context.Context, query string) (*sqlx.Stmt, error)
	PrepareNamed(query string) (*sqlx.NamedStmt, error)
	PrepareNamedContext(ctx context.Context, query string) (*sqlx.NamedStmt, error)

	sqlx.Execer
	MustExec(query string, args ...interface{}) sql.Result
	MustExecContext(ctx context.Context, query string, args ...interface{}) sql.Result
	NamedExec(query string, arg interface{}) (sql.Result, error)
	NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)

	sqlx.Queryer
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	NamedQuery(query string, arg interface{}) (*sqlx.Rows, error)

	Select(dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error

	Get(dest interface{}, query string, args ...interface{}) error
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

type txCtxKey struct{}

// txKey is the context key under which an active *sqlx.Tx is stored.
var txKey = txCtxKey{}

func withTx(ctx context.Context, tx *sqlx.Tx) context.Context {
	return context.WithValue(ctx, txKey, tx)
}

func txFromCtx(ctx context.Context) *sqlx.Tx {
	tx, _ := ctx.Value(txKey).(*sqlx.Tx)
	return tx
}

// TxManager runs functions inside a database transaction.
type TxManager struct {
	db *sqlx.DB
}

// NewTxManager creates a TxManager backed by db.
func NewTxManager(db *sqlx.DB) *TxManager {
	return &TxManager{db: db}
}

// Do runs fn inside a single database transaction whose handle is propagated
// through ctx; repository calls made with that ctx join the transaction. The
// transaction commits when fn returns nil and rolls back when it returns an
// error or panics (the panic is re-raised after rollback).
//
// If ctx already carries a transaction (a nested Do), fn joins it directly and
// commit/rollback is left to the outermost Do — the inner Do neither opens nor
// closes a transaction.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if txFromCtx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// Already panicking — rollback is best-effort cleanup; the panic carries the real failure.
			_ = tx.Rollback()
			panic(p)
		}

		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = fmt.Errorf("db: rollback after transaction error: %w", errors.Join(err, rollbackErr))
			}
			return
		}

		if commitErr := tx.Commit(); commitErr != nil {
			err = fmt.Errorf("db: commit: %w", commitErr)
		}
	}()

	return fn(withTx(ctx, tx))
}

// QuerierProvider resolves the Querier a statement should run on: the
// transaction active in the context, or the connection pool when none is.
type QuerierProvider struct{}

// NewQuerierProvider creates a QuerierProvider.
func NewQuerierProvider() *QuerierProvider {
	return &QuerierProvider{}
}

// DefaultQuerierProvider is a ready-to-use QuerierProvider.
var DefaultQuerierProvider = NewQuerierProvider()

// Querier returns the transaction stored in ctx, or db if none is active.
func (p *QuerierProvider) Querier(ctx context.Context, db Querier) Querier {
	if tx := txFromCtx(ctx); tx != nil {
		return tx
	}
	return db
}
