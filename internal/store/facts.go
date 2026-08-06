package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Facts are the notebook: durable things learned while working, as distinct
// from any one job's result. Each fact is indexed by SCOPE — what it is
// about — because a quirk of one file belongs to that file, a tool's
// behaviour belongs to that tool, and a preference belongs to the user.
// Scope match is the primary retrieval mechanism; a BM25 full-text index
// (SQLite FTS5, no extra dependency) catches what scope cues miss. Both are
// local queries: retrieval never waits on a model.

// MaxFactBytes bounds one fact. A fact is one standalone line, not a report.
const MaxFactBytes = 512

// FactKind classifies what a notebook entry teaches.
// AgeLabel renders how old a fact is, for retrieval surfaces: every reader
// of a memory sees when it was written, because a claim's age is part of its
// evidence — "the dev server has been up for days" means something different
// noted yesterday versus noted last quarter.
func AgeLabel(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	age := now.Sub(at)
	switch {
	case age < time.Hour:
		return "just now"
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	case age < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	case age < 60*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(age.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo ago", int(age.Hours()/(24*30)))
	}
}

type FactKind string

const (
	// FactPreference is how the user wants things done.
	FactPreference FactKind = "preference"
	// FactQuirk is a scoped oddity of one file, repo, tool, or product.
	FactQuirk FactKind = "quirk"
	// FactLesson is a transferable mistake-turned-rule.
	FactLesson FactKind = "lesson"
	// FactPlain is an entity and its stable attributes.
	FactPlain FactKind = "fact"
)

// Fact statuses. Settled facts stay in the table — the journal never forgets
// — but retrieval returns active facts only.
const (
	FactActive      = "active"
	FactSuperseded  = "superseded"
	FactQuarantined = "quarantined"
)

// FactChangeOrigin names who changed a fact's retrieval status.
type FactChangeOrigin string

const (
	FactOriginUser         FactChangeOrigin = "user"
	FactOriginCLI          FactChangeOrigin = "cli"
	FactOriginConsolidator FactChangeOrigin = "consolidator"
	FactOriginSupersession FactChangeOrigin = "supersession"
)

// Fact is one materialized notebook entry.
type Fact struct {
	Seq          int64
	Time         time.Time
	NodeID       string // the node whose work taught this
	Scope        string // what it is about: user, env, tool:x, repo:/p, file:/p/f, domain:x
	Kind         FactKind
	Body         string
	Status       string
	StatusSeq    int64
	EvidenceSeq  int64
	StatusOrigin FactChangeOrigin

	// Uses and LastUsed are retrieval telemetry for consolidation, not
	// journaled truth: Rebuild resets them, deliberately.
	Uses     int
	LastUsed time.Time
}

const factsSchema = `
CREATE TABLE IF NOT EXISTS facts (
    seq       INTEGER PRIMARY KEY REFERENCES events(seq),
    ts        TEXT NOT NULL,
    node_id   TEXT NOT NULL,
    scope     TEXT NOT NULL DEFAULT 'user',
    kind      TEXT NOT NULL DEFAULT 'fact' CHECK (kind IN ('preference', 'quirk', 'lesson', 'fact')),
    body      TEXT NOT NULL,
    status    TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'quarantined')),
    status_seq INTEGER NOT NULL DEFAULT 0,
    evidence_seq INTEGER NOT NULL DEFAULT 0,
    status_origin TEXT NOT NULL DEFAULT '',
    uses      INTEGER NOT NULL DEFAULT 0,
    last_used TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS facts_scope ON facts (scope, status);
CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(body, scope);
`

type factPayload struct {
	NodeID string   `json:"node_id"`
	Scope  string   `json:"scope,omitempty"`
	Kind   FactKind `json:"kind,omitempty"`
	Body   string   `json:"body"`
}

type factSupersededPayload struct {
	FactSeq int64 `json:"fact_seq"`
	BySeq   int64 `json:"by_seq"`
}

type factInjectionPayload struct {
	FactSeqs []int64 `json:"fact_seqs"`
}

type factStatusPayload struct {
	FactSeq     int64            `json:"fact_seq"`
	EvidenceSeq int64            `json:"evidence_seq,omitempty"`
	Origin      FactChangeOrigin `json:"origin"`
}

// RecordFact appends one learned fact. An identical active or quarantined fact
// in the same scope is superseded rather than duplicated — write-time hygiene
// is what keeps the notebook worth reading.
func (s *Store) RecordFact(nodeID, scope string, kind FactKind, body string) (Fact, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fact{}, fmt.Errorf("record fact: %w: empty fact", ErrInvalid)
	}
	if len(body) > MaxFactBytes {
		return Fact{}, fmt.Errorf("record fact: %w: fact is %d bytes (limit %d)", ErrInvalid, len(body), MaxFactBytes)
	}
	scope = normalizeScope(scope)
	if !validFactKind(kind) {
		kind = FactPlain
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	defer tx.Rollback()

	if nodeID != "" {
		if err := requireNode(tx, nodeID); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}

	var duplicate int64
	err = tx.QueryRow(`
		SELECT seq FROM facts
		WHERE scope = ? AND status IN (?, ?) AND lower(body) = lower(?)
		ORDER BY status = ? DESC, seq DESC LIMIT 1`,
		scope, FactActive, FactQuarantined, body, FactActive).Scan(&duplicate)
	if err != nil && err != sql.ErrNoRows {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}

	payload := factPayload{NodeID: nodeID, Scope: scope, Kind: kind, Body: body}
	seq, at, err := appendEvent(tx, nodeID, EventFactLearned, payload)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if err := applyFactView(tx, payload, seq, at); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if duplicate != 0 {
		superseded := factSupersededPayload{FactSeq: duplicate, BySeq: seq}
		supersessionSeq, _, err := appendEvent(tx, "", EventFactSuperseded, superseded)
		if err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
		if err := applyFactSupersession(tx, superseded, supersessionSeq); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	return Fact{Seq: seq, Time: at, NodeID: nodeID, Scope: scope, Kind: kind, Body: body,
		Status: FactActive, StatusSeq: seq}, nil
}

// SupersedeFact retires one active or quarantined fact in favour of another,
// journaled.
// Consolidation uses it to rewrite a scope into fewer, better lines.
func (s *Store) SupersedeFact(factSeq, bySeq int64) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	defer tx.Rollback()

	payload := factSupersededPayload{FactSeq: factSeq, BySeq: bySeq}
	seq, _, err := appendEvent(tx, "", EventFactSuperseded, payload)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	if err := applyFactSupersession(tx, payload, seq); err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	return tx.Commit()
}

// RecordFactInjection attributes one bounded notebook batch to the node whose
// context received it. Repeated calls are legal; outcome accounting counts a
// fact's ride on a node once.
func (s *Store) RecordFactInjection(nodeID string, factSeqs []int64) error {
	nodeID = strings.TrimSpace(nodeID)
	factSeqs = normalizedFactSeqs(factSeqs)
	if nodeID == "" {
		return fmt.Errorf("record fact injection: %w: empty node id", ErrInvalid)
	}
	if len(factSeqs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record fact injection: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record fact injection: %w", err)
	}
	for _, factSeq := range factSeqs {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM facts WHERE seq = ?`, factSeq).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("record fact injection: %w: fact %d not found", ErrNotFound, factSeq)
			}
			return fmt.Errorf("record fact injection: %w", err)
		}
	}
	if _, _, err := appendEvent(tx, nodeID, EventFactInjected,
		factInjectionPayload{FactSeqs: factSeqs}); err != nil {
		return fmt.Errorf("record fact injection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record fact injection: %w", err)
	}
	return nil
}

// QuarantineFact removes one active fact from every retrieval path. The event
// itself is the evidence when evidenceSeq is zero, as for a direct CLI veto.
func (s *Store) QuarantineFact(factSeq, evidenceSeq int64, origin FactChangeOrigin) error {
	if factSeq <= 0 || !validFactChangeOrigin(origin) {
		return fmt.Errorf("quarantine fact: %w: invalid fact or origin", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("quarantine fact: %w", err)
	}
	defer tx.Rollback()
	if evidenceSeq > 0 {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM events WHERE seq = ?`, evidenceSeq).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("quarantine fact: %w: evidence event %d not found", ErrNotFound, evidenceSeq)
			}
			return fmt.Errorf("quarantine fact: %w", err)
		}
	}
	payload := factStatusPayload{FactSeq: factSeq, EvidenceSeq: evidenceSeq, Origin: origin}
	seq, _, err := appendEvent(tx, "", EventFactQuarantined, payload)
	if err != nil {
		return fmt.Errorf("quarantine fact: %w", err)
	}
	if err := applyFactQuarantine(tx, payload, seq); err != nil {
		return fmt.Errorf("quarantine fact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("quarantine fact: %w", err)
	}
	return nil
}

