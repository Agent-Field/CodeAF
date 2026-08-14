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
	// Subharness is the worker the remainder was judged to belong to, held
	// across the wait for the same reason everything else here is: the judgement
	// was made and paid for before the rail stopped the splice, and a repair
	// that resumed on a different worker than the one it was planned for would
	// be a decision quietly unmade by a budget question.
	Subharness string `json:"subharness,omitempty"`
	// State is the dead leaf's structured findings, carried across the rail
	// for the same reason everything else here is: the continuation needs it
	// to resume rather than restart, and a repair that resumed without it
	// would re-read everything the dead leaf already diagnosed.
	State string `json:"state,omitempty"`
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

// OverrunEvidence is one straggler, recorded as the comparison that found it
// rather than as a conclusion drawn from it.
//
// Both sides of the comparison are here on purpose. "This leaf spent 150,000
// tokens" is not evidence of anything — it is evidence once it sits beside the
// 4,000 its siblings spent and the number of runs that median came from, and a
// record that kept only the first would be asking every later reader to take the
// judgement on trust. Verdict is what the judge said, in the judge's own words,
// including when the judge said to carry on: a threshold that fires and is
// overruled is exactly as interesting as one that fires and is upheld, and a
// journal that only kept the upholdings would make the threshold look perfect.
type OverrunEvidence struct {
	Seq int64 `json:"-"`
	// NodeID is the leaf this is about.
	NodeID string `json:"-"`
	// Spent is what the leaf had cost when the comparison was made, and Turns is
	// how many rounds it took to spend it.
	Spent int `json:"spent"`
	Turns int `json:"turns,omitempty"`
	// Anchor is the measured median for this worker, Threshold the point that
	// was crossed, Multiple how many anchors that is, and Samples how many
	// measured leaves the pair was derived from.
	Anchor    int     `json:"anchor"`
	Threshold int     `json:"threshold"`
	Multiple  float64 `json:"multiple,omitempty"`
	Samples   int     `json:"samples,omitempty"`
	// Worker is who was running it when the threshold was crossed.
	Worker string `json:"worker,omitempty"`
	// Verdict is what was decided, in whatever words the deciding party uses.
	Verdict string `json:"verdict,omitempty"`
	// Chosen names the worker the work was handed to, when it was handed to one.
	Chosen string `json:"chosen,omitempty"`
}

// RecordOverrunEvidence journals one straggler comparison.
//
// It is deliberately append-only and unguarded by any de-duplication: the loop
// asks the question once per leaf, so a second record against the same node
// means a second attempt crossed the line too, and collapsing the two would hide
// the fact that escalating did not help.
func (s *Store) RecordOverrunEvidence(evidence OverrunEvidence) error {
	evidence.NodeID = strings.TrimSpace(evidence.NodeID)
	if evidence.NodeID == "" {
		return fmt.Errorf("record overrun evidence: %w: node id is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record overrun evidence: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, evidence.NodeID); err != nil {
		return fmt.Errorf("record overrun evidence: %w", err)
	}
	if _, _, err := appendEvent(tx, evidence.NodeID, EventOverrunEvidence, evidence); err != nil {
		return fmt.Errorf("record overrun evidence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record overrun evidence: %w", err)
	}
	return nil
}

// OverrunEvidenceFor reads back what was recorded against one node, oldest
// first. Its only readers are the audit that asks how often the threshold fires
// and the test that proves it does.
func (s *Store) OverrunEvidenceFor(nodeID string) ([]OverrunEvidence, error) {
	rows, err := s.db.Query(`
		SELECT seq, node_id, payload FROM events
		WHERE kind = ? AND node_id = ?
		ORDER BY seq`, EventOverrunEvidence, strings.TrimSpace(nodeID))
	if err != nil {
		return nil, fmt.Errorf("overrun evidence: %w", err)
	}
	defer rows.Close()
	var recorded []OverrunEvidence
	for rows.Next() {
		var evidence OverrunEvidence
		var payload string
		if err := rows.Scan(&evidence.Seq, &evidence.NodeID, &payload); err != nil {
			return nil, fmt.Errorf("overrun evidence: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &evidence); err != nil {
			return nil, fmt.Errorf("overrun evidence %d: %w", evidence.Seq, err)
		}
		recorded = append(recorded, evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("overrun evidence: %w", err)
	}
	return recorded, nil
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
