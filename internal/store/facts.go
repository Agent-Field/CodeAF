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
	// FactSkill is an execution-verified procedure offered to future work.
	FactSkill FactKind = "skill"
)

// Fact statuses. Candidates and superseded facts stay in the table — the
// journal never forgets — but retrieval returns active facts only.
const (
	FactCandidate  = "candidate"
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

	// Artifact points at a skill candidate's teaching directory, then at its
	// installed directory after execution promotion. StatusNote records why a
	// candidate or active skill was retired.
	Artifact   string
	StatusNote string

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
    kind      TEXT NOT NULL DEFAULT 'fact' CHECK (kind IN ('preference', 'quirk', 'lesson', 'fact', 'skill')),
    body      TEXT NOT NULL,
    status    TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('candidate', 'active', 'superseded')),
    artifact  TEXT NOT NULL DEFAULT '',
    status_note TEXT NOT NULL DEFAULT '',
    uses      INTEGER NOT NULL DEFAULT 0,
    last_used TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS facts_scope ON facts (scope, status);
CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(body, scope);
`

type factPayload struct {
	NodeID   string   `json:"node_id"`
	Scope    string   `json:"scope,omitempty"`
	Kind     FactKind `json:"kind,omitempty"`
	Body     string   `json:"body"`
	Status   string   `json:"status,omitempty"`
	Artifact string   `json:"artifact,omitempty"`
}

type factActivatedPayload struct {
	FactSeq  int64  `json:"fact_seq"`
	Artifact string `json:"artifact"`
}

type factSupersededPayload struct {
	FactSeq int64  `json:"fact_seq"`
	BySeq   int64  `json:"by_seq"`
	Reason  string `json:"reason,omitempty"`
}

// RecordFact appends one learned fact. An identical active fact in the same
// scope is superseded rather than duplicated — write-time hygiene is what
// keeps the notebook worth reading.
func (s *Store) RecordFact(nodeID, scope string, kind FactKind, body string) (Fact, error) {
	if kind == FactSkill {
		return Fact{}, fmt.Errorf("record fact: %w: skills begin as candidates", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, kind, body, FactActive, "", true)
}

// RecordSkillCandidate journals a procedure the distiller found in one job.
// It is intentionally absent from retrieval until a later execution event
// activates it.
func (s *Store) RecordSkillCandidate(nodeID, scope, body, artifact string) (Fact, error) {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return Fact{}, fmt.Errorf("record skill candidate: %w: empty artifact", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, FactSkill, body, FactCandidate, artifact, false)
}

// RewriteActiveSkill preserves an execution-verified artifact while notebook
// consolidation rewrites its doc line. Naming the active source rather than an
// arbitrary path keeps this maintenance API from manufacturing a capability.
func (s *Store) RewriteActiveSkill(nodeID, scope, body string, sourceSeq int64) (Fact, error) {
	sources, err := s.factsWhere(`seq = ? AND kind = ? AND status = ?`,
		sourceSeq, FactSkill, FactActive)
	if err != nil {
		return Fact{}, fmt.Errorf("rewrite active skill: %w", err)
	}
	if len(sources) != 1 || strings.TrimSpace(sources[0].Artifact) == "" {
		return Fact{}, fmt.Errorf("rewrite active skill: %w: source %d is not active", ErrInvalid, sourceSeq)
	}
	return s.recordFact(nodeID, scope, FactSkill, body, FactActive, sources[0].Artifact, true)
}

func (s *Store) recordFact(nodeID, scope string, kind FactKind, body, status, artifact string, deduplicate bool) (Fact, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fact{}, fmt.Errorf("record fact: %w: empty fact", ErrInvalid)
	}
	if len(body) > MaxFactBytes {
		return Fact{}, fmt.Errorf("record fact: %w: fact is %d bytes (limit %d)", ErrInvalid, len(body), MaxFactBytes)
	}
	if kind == FactSkill && strings.ContainsAny(body, "\r\n") {
		return Fact{}, fmt.Errorf("record skill: %w: doc must be one line", ErrInvalid)
	}
	scope = normalizeScope(scope)
	if !validFactKind(kind) {
		kind = FactPlain
	}
	if !validFactStatus(status) {
		status = FactActive
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
	if deduplicate {
		err = tx.QueryRow(`
			SELECT seq FROM facts
			WHERE scope = ? AND status = ? AND lower(body) = lower(?)
			LIMIT 1`, scope, FactActive, body).Scan(&duplicate)
		if err != nil && err != sql.ErrNoRows {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}

	payload := factPayload{NodeID: nodeID, Scope: scope, Kind: kind, Body: body, Status: status, Artifact: artifact}
	seq, at, err := appendEvent(tx, nodeID, EventFactLearned, payload)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if err := applyFactView(tx, payload, seq, at); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if duplicate != 0 {
		superseded := factSupersededPayload{FactSeq: duplicate, BySeq: seq}
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
	return Fact{Seq: seq, Time: at, NodeID: nodeID, Scope: scope, Kind: kind, Body: body, Status: status, Artifact: artifact}, nil
}

// ActivateSkill journals the only transition that makes a candidate
// retrievable. The caller has already copied and executed the artifact check.
func (s *Store) ActivateSkill(factSeq int64, artifact string) error {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return fmt.Errorf("activate skill: %w: empty artifact", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}
	defer tx.Rollback()

	payload := factActivatedPayload{FactSeq: factSeq, Artifact: artifact}
	if _, _, err := appendEvent(tx, "", EventFactActivated, payload); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}
	if err := applyFactActivation(tx, payload); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}
	return tx.Commit()
}

// SupersedeFact retires one active fact in favour of another, journaled.
// Consolidation uses it to rewrite a scope into fewer, better lines.
func (s *Store) SupersedeFact(factSeq, bySeq int64) error {
	return s.SupersedeFactWithReason(factSeq, bySeq, "")
}

// SupersedeFactWithReason records why a belief retired. Skill trial failures
// use the reason as durable execution evidence even when there is no replacing
// fact and bySeq is zero.
func (s *Store) SupersedeFactWithReason(factSeq, bySeq int64, reason string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	defer tx.Rollback()

	payload := factSupersededPayload{FactSeq: factSeq, BySeq: bySeq, Reason: strings.TrimSpace(reason)}
	if _, _, err := appendEvent(tx, "", EventFactSuperseded, payload); err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	if err := applyFactSupersession(tx, payload); err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	return tx.Commit()
}

// SkillFacts lists skills in one status, newest first. Empty status includes
// candidates, active skills, and retired entries for reconciliation.
func (s *Store) SkillFacts(status string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 100
	}
	if status == "" {
		return s.factsWhere(`kind = ? ORDER BY seq DESC LIMIT ?`, FactSkill, limit)
	}
	if !validFactStatus(status) {
		return nil, fmt.Errorf("query skills: %w: invalid status %q", ErrInvalid, status)
	}
	return s.factsWhere(`kind = ? AND status = ? ORDER BY seq DESC LIMIT ?`, FactSkill, status, limit)
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
			SELECT f.seq, f.ts, f.node_id, f.scope, f.kind, f.body, f.status, f.artifact, f.status_note, f.uses, f.last_used
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
		SELECT seq, ts, node_id, scope, kind, body, status, artifact, status_note, uses, last_used
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
			&fact.Body, &fact.Status, &fact.Artifact, &fact.StatusNote, &fact.Uses, &lastUsed); err != nil {
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
	status := payload.Status
	if !validFactStatus(status) {
		// fact_learned events predating skill candidacy have no status field.
		status = FactActive
	}

	if _, err := tx.Exec(`
		INSERT INTO facts (seq, ts, node_id, scope, kind, body, status, artifact)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.NodeID, scope, kind, payload.Body, status, payload.Artifact); err != nil {
		return err
	}
	if status != FactActive {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO facts_fts (rowid, body, scope) VALUES (?, ?, ?)`,
		seq, payload.Body, scope)
	return err
}

func applyFactActivation(tx *sql.Tx, payload factActivatedPayload) error {
	result, err := tx.Exec(`
		UPDATE facts SET status = ?, artifact = ?, status_note = ''
		WHERE seq = ? AND kind = ? AND status = ?`,
		FactActive, payload.Artifact, payload.FactSeq, FactSkill, FactCandidate)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("activation targets missing or settled skill %d", payload.FactSeq)
	}
	var body, scope string
	if err := tx.QueryRow(`SELECT body, scope FROM facts WHERE seq = ?`, payload.FactSeq).Scan(&body, &scope); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO facts_fts (rowid, body, scope) VALUES (?, ?, ?)`,
		payload.FactSeq, body, scope)
	return err
}
func applyFactSupersession(tx *sql.Tx, payload factSupersededPayload) error {
	result, err := tx.Exec(`
		UPDATE facts SET status = ?, status_note = ?
		WHERE seq = ? AND status <> ?`,
		FactSuperseded, payload.Reason, payload.FactSeq, FactSuperseded)
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
	case FactPreference, FactQuirk, FactLesson, FactPlain, FactSkill:
		return true
	}
	return false
}

func validFactStatus(status string) bool {
	switch status {
	case FactCandidate, FactActive, FactSuperseded:
		return true
	}
	return false
}

// migrateFactsSchema upgrades an older facts table in place: drop the
// materialized view and index, recreate, and replay the journal's fact
// events. The journal is the truth; the view is disposable.
func migrateFactsSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(facts)`)
	if err != nil {
		return err
	}
	hasScope := false
	hasArtifact := false
	hasStatusNote := false
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
		if name == "artifact" {
			hasArtifact = true
		}
		if name == "status_note" {
			hasStatusNote = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var createSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'facts'`).Scan(&createSQL); err != nil {
		return err
	}
	if hasScope && hasArtifact && hasStatusNote && strings.Contains(createSQL, "'skill'") && strings.Contains(createSQL, "'candidate'") {
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
		if event.Kind != EventFactLearned && event.Kind != EventFactActivated && event.Kind != EventFactSuperseded {
			continue
		}
		if err := replayEvent(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}
