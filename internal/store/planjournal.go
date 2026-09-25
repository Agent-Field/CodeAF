package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A job's plan used to live only in the memory of the process that produced
// it. That was defended as telemetry — lose it and you lose a recalibration
// record — right up until result-driven revision began reading the same map:
// editing a running job's unstarted remainder is work, and a redirect that
// found no plan answered the user with a receipt saying the plan had been read
// and needed no changes.
//
// So the plan is journaled where every other durable fact about a job lives:
// as an event on the job's own root node. The store deliberately learns nothing
// about what a plan is — the bytes arrive already encoded and go back out the
// same way — because the planner is a consumer of the store and inverting that
// would make the graph package a dependency of the journal.
//
// EventPlanGraph is declared here rather than in the block in store.go for the
// same reason the question-practice kinds are declared beside their writer: a
// kind whose payload only one file understands is easier to keep honest next to
// that file.
const EventPlanGraph EventKind = "plan_graph"

// PlanGraph is one job's structure as the planner wrote it, plus the two facts
// execution needs to use it again: which node's landing means the job is over,
// and which model its leaves were sized for.
type PlanGraph struct {
	Root  string          `json:"root"`
	Model string          `json:"model,omitempty"`
	Graph json.RawMessage `json:"graph"`
}

// RecordPlanGraph journals a job's plan against the id namespace its nodes were
// minted under. It is written on every revision as well as at plan time, and
// later events simply win: an append-only journal corrects by appending, and
// the reader below asks only for the newest.
func (s *Store) RecordPlanGraph(prefix string, plan PlanGraph) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return fmt.Errorf("record plan graph: %w: prefix is required", ErrInvalid)
	}
	if len(plan.Graph) == 0 || !json.Valid(plan.Graph) {
		return fmt.Errorf("record plan graph: %w: graph must be valid JSON", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record plan graph: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, prefix, EventPlanGraph, plan); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record plan graph: %w", err)
	}
	return nil
}

// PlanGraphFor returns the newest plan journaled for a namespace. It reads the
// event directly, exactly as DeliveryGateFor does and for the same reason: the
// payload is sparse, looked up by id, and has no query anyone would run across
// it — so a materialized table would be a second copy of the truth with nothing
// to gain by existing.
func (s *Store) PlanGraphFor(prefix string) (PlanGraph, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, prefix, EventPlanGraph).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanGraph{}, false, nil
	}
	if err != nil {
		return PlanGraph{}, false, fmt.Errorf("read plan graph: %w", err)
	}
	var plan PlanGraph
	if err := json.Unmarshal([]byte(payload), &plan); err != nil {
		return PlanGraph{}, false, fmt.Errorf("read plan graph: %w", err)
	}
	return plan, true, nil
}

// EventNodeBrief is one node's rendered brief journaled as a first-class,
// queryable event. A run's sufficiency sentence used to live only as an
// opaque field inside the single plan_graph blob, so a finished leaf's
// criterion was unfalsifiable from the run's own artifacts: the question
// "what was this node told to be done by?" could be answered only by reading
// the whole plan back and finding the node inside it. This event carries the
// rendered instruction, the criterion and the subharness, one row per briefed
// node, written beside the blob rather than in place of it — RecordPlanGraph
// still writes the whole graph, additively.
const EventNodeBrief EventKind = "node_briefed"

// NodeBrief is one node's brief as it was rendered for the agent that runs it.
// Node is the plan node id; the event's node_id column carries the store
// spelling of the same node under its job's prefix (the bare prefix for the
// deliverable sink, "<prefix>-n<id>" otherwise). Criterion is the rendered
// sufficiency sentence (Spec.Done), the one statement a reader holding only
// the result can test.
type NodeBrief struct {
	Node      int    `json:"node"`
	Brief     string `json:"brief"`
	Criterion string `json:"criterion"`
	// Fault names why the model did not write this brief, on the nodes where it
	// did not. The brief beside it is then composed from what the plan already
	// knew about the node rather than absent, so the only way an autopsy can
	// tell a written instruction from a composed one is this field — and
	// without it a plan that quietly briefed itself reads exactly like a plan
	// every call answered. Empty is the ordinary case.
	Fault      string `json:"fault,omitempty"`
	Subharness string `json:"subharness,omitempty"`
	// Skills is the ordered list of skill names attached to this node.
	// Pinned skills (named by the person) come first, followed by retrieval
	// candidates. Order is precedence: earlier-listed skills win conflicts.
	Skills []string `json:"skills,omitempty"`
}

// RecordNodeBrief journals one node's rendered brief against the store id the
// node was minted under. It is best-effort in the same sense RecordPlanGraph
// is: a write that fails costs an audit and never the plan. The blob in
// RecordPlanGraph stays the source of the whole graph; this is the per-node
// index that makes one leaf's criterion answerable without re-reading it.
func (s *Store) RecordNodeBrief(nodeID string, brief NodeBrief) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record node brief: %w: node id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record node brief: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, nodeID, EventNodeBrief, brief); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record node brief: %w", err)
	}
	return nil
}

// BriefFor returns the newest brief journaled for a node. It reads the event
// directly, exactly as PlanGraphFor does and for the same reason: the payload
// is sparse, looked up by id, and has no query anyone would run across it —
// so a materialized table would be a second copy of the truth with nothing to
// gain by existing.
func (s *Store) BriefFor(nodeID string) (NodeBrief, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, nodeID, EventNodeBrief).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return NodeBrief{}, false, nil
	}
	if err != nil {
		return NodeBrief{}, false, fmt.Errorf("read node brief: %w", err)
	}
	var brief NodeBrief
	if err := json.Unmarshal([]byte(payload), &brief); err != nil {
		return NodeBrief{}, false, fmt.Errorf("read node brief: %w", err)
	}
	return brief, true, nil
}