// RestoreFact returns one quarantined fact to retrieval. Restoration is a new
// journal event; the quarantine evidence remains intact in the earlier event.
func (s *Store) RestoreFact(factSeq int64, origin FactChangeOrigin) error {
	if factSeq <= 0 || !validFactChangeOrigin(origin) {
		return fmt.Errorf("restore fact: %w: invalid fact or origin", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("restore fact: %w", err)
	}
	defer tx.Rollback()
	payload := factStatusPayload{FactSeq: factSeq, Origin: origin}
	seq, _, err := appendEvent(tx, "", EventFactRestored, payload)
	if err != nil {
		return fmt.Errorf("restore fact: %w", err)
	}
	if err := applyFactRestore(tx, payload, seq); err != nil {
		return fmt.Errorf("restore fact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("restore fact: %w", err)
	}
	return nil
}

// FactBySeq returns one notebook entry regardless of retrieval status.
func (s *Store) FactBySeq(seq int64) (Fact, bool, error) {
	facts, err := s.factsWhere(`seq = ?`, seq)
	if err != nil {
		return Fact{}, false, err
	}
	if len(facts) == 0 {
		return Fact{}, false, nil
	}
	return facts[0], true, nil
}

// Facts lists notebook entries in journal order, including settled and
// quarantined beliefs. A non-positive limit returns the complete notebook.
func (s *Store) Facts(limit int) ([]Fact, error) {
	if limit <= 0 {
		return s.factsWhere(`1 = 1 ORDER BY seq DESC`)
	}
	return s.factsWhere(`1 = 1 ORDER BY seq DESC LIMIT ?`, limit)
}

// FactOutcome is the outcome co-occurrence attached to one notebook fact.
// Bad counts distinct injected nodes that failed, hit a failed delivery gate,
// or grew an overrun continuation. LatestBadSeq is evidence for quarantine.
type FactOutcome struct {
	FactSeq      int64
	Rides        int
	Bad          int
	LatestBadSeq int64
}

// FactOutcomes joins journal-native injections to graph and gate outcomes.
// The query deliberately assigns correlation, not causation; policy about how
// much repeated evidence warrants quarantine belongs to consolidation.
func (s *Store) FactOutcomes() (map[int64]FactOutcome, error) {
	rows, err := s.db.Query(`
		WITH injection_pairs AS (
			SELECT DISTINCT injected.node_id AS node_id,
			       CAST(fact.value AS INTEGER) AS fact_seq
			FROM events AS injected, json_each(injected.payload, '$.fact_seqs') AS fact
			WHERE injected.kind = ?
		), latest_gates AS (
			SELECT gate.node_id, gate.seq,
			       CAST(json_extract(gate.payload, '$.pass') AS INTEGER) AS pass
			FROM events AS gate
			JOIN (
				SELECT node_id, MAX(seq) AS seq
				FROM events WHERE kind = ? GROUP BY node_id
			) AS latest ON latest.seq = gate.seq
		), failures AS (
			SELECT node_id, MAX(seq) AS seq
			FROM events WHERE kind = ? GROUP BY node_id
		), overruns AS (
			SELECT injected.node_id, MAX(continuation.created_seq) AS seq
			FROM (SELECT DISTINCT node_id FROM injection_pairs) AS injected
			JOIN nodes AS continuation
			  ON instr(continuation.id, injected.node_id || '-x1') = 1
			GROUP BY injected.node_id
		), outcomes AS (
			SELECT injected.fact_seq, injected.node_id,
			       max(
				CASE WHEN node.status = ? THEN COALESCE(failures.seq, 0) ELSE 0 END,
				CASE WHEN latest_gates.pass = 0 THEN latest_gates.seq ELSE 0 END,
				COALESCE(overruns.seq, 0)
			       ) AS bad_seq
			FROM injection_pairs AS injected
			JOIN nodes AS node ON node.id = injected.node_id
			LEFT JOIN latest_gates ON latest_gates.node_id = injected.node_id
			LEFT JOIN failures ON failures.node_id = injected.node_id
			LEFT JOIN overruns ON overruns.node_id = injected.node_id
		)
		SELECT fact_seq, COUNT(*) AS rides,
		       SUM(CASE WHEN bad_seq > 0 THEN 1 ELSE 0 END) AS bad,
		       MAX(bad_seq) AS latest_bad_seq
		FROM outcomes GROUP BY fact_seq`,
		EventFactInjected, EventDeliveryGate, EventNodeFailed, Failed)
	if err != nil {
		return nil, fmt.Errorf("fact outcomes: %w", err)
	}
	defer rows.Close()
	outcomes := make(map[int64]FactOutcome)
	for rows.Next() {
		var outcome FactOutcome
		if err := rows.Scan(&outcome.FactSeq, &outcome.Rides, &outcome.Bad,
			&outcome.LatestBadSeq); err != nil {
			return nil, fmt.Errorf("fact outcomes: %w", err)
		}
		outcomes[outcome.FactSeq] = outcome
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fact outcomes: %w", err)
	}
	return outcomes, nil
}

func normalizedFactSeqs(seqs []int64) []int64 {
	seen := make(map[int64]bool, len(seqs))
	normalized := make([]int64, 0, len(seqs))
	for _, seq := range seqs {
		if seq <= 0 || seen[seq] {
			continue
		}
		seen[seq] = true
		normalized = append(normalized, seq)
	}
	return normalized
}

// FactQuery is one retrieval: ordered scope cues (most specific first) plus
// optional free-text terms for the BM25 layer.
type FactQuery struct {
	// Cues are scope keys to match exactly, in priority order — the caller
	// walks hierarchies (file → folder → repo) into this list.
	Cues []string
	// Terms feed FTS5/BM25 and may be empty.
	Terms string
	Limit int
}

// SearchFacts blends the two retrieval layers: scope-cue matches first in
// cue order, then BM25 matches for the terms, deduplicated, capped. Returned
// facts have their use telemetry bumped.
func (s *Store) SearchFacts(query FactQuery) ([]Fact, error) {
	return s.searchFacts(query, true)
}

// SearchFactsUncounted performs the same retrieval without changing use
// telemetry. Notebook maintenance calls use it so the notebook cannot make
// its own lines look useful merely by inspecting them.
func (s *Store) SearchFactsUncounted(query FactQuery) ([]Fact, error) {
	return s.searchFacts(query, false)
}

func (s *Store) searchFacts(query FactQuery, countUses bool) ([]Fact, error) {
	if query.Limit <= 0 {
		query.Limit = 12
	}
	seen := make(map[int64]bool)
	results := make([]Fact, 0, query.Limit)

	for _, cue := range query.Cues {
		if len(results) >= query.Limit {
			break
		}
		cue = normalizeScope(cue)
		facts, err := s.factsWhere(`scope = ? AND status = ? ORDER BY seq DESC LIMIT ?`,
			cue, FactActive, query.Limit)
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			if !seen[fact.Seq] && len(results) < query.Limit {
				seen[fact.Seq] = true
				results = append(results, fact)
			}
		}
	}

	if terms := ftsQueryFrom(query.Terms); terms != "" && len(results) < query.Limit {
		rows, err := s.db.Query(`
			SELECT f.seq, f.ts, f.node_id, f.scope, f.kind, f.body, f.status,
			       f.status_seq, f.evidence_seq, f.status_origin, f.uses, f.last_used
			FROM facts_fts
			JOIN facts AS f ON f.seq = facts_fts.rowid
			WHERE facts_fts MATCH ? AND f.status = ?
			ORDER BY bm25(facts_fts) LIMIT ?`, terms, FactActive, query.Limit)
		if err == nil {
			facts, scanErr := scanFacts(rows)
			if scanErr != nil {
				return nil, scanErr
			}
			for _, fact := range facts {
				if !seen[fact.Seq] && len(results) < query.Limit {
					seen[fact.Seq] = true
					results = append(results, fact)
				}
			}
		}
		// An FTS syntax error from hostile terms is a miss, not a failure.
	}

	if countUses && len(results) > 0 {
		ids := make([]any, 0, len(results)+1)
		placeholders := make([]string, 0, len(results))
		now := formatTime(time.Now())
		ids = append(ids, now)
		for _, fact := range results {
			placeholders = append(placeholders, "?")
			ids = append(ids, fact.Seq)
		}
		_, _ = s.db.Exec(`UPDATE facts SET uses = uses + 1, last_used = ? WHERE seq IN (`+
			strings.Join(placeholders, ",")+`)`, ids...)
	}
	return results, nil
}

// ActiveFacts lists a scope's live entries, newest first — consolidation's
// working set. An empty scope lists every active fact.
func (s *Store) ActiveFacts(scope string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 100
	}
	if scope == "" {
		return s.factsWhere(`status = ? ORDER BY seq DESC LIMIT ?`, FactActive, limit)
	}
	return s.factsWhere(`scope = ? AND status = ? ORDER BY seq DESC LIMIT ?`,
		normalizeScope(scope), FactActive, limit)
}

