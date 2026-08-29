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
	// EventLeafStopped is the worker behind a claim reporting that it is gone,
	// naming the token it held.
	//
	// IT IS THE ORDERING PROOF AND THAT IS ITS WHOLE JOB. A claim taken back
	// while its worker is still running does not free the node, it doubles it:
	// two leaves on one node, writing one workspace, each undoing the other's
	// edits. So a reaped claim is released by the worker's OWN landing, after
	// its context has been cancelled and it has actually returned, and this row
	// is journaled immediately before that release. In any store's journal,
	// `leaf_stopped` for a token strictly precedes the `node_released` that
	// frees it — and where it does not, a worker was overtaken.
	EventLeafStopped EventKind = "leaf_stopped"
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
	// Meter, Reached, Allowance and Unit are the bound that actually fired,
	// named, with its own two numbers.
	//
	// Bound above is the executor's StopReason, and three different ceilings
	// used to share one of those: a leaf could be landed by its cost grant, by
	// an undiscounted ceiling three times that grant, or by a cumulative bound
	// on prompt sent, and every one of them journaled "budget". So the record
	// said "it ran out of its tokens" and Allowed printed the grant — which in
	// the ink run of 2026-08-29 was 150,000 against three leaves landed at
	// 240,000 by a different meter, and no reading of the store could tell.
	//
	// Empty on an attempt whose executor does not name its bounds, which reads
	// as "not said" rather than as a bound called "".
	Meter     string `json:"meter,omitempty"`
	Reached   int    `json:"reached,omitempty"`
	Allowance int    `json:"allowance,omitempty"`
	Unit      string `json:"unit,omitempty"`
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

// LeafStopped is one worker reporting that it has stopped, and why it was asked
// to.
type LeafStopped struct {
	// Token is the claim this worker held. It is the identity that matters: a
	// node id alone cannot say WHICH of a node's workers stopped, and telling
	// them apart is the entire reason this row exists.
	Token uint64 `json:"token"`
	// Reason is why it was asked to stop, carried from the sweep that asked.
	Reason string `json:"reason,omitempty"`
}

// RecordLeafStopped appends one worker's report that it is gone.
func (s *Store) RecordLeafStopped(nodeID string, record LeafStopped) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record leaf stop: %w: empty node id", ErrInvalid)
	}
	record.Reason = bounded(strings.TrimSpace(record.Reason), MaxDigestBytes)
	return s.appendLeafRun(nodeID, EventLeafStopped, record, "record leaf stop")
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
