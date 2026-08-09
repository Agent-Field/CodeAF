// Projector database bootstrap — port of src/storage/db.ts:90-106
// (swe-pro 3b25a1a). Migrations and the global path policy remain owned by the
// storage layer; this package reproduces the projector connection contract.
package projectors

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// SQLExecutor is the minimal database surface needed by Configure.
type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Configure applies the exact startup PRAGMA sequence. Only journal_mode is
// retried: it is the first file-touching statement and the cold-start recovery
// collision point in the TypeScript implementation.
func Configure(ctx context.Context, db SQLExecutor, retry BusyRetryOptions) error {
	_, err := WithBusyRetry(func() (sql.Result, error) {
		return db.ExecContext(ctx, "PRAGMA journal_mode = WAL")
	}, retry)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA wal_checkpoint(PASSIVE)",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// Open creates the cgo-free SQLite connection and configures it for projector
// use. A single physical connection mirrors Bun's synchronous sqlite client and
// keeps connection-local PRAGMAs effective.
func Open(ctx context.Context, path string, retry BusyRetryOptions) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("projectors: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	retry.DBPath = path
	if err := Configure(ctx, db, retry); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
