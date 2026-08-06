package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	// FactUnsettled is a machine-readable pair of approaches whose evidence
	// does not yet establish one standing rule.
	FactUnsettled FactKind = "unsettled"
)

// UnsettledApproach is one side of a competing pair. Scope describes where
// the approach worked; Evidence names the durable fact sequences behind it.
type UnsettledApproach struct {
	Approach string  `json:"approach"`
	Scope    string  `json:"scope"`
	Evidence []int64 `json:"evidence"`
}

// UnsettledTrial records that one trial ran without resolving the pair. A
// conclusive trial replaces the pair with an ordinary fact instead.
type UnsettledTrial struct {
	NodeID  string `json:"node_id"`
	Outcome string `json:"outcome"`
}

const TrialDidNotSettle = "ran, didn't settle"

// UnsettledFactFlag is the deterministic compiler-context signal. The fact
// number following it becomes Provenance.TrialOf when the compiler acts.
const UnsettledFactFlag = "an unsettled pair applies here: fact #"

// UnsettledPair is the structured payload of an unsettled fact. Approaches
// must contain exactly two entries; Trials is append-only evidence carried
// forward when an experiment cannot choose a winner.
type UnsettledPair struct {
	Approaches []UnsettledApproach `json:"approaches"`
	Trials     []UnsettledTrial    `json:"trials,omitempty"`
}

// Validate rejects prose-only or ambiguous pairs before they enter the
// journal. Evidence sequences are positive and deduplicated per approach.
func (pair UnsettledPair) Validate() error {
	if len(pair.Approaches) != 2 {
		return fmt.Errorf("unsettled pair requires exactly two approaches")
	}
	for index, approach := range pair.Approaches {
		if strings.TrimSpace(approach.Approach) == "" {
			return fmt.Errorf("unsettled approach %d has no name", index+1)
		}
		if strings.TrimSpace(approach.Scope) == "" {
			return fmt.Errorf("unsettled approach %d has no scope", index+1)
		}
		if len(approach.Evidence) == 0 {
			return fmt.Errorf("unsettled approach %d has no evidence", index+1)
		}
		seen := make(map[int64]bool, len(approach.Evidence))
		for _, seq := range approach.Evidence {
			if seq <= 0 || seen[seq] {
				return fmt.Errorf("unsettled approach %d has invalid evidence sequence %d", index+1, seq)
			}
			seen[seq] = true
		}
	}
	if strings.EqualFold(strings.TrimSpace(pair.Approaches[0].Approach), strings.TrimSpace(pair.Approaches[1].Approach)) {
		return fmt.Errorf("unsettled approaches must be distinct")
	}
	for index, trial := range pair.Trials {
		if strings.TrimSpace(trial.NodeID) == "" || trial.Outcome != TrialDidNotSettle {
			return fmt.Errorf("unsettled trial %d is invalid", index+1)
		}
	}
	return nil
}

// WithInconclusiveTrial carries the pair forward with one new piece of
// evidence. The returned value owns its slices and does not mutate pair.
func (pair UnsettledPair) WithInconclusiveTrial(nodeID string) UnsettledPair {
	cloned := UnsettledPair{
		Approaches: make([]UnsettledApproach, len(pair.Approaches)),
		Trials:     append([]UnsettledTrial(nil), pair.Trials...),
	}
	for index, approach := range pair.Approaches {
		cloned.Approaches[index] = approach
		cloned.Approaches[index].Evidence = append([]int64(nil), approach.Evidence...)
	}
	cloned.Trials = append(cloned.Trials, UnsettledTrial{
		NodeID: strings.TrimSpace(nodeID), Outcome: TrialDidNotSettle,
	})
	return cloned
}

// FormatUnsettledPair is the readable projection indexed by FTS and handed
// to models. The Unsettled field remains the authoritative representation.
func FormatUnsettledPair(pair UnsettledPair) string {
	if len(pair.Approaches) != 2 {
		return "unsettled pair"
	}
	formatApproach := func(approach UnsettledApproach) string {
		seqs := make([]string, 0, len(approach.Evidence))
		for _, seq := range approach.Evidence {
			seqs = append(seqs, fmt.Sprintf("#%d", seq))
		}
		return fmt.Sprintf("%s [%s; evidence %s]", strings.TrimSpace(approach.Approach),
			strings.TrimSpace(approach.Scope), strings.Join(seqs, ", "))
	}
	body := formatApproach(pair.Approaches[0]) + " vs " + formatApproach(pair.Approaches[1]) + " — unsettled"
	if count := len(pair.Trials); count > 0 {
		word := "trials"
		if count == 1 {
			word = "trial"
		}
		body += fmt.Sprintf("; %d inconclusive %s (latest: %s)", count, word, pair.Trials[count-1].NodeID)
	}
	return bounded(body, MaxFactBytes)
}

// Fact statuses. Superseded facts stay in the table — the journal never
// forgets — but retrieval returns active facts only.
const (
	FactActive     = "active"
	FactSuperseded = "superseded"
)

