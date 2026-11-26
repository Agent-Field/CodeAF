package data

import (
	"database/sql"
	"errors"
)

const sourceIndexMigration = `
	CREATE TABLE source_indexes (
		binding TEXT PRIMARY KEY,
		digest TEXT NOT NULL,
		state BLOB NOT NULL
	);
`

// SourceIndex reads adapter-owned state from the person's database. It does
// not project a record, a source credential or an app-visible field.
func (s *Store) SourceIndex(binding string) (state []byte, digest string, err error) {
	err = s.rdb.QueryRow("SELECT state, digest FROM source_indexes WHERE binding = ?", binding).Scan(&state, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return
}

// SaveSourceIndex commits the cursor and the corresponding metadata together.
// An older concurrent scan never overwrites a newer cursor.
func (s *Store) SaveSourceIndex(binding, previous, digest string, state []byte) error {
	if s.draft != "" || binding == "" || digest == "" || len(state) == 0 || len(state) > 1<<20 {
		return errorf(CodeInvalid, "the source index is outside the permitted scope or size")
	}
	return s.write(func(w *writeTx) error {
		_, err := w.tx.Exec(`INSERT INTO source_indexes(binding, digest, state) VALUES (?, ?, ?)
			ON CONFLICT(binding) DO UPDATE SET digest = excluded.digest, state = excluded.state
			WHERE source_indexes.digest = ?`, binding, digest, state, previous)
		return err
	})
}
