package store

import (
	"database/sql"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// ResourceMove keeps the writer lock across the registry commit and the file
// rename. Its operations must only be called inside WithResourceMove.
type ResourceMove struct{ db *sql.DB }

// WithResourceMove acquires the writer lock before the caller takes any seat
// lock. Task changes can already take the seat lock during a store write.
func (st *Store) WithResourceMove(move func(ResourceMove) error) error {
	db, err := st.wsDB(true)
	if err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, workstreamsLock), func() error {
		return move(ResourceMove{db: db})
	})
}

// MoveResource commits the new location and returns the previous row so a
// failed rename can restore it. A pending grant drop survives a crash.
func (m ResourceMove) MoveResource(next Resource, dropGrants bool) (Resource, error) {
	var before Resource
	err := workstreamTx(m.db, func(tx *sql.Tx) error {
		var err error
		before, err = scanResource(tx.QueryRow(`SELECT `+resourceColumns+` FROM resources WHERE kind = ? AND id = ?`, next.Kind, next.ID))
		if err != nil {
			return err
		}
		if err := writeResource(tx, next, true, false); err != nil {
			return err
		}
		if dropGrants {
			_, err = tx.Exec(`INSERT OR IGNORE INTO resource_move_cleanup (kind, id) VALUES (?, ?)`, next.Kind, next.ID)
		}
		return err
	})
	return before, err
}

// RestoreResource rolls back a registry commit when the files could not move.
func (m ResourceMove) RestoreResource(before Resource) error {
	return workstreamTx(m.db, func(tx *sql.Tx) error {
		if err := writeResource(tx, before, true, false); err != nil {
			return err
		}
		return clearResourceMove(tx, before.Resource)
	})
}

// PendingGrantDrops finds the interrupted moves that may already have their
// folders in place and still need to finish their grant cleanup.
func (st *Store) PendingGrantDrops(kind string) (map[string]bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id FROM resource_move_cleanup WHERE kind = ?`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// GrantDropPending distinguishes interrupted private moves from grants
// deliberately added to private registrations through the sharing API.
func (m ResourceMove) GrantDropPending(ref acl.Resource) (bool, error) {
	var pending bool
	err := m.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM resource_move_cleanup WHERE kind = ? AND id = ?)`, ref.Kind, ref.ID).Scan(&pending)
	return pending, err
}

// DropGrantsOn completes the cleanup and retires its marker together.
func (m ResourceMove) DropGrantsOn(ref acl.Resource) ([]acl.Grant, error) {
	var dropped []acl.Grant
	err := workstreamTx(m.db, func(tx *sql.Tx) error {
		var err error
		dropped, err = dropGrantsOn(tx, ref.Kind, ref.ID)
		if err != nil {
			return err
		}
		return clearResourceMove(tx, ref)
	})
	return dropped, err
}

func clearResourceMove(tx *sql.Tx, ref acl.Resource) error {
	_, err := tx.Exec(`DELETE FROM resource_move_cleanup WHERE kind = ? AND id = ?`, ref.Kind, ref.ID)
	return err
}
