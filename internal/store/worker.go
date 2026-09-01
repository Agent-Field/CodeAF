package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The worker a node runs on is settled at splice time and journaled there.
// This file holds the two durable records of who took a node's work — the
// journal's own account of a hand-over written by an older build, and the fact
// this build records about every run it dispatches.
//
// Both are events and not UPDATEs, for the reason everything here is: the row
// is a view of the journal. A change that touched the row alone would be undone
// by the next rebuild.
//
// The store never reads the value it stores. It carries a name and no opinion
// about which names exist; whether one reaches a registered worker is a
// question for the registry, one layer up, at dispatch.

// nodeWorkerPayload is the durable record of a worker change: who now runs the
// node, who was running it, and why it moved. NOTHING WRITES ONE. It is here so
// that a graph written by a build that had more than one worker still replays —
// the replayer is [applyNodeWorkerView] below, reached from rebuild.go.
type nodeWorkerPayload struct {
	Subharness string `json:"subharness"`
	Previous   string `json:"previous,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func applyNodeWorkerView(tx *sql.Tx, id, subharness string, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET subharness = ?, updated_seq = ? WHERE id = ?`,
		subharness, seq, id)
}

// nodeRanPayload is the durable record of who actually did the work: the worker
// the dispatch path built, the one it replaced when a second attempt changed
// hands, and why the change happened.
type nodeRanPayload struct {
	Subharness string `json:"subharness"`
	Previous   string `json:"previous,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// RecordNodeRan journals the worker that is running this node, and returns
// whether anything changed.
//
// It is the answer to a question the `subharness` column cannot answer. That
// column records an ASSIGNMENT — what the node was to be run on — and it is
// legitimately empty for the great majority of nodes, because nothing routes a
// node and the generalist is what a node with no assignment gets. This records
// a FACT ABOUT THE RUN: the dispatch path resolved that empty assignment to an
// executor, and the executor has a name. An autopsy that can only read the
// assignment cannot tell an unrouted node from a node nobody ran.
//
// It refuses nothing but a blank. A node may be recorded before it settles and
// once more per hand-over, and a name this build does not have is stored as
// faithfully as one it does — the store carries names and holds no opinion
// about which ones exist.
func (s *Store) RecordNodeRan(id, subharness, reason string) (bool, error) {
	id = strings.TrimSpace(id)
	subharness = strings.TrimSpace(subharness)
	if subharness == "" {
		// A blank is the one thing this column may never hold: it is the
		// absence the whole seam exists to remove, and writing it would put the
		// unreadable value back under a name that promises it is readable.
		return false, fmt.Errorf("record node worker: %w: a node that ran ran on something", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return false, fmt.Errorf("record node worker: %w", err)
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRow(`SELECT ran FROM nodes WHERE id = ? AND folded = 0`, id).
		Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("record node worker: %w: %q", ErrNotFound, id)
		}
		return false, fmt.Errorf("record node worker: %w", err)
	}
	if current == subharness {
		// The same worker rebuilt — a released fold, a repair round on the
		// executor already in hand — is not a hand-over, and a journal that
		// recorded it as one would read as a run that changed workers twice.
		return false, nil
	}
	payload := nodeRanPayload{
		Subharness: subharness, Previous: current, Reason: strings.TrimSpace(reason),
	}
	seq, _, err := appendEvent(tx, id, EventNodeRan, payload)
	if err != nil {
		return false, fmt.Errorf("record node worker: %w", err)
	}
	if err := applyNodeRanView(tx, id, subharness, seq); err != nil {
		return false, fmt.Errorf("record node worker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("record node worker: %w", err)
	}
	return true, nil
}

func applyNodeRanView(tx *sql.Tx, id, subharness string, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET ran = ?, updated_seq = ? WHERE id = ?`,
		subharness, seq, id)
}