// RecentFacts returns the newest active facts across all scopes.
func (s *Store) RecentFacts(limit int) ([]Fact, error) {
	return s.ActiveFacts("", limit)
}

func (s *Store) factsWhere(where string, args ...any) ([]Fact, error) {
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, scope, kind, body, status,
		       status_seq, evidence_seq, status_origin, uses, last_used
		FROM facts WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	return scanFacts(rows)
}

func scanFacts(rows *sql.Rows) ([]Fact, error) {
	defer rows.Close()
	facts := make([]Fact, 0)
	for rows.Next() {
		var fact Fact
		var timestamp, lastUsed string
		if err := rows.Scan(&fact.Seq, &timestamp, &fact.NodeID, &fact.Scope, &fact.Kind,
			&fact.Body, &fact.Status, &fact.StatusSeq, &fact.EvidenceSeq, &fact.StatusOrigin, &fact.Uses, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("scan fact time: %w", err)
		}
		fact.Time = at
		if lastUsed != "" {
			if used, err := parseTime(lastUsed); err == nil {
				fact.LastUsed = used
			}
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan facts: %w", err)
	}
	return facts, nil
}

func applyFactView(tx *sql.Tx, payload factPayload, seq int64, at time.Time) error {
	scope := normalizeScope(payload.Scope)
	kind := payload.Kind
	if !validFactKind(kind) {
		kind = FactPlain
	}
	if _, err := tx.Exec(`
		INSERT INTO facts (seq, ts, node_id, scope, kind, body, status_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.NodeID, scope, kind, payload.Body, seq); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO facts_fts (rowid, body, scope) VALUES (?, ?, ?)`,
		seq, payload.Body, scope)
	return err
}

func applyFactSupersession(tx *sql.Tx, payload factSupersededPayload, seq int64) error {
	result, err := tx.Exec(`
		UPDATE facts
		SET status = ?, status_seq = ?, evidence_seq = ?, status_origin = ?
		WHERE seq = ? AND status IN (?, ?)`,
		FactSuperseded, seq, payload.BySeq, FactOriginSupersession,
		payload.FactSeq, FactActive, FactQuarantined)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("supersession targets missing or settled fact %d", payload.FactSeq)
	}
	_, err = tx.Exec(`DELETE FROM facts_fts WHERE rowid = ?`, payload.FactSeq)
	return err
}

