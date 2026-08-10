package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The worker a node runs on is settled at splice time and journaled there, and
// that is still where nearly every node's answer comes from. This file is the
// one exception the graph allows: a node whose first attempt failed may be
// handed to a different kind of worker for its second, and when that happens the
// change has to survive the process that made it.
//
// It is written as an event and not as an UPDATE for the reason everything here
// is: the row is a view of the journal. A retry that changed the row alone would
// be undone by the next rebuild, and a leaf that was promised a specialist would
// quietly go back to the generalist — which is precisely the silent degradation
// the whole seam exists to make visible.
//
// The store never reads the value it stores. It carries a name and no opinion
// about which names exist; whether one reaches a registered worker is a
// question for the registry, one layer up, at dispatch.

// nodeWorkerPayload is the durable record of a worker change: who now runs the
// node, who was running it, and why it moved.
type nodeWorkerPayload struct {
	Subharness string `json:"subharness"`
	Previous   string `json:"previous,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// SetNodeSubharness records that a node's work has been handed to a different
// worker, and returns whether anything changed.
//
// It refuses a settled node, because a worker change is a claim about work that
// is still to be done. It refuses nothing else: an unregistered name is stored
// as faithfully as a registered one, since the store's job is to remember the
// choice and the registry's is to keep it or degrade it.
func (s *Store) SetNodeSubharness(id, subharness, reason string) (bool, error) {
	id = strings.TrimSpace(id)
	subharness = strings.TrimSpace(subharness)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, fmt.Errorf("set node worker: %w", err)
	}
	defer tx.Rollback()
	var status Status
	var current string
	if err := tx.QueryRow(`SELECT status, subharness FROM nodes WHERE id = ? AND folded = 0`, id).
		Scan(&status, &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("set node worker: %w: %q", ErrNotFound, id)
		}
		return false, fmt.Errorf("set node worker: %w", err)
	}
	if terminal(status) {
		return false, fmt.Errorf("set node worker: %w: %q is %s", ErrInvalid, id, status)
	}
	if current == subharness {
		return false, nil
	}
	payload := nodeWorkerPayload{
		Subharness: subharness, Previous: current, Reason: strings.TrimSpace(reason),
	}
	seq, _, err := appendEvent(tx, id, EventNodeWorkerChanged, payload)
	if err != nil {
		return false, fmt.Errorf("set node worker: %w", err)
	}
	if err := applyNodeWorkerView(tx, id, subharness, seq); err != nil {
		return false, fmt.Errorf("set node worker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("set node worker: %w", err)
	}
	return true, nil
}

func applyNodeWorkerView(tx *sql.Tx, id, subharness string, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET subharness = ?, updated_seq = ? WHERE id = ?`,
		subharness, seq, id)
}
