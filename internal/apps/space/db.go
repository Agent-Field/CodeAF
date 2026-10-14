package space

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	_ "modernc.org/sqlite"
)

const (
	sessionTTL = 30 * 24 * time.Hour
	inviteTTL  = 7 * 24 * time.Hour
	codeTTL    = 10 * time.Minute
	codeTries  = 5
)

type memberRow struct {
	ID, Handle, Email, Role string
	Joined, LastSeen        time.Time
}

type inviteRow struct {
	ID, SecretHash, Email, Role, By string
	Expires                         time.Time
	Used, Cancelled                 bool
}

func openDB(home string) (*sql.DB, error) {
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "space.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS members (
  id TEXT PRIMARY KEY,
  handle TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  joined TEXT NOT NULL,
  last_seen TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS invites (
  id TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  by_handle TEXT NOT NULL,
  expires TEXT NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  cancelled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  member TEXT NOT NULL,
  expires TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS codes (
  email TEXT PRIMARY KEY,
  code_hash TEXT NOT NULL,
  invite_id TEXT NOT NULL DEFAULT '',
  expires TEXT NOT NULL,
  tries INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseStamp(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

func metaGet(tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func metaSet(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func insertMember(tx *sql.Tx, row memberRow) error {
	_, err := tx.Exec(`INSERT INTO members (id, handle, email, role, joined, last_seen) VALUES (?, ?, ?, ?, ?, ?)`,
		row.ID, row.Handle, row.Email, row.Role, stamp(row.Joined), stamp(row.LastSeen))
	return err
}

func scanMember(sc interface{ Scan(...any) error }) (memberRow, error) {
	var row memberRow
	var joined, seen string
	err := sc.Scan(&row.ID, &row.Handle, &row.Email, &row.Role, &joined, &seen)
	if err != nil {
		return row, err
	}
	row.Joined, err = parseStamp(joined)
	if err != nil {
		return row, err
	}
	row.LastSeen, err = parseStamp(seen)
	return row, err
}

func memberByID(tx *sql.Tx, id string) (memberRow, error) {
	row, err := scanMember(tx.QueryRow(`SELECT id, handle, email, role, joined, last_seen FROM members WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return memberRow{}, notFound("no member " + id)
	}
	return row, err
}

func memberByEmail(tx *sql.Tx, email string) (memberRow, bool, error) {
	row, err := scanMember(tx.QueryRow(`SELECT id, handle, email, role, joined, last_seen FROM members WHERE email = ? AND email != ''`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return memberRow{}, false, nil
	}
	return row, err == nil, err
}

func listMembers(tx *sql.Tx) ([]memberRow, error) {
	rows, err := tx.Query(`SELECT id, handle, email, role, joined, last_seen FROM members ORDER BY joined, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memberRow
	for rows.Next() {
		row, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func countRole(tx *sql.Tx, role svc.WorkspaceRole) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM members WHERE role = ?`, string(role)).Scan(&n)
	return n, err
}

func scanInvite(sc interface{ Scan(...any) error }) (inviteRow, error) {
	var row inviteRow
	var expires string
	var used, cancelled int
	err := sc.Scan(&row.ID, &row.SecretHash, &row.Email, &row.Role, &row.By, &expires, &used, &cancelled)
	if err != nil {
		return row, err
	}
	row.Expires, err = parseStamp(expires)
	row.Used, row.Cancelled = used != 0, cancelled != 0
	return row, err
}

func inviteByID(tx *sql.Tx, id string) (inviteRow, error) {
	row, err := scanInvite(tx.QueryRow(`SELECT id, secret_hash, email, role, by_handle, expires, used, cancelled FROM invites WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return inviteRow{}, notFound("no invite " + id)
	}
	return row, err
}

func listInvites(tx *sql.Tx) ([]inviteRow, error) {
	rows, err := tx.Query(`SELECT id, secret_hash, email, role, by_handle, expires, used, cancelled FROM invites ORDER BY expires, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inviteRow
	for rows.Next() {
		row, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (row inviteRow) dead(now time.Time) bool {
	return row.Used || row.Cancelled || !now.Before(row.Expires)
}

func (row memberRow) wire() bridge.Member {
	return bridge.Member{ID: row.ID, Name: row.Handle, Email: row.Email, Role: row.Role}
}
