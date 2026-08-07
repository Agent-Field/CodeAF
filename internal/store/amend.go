package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The operations in this file are the store half of live revision: when a
// landed result contradicts the remaining plan, the sentinel edits only work
// that has not started. Every mutation is a journaled event, so a rebuilt
// view carries the same revisions, and every one refuses non-pending targets
// — the past is appended to, never rewritten.

type nodeAmendedPayload struct {
	Brief string `json:"brief,omitempty"`
	Title string `json:"title,omitempty"`
}

type nodeCancelledPayload struct {
	Reason string `json:"reason"`
}

type nodeReparentedPayload struct {
	Parent string `json:"parent"`
}

type edgeRemovedPayload struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// AmendPending rewrites a pending node's brief and/or display title. Empty
// arguments leave that field as it is.
func (s *Store) AmendPending(id, brief, title string) error {
	brief, title = strings.TrimSpace(brief), strings.TrimSpace(title)
	if brief == "" && title == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("amend node: %w", err)
	}
	defer tx.Rollback()
	if err := requirePending(tx, id, "amend node"); err != nil {
		return err
	}
	seq, _, err := appendEvent(tx, id, EventNodeAmended, nodeAmendedPayload{Brief: brief, Title: title})
	if err != nil {
		return fmt.Errorf("amend node: %w", err)
	}
	if err := applyNodeAmendedView(tx, id, brief, title, seq); err != nil {
		return fmt.Errorf("amend node: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("amend node: %w", err)
	}
	return nil
}

// CancelPending retires a pending node whose purpose no longer exists. Unlike
// the claim-based cancel path, it does not require the node to be ready —
// revision most often removes nodes still waiting on their inputs.
func (s *Store) CancelPending(id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by revision"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	defer tx.Rollback()
	if err := requirePending(tx, id, "cancel node"); err != nil {
		return err
	}
	seq, at, err := appendEvent(tx, id, EventNodeCancelled, nodeCancelledPayload{Reason: reason})
	if err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	if err := applyNodeCancelledView(tx, id, reason, seq, formatTime(at)); err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	return nil
}

// RemoveEdge withdraws a dependency a pending consumer no longer needs —
// the other half of rewiring, with AddEdge as the first.
func (s *Store) RemoveEdge(from, to string, kind EdgeKind) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}
	defer tx.Rollback()
	if err := requirePending(tx, to, "remove edge"); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM edges WHERE from_id = ? AND to_id = ? AND kind = ?`,
		from, to, kind).Scan(&existing); err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}
	if existing == 0 {
		return nil
	}
	seq, _, err := appendEvent(tx, to, EventEdgeRemoved, edgeRemovedPayload{From: from, To: to, Kind: kind})
	if err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}
	if err := applyEdgeRemovedView(tx, from, to, kind); err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}
	_ = seq
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}
	return nil
}

func requirePending(tx *sql.Tx, id, operation string) error {
	var status Status
	if err := tx.QueryRow(`SELECT status FROM nodes WHERE id = ? AND folded = 0`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s: %w: unknown node %q", operation, ErrInvalid, id)
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	if status != Pending {
		return fmt.Errorf("%s: %w: %q already %s", operation, ErrInvalid, id, status)
	}
	return nil
}

func applyNodeAmendedView(tx *sql.Tx, id, brief, title string, seq int64) error {
	if brief != "" {
		if _, err := tx.Exec(`UPDATE nodes SET brief = ?, updated_seq = ? WHERE id = ?`, brief, seq, id); err != nil {
			return err
		}
	}
	if title != "" {
		if _, err := tx.Exec(`UPDATE nodes SET title = ?, updated_seq = ? WHERE id = ?`, title, seq, id); err != nil {
			return err
		}
	}
	return nil
}

func applyNodeReparentedView(tx *sql.Tx, id, parent string, seq int64) error {
	result, err := tx.Exec(`UPDATE nodes SET parent_id = ?, updated_seq = ? WHERE id = ?`, parent, seq, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func applyNodeCancelledView(tx *sql.Tx, id, reason string, seq int64, finishedAt string) error {
	_, err := tx.Exec(`UPDATE nodes SET status = ?, error = ?, held = 0, cancel_requested = 0,
		owner = '', updated_seq = ?, finished_at = ? WHERE id = ?`,
		Cancelled, reason, seq, finishedAt, id)
	return err
}

func applyEdgeRemovedView(tx *sql.Tx, from, to string, kind EdgeKind) error {
	_, err := tx.Exec(`DELETE FROM edges WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
	return err
}
