package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// edgeAddedPayload journals one dependency added after admission — the
// mechanism just-in-time re-planning uses to point existing consumers at
// newly spliced work.
type edgeAddedPayload struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// AddEdge appends one dependency between existing nodes: to now also waits
// for from. It is restricted to pending consumers on purpose — rewiring a
// node that already started would change what its transcript was built from.
// Adding an edge that already exists is a no-op, so callers can wire
// idempotently.
func (s *Store) AddEdge(from, to string, kind EdgeKind) error {
	if !validEdgeKind(kind) {
		return fmt.Errorf("add edge: %w: unknown kind %q", ErrInvalid, kind)
	}
	if from == to {
		return fmt.Errorf("add edge: %w: self-edge %q", ErrInvalid, from)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("add edge: %w", err)
	}
	defer tx.Rollback()

	var status Status
	if err := tx.QueryRow(`SELECT status FROM nodes WHERE id = ?`, from).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("add edge: %w: unknown node %q", ErrInvalid, from)
		}
		return fmt.Errorf("add edge: %w", err)
	}
	if err := tx.QueryRow(`SELECT status FROM nodes WHERE id = ?`, to).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("add edge: %w: unknown node %q", ErrInvalid, to)
		}
		return fmt.Errorf("add edge: %w", err)
	}
	if status != Pending {
		return fmt.Errorf("add edge: %w: %q already %s", ErrInvalid, to, status)
	}
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM edges WHERE from_id = ? AND to_id = ? AND kind = ?`,
		from, to, kind).Scan(&existing); err != nil {
		return fmt.Errorf("add edge: %w", err)
	}
	if existing > 0 {
		return nil
	}

	seq, _, err := appendEvent(tx, to, EventEdgeAdded, edgeAddedPayload{From: from, To: to, Kind: kind})
	if err != nil {
		return fmt.Errorf("add edge: %w", err)
	}
	if err := applyEdgeAddedView(tx, from, to, kind, seq); err != nil {
		return fmt.Errorf("add edge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add edge: %w", err)
	}
	return nil
}

func applyEdgeAddedView(tx *sql.Tx, from, to string, kind EdgeKind, seq int64) error {
	_, err := tx.Exec(`INSERT INTO edges (from_id, to_id, kind, created_seq, created_order) VALUES (?, ?, ?, ?, 0)`,
		from, to, kind, seq)
	return err
}
