package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DeferredOverrun is a repair plan held at the dollar rail. Seq and NodeID
// identify its journal record; the remaining fields are the durable planner
// input needed to resume without rerunning or discarding the landed partial.
type DeferredOverrun struct {
	Seq       int64    `json:"-"`
	NodeID    string   `json:"-"`
	Partial   string   `json:"partial,omitempty"`
	Gap       string   `json:"gap,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
	Prefix    string   `json:"prefix"`
}

type overrunResumed struct {
	DeferredSeq int64 `json:"deferred_seq"`
}

// DeferOverrun journals a repair that must wait for the user's word. Repeated
// checks of the same exhausted node leave the original pending record intact.
func (s *Store) DeferOverrun(deferred DeferredOverrun) error {
	deferred.NodeID = strings.TrimSpace(deferred.NodeID)
	deferred.Prefix = strings.TrimSpace(deferred.Prefix)
	if deferred.NodeID == "" || deferred.Prefix == "" {
		return fmt.Errorf("defer overrun: %w: node id and prefix are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("defer overrun: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, deferred.NodeID); err != nil {
		return fmt.Errorf("defer overrun: %w", err)
	}
	var pending int
	if err := tx.QueryRow(`
		SELECT COUNT(*) FROM events AS deferred
		WHERE deferred.kind = ? AND deferred.node_id = ?
		  AND NOT EXISTS (
			SELECT 1 FROM events AS resumed
			WHERE resumed.kind = ?
			  AND CAST(json_extract(resumed.payload, '$.deferred_seq') AS INTEGER) = deferred.seq
		  )`, EventOverrunDeferred, deferred.NodeID, EventOverrunResumed).Scan(&pending); err != nil {
		return fmt.Errorf("defer overrun: inspect pending: %w", err)
	}
	if pending > 0 {
		return nil
	}
	if _, _, err := appendEvent(tx, deferred.NodeID, EventOverrunDeferred, deferred); err != nil {
		return fmt.Errorf("defer overrun: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("defer overrun: %w", err)
	}
	return nil
}

// OverrunDeferred reports whether a node ever handed its unfinished remainder
// to the durable rail queue. Completion narration uses it to keep the partial
// as internal evidence even after the continuation has resumed.
func (s *Store) OverrunDeferred(nodeID string) (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = ? AND node_id = ?`,
		EventOverrunDeferred, nodeID).Scan(&count); err != nil {
		return false, fmt.Errorf("overrun deferred: %w", err)
	}
	return count > 0, nil
}

// PendingOverruns returns repair plans that have not yet recorded a resumed
// event, oldest first. No process-local queue participates in correctness.
func (s *Store) PendingOverruns(limit int) ([]DeferredOverrun, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT deferred.seq, deferred.node_id, deferred.payload
		FROM events AS deferred
		WHERE deferred.kind = ?
		  AND NOT EXISTS (
			SELECT 1 FROM events AS resumed
			WHERE resumed.kind = ?
			  AND CAST(json_extract(resumed.payload, '$.deferred_seq') AS INTEGER) = deferred.seq
		  )
		ORDER BY deferred.seq
		LIMIT ?`, EventOverrunDeferred, EventOverrunResumed, limit)
	if err != nil {
		return nil, fmt.Errorf("pending overruns: %w", err)
	}
	defer rows.Close()
	var pending []DeferredOverrun
	for rows.Next() {
		var deferred DeferredOverrun
		var payload string
		if err := rows.Scan(&deferred.Seq, &deferred.NodeID, &payload); err != nil {
			return nil, fmt.Errorf("pending overruns: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &deferred); err != nil {
			return nil, fmt.Errorf("pending overrun %d: %w", deferred.Seq, err)
		}
		pending = append(pending, deferred)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending overruns: %w", err)
	}
	return pending, nil
}

// ResolveOverrun journals that one deferred repair is now represented in the
// graph. The event follows the splice, so a crash can never discard the plan.
func (s *Store) ResolveOverrun(deferred DeferredOverrun) error {
	if deferred.Seq <= 0 || strings.TrimSpace(deferred.NodeID) == "" {
		return fmt.Errorf("resolve overrun: %w: deferred sequence and node id are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("resolve overrun: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, deferred.NodeID, EventOverrunResumed, overrunResumed{DeferredSeq: deferred.Seq}); err != nil {
		return fmt.Errorf("resolve overrun: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("resolve overrun: %w", err)
	}
	return nil
}
