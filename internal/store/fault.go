package store

import (
	"fmt"
	"strings"
)

// A CAUGHT FAULT IS A FACT ABOUT THE RUN, AND UNTIL THIS FILE IT WAS ONLY A FACT
// ABOUT A LOG FILE.
//
// guard.Note writes a recovered panic and its stack to the standard logger and
// returns an error. That is the right thing to do with a stack; it is not enough
// for the panic itself. In the crashed run of 2026-08-28 a leaf faulted, the
// scheduler handed it to a different worker two seconds later, that worker began
// a seven-minute pass over the repository's own tests, and the headless stream
// said `still waiting: 0 tasks pending, 1 running`. Every one of those facts was
// known to some part of the program and none of them reached the person, who
// read the silence as a hang and killed a run that was recovering correctly.
//
// So the fault is journaled where every other fact about a node already lives.
// It has no materialized view — nothing about a node's status changes because a
// fault was recovered; the scheduler decides that — and rebuild's default arm
// deliberately ignores kinds like this one, so the row is durable, replayable
// and inert. What it buys is a reader: the headless progress stream, the record
// a later autopsy reads, and anything else that wants to know that this node's
// work was interrupted by something nobody wrote on purpose.

// EventNodeFaulted is a recovered panic inside one node's work. It is not a node
// failure: a fault is frequently followed by a retry or an escalation that
// succeeds, and a node that recovered is not a node that failed.
const EventNodeFaulted EventKind = "node_faulted"

// nodeFaultPayload is the fault as a reader needs it. Scope is guard's own
// vocabulary for where it happened ("chat/leaf executor"), kept because an
// autopsy wants it; Fault is the one sentence a person is shown.
type nodeFaultPayload struct {
	Scope string `json:"scope,omitempty"`
	Fault string `json:"fault"`
}

// RecordNodeFault appends one recovered fault against a node.
//
// It is lenient about the node in one direction only: an empty id is refused,
// because a fault filed against nothing is a row no reader can find. Everything
// else about it is deliberately cheap — one append, no view, no lock beyond the
// write transaction — because it is called from a deferred recover, and a
// failsafe that can itself be slow or fussy on the unwinding path is a failsafe
// that will one day swallow the very fault it exists to report.
func (s *Store) RecordNodeFault(nodeID, scope, fault string) error {
	nodeID = strings.TrimSpace(nodeID)
	fault = strings.TrimSpace(fault)
	if nodeID == "" {
		return fmt.Errorf("record node fault: %w: empty node id", ErrInvalid)
	}
	if fault == "" {
		return fmt.Errorf("record node fault: %w: a fault with nothing to say", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record node fault: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record node fault: %w", err)
	}
	payload := nodeFaultPayload{
		Scope: bounded(strings.TrimSpace(scope), MaxDigestBytes),
		Fault: bounded(fault, MaxDigestBytes),
	}
	if _, _, err := appendEvent(tx, nodeID, EventNodeFaulted, payload); err != nil {
		return fmt.Errorf("record node fault: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record node fault: %w", err)
	}
	return nil
}
