package store

import (
	"context"
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
	tx, err := s.db.BeginTx(context.Background(), nil)
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