func applyFactQuarantine(tx *sql.Tx, payload factStatusPayload, seq int64) error {
	evidenceSeq := payload.EvidenceSeq
	if evidenceSeq == 0 {
		evidenceSeq = seq
	}
	result, err := tx.Exec(`
		UPDATE facts
		SET status = ?, status_seq = ?, evidence_seq = ?, status_origin = ?
		WHERE seq = ? AND status = ?`, FactQuarantined, seq, evidenceSeq,
		payload.Origin, payload.FactSeq, FactActive)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("quarantine targets missing or inactive fact %d", payload.FactSeq)
	}
	_, err = tx.Exec(`DELETE FROM facts_fts WHERE rowid = ?`, payload.FactSeq)
	return err
}

func applyFactRestore(tx *sql.Tx, payload factStatusPayload, seq int64) error {
	var scope, body string
	if err := tx.QueryRow(`SELECT scope, body FROM facts WHERE seq = ? AND status = ?`,
		payload.FactSeq, FactQuarantined).Scan(&scope, &body); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("restore targets missing or unquarantined fact %d", payload.FactSeq)
		}
		return err
	}
	var duplicate int64
	err := tx.QueryRow(`
		SELECT seq FROM facts
		WHERE seq != ? AND scope = ? AND status = ? AND lower(body) = lower(?)
		LIMIT 1`, payload.FactSeq, scope, FactActive, body).Scan(&duplicate)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if duplicate != 0 {
		return fmt.Errorf("restore would duplicate active fact %d", duplicate)
	}
	result, err := tx.Exec(`
		UPDATE facts
		SET status = ?, status_seq = ?, evidence_seq = 0, status_origin = ?
		WHERE seq = ? AND status = ?`, FactActive, seq, payload.Origin,
		payload.FactSeq, FactQuarantined)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("restore targets missing or unquarantined fact %d", payload.FactSeq)
	}
	_, err = tx.Exec(`INSERT INTO facts_fts (rowid, body, scope) VALUES (?, ?, ?)`,
		payload.FactSeq, body, scope)
	return err
}

