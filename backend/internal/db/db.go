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
	Users                       *UsersRepository
	Roles                       *RolesRepository
	Permissions                 *PermissionsRepository
	DataSyncJobs                *DataSyncJobsRepository
	Entries                     *EntriesRepository
	EntrySearch                 *EntrySearchIndexRepository
	Models                      *ModelsRepository
	Artifacts                   *ArtifactsRepository
	Metrics                     *MetricsRepository
	Runs                        *RunsRepository
	ProteinSequences            *ProteinSequencesRepository
	ProteinSequenceSimilarities *ProteinSequenceSimilaritiesRepository

	sqlx      *sqlx.DB
	txManager *TxManager
}

const advisoryLockCleanupTimeout = 5 * time.Second

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
		Users:                       NewUsersRepository(sqlxDB, queriers),
		Roles:                       NewRolesRepository(sqlxDB, queriers),
		Permissions:                 NewPermissionsRepository(sqlxDB, queriers),
		DataSyncJobs:                NewDataSyncJobsRepository(sqlxDB, queriers),
		Entries:                     NewEntriesRepository(sqlxDB, queriers),
		EntrySearch:                 NewEntrySearchIndexRepository(sqlxDB, queriers),
		Models:                      NewModelsRepository(sqlxDB, queriers),
		Artifacts:                   NewArtifactsRepository(sqlxDB, queriers),
		Metrics:                     NewMetricsRepository(sqlxDB, queriers),
		Runs:                        NewRunsRepository(sqlxDB, queriers),
		ProteinSequences:            NewProteinSequencesRepository(sqlxDB, queriers),
		ProteinSequenceSimilarities: NewProteinSequenceSimilaritiesRepository(sqlxDB, queriers),
		sqlx:                        sqlxDB,
		txManager:                   txManager,
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

func (d *DB) RunLocked(
	ctx context.Context,
	lockName string,
	run func(context.Context) error,
) (ran bool, err error) {
	connection, err := d.sqlx.Connx(ctx)
	if err != nil {
		return false, fmt.Errorf("get advisory lock connection: %w", err)
	}

	locked := false
	defer func() {
		if locked {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), advisoryLockCleanupTimeout)
			defer cancel()

			var unlocked bool
			if unlockErr := connection.GetContext(
				cleanupCtx,
				&unlocked,
				`select pg_advisory_unlock(hashtextextended($1, 0))`,
				lockName,
			); unlockErr != nil {
				err = errors.Join(err, fmt.Errorf("release advisory lock: %w", unlockErr))
			} else if !unlocked {
				err = errors.Join(err, errors.New("release advisory lock: lock was not held"))
			}
		}
		if closeErr := connection.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close advisory lock connection: %w", closeErr))
		}
	}()

	if err := connection.GetContext(
		ctx,
		&locked,
		`select pg_try_advisory_lock(hashtextextended($1, 0))`,
		lockName,
	); err != nil {
		return false, fmt.Errorf("acquire advisory lock: %w", err)
	}
	if !locked {
		return false, nil
	}

	if err := run(ctx); err != nil {
		return true, fmt.Errorf("run locked function: %w", err)
	}
	return true, nil
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
