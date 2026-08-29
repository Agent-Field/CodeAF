package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A job that grows while it runs used to leave no single trace of having grown.
// An overrun replan spliced a subtree, a revision sentinel spliced a node, and
// afterwards the two were indistinguishable without reading intents node by
// node — so "why did this job end up with 41 nodes, and who asked for the last
// eleven?" had no answer, and neither did "what did a governor refuse".
//
// So every execution-time growth decision — admitted or refused — is journaled
// against the job root it grew, the same key the job's own nodes are minted
// under. It is diagnosis for the refusals and accounting for the admissions:
// the round counter that bounds growth is derived from these events, so the
// journal is the counter rather than a description of one.
//
// EventJobGrowth is declared here rather than in store.go's block for the same
// reason EventScaleGate is declared beside its writer: a kind whose payload one
// file understands is easier to keep honest next to that file.
const EventJobGrowth EventKind = "job_growth"

// JobGrowth is one decision by the growth governor.
//
// Reason names the path that asked — an overrun replan, a delivery gap, a
// revision sentinel — because the whole point of the journal is that those were
// indistinguishable afterwards. Lineage is the namespace the rounds are counted
// under: a leaf's own split lineage for a replan, the job root for growth that
// belongs to the job as a whole. Two siblings that each split once are two
// lineages with one round each, not one lineage with two.
//
// Refused carries the governor's own words when it said no, so a reader of the
// journal sees the same sentence the work's record got.
type JobGrowth struct {
	Reason  string `json:"reason"`
	Lineage string `json:"lineage,omitempty"`
	Adding  int    `json:"adding,omitempty"`
	Round   int    `json:"round"`
	Allowed bool   `json:"allowed"`
	Refused string `json:"refused,omitempty"`
	// Cause is the machine-readable half of Refused: which governor spoke.
	Cause string `json:"cause,omitempty"`

	// Produced is how many files the work this round grows FROM left behind in
	// the world — the workspace's own before-and-after reading, never the
	// worker's account of itself. It is journaled because it is the only
	// evidence the next round can be weighed against: a lineage whose last two
	// bodies of work both changed nothing on disk is not making slow progress,
	// it is at a standstill, and no round after that buys anything.
	Produced int `json:"produced,omitempty"`

	// Measured says somebody actually looked, so a Produced of zero reads as
	// "nothing was written" rather than as "nobody counted". A row written
	// before this field existed decodes false, which is the honest reading of
	// it: nothing counted anything.
	Measured bool `json:"measured,omitempty"`

	// Remainder is a digest of the work this round was planned to finish. Two
	// consecutive rounds handed the same remainder are a fixed point: the round
	// that just ran was aimed at exactly this and did not move it. The digest
	// rather than the text because the text is unbounded and this is only ever
	// compared for equality.
	Remainder string `json:"remainder,omitempty"`
}

// RecordJobGrowth journals one growth decision against a job root. Losing it
// costs the round counter its memory and diagnosis its record, so callers treat
// a failure as a note — but they do treat it: an unrecorded admission is a
// round nobody spent.
func (s *Store) RecordJobGrowth(jobRoot string, growth JobGrowth) error {
	jobRoot = strings.TrimSpace(jobRoot)
	if jobRoot == "" {
		return fmt.Errorf("record job growth: %w: job root is required", ErrInvalid)
	}
	if strings.TrimSpace(growth.Reason) == "" {
		return fmt.Errorf("record job growth: %w: reason is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record job growth: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, jobRoot, EventJobGrowth, growth); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record job growth: %w", err)
	}
	return nil
}

// JobGrowths returns every growth decision journaled for a job root, oldest
// first. It reads the events directly, as ScaleGateFor does: the payload is
// sparse, looked up by one id, and has no query anyone would run across it.
func (s *Store) JobGrowths(jobRoot string) ([]JobGrowth, error) {
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq`, strings.TrimSpace(jobRoot), EventJobGrowth)
	if err != nil {
		return nil, fmt.Errorf("read job growth: %w", err)
	}
	defer rows.Close()
	growths := make([]JobGrowth, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("read job growth: %w", err)
		}
		var growth JobGrowth
		if err := json.Unmarshal([]byte(payload), &growth); err != nil {
			return nil, fmt.Errorf("read job growth: %w", err)
		}
		growths = append(growths, growth)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read job growth: %w", err)
	}
	return growths, nil
}