func replayFactInjection(tx *sql.Tx, nodeID string, payload factInjectionPayload) error {
	if err := requireNode(tx, nodeID); err != nil {
		return err
	}
	seqs := normalizedFactSeqs(payload.FactSeqs)
	if len(seqs) == 0 || len(seqs) != len(payload.FactSeqs) {
		return fmt.Errorf("invalid fact injection payload")
	}
	for _, factSeq := range seqs {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM facts WHERE seq = ?`, factSeq).Scan(&exists); err != nil {
			return err
		}
	}
	return nil
}

// ftsQueryFrom turns free text into a safe FTS5 query: bare terms OR-ed, so
// any hit ranks rather than all terms being required.
func ftsQueryFrom(terms string) string {
	fields := strings.FieldsFunc(strings.ToLower(terms), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	})
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		kept = append(kept, `"`+field+`"`)
		if len(kept) == 12 {
			break
		}
	}
	return strings.Join(kept, " OR ")
}

func normalizeScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		return "user"
	}
	return scope
}

func validFactKind(kind FactKind) bool {
	switch kind {
	case FactPreference, FactQuirk, FactLesson, FactPlain:
		return true
	}
	return false
}

func validFactChangeOrigin(origin FactChangeOrigin) bool {
	switch origin {
	case FactOriginUser, FactOriginCLI, FactOriginConsolidator, FactOriginSupersession:
		return true
	default:
		return false
	}
}

// migrateFactsSchema upgrades older facts tables in place: drop the
// materialized view and index, recreate, and replay the journal's fact
// events. The journal is the truth; the view is disposable.
func migrateFactsSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(facts)`)
	if err != nil {
		return err
	}
	hasScope, hasStatusSeq, hasEvidenceSeq, hasStatusOrigin := false, false, false, false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		switch name {
		case "scope":
			hasScope = true
		case "status_seq":
			hasStatusSeq = true
		case "evidence_seq":
			hasEvidenceSeq = true
		case "status_origin":
			hasStatusOrigin = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'facts'`).Scan(&definition); err != nil {
		return err
	}
	if hasScope && hasStatusSeq && hasEvidenceSeq && hasStatusOrigin &&
		strings.Contains(definition, "'quarantined'") {
		return nil
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DROP TABLE IF EXISTS facts`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS facts_fts`); err != nil {
		return err
	}
	if _, err := tx.Exec(factsSchema); err != nil {
		return err
	}
	events, err := readEvents(tx)
	if err != nil {
		return err
	}
	for _, event := range events {
		switch event.Kind {
		case EventFactLearned, EventFactSuperseded, EventFactInjected,
			EventFactQuarantined, EventFactRestored:
		default:
			continue
		}
		if err := replayEvent(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}
