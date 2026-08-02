package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // register the PostgreSQL driver with database/sql
)

type DB struct {
	Users            *UsersRepository
	Entries          *EntriesRepository
	EntrySearch      *EntrySearchIndexRepository
	Models           *ModelsRepository
	Entities         *EntitiesRepository
	EntityRelations  *EntityRelationsRepository
	ProteinSequences *ProteinSequencesRepository

	sqlx      *sqlx.DB
	txManager *TxManager
}

func NewDB(cfg Config) (*DB, error) {
	if cfg.Host == "" {
		return nil, errors.New("db: host is empty")
	}

	sqlxDB, err := sqlx.Connect("postgres", dsnFrom(cfg))
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}

	if cfg.MaxOpenConnections > 0 {
		sqlxDB.SetMaxOpenConns(cfg.MaxOpenConnections)
	}
	if cfg.MaxIdleConnections > 0 {
		sqlxDB.SetMaxIdleConns(cfg.MaxIdleConnections)
	}
	if cfg.MaxConnectionIdleTime != "" {
		idleTime, err := time.ParseDuration(cfg.MaxConnectionIdleTime)
		if err != nil {
			return nil, fmt.Errorf("db: invalid max_connection_idle_time: %w", err)
		}
		sqlxDB.SetConnMaxIdleTime(idleTime)
	}
	if cfg.MaxConnectionLifetime != "" {
		lifetime, err := time.ParseDuration(cfg.MaxConnectionLifetime)
		if err != nil {
			return nil, fmt.Errorf("db: invalid max_connection_lifetime: %w", err)
		}
		sqlxDB.SetConnMaxLifetime(lifetime)
	}

	txManager := NewTxManager(sqlxDB)
	queriers := DefaultQuerierProvider
	return &DB{
		Users:            NewUsersRepository(sqlxDB, queriers),
		Entries:          NewEntriesRepository(sqlxDB, queriers),
		EntrySearch:      NewEntrySearchIndexRepository(sqlxDB, queriers),
		Models:           NewModelsRepository(sqlxDB, queriers),
		Entities:         NewEntitiesRepository(sqlxDB, queriers),
		EntityRelations:  NewEntityRelationsRepository(sqlxDB, queriers),
		ProteinSequences: NewProteinSequencesRepository(sqlxDB, queriers),
		sqlx:             sqlxDB,
		txManager:        txManager,
	}, nil
}

func (d *DB) Close() error {
	if err := d.sqlx.Close(); err != nil {
		return fmt.Errorf("db: close: %w", err)
	}
	return nil
}

func (d *DB) Ping(ctx context.Context) error {
	if err := d.sqlx.PingContext(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

func (d *DB) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return d.txManager.Do(ctx, fn)
}

func dsnFrom(cfg Config) string {
	parts := []string{
		fmt.Sprintf("host=%s", cfg.Host),
		fmt.Sprintf("port=%d", cfg.Port),
		fmt.Sprintf("user=%s", cfg.Username),
		fmt.Sprintf("password=%s", cfg.Password),
		fmt.Sprintf("dbname=%s", cfg.Name),
	}
	if cfg.ConnectionParams != "" {
		parts = append(parts, cfg.ConnectionParams)
	}
	return strings.Join(parts, " ")
}
