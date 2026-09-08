package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	// exec.StopReason, so "deadline", "turn-cap", "budget", "overrun" or
	// "tool-timeouts". It is carried verbatim rather than reworded because the
	// words a person reads are composed at the surface and the record keeps the
	// fact.
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

// LeafExhaustedFor is the newest record of this node running out of room, or
// that there is none.
//
// It exists because the fact has to be READ and not only written. The record was
// journaled and then nothing ever opened it: the judgement that decided whether
// an exhausted leaf had left work behind was shown the brief and the worker's
// last paragraph, and was not told that the worker had been cut off at all. See
// revision.JudgeRemainder.
func (s *Store) LeafExhaustedFor(nodeID string) (LeafExhausted, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, strings.TrimSpace(nodeID), EventLeafExhausted).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return LeafExhausted{}, false, nil
	}
	if err != nil {
		return LeafExhausted{}, false, fmt.Errorf("read leaf exhaustion: %w", err)
	}
	var record LeafExhausted
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return LeafExhausted{}, false, fmt.Errorf("read leaf exhaustion: %w", err)
	}
	return record, true, nil
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

// EventLeafSelfClose is one leaf reading its own finished work, finding a fault
// it caused, and being held back to fix it before it lands.
//
// IT IS NOT A ROUND AND IT IS NOT A RETRY. The finding the leaf's own closing
// photograph raises — a public name it deleted, a name it reads that nothing
// binds, its own checks red, a check it turned red — used to reach nobody until
// the leaf had landed, a gate had weighed it, and the growth governor had bought
// a repair round: a COLD leaf with a fresh brief and none of the context that
// made the mistake. igel s14 bought three of them and every one deleted what the
// last had relied on. This row is the other answer: the leaf that caused it is
// still standing, still holds its own transcript, and is asked once.
//
// Nothing reads it to decide anything. It is a record, so that a run where the
// leaf fixed its own work and a run where nobody looked stop being the same
// silence (FAILSAFE.md clause 4).
const EventLeafSelfClose EventKind = "leaf_self_close"

// LeafSelfClose is one leaf's own closing reading, and what was done about it.
type LeafSelfClose struct {
	// Kinds names the findings the leaf raised against itself, in this
	// program's own vocabulary: "lost public names", "unbound names",
	// "its own checks", "checks it turned red".
	Kinds []string `json:"kinds"`
	// Names is a bounded sample of what the findings are ABOUT. A kind alone
	// sends a reader to run a suite; the name is the whole diagnosis, and it
	// was readable off the tree for nothing.
	Names []string `json:"names,omitempty"`
	// Turns is how many turns the leaf had taken when it read its own work. It
	// is the turns USED and not the turns a close was granted: the room a close
	// may spend is whatever the leaf has left of its own meter, so subtracting
	// this row from the leaf's final turn count is what says how much one cost.
	Turns int `json:"turns,omitempty"`
	// Closed says the leaf was actually held back and asked. False is the floor
	// arm and is journaled just as loudly: the finding stands, the leaf lands
	// with it, and the gate weighs it exactly as it did before — because the
	// leaf had already closed this kind once, or because its meter was spent.
	Closed bool `json:"closed"`
	// Why is the one sentence saying which of those it was, for a person and
	// for an autopsy. Empty on the arm that closed.
	Why string `json:"why,omitempty"`
}

// RecordLeafSelfClose appends one leaf's reading of its own work. A row naming
// no finding is refused: this record exists to say what the leaf found against
// itself, and "it found nothing" is what the surface, unbound and verification
// rows beside it already say.
func (s *Store) RecordLeafSelfClose(nodeID string, record LeafSelfClose) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record leaf self-close: %w: empty node id", ErrInvalid)
	}
	if len(record.Kinds) == 0 {
		return fmt.Errorf("record leaf self-close: %w: no finding to close", ErrInvalid)
	}
	record.Why = bounded(strings.TrimSpace(record.Why), MaxDigestBytes)
	return s.appendLeafRun(nodeID, EventLeafSelfClose, record, "record leaf self-close")
}