// Fact is one materialized notebook entry.
type Fact struct {
	Seq    int64
	Time   time.Time
	NodeID string // the node whose work taught this
	Scope  string // what it is about: user, env, tool:x, repo:/p, file:/p/f, domain:x
	Kind   FactKind
	Body   string
	Status string

	// Unsettled is present only when Kind is FactUnsettled. Body is its
	// readable projection; this payload is what code branches on.
	Unsettled *UnsettledPair

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
    kind      TEXT NOT NULL DEFAULT 'fact' CHECK (kind IN ('preference', 'quirk', 'lesson', 'fact', 'unsettled')),
    body      TEXT NOT NULL,
    unsettled JSON NOT NULL DEFAULT 'null' CHECK (json_valid(unsettled)),
    status    TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
    uses      INTEGER NOT NULL DEFAULT 0,
    last_used TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS facts_scope ON facts (scope, status);
CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(body, scope);
`

type factPayload struct {
	NodeID    string         `json:"node_id"`
	Scope     string         `json:"scope,omitempty"`
	Kind      FactKind       `json:"kind,omitempty"`
	Body      string         `json:"body"`
	Unsettled *UnsettledPair `json:"unsettled,omitempty"`
}

type factSupersededPayload struct {
	FactSeq int64 `json:"fact_seq"`
	BySeq   int64 `json:"by_seq"`
}

// RecordFact appends one learned fact. An identical active fact in the same
// scope is superseded rather than duplicated — write-time hygiene is what
// keeps the notebook worth reading.
func (s *Store) RecordFact(nodeID, scope string, kind FactKind, body string) (Fact, error) {
	if kind == FactUnsettled {
		return Fact{}, fmt.Errorf("record fact: %w: unsettled fact requires a structured pair", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, kind, body, nil, 0)
}

// RecordUnsettledFact appends one structured competing pair. Its Body is
// derived from the pair so the searchable prose cannot disagree with code.
func (s *Store) RecordUnsettledFact(nodeID, scope string, pair UnsettledPair) (Fact, error) {
	if err := pair.Validate(); err != nil {
		return Fact{}, fmt.Errorf("record unsettled fact: %w: %v", ErrInvalid, err)
	}
	return s.recordFact(nodeID, scope, FactUnsettled, FormatUnsettledPair(pair), &pair, 0)
}

// ReplaceFact records a new ordinary fact and supersedes factSeq in the same
// transaction. A failed replacement leaves neither event behind.
func (s *Store) ReplaceFact(factSeq int64, nodeID, scope string, kind FactKind, body string) (Fact, error) {
	if factSeq <= 0 {
		return Fact{}, fmt.Errorf("replace fact: %w: invalid replaced sequence %d", ErrInvalid, factSeq)
	}
	if kind == FactUnsettled {
		return Fact{}, fmt.Errorf("replace fact: %w: unsettled fact requires a structured pair", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, kind, body, nil, factSeq)
}

// ReplaceUnsettledFact carries an unresolved pair forward and consumes its
// prior fact sequence atomically.
func (s *Store) ReplaceUnsettledFact(factSeq int64, nodeID, scope string, pair UnsettledPair) (Fact, error) {
	if factSeq <= 0 {
		return Fact{}, fmt.Errorf("replace unsettled fact: %w: invalid replaced sequence %d", ErrInvalid, factSeq)
	}
	if err := pair.Validate(); err != nil {
		return Fact{}, fmt.Errorf("replace unsettled fact: %w: %v", ErrInvalid, err)
	}
	return s.recordFact(nodeID, scope, FactUnsettled, FormatUnsettledPair(pair), &pair, factSeq)
}

func (s *Store) recordFact(nodeID, scope string, kind FactKind, body string, unsettled *UnsettledPair, replaces int64) (Fact, error) {
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
	if kind == FactUnsettled {
		if unsettled == nil {
			return Fact{}, fmt.Errorf("record fact: %w: unsettled fact requires a structured pair", ErrInvalid)
		}
	} else if unsettled != nil {
		return Fact{}, fmt.Errorf("record fact: %w: only unsettled facts carry a pair", ErrInvalid)
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
	if unsettled != nil {
		if err := requireUnsettledEvidence(tx, *unsettled); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}

	var duplicate int64
	err = tx.QueryRow(`
		SELECT seq FROM facts
		WHERE scope = ? AND status = ? AND lower(body) = lower(?)
		LIMIT 1`, scope, FactActive, body).Scan(&duplicate)
	if err != nil && err != sql.ErrNoRows {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}

	payload := factPayload{NodeID: nodeID, Scope: scope, Kind: kind, Body: body, Unsettled: unsettled}
	seq, at, err := appendEvent(tx, nodeID, EventFactLearned, payload)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if err := applyFactView(tx, payload, seq, at); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	supersededSeqs := make([]int64, 0, 2)
	if duplicate != 0 {
		supersededSeqs = append(supersededSeqs, duplicate)
	}
	if replaces != 0 && replaces != duplicate {
		supersededSeqs = append(supersededSeqs, replaces)
	}
	for _, supersededSeq := range supersededSeqs {
		superseded := factSupersededPayload{FactSeq: supersededSeq, BySeq: seq}
		if _, _, err := appendEvent(tx, "", EventFactSuperseded, superseded); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
		if err := applyFactSupersession(tx, superseded); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	return Fact{Seq: seq, Time: at, NodeID: nodeID, Scope: scope, Kind: kind, Body: body,
		Status: FactActive, Unsettled: unsettled}, nil
}

// SupersedeFact retires one active fact in favour of another, journaled.
// Consolidation uses it to rewrite a scope into fewer, better lines.
func (s *Store) SupersedeFact(factSeq, bySeq int64) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	defer tx.Rollback()

	payload := factSupersededPayload{FactSeq: factSeq, BySeq: bySeq}
	if _, _, err := appendEvent(tx, "", EventFactSuperseded, payload); err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	if err := applyFactSupersession(tx, payload); err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	return tx.Commit()
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
			SELECT f.seq, f.ts, f.node_id, f.scope, f.kind, f.body, f.unsettled, f.status, f.uses, f.last_used
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

// Fact returns one notebook entry by its durable event sequence, including a
// superseded entry needed to explain an already-fired trial.
func (s *Store) Fact(seq int64) (Fact, bool, error) {
	facts, err := s.factsWhere(`seq = ?`, seq)
	if err != nil {
		return Fact{}, false, err
	}
	if len(facts) == 0 {
		return Fact{}, false, nil
	}
	return facts[0], true, nil
}

func (s *Store) factsWhere(where string, args ...any) ([]Fact, error) {
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, scope, kind, body, unsettled, status, uses, last_used
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
		var timestamp, unsettled, lastUsed string
		if err := rows.Scan(&fact.Seq, &timestamp, &fact.NodeID, &fact.Scope, &fact.Kind,
			&fact.Body, &unsettled, &fact.Status, &fact.Uses, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("scan fact time: %w", err)
		}
		fact.Time = at
		if unsettled != "null" {
			if err := json.Unmarshal([]byte(unsettled), &fact.Unsettled); err != nil {
				return nil, fmt.Errorf("decode unsettled fact %d: %w", fact.Seq, err)
			}
		}
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
	if kind == FactUnsettled {
		if payload.Unsettled == nil {
			return fmt.Errorf("unsettled fact %d has no structured pair", seq)
		}
		if err := payload.Unsettled.Validate(); err != nil {
			return fmt.Errorf("unsettled fact %d: %w", seq, err)
		}
		if err := requireUnsettledEvidence(tx, *payload.Unsettled); err != nil {
			return fmt.Errorf("unsettled fact %d: %w", seq, err)
		}
	} else if payload.Unsettled != nil {
		return fmt.Errorf("non-unsettled fact %d carries an unsettled pair", seq)
	}
	encoded, err := json.Marshal(payload.Unsettled)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO facts (seq, ts, node_id, scope, kind, body, unsettled) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.NodeID, scope, kind, payload.Body, string(encoded)); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO facts_fts (rowid, body, scope) VALUES (?, ?, ?)`,
		seq, payload.Body, scope)
	return err
}

