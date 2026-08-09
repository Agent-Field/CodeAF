package projectors

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type recordingExecutor struct {
	statements []string
	journal    int
}

func (r *recordingExecutor) ExecContext(_ context.Context, statement string, _ ...any) (sql.Result, error) {
	r.statements = append(r.statements, statement)
	if statement == "PRAGMA journal_mode = WAL" {
		r.journal++
		if r.journal == 1 {
			return nil, &codedError{name: "SQLITE_BUSY_RECOVERY", code: 261}
		}
	}
	return driver.RowsAffected(0), nil
}

func TestConfigurePragmaOrderAndNarrowRetry(t *testing.T) {
	exec := &recordingExecutor{}
	if err := Configure(context.Background(), exec, BusyRetryOptions{
		Random: func() float64 { return 0 },
		Sleep:  func(time.Duration) {},
		Log:    io.Discard,
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA wal_checkpoint(PASSIVE)",
	}
	if !reflect.DeepEqual(exec.statements, want) {
		t.Fatalf("statements:\n%q\nwant:\n%q", exec.statements, want)
	}
}

func TestOpenConfiguresRealSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "working-memory.db")
	db, err := Open(ctx, path, BusyRetryOptions{Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	checkPragma(t, db, "journal_mode", "wal")
	checkPragma(t, db, "synchronous", int64(1))
	checkPragma(t, db, "busy_timeout", int64(5000))
	checkPragma(t, db, "cache_size", int64(-64000))
	checkPragma(t, db, "foreign_keys", int64(1))
}

func TestOpenRetriesRealSQLiteJournalContention(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "contended.db")

	holder, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	holder.SetMaxOpenConns(1)
	if _, err := holder.ExecContext(ctx, "CREATE TABLE hold (id integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, "INSERT INTO hold VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	sleeps := 0
	db, err := Open(ctx, path, BusyRetryOptions{
		MaxAttempts: intPtr(3),
		Random:      func() float64 { return 0 },
		Sleep: func(time.Duration) {
			sleeps++
			if _, rollbackErr := holder.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
				t.Fatalf("release holder: %v", rollbackErr)
			}
		},
		Log: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if sleeps != 1 {
		t.Fatalf("retry sleeps = %d, want 1", sleeps)
	}
	checkPragma(t, db, "journal_mode", "wal")
}

func checkPragma(t *testing.T, db *sql.DB, name string, want any) {
	t.Helper()
	var got any
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PRAGMA %s = %#v (%T), want %#v (%T)", name, got, got, want, want)
	}
}
