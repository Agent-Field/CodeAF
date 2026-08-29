package store

import (
	"fmt"
	"strings"
)

// What one ATTEMPT at a leaf did with the room it was given, and what the next
// one was handed.
//
// THE DEFECT THESE ANSWER. On the ink run of 2026-08-29 a single node started
// five times and its store says only that: five claims, five starts, five
// releases, no reason on any of them. Reading it afterwards, four different
// things are indistinguishable — a worker that hung, a worker whose deadline
// legitimately expired, a claim taken back by the reaper, and a leaf that was
// handed its predecessor's work and chose to start over anyway. They cost the
// run nothing, one attempt, one attempt, and the entire ninety-minute wall
// respectively, and every one of them left the same silence.
//
// FAILSAFE.md's rule about absence applies exactly: a fact about the run is
// written down with its reason and its price, because an absence in the record
// is never a diagnosis, it is the diagnoses nobody can now tell apart. So the
// two facts that were missing are journaled here — an attempt that ran out of
// its own room, and a claim that picked up work already recorded — and the
// headless stream reads both (cmd/aforge's narrateOne).
//
// Neither has a materialized view. Nothing about a node's status changes because
// an attempt was exhausted — the scheduler decides that, and it usually decides
// to retry — and rebuild's default arm deliberately ignores kinds like these, so
// the rows are durable, replayable and inert.

const (
	// EventLeafExhausted is one attempt ending because it ran out of the room
	// it was granted, rather than because it finished or failed.
	//
	// IT IS NOT A FAILURE AND IT IS NOT A RESTART. The growth governor already
	// weighs exhaustion when it decides whether more room is worth buying; what
	// it could not do was tell a person, or a later reader, that this is what
	// happened. A leaf that was still working when the clock ran out looks
	// exactly like a leaf that hung, and the two want opposite responses.
	EventLeafExhausted EventKind = "leaf_exhausted"
	// EventLeafResumed is a claim taking over work that is already recorded,
	// with the size of the record it was handed. It is the receipt for the
	// promise in resident.Bank — that a restarted leaf does not start over —
	// and until it existed nothing anywhere said whether the promise was kept.
	EventLeafResumed EventKind = "leaf_resumed"
)

// LeafExhausted is one attempt that ran out of room, as a reader needs it.
type LeafExhausted struct {
	// Attempt is which try this was within the claim, counted from one.
	Attempt int `json:"attempt"`
	// Bound names what ran out in the executor's own vocabulary — the
	// exec.StopReason, so "deadline", "turn-cap", "budget" or "overrun". It is
	// carried verbatim rather than reworded because the words a person reads
	// are composed at the surface and the record keeps the fact.
	Bound string `json:"bound"`
	// Allowed is the room it was given, spelled as the surface granted it
	// ("15m0s", "200 turns"). Empty when the surface did not say.
	Allowed string `json:"allowed,omitempty"`
	// Turns is how many turns it had taken when it stopped.
	Turns int `json:"turns,omitempty"`
	// Reason is the one sentence a person is shown.
	Reason string `json:"reason"`
}

// LeafResumed is what a fresh claim was handed from the record of the attempts
// before it.
type LeafResumed struct {
	// Turns is how many recorded turns the seed carries. Zero never reaches the
	// journal: a claim that resumed from nothing did not resume.
	Turns int `json:"turns"`
	// Files are the paths the earlier attempts were SEEN to change — the
	// workspace's own before-and-after reading, not the worker's claim about it
	// (FAILSAFE.md rule 2). Bounded by the caller.
	Files []string `json:"files,omitempty"`
}

// RecordLeafExhausted appends one attempt's exhaustion against a node.
func (s *Store) RecordLeafExhausted(nodeID string, record LeafExhausted) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record leaf exhaustion: %w: empty node id", ErrInvalid)
	}
	record.Bound = strings.TrimSpace(record.Bound)
	record.Reason = bounded(strings.TrimSpace(record.Reason), MaxDigestBytes)
	if record.Bound == "" || record.Reason == "" {
		return fmt.Errorf("record leaf exhaustion: %w: nothing ran out", ErrInvalid)
	}
	record.Allowed = strings.TrimSpace(record.Allowed)
	return s.appendLeafRun(nodeID, EventLeafExhausted, record, "record leaf exhaustion")
}

// RecordLeafResumed appends what a fresh claim picked up. A resumption of zero
// turns is refused rather than journaled: the whole value of this row is that it
// distinguishes a leaf that carried its predecessor's work from one that did
// not, and a row saying "resumed from nothing" would blur exactly that line.
func (s *Store) RecordLeafResumed(nodeID string, record LeafResumed) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record leaf resumption: %w: empty node id", ErrInvalid)
	}
	if record.Turns <= 0 {
		return fmt.Errorf("record leaf resumption: %w: no recorded turns to resume from", ErrInvalid)
	}
	return s.appendLeafRun(nodeID, EventLeafResumed, record, "record leaf resumption")
}

// appendLeafRun is the one append both share: no view, one write transaction,
// and the node is required so a row is never filed against nothing.
func (s *Store) appendLeafRun(nodeID string, kind EventKind, payload any, what string) error {
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if _, _, err := appendEvent(tx, nodeID, kind, payload); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}