func requireUnsettledEvidence(tx *sql.Tx, pair UnsettledPair) error {
	for _, approach := range pair.Approaches {
		for _, evidenceSeq := range approach.Evidence {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM facts WHERE seq = ?`, evidenceSeq).Scan(&exists); err != nil {
				return fmt.Errorf("verify evidence #%d: %w", evidenceSeq, err)
			}
			if exists == 0 {
				return fmt.Errorf("%w: evidence fact #%d does not exist", ErrInvalid, evidenceSeq)
			}
		}
	}
	return nil
}

func applyFactSupersession(tx *sql.Tx, payload factSupersededPayload) error {
	result, err := tx.Exec(`UPDATE facts SET status = ? WHERE seq = ? AND status = ?`,
		FactSuperseded, payload.FactSeq, FactActive)
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
	case FactPreference, FactQuirk, FactLesson, FactPlain, FactUnsettled:
		return true
	}
	return false
}

// migrateFactsSchema upgrades a pre-scope facts table in place: drop the
// materialized view and index, recreate, and replay the journal's fact
// events. The journal is the truth; the view is disposable.
func migrateFactsSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(facts)`)
	if err != nil {
		return err
	}
	hasScope := false
	hasUnsettled := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "scope" {
			hasScope = true
		}
		if name == "unsettled" {
			hasUnsettled = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var tableSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'facts'`).Scan(&tableSQL); err != nil {
		return err
	}
	if hasScope && hasUnsettled && strings.Contains(tableSQL, "'unsettled'") {
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
		if event.Kind != EventFactLearned && event.Kind != EventFactSuperseded {
			continue
		}
		if err := replayEvent(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}
