package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
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
CREATE TABLE IF NOT EXISTS version  (id INTEGER PRIMARY KEY CHECK (id = 1), n INTEGER NOT NULL);
`

// SQLite is a directory kept in one SQLite file, so a relay restart keeps every
// lease, fence and absolute expiry. Like Memory it runs the pure rules; unlike
// Memory the rules run inside one write transaction, which is what makes two
// connections to the same file agree on a single winner.
type SQLite struct {
	pairing
	db    *sql.DB
	clock func() time.Time

	// feed is the Status as last committed: the revoked devices a relay turns
	// away and the rotation the blob wire asks on every frame put, both without
	// a query, and the version watchers hear. inTx is the only thing that
	// publishes to it, after a commit, so it is never ahead of the records and
	// only behind them while a write is in flight.
	feed *Feed

	mu    sync.RWMutex
	grace GraceBounds
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
	s := &SQLite{db: db, clock: clock, feed: NewFeed(clock), grace: DefaultGraceBounds}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	wireFeed(s.feed, &s.pairing, s.seen)
	return s, nil
}

// init makes the tables and their one row each, and publishes where the file
// stands. It cannot run through inTx, which reads the version row first.
func (s *SQLite) init() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	st, err := seed(tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.feed.Publish(st)
	return nil
}

// seed creates what is missing in tx and answers the Status the file holds.
func seed(tx *sql.Tx) (Status, error) {
	start, _ := json.Marshal(IdentityRec{V: 1})
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{schema, nil},
		{`INSERT OR IGNORE INTO identity (id, rec) VALUES (1, ?)`, []any{string(start)}},
		{`INSERT OR IGNORE INTO version (id, n) VALUES (1, 0)`, nil},
	} {
		if _, err := tx.Exec(q.sql, q.args...); err != nil {
			return Status{}, err
		}
	}
	return statusIn(tx)
}

var _ Watchable = (*SQLite)(nil)

// Feed is how a watcher hears of changes.
func (s *SQLite) Feed() *Feed { return s.feed }

// SetMaxWatchers changes the per-identity watcher cap.
func (s *SQLite) SetMaxWatchers(n int) { s.feed.SetMaxWatchers(n) }

// versionIn reads the directory version inside tx.
func versionIn(tx *sql.Tx) (n uint64, err error) {
	return n, tx.QueryRow(`SELECT n FROM version WHERE id = 1`).Scan(&n)
}

// statusIn reads the whole Status inside tx.
func statusIn(tx *sql.Tx) (Status, error) {
	var rec IdentityRec
	devices := map[string]Device{}
	n, err := versionIn(tx)
	if err == nil {
		err = get(tx, "identity", "1", &rec)
	}
	if err == nil {
		err = scanAll(tx, "devices", devices)
	}
	return statusOf(n, rec, devices), err
}

// SetGraceBounds changes what a retire may ask for.
func (s *SQLite) SetGraceBounds(b GraceBounds) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grace = b
}

func (s *SQLite) bounds() GraceBounds {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.grace
}

// Rotated answers the identity's Rotation, nil while it is live. It never reads
// the file, so a relay may ask it on every request.
func (s *SQLite) Rotated() *Rotation { return s.feed.Status().Rotation }

// Revoked says whether device has been stopped. It never reads the file, so a
// relay may ask it on every request.
func (s *SQLite) Revoked(device string) bool { return s.feed.Status().Stopped[device] }

// Close ends every watch and releases the file.
func (s *SQLite) Close() error {
	s.feed.Close()
	return s.db.Close()
}

// For returns the Client that device would hold.
func (s *SQLite) For(device string) Client {
	return &sqliteClient{pairing: &s.pairing, s: s, device: device}
}

type sqliteClient struct {
	*pairing
	s      *SQLite
	device string
}

func (s *SQLite) now() int64 { return s.clock().UnixMilli() }

// tx is inTx for the calling device: inside the same transaction that would
// write, it first refuses a revoked device, so no verb can be the one that
// forgets to. The record is read from the file, not from the revoked set, so
// two connections to one file agree.
func (c *sqliteClient) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return c.s.inTx(ctx, func(tx *sql.Tx) error {
		var me Device
		if err := get(tx, "devices", c.device, &me); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if me.Revoked {
			return ErrRevoked
		}
		return fn(tx)
	})
}

// txw is tx for a verb that changes a record: inside the same transaction it
// refuses a replaced identity, so no verb can be the one that forgets to.
func (c *sqliteClient) txw(ctx context.Context, fn func(*sql.Tx) error) error {
	return c.tx(ctx, func(tx *sql.Tx) error {
		var rec IdentityRec
		if err := get(tx, "identity", "1", &rec); err != nil {
			return err
		}
		if err := refusesWrites(rec); err != nil {
			return err
		}
		return fn(tx)
	})
}

// inTx runs fn in one write transaction and commits only if fn succeeds, so a
// refused rule or a crash leaves the old records untouched. It is also the one
// place a change is announced: put moves the version inside the transaction, and
// when the version moved, the Status read before the commit is published after
// it. Every verb runs here, so no verb can change a record and forget to tell
// the watchers.
func (s *SQLite) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	st, err := s.run(tx, fn)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if st != nil {
		s.feed.Publish(*st)
	}
	return nil
}

// run is fn inside tx, answering the Status to publish when fn changed the
// version and nil when it did not.
func (s *SQLite) run(tx *sql.Tx, fn func(*sql.Tx) error) (*Status, error) {
	before, err := versionIn(tx)
	if err != nil {
		return nil, err
	}
	if err := fn(tx); err != nil {
		return nil, err
	}
	if after, err := versionIn(tx); err != nil || after == before {
		return nil, err
	}
	st, err := statusIn(tx)
	return &st, err
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

// put stores v under (table, id), replacing any earlier record, and moves the
// directory version by one when the record is new or differs from the stored
// one in a way a person could see at time now.
func put[T any](tx *sql.Tx, feed *Feed, table, id string, v T, now int64) error {
	changed, err := visiblyChanges(tx, feed, table, id, v, now)
	if err == nil && changed {
		err = bump(tx)
	}
	if err != nil {
		return err
	}
	rec, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO `+table+` (id, rec) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET rec = excluded.rec`, id, string(rec))
	return err
}

