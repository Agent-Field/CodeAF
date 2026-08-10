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

// UserCancelReason is the reason a person's own cancellation is journaled
// with. It is a named constant because it is read as well as written: the
// upward-rethink seam convenes the plan sentinel for a cancellation the user
// asked for and for no other kind, and a revision's own removals, the practice
// rail's budget stop and a craft run's early landing all cancel pending work
// with reasons of their own.
const UserCancelReason = "cancelled by user"

type nodeCancelledPayload struct {
	Reason string `json:"reason"`
	// Partial is whatever the worker had already written when the user stopped
	// it. Added after the fact and omitted when empty, so every cancellation
	// journaled before it existed replays as the empty partial it was.
	Partial string `json:"partial,omitempty"`
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
	return s.CancelPendingWithPartial(id, reason, "")
}

// CancelPendingWithPartial is the same retirement carrying what the worker had
// already written before it was stopped.
//
// The partial used to be computed and then dropped on the floor: the cancel
// path built it, handed it to the runner as a result summary, and the store
// wrote only the reason — so a cancelled node's whole contribution to whatever
// came next was "(not run): cancelled by user". The restart wires a fresh
// subtree to the dead attempt's digest precisely so the retry can read what its
// predecessor got done, and there was never anything there to read. It goes in
// the summary column because that is where every other settled node's words
// already live, and the digest reads it from there.
func (s *Store) CancelPendingWithPartial(id, reason, partial string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by revision"
	}
	partial = strings.TrimSpace(partial)
	if partial != "" {
		partial = bounded(partial, MaxDigestBytes)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	defer tx.Rollback()
	if err := requirePending(tx, id, "cancel node"); err != nil {
		return err
	}
	seq, at, err := appendEvent(tx, id, EventNodeCancelled,
		nodeCancelledPayload{Reason: reason, Partial: partial})
	if err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	if err := applyNodeCancelledView(tx, id, reason, partial, seq, formatTime(at)); err != nil {
		return fmt.Errorf("cancel node: %w", err)
	}
	if err := recordSelfReceipt(tx, id); err != nil {
		return fmt.Errorf("receipt for cancellation %q: %w", id, err)
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

func applyNodeCancelledView(tx *sql.Tx, id, reason, partial string, seq int64, finishedAt string) error {
	if strings.TrimSpace(partial) == "" {
		_, err := tx.Exec(`UPDATE nodes SET status = ?, error = ?, held = 0, cancel_requested = 0,
			owner = '', updated_seq = ?, finished_at = ? WHERE id = ?`,
			Cancelled, reason, seq, finishedAt, id)
		return err
	}
	_, err := tx.Exec(`UPDATE nodes SET status = ?, error = ?, summary = ?, held = 0, cancel_requested = 0,
		owner = '', updated_seq = ?, finished_at = ? WHERE id = ?`,
		Cancelled, reason, partial, seq, finishedAt, id)
	return err
}

func applyEdgeRemovedView(tx *sql.Tx, from, to string, kind EdgeKind) error {
	_, err := tx.Exec(`DELETE FROM edges WHERE from_id = ? AND to_id = ? AND kind = ?`, from, to, kind)
	return err
}
