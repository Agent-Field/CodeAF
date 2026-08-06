package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// Usage accounting lives in the same journal as the work it measures. One
// event per executed node, materialized into a running total, so any lens can
// answer "what has this graph cost" without replaying history.

// NodeUsage is what one node's execution spent.
type NodeUsage struct {
	NodeID           string  `json:"node_id"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// NodeSurprise is the prediction attached to one leaf when its profile record
// lands. ActualTokens is repeated here so job prediction comparisons exclude
// leaves whose expectation was undefined.
type NodeSurprise struct {
	NodeID         string  `json:"node_id"`
	ActualTokens   int     `json:"actual_tokens"`
	ExpectedTokens int     `json:"expected_tokens"`
	Surprise       float64 `json:"surprise"`
}

// TotalUsage is the graph-wide running total.
type TotalUsage struct {
	Nodes            int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// JobUsage is one top-level job's full subtree size and measured spend.
// NodeCount is graph structure; Runs is the number of recorded executions,
// which may exceed NodeCount when a node is attempted more than once.
type JobUsage struct {
	NodeCount        int
	Runs             int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	SurpriseSamples  int
	SurpriseTokens   int
	ExpectedTokens   int
	Surprise         *float64
}

const usageSchema = `
CREATE TABLE IF NOT EXISTS usage (
    seq               INTEGER PRIMARY KEY REFERENCES events(seq),
    ts                TEXT NOT NULL,
    node_id           TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost              REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS usage_node ON usage (node_id);
`

const surpriseSchema = `
CREATE TABLE IF NOT EXISTS surprises (
    seq             INTEGER PRIMARY KEY REFERENCES events(seq),
    ts              TEXT NOT NULL,
    node_id         TEXT NOT NULL,
    actual_tokens   INTEGER NOT NULL DEFAULT 0,
    expected_tokens INTEGER NOT NULL DEFAULT 0,
    surprise        REAL NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS surprises_node ON surprises (node_id);
`

// RecordUsage appends one node's spend to the journal. Zero-valued usage is
// recorded too: "this ran and cost nothing measurable" is information.
func (s *Store) RecordUsage(usage NodeUsage) error {
	if usage.NodeID == "" {
		return fmt.Errorf("record usage: %w: empty node id", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, usage.NodeID); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	seq, at, err := appendEvent(tx, usage.NodeID, EventUsageRecorded, usage)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	if err := applyUsageView(tx, usage, seq, at); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

// RecordSurprise attaches a defined profile prediction residual to one node.
// Undefined expectations produce no event, preserving absent versus zero.
func (s *Store) RecordSurprise(surprise NodeSurprise) error {
	if surprise.NodeID == "" {
		return fmt.Errorf("record surprise: %w: empty node id", ErrInvalid)
	}
	if surprise.ActualTokens < 0 || surprise.ExpectedTokens < 0 || surprise.Surprise < 0 ||
		math.IsNaN(surprise.Surprise) || math.IsInf(surprise.Surprise, 0) {
		return fmt.Errorf("record surprise: %w: invalid measurement", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, surprise.NodeID); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	seq, at, err := appendEvent(tx, surprise.NodeID, EventSurpriseRecorded, surprise)
	if err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	if err := applySurpriseView(tx, surprise, seq, at); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	return nil
}

// Usage returns the graph-wide total.
func (s *Store) Usage() (TotalUsage, error) {
	var total TotalUsage
	err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cost), 0)
		FROM usage`).Scan(&total.Nodes, &total.PromptTokens, &total.CompletionTokens, &total.Cost)
	if err != nil {
		return TotalUsage{}, fmt.Errorf("total usage: %w", err)
	}
	return total, nil
}

// TopLevelJobUsage joins every node and usage event to its job root. A job
// remains a job root when a territory moves it one level below the spine.
// Folded history therefore keeps its original node count and measured cost.
func (s *Store) TopLevelJobUsage() (map[string]JobUsage, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE job_roots(id) AS (
			SELECT node.id
			FROM nodes AS node
			LEFT JOIN nodes AS parent ON parent.id = node.parent_id
			WHERE node.grp <> ?
			  AND (node.parent_id = ? OR parent.grp = ?)
		), descendants(job_id, node_id) AS (
			SELECT id, id FROM job_roots
			UNION ALL
			SELECT descendants.job_id, child.id
			FROM descendants
			JOIN nodes AS child ON child.parent_id = descendants.node_id
		), node_counts AS (
			SELECT job_id, COUNT(*) AS node_count
			FROM descendants
			GROUP BY job_id
		), spends AS (
			SELECT descendants.job_id, COUNT(usage.seq) AS runs,
			       COALESCE(SUM(usage.prompt_tokens), 0) AS prompt_tokens,
			       COALESCE(SUM(usage.completion_tokens), 0) AS completion_tokens,
			       COALESCE(SUM(usage.cost), 0) AS cost
			FROM descendants
			LEFT JOIN usage ON usage.node_id = descendants.node_id
			GROUP BY descendants.job_id
		), mispredictions AS (
			SELECT descendants.job_id, COUNT(surprises.seq) AS surprise_samples,
			       COALESCE(SUM(surprises.actual_tokens), 0) AS actual_tokens,
			       COALESCE(SUM(surprises.expected_tokens), 0) AS expected_tokens,
			       COALESCE(AVG(surprises.surprise), 0) AS surprise
			FROM descendants
			LEFT JOIN surprises ON surprises.node_id = descendants.node_id
			GROUP BY descendants.job_id
		)
		SELECT node_counts.job_id, node_counts.node_count, spends.runs,
		       spends.prompt_tokens, spends.completion_tokens, spends.cost,
		       mispredictions.surprise_samples, mispredictions.actual_tokens,
		       mispredictions.expected_tokens, mispredictions.surprise
		FROM node_counts
		JOIN spends ON spends.job_id = node_counts.job_id
		JOIN mispredictions ON mispredictions.job_id = node_counts.job_id`, TerritoryGroup, RootID, TerritoryGroup)
	if err != nil {
		return nil, fmt.Errorf("top-level job usage: %w", err)
	}
	defer rows.Close()

	result := make(map[string]JobUsage)
	for rows.Next() {
		var jobID string
		var usage JobUsage
		var usageSurprise float64
		if err := rows.Scan(&jobID, &usage.NodeCount, &usage.Runs, &usage.PromptTokens,
			&usage.CompletionTokens, &usage.Cost, &usage.SurpriseSamples,
			&usage.SurpriseTokens, &usage.ExpectedTokens, &usageSurprise); err != nil {
			return nil, fmt.Errorf("top-level job usage: %w", err)
		}
		if usage.SurpriseSamples > 0 {
			usage.Surprise = &usageSurprise
		}
		result[jobID] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top-level job usage: %w", err)
	}
	return result, nil
}

func applyUsageView(tx *sql.Tx, usage NodeUsage, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO usage (seq, ts, node_id, prompt_tokens, completion_tokens, cost)
		VALUES (?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), usage.NodeID, usage.PromptTokens, usage.CompletionTokens, usage.Cost)
	return err
}

func applySurpriseView(tx *sql.Tx, surprise NodeSurprise, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO surprises (seq, ts, node_id, actual_tokens, expected_tokens, surprise)
		VALUES (?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), surprise.NodeID, surprise.ActualTokens, surprise.ExpectedTokens, surprise.Surprise)
	return err
}
