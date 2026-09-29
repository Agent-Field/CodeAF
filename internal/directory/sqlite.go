package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite" // the one SQLite driver the repo uses
)

// schema has one table per record kind. Each row holds the record as JSON, so
// the stored shape is exactly the wire shape and additive fields need no
// migration. The identity table has at most one row: a SQLite file is one
// identity's directory.
const schema = `
CREATE TABLE IF NOT EXISTS identity (id INTEGER PRIMARY KEY CHECK (id = 1), rec TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS devices  (id TEXT PRIMARY KEY, rec TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS cells    (id TEXT PRIMARY KEY, rec TEXT NOT NULL);
`

// SQLite is a directory kept in one SQLite file, so a relay restart keeps every
// lease, fence and absolute expiry. Like Memory it runs the pure rules; unlike
// Memory the rules run inside one write transaction, which is what makes two
// connections to the same file agree on a single winner.
type SQLite struct {
	db    *sql.DB
	clock func() time.Time
}

// OpenSQLite opens (creating when absent) the directory file at path. Write
// transactions take the file lock up front (_txlock=immediate) and wait for it
// (busy_timeout), so concurrent writers queue instead of failing.
func OpenSQLite(path string, clock func() time.Time) (*SQLite, error) {
	dsn := "file:" + path + "?_txlock=immediate&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &SQLite{db: db, clock: clock}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) init() error {
	return s.inTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		start, _ := json.Marshal(IdentityRec{V: 1})
		_, err := tx.Exec(`INSERT OR IGNORE INTO identity (id, rec) VALUES (1, ?)`, string(start))
		return err
	})
}

// Close releases the file.
func (s *SQLite) Close() error { return s.db.Close() }

// For returns the Client that device would hold.
func (s *SQLite) For(device string) Client { return &sqliteClient{s: s, device: device} }

type sqliteClient struct {
	s      *SQLite
	device string
}

func (s *SQLite) now() int64 { return s.clock().UnixMilli() }

// inTx runs fn in one write transaction and commits only if fn succeeds, so a
// refused rule or a crash leaves the old records untouched.
func (s *SQLite) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// get decodes the record stored under (table, id) into v; ErrNotFound if absent.
func get(tx *sql.Tx, table, id string, v any) error {
	var rec string
	err := tx.QueryRow(`SELECT rec FROM `+table+` WHERE id = ?`, id).Scan(&rec)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(rec), v)
}

// put stores v under (table, id), replacing any earlier record.
func put(tx *sql.Tx, table, id string, v any) error {
	rec, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO `+table+` (id, rec) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET rec = excluded.rec`, id, string(rec))
	return err
}

// change reads cell id, applies fn at the directory time and stores the result.
func (c *sqliteClient) change(ctx context.Context, id string, fn func(Cell, int64) (Cell, error)) (v CellView, err error) {
	err = c.s.inTx(ctx, func(tx *sql.Tx) error {
		var cell Cell
		if err := get(tx, "cells", id, &cell); err != nil {
			return err
		}
		now := c.s.now()
		next, err := fn(cell, now)
		if err != nil {
			return err
		}
		v = CellView{Now: now, Cell: next}
		return put(tx, "cells", id, next)
	})
	return v, err
}

func (c *sqliteClient) List(ctx context.Context) (l Listing, err error) {
	err = c.s.inTx(ctx, func(tx *sql.Tx) error {
		l = Listing{Now: c.s.now(), Devices: map[string]Device{}, Cells: map[string]Cell{}}
		if err := get(tx, "identity", "1", &l.Identity); err != nil {
			return err
		}
		if err := scanAll(tx, "devices", l.Devices); err != nil {
			return err
		}
		return scanAll(tx, "cells", l.Cells)
	})
	return l, err
}

// scanAll fills out with every record in table, keyed by id.
func scanAll[T any](tx *sql.Tx, table string, out map[string]T) error {
	rows, err := tx.Query(`SELECT id, rec FROM ` + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, rec string
		var v T
		if err := rows.Scan(&id, &rec); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(rec), &v); err != nil {
			return err
		}
		out[id] = v
	}
	return rows.Err()
}

func (c *sqliteClient) Cell(ctx context.Context, id string) (CellView, error) {
	return c.change(ctx, id, func(cell Cell, _ int64) (Cell, error) { return cell, nil })
}

func (c *sqliteClient) PutDevice(ctx context.Context, id string, d Device) error {
	return c.s.inTx(ctx, func(tx *sql.Tx) error { return put(tx, "devices", id, d) })
}

func (c *sqliteClient) SetVault(ctx context.Context, old, next string) error {
	return c.s.inTx(ctx, func(tx *sql.Tx) error {
		var rec IdentityRec
		if err := get(tx, "identity", "1", &rec); err != nil {
			return err
		}
		if rec.Vault != old {
			return ErrCAS
		}
		rec.Vault = next
		return put(tx, "identity", "1", rec)
	})
}

func (c *sqliteClient) Create(ctx context.Context, id string, in CellInit) (v CellView, err error) {
	err = c.s.inTx(ctx, func(tx *sql.Tx) error {
		var taken Cell
		switch err := get(tx, "cells", id, &taken); {
		case err == nil:
			return ErrExists
		case !errors.Is(err, ErrNotFound):
			return err
		}
		now := c.s.now()
		cell := Created(in, c.device, now)
		v = CellView{Now: now, Cell: cell}
		return put(tx, "cells", id, cell)
	})
	return v, err
}

func (c *sqliteClient) Acquire(ctx context.Context, id string) (CellView, error) {
	return c.change(ctx, id, func(cell Cell, now int64) (Cell, error) { return Acquire(cell, c.device, now) })
}

func (c *sqliteClient) Heartbeat(ctx context.Context, id string, b Beat) (CellView, error) {
	return c.change(ctx, id, func(cell Cell, now int64) (Cell, error) { return Heartbeat(cell, c.device, b, now) })
}

func (c *sqliteClient) Publish(ctx context.Context, id string, p Publish) (CellView, error) {
	return c.change(ctx, id, func(cell Cell, now int64) (Cell, error) { return PublishTo(cell, c.device, p, now) })
}

func (c *sqliteClient) Release(ctx context.Context, id string, fence uint64) error {
	_, err := c.change(ctx, id, func(cell Cell, _ int64) (Cell, error) { return ReleaseOf(cell, c.device, fence) })
	return err
}

func (c *sqliteClient) Archive(ctx context.Context, id string) error {
	_, err := c.change(ctx, id, func(cell Cell, _ int64) (Cell, error) {
		cell.Archived = true
		return cell, nil
	})
	return err
}
