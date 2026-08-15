package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The shape a leaf was dispatched in, journaled where every other durable fact
// about a node lives.
//
// It is diagnosis, not control — nothing reads it back to decide anything — and
// it exists for the same reason the scale gate's reading does: a decision taken
// at claim time against numbers that no longer exist afterwards is a decision
// nobody can check. A node that ran as a fold and a node that happened to
// finish in two turns look identical in the record, and only one of them is
// evidence that the predicate fired. The counts ride with it so the predicate
// can be argued with rather than merely observed: how many results fed it, how
// many bytes of them were pushed whole, how many had to be left on a handle.
const EventLeafMode EventKind = "leaf_mode"

// The modes a leaf can be dispatched in. Constants because a reader counting
// them across a week of runs must not have to guess whether two spellings mean
// one thing.
const (
	// LeafModeOpen is the ordinary loop: a leaf that has things to go and find,
	// bounded by its turn and token grant and by nothing else.
	LeafModeOpen = "open"
	// LeafModeFold is the assembly: every result that feeds this node was
	// pushed into its prompt whole, and the plan gives it nothing to go and
	// touch, so it runs as one pass.
	LeafModeFold = "fold"
)

// LeafMode is one dispatch decision and the measurements behind it.
type LeafMode struct {
	Mode string `json:"mode"`
	// Deps is how many settled results fed this node.
	Deps int `json:"deps,omitempty"`
	// Pushed is how many bytes of them went into the prompt, and Handles how
	// many of them were too large for it and travelled as a path instead. A
	// single handle is enough to disqualify a fold: a node told to assemble
	// material it holds only a pointer to has to go and get it.
	Pushed  int `json:"pushed,omitempty"`
	Handles int `json:"handles,omitempty"`
	// Turns and Tokens are the grant this decision bought.
	Turns  int `json:"turns,omitempty"`
	Tokens int `json:"tokens,omitempty"`
}

// RecordLeafMode journals one dispatch. Losing it costs diagnosis and nothing
// else, so every caller treats a failure as a note rather than an error.
func (s *Store) RecordLeafMode(nodeID string, mode LeafMode) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record leaf mode: %w: node id is required", ErrInvalid)
	}
	if strings.TrimSpace(mode.Mode) == "" {
		return fmt.Errorf("record leaf mode: %w: mode is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record leaf mode: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record leaf mode: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventLeafMode, mode); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record leaf mode: %w", err)
	}
	return nil
}

// LeafModeFor returns the newest dispatch journaled against one node. A node
// dispatched more than once — a retry, an escalation — reports its latest.
func (s *Store) LeafModeFor(nodeID string) (LeafMode, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, strings.TrimSpace(nodeID), EventLeafMode).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return LeafMode{}, false, nil
	}
	if err != nil {
		return LeafMode{}, false, fmt.Errorf("read leaf mode: %w", err)
	}
	var mode LeafMode
	if err := json.Unmarshal([]byte(payload), &mode); err != nil {
		return LeafMode{}, false, fmt.Errorf("read leaf mode: %w", err)
	}
	return mode, true, nil
}