// visiblyChanges says whether storing v under (table, id) would show a person
// something new: the record is absent, or differs from the stored one.
func visiblyChanges[T any](tx *sql.Tx, feed *Feed, table, id string, v T, now int64) (bool, error) {
	var old T
	err := get(tx, table, id, &old)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	return err == nil && feed.differs(id, old, v, now), err
}

func bump(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE version SET n = n + 1 WHERE id = 1`)
	return err
}

// change reads cell id, applies fn at the directory time and stores the result.
func (c *sqliteClient) change(ctx context.Context, id string, fn func(Cell, int64) (Cell, error)) (v CellView, err error) {
	err = c.txw(ctx, func(tx *sql.Tx) error {
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
		return put(tx, c.s.feed, "cells", id, next, now)
	})
	return v, err
}

func (c *sqliteClient) List(ctx context.Context) (l Listing, err error) {
	err = c.tx(ctx, func(tx *sql.Tx) error {
		l = Listing{Now: c.s.now(), Devices: map[string]Device{}, Cells: map[string]Cell{}}
		var err error
		if l.Version, err = versionIn(tx); err != nil {
			return err
		}
		if err := get(tx, "identity", "1", &l.Identity); err != nil {
			return err
		}
		if err := scanAll(tx, "devices", l.Devices); err != nil {
			return err
		}
		if err := scanAll(tx, "cells", l.Cells); err != nil {
			return err
		}
		for id, cell := range l.Cells {
			l.Cells[id] = c.s.feed.Lifted(id, cell)
		}
		return nil
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

func (c *sqliteClient) Cell(ctx context.Context, id string) (v CellView, err error) {
	err = c.tx(ctx, func(tx *sql.Tx) error {
		v.Now = c.s.now()
		if err := get(tx, "cells", id, &v.Cell); err != nil {
			return err
		}
		v.Cell = c.s.feed.Lifted(id, v.Cell)
		return nil
	})
	return v, err
}

func (c *sqliteClient) Rotate(ctx context.Context, req RotationReq) (v RotationView, err error) {
	err = c.tx(ctx, func(tx *sql.Tx) error {
		var rec IdentityRec
		if err := get(tx, "identity", "1", &rec); err != nil {
			return err
		}
		now := c.s.now()
		if rec, err = RotateBy(rec, c.device, req, now, c.s.bounds()); err != nil {
			return err
		}
		v = viewOf(rec, now, c.s.bounds())
		return put(tx, c.s.feed, "identity", "1", rec, now)
	})
	return v, err
}

func (c *sqliteClient) Rotation(ctx context.Context) (v RotationView, err error) {
	err = c.tx(ctx, func(tx *sql.Tx) error {
		var rec IdentityRec
		if err := get(tx, "identity", "1", &rec); err != nil {
			return err
		}
		v = viewOf(rec, c.s.now(), c.s.bounds())
		return nil
	})
	return v, err
}

// PutDevice keeps the stored Revoked flag whatever the record says, so the
// only way to stop a device is Revoke and a device cannot clear its own stop.
func (c *sqliteClient) PutDevice(ctx context.Context, id string, d Device) error {
	return c.txw(ctx, func(tx *sql.Tx) error {
		_, err := c.s.store(tx, id, d)
		return err
	})
}

// store writes a device record as the directory keeps it and answers it.
func (s *SQLite) store(tx *sql.Tx, id string, d Device) (Device, error) {
	var old Device
	err := get(tx, "devices", id, &old)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return d, err
	}
	now := s.now()
	d = Stamped(old, err == nil, d, now)
	return d, put(tx, s.feed, "devices", id, d, now)
}

func (c *sqliteClient) ApproveRequest(ctx context.Context, code string, a Approval) error {
	return c.approve(code, a, func(r Request) (d Device, err error) {
		err = c.txw(ctx, func(tx *sql.Tx) (err error) {
			d, err = c.s.store(tx, r.Device, a.Device)
			return err
		})
		return d, err
	})
}

func (c *sqliteClient) DenyRequest(ctx context.Context, code string) error {
	return c.deny(code, func() error { return c.txw(ctx, func(*sql.Tx) error { return nil }) })
}

func (c *sqliteClient) Revoke(ctx context.Context, id string) error {
	return c.txw(ctx, func(tx *sql.Tx) error {
		var d Device
		if err := get(tx, "devices", id, &d); err != nil {
			return err
		}
		d, err := RevokeOf(d, c.device, id)
		if err != nil {
			return err
		}
		return put(tx, c.s.feed, "devices", id, d, c.s.now())
	})
}

func (c *sqliteClient) SetVault(ctx context.Context, old, next string) error {
	return c.txw(ctx, func(tx *sql.Tx) error {
		var rec IdentityRec
		if err := get(tx, "identity", "1", &rec); err != nil {
			return err
		}
		if rec.Vault != old {
			return ErrCAS
		}
		rec.Vault = next
		return put(tx, c.s.feed, "identity", "1", rec, c.s.now())
	})
}

func (c *sqliteClient) Create(ctx context.Context, id string, in CellInit) (v CellView, err error) {
	err = c.txw(ctx, func(tx *sql.Tx) error {
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
		return put(tx, c.s.feed, "cells", id, cell, now)
	})
	return v, err
}

func (c *sqliteClient) Acquire(ctx context.Context, id string, o AcquireOpts) (CellView, error) {
	return c.change(ctx, id, func(cell Cell, now int64) (Cell, error) {
		return Acquire(c.s.feed.Lifted(id, cell), c.device, now, o.Force)
	})
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
