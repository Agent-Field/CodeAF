package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A run that repaired its own model calls twice used to look exactly like a run
// that never had to.
//
// FAILSAFE.md clause 4: a fail-safe that fires without leaving a record is a
// fail-safe nobody can autopsy, and clause 3: a fact that changes what somebody
// watching should expect is a line in the stream. Both are about the same events
// — a structured answer cut off at the output ceiling and continued, one asked
// again because it came back as prose, one given up on. Every one of them costs
// money, changes how long the run takes, and is the first thing anyone reading a
// $0.50 run's autopsy wants to know about.
//
// EventStructuredRepair is declared here rather than in store.go's block for the
// reason EventJobGrowth is: a kind whose payload one file understands is easier
// to keep honest next to that file.
const EventStructuredRepair EventKind = "structured_repair"

// StructuredRepair is one repair internal/shaped performed to get an answer.
//
// Lane is the pass that asked, in a person's words — "plan", "gate", "compile".
// Kind is what was done about it. Line is the whole sentence as a person reads
// it, written by the seam and kept verbatim, so a reader of the journal and a
// person watching the stream are looking at the same words rather than at two
// renderings of one fact that will eventually disagree.
type StructuredRepair struct {
	Lane    string `json:"lane"`
	Model   string `json:"model,omitempty"`
	Kind    string `json:"kind"`
	Round   int    `json:"round,omitempty"`
	Spent   int    `json:"spent,omitempty"`
	Ceiling int    `json:"ceiling,omitempty"`
	Line    string `json:"line"`
}

// RecordStructuredRepair journals one repair against the node it was made for —
// or against the job root, when it happened before any node existed, which is
// where the planner's repairs happen and where the s4 sweep lost a whole run.
//
// Losing the row costs the autopsy its evidence and nothing else, so callers
// treat a failure as a note: a repair that could not be written down is not a
// reason to stop a run that is otherwise recovering.
func (s *Store) RecordStructuredRepair(nodeID string, repair StructuredRepair) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record structured repair: %w: node is required", ErrInvalid)
	}
	if strings.TrimSpace(repair.Kind) == "" {
		return fmt.Errorf("record structured repair: %w: kind is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record structured repair: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, nodeID, EventStructuredRepair, repair); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record structured repair: %w", err)
	}
	return nil
}

// StructuredRepairs returns every repair journaled against one node, oldest
// first. It reads the events directly, as JobGrowths does: the payload is
// sparse, looked up by one id, and has no query anyone would run across it.
func (s *Store) StructuredRepairs(nodeID string) ([]StructuredRepair, error) {
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq`, strings.TrimSpace(nodeID), EventStructuredRepair)
	if err != nil {
		return nil, fmt.Errorf("read structured repairs: %w", err)
	}
	defer rows.Close()
	repairs := make([]StructuredRepair, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("read structured repairs: %w", err)
		}
		var repair StructuredRepair
		if err := json.Unmarshal([]byte(payload), &repair); err != nil {
			return nil, fmt.Errorf("read structured repairs: %w", err)
		}
		repairs = append(repairs, repair)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read structured repairs: %w", err)
	}
	return repairs, nil
}
