package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A job's shape is decided in one branch, before any node exists, and until now
// that branch left no trace of itself. A run that came out as a single leaf and
// a run that fanned into eleven look identical afterwards except for the nodes
// themselves, so the only way to answer "why did this have no parallelism?" was
// to run it again and watch — which is not an answer, because the reading that
// decided it is a model's and does not repeat.
//
// So the reading is journaled where every other durable fact about a job lives:
// as an event on the id namespace the job's nodes are minted under. It is
// diagnosis, not control — nothing reads it back to decide anything — and it is
// written in the same breath as the decision it explains rather than inferred
// from the shape that resulted, because the shape is the thing being explained.
//
// EventScaleGate is declared here rather than in the block in store.go for the
// same reason EventPlanGraph is declared beside its writer: a kind whose payload
// only one file understands is easier to keep honest next to that file.
const EventScaleGate EventKind = "scale_gate"

// ScaleGate is one job's shape and the reading behind it.
//
// Structure is the compiler's structural reading of the ask — whether it
// enumerates, stratifies, is one judgement, or is a single act — and Scale is
// the size that reading was reconciled to. They are two fields rather than one
// because the reconciliation is exactly where a surprise lives: a goal read as
// "enumerates" that still arrived as a lookup is a different story from one read
// as "single_act", and only the pair tells them apart.
//
// Route names the branch actually taken, and Leaves is how many nodes the splice
// carried. Together they are the answer to the question the audit asked of this
// gate: whether a run had any parallelism at all, and on whose reading.
type ScaleGate struct {
	Goal      string `json:"goal,omitempty"`
	Structure string `json:"structure,omitempty"`
	Scale     string `json:"scale,omitempty"`
	Route     string `json:"route"`
	Leaves    int    `json:"leaves"`
	// Parts is how many separable requests the compiler read in the ask. Zero
	// is the ordinary ask; two or more once meant a second route out of this
	// gate — a flat layout with no planner in it — and now means only that the
	// planner was handed the person's own division as evidence. It is kept
	// because it is the one number that says whether a wide plan followed a
	// wide ask or was found by the planner in a single one.
	Parts int `json:"parts,omitempty"`
}

// The routes a job can leave this gate by. They are constants because a reader
// counting them across a week of runs must not have to guess whether two
// spellings mean one thing.
const (
	// ScaleRouteSingleLeaf is the collapse: anything the compiler did not read
	// as project scale becomes one leaf, with no planner and no fan-out.
	ScaleRouteSingleLeaf = "single_leaf"
	// ScaleRoutePlanned is the full structuring pipeline, and it is now the
	// only road a project-scale job takes. A third value, "bundle", used to
	// name a flat layout the planner never saw; a reader counting routes
	// across a week of runs that spans the change will still find it in the
	// journal, and it means the shape below is not explained by any plan
	// document.
	ScaleRoutePlanned = "planned"
)

// RecordScaleGate journals one job's shape against the id namespace its nodes
// were minted under, which is the same key RecordPlanGraph uses so the two read
// together. It is written before the splice — there is no node to charge yet,
// exactly as with planning spend — and losing it costs diagnosis and nothing
// else, so every caller treats a failure as a note rather than an error.
func (s *Store) RecordScaleGate(prefix string, gate ScaleGate) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return fmt.Errorf("record scale gate: %w: prefix is required", ErrInvalid)
	}
	if strings.TrimSpace(gate.Route) == "" {
		return fmt.Errorf("record scale gate: %w: route is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record scale gate: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, prefix, EventScaleGate, gate); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record scale gate: %w", err)
	}
	return nil
}

// ScaleGateFor returns the newest reading journaled for a namespace. It reads
// the event directly, exactly as PlanGraphFor does and for the same reason: the
// payload is sparse, looked up by id, and has no query anyone would run across
// it.
func (s *Store) ScaleGateFor(prefix string) (ScaleGate, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, prefix, EventScaleGate).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ScaleGate{}, false, nil
	}
	if err != nil {
		return ScaleGate{}, false, fmt.Errorf("read scale gate: %w", err)
	}
	var gate ScaleGate
	if err := json.Unmarshal([]byte(payload), &gate); err != nil {
		return ScaleGate{}, false, fmt.Errorf("read scale gate: %w", err)
	}
	return gate, true, nil
}
