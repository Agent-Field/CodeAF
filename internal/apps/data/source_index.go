package data

import (
	"database/sql"
	"errors"
	"time"
)

const sourceIndexMigration = `
	CREATE TABLE source_indexes (
		binding TEXT PRIMARY KEY,
		app TEXT NOT NULL,
		connection TEXT NOT NULL,
		digest TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		state BLOB NOT NULL
	);
	CREATE INDEX source_indexes_expiry ON source_indexes(expires_at);
`

// SourceIndex reads adapter-owned state from the person's database. Expired
// metadata is never returned, even if the engine was closed during expiry.
func (s *Store) SourceIndex(binding string) (state []byte, digest string, err error) {
	var expiry string
	err = s.rdb.QueryRow("SELECT state, digest, expires_at FROM source_indexes WHERE binding = ?", binding).
		Scan(&state, &digest, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err == nil && expiry <= s.now().UTC().Format(stamp) {
		state = nil
	}
	return
}

const sourceIndexRetention = 24 * time.Hour

// SaveSourceIndex commits the cursor and the corresponding metadata together.
// An older concurrent scan never overwrites a newer cursor.
func (s *Store) SaveSourceIndex(app, connection, binding, previous, digest string, state []byte) error {
	if s.draft != "" || app == "" || connection == "" || binding == "" || digest == "" || len(state) == 0 || len(state) > 1<<20 {
		return errorf(CodeInvalid, "the source index is outside the permitted scope or size")
	}
	return s.write(func(w *writeTx) error {
		pub, err := readPublication(w.tx, app)
		if err != nil || pub.Archived || pub.Deleted {
			return errorf(CodeNotAllowed, "the source index belongs to a kept app")
		}
		_, err = w.tx.Exec(`INSERT INTO source_indexes(binding, app, connection, digest, expires_at, state)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(binding) DO UPDATE SET digest = excluded.digest,
			expires_at = excluded.expires_at, state = excluded.state WHERE source_indexes.digest = ?`,
			binding, app, connection, digest, w.now.Add(sourceIndexRetention).UTC().Format(stamp), state, previous)
		return err
	})
}

// ClearSourceIndexes removes the data belonging to an operator's old binding.
func (s *Store) ClearSourceIndexes(app, connection string) error {
	return s.write(func(w *writeTx) error {
		_, err := w.tx.Exec("DELETE FROM source_indexes WHERE app = ? AND connection = ?", app, connection)
		return err
	})
}

// PruneSourceIndexes expires metadata without running a write on every poll.
func (s *Store) PruneSourceIndexes() error {
	var due bool
	err := s.rdb.QueryRow("SELECT EXISTS(SELECT 1 FROM source_indexes WHERE expires_at <= ?)", s.now().UTC().Format(stamp)).Scan(&due)
	if err != nil || !due {
		return err
	}
	return s.write(func(w *writeTx) error {
		_, err := w.tx.Exec("DELETE FROM source_indexes WHERE expires_at <= ?", w.now.UTC().Format(stamp))
		return err
	})
}
