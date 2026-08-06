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
	// FactSkill is an execution-verified procedure offered to future work.
	FactSkill FactKind = "skill"
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

// Fact statuses. Candidates, settled, and quarantined facts stay in the table
// — the journal never forgets — but retrieval returns active facts only.
const (
	FactCandidate   = "candidate"
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

	// Unsettled is present only when Kind is FactUnsettled. Body is its
	// readable projection; this payload is what code branches on.
	Unsettled *UnsettledPair

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
    kind      TEXT NOT NULL DEFAULT 'fact' CHECK (kind IN ('preference', 'quirk', 'lesson', 'fact', 'unsettled', 'skill')),
    body      TEXT NOT NULL,
    unsettled JSON NOT NULL DEFAULT 'null' CHECK (json_valid(unsettled)),
    status    TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('candidate', 'active', 'superseded', 'quarantined')),
    artifact  TEXT NOT NULL DEFAULT '',
    status_note TEXT NOT NULL DEFAULT '',
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
	NodeID    string         `json:"node_id"`
	Scope     string         `json:"scope,omitempty"`
	Kind      FactKind       `json:"kind,omitempty"`
	Body      string         `json:"body"`
	Unsettled *UnsettledPair `json:"unsettled,omitempty"`
	Status    string         `json:"status,omitempty"`
	Artifact  string         `json:"artifact,omitempty"`
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
	if kind == FactUnsettled {
		return Fact{}, fmt.Errorf("record fact: %w: unsettled fact requires a structured pair", ErrInvalid)
	}
	if kind == FactSkill {
		return Fact{}, fmt.Errorf("record fact: %w: skills begin as candidates", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, kind, body, nil, 0, FactActive, "", true)
}

// RecordUnsettledFact appends one structured competing pair. Its Body is
// derived from the pair so the searchable prose cannot disagree with code.
func (s *Store) RecordUnsettledFact(nodeID, scope string, pair UnsettledPair) (Fact, error) {
	if err := pair.Validate(); err != nil {
		return Fact{}, fmt.Errorf("record unsettled fact: %w: %v", ErrInvalid, err)
	}
	return s.recordFact(nodeID, scope, FactUnsettled, FormatUnsettledPair(pair), &pair, 0, FactActive, "", true)
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
	if kind == FactSkill {
		return Fact{}, fmt.Errorf("replace fact: %w: skills begin as candidates", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, kind, body, nil, factSeq, FactActive, "", true)
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
	return s.recordFact(nodeID, scope, FactUnsettled, FormatUnsettledPair(pair), &pair, factSeq, FactActive, "", true)
}

// RecordSkillCandidate journals a procedure the distiller found in one job.
// It is intentionally absent from retrieval until a later execution event
// activates it.
func (s *Store) RecordSkillCandidate(nodeID, scope, body, artifact string) (Fact, error) {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return Fact{}, fmt.Errorf("record skill candidate: %w: empty artifact", ErrInvalid)
	}
	return s.recordFact(nodeID, scope, FactSkill, body, nil, 0, FactCandidate, artifact, false)
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
	return s.recordFact(nodeID, scope, FactSkill, body, nil, 0, FactActive, sources[0].Artifact, true)
}

func (s *Store) recordFact(nodeID, scope string, kind FactKind, body string, unsettled *UnsettledPair, replaces int64, status, artifact string, deduplicate bool) (Fact, error) {
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
	if kind == FactUnsettled {
		if unsettled == nil {
			return Fact{}, fmt.Errorf("record fact: %w: unsettled fact requires a structured pair", ErrInvalid)
		}
	} else if unsettled != nil {
		return Fact{}, fmt.Errorf("record fact: %w: only unsettled facts carry a pair", ErrInvalid)
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
	if unsettled != nil {
		if err := requireUnsettledEvidence(tx, *unsettled); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}

	var duplicate int64
	if deduplicate {
		err = tx.QueryRow(`
			SELECT seq FROM facts
			WHERE scope = ? AND status IN (?, ?) AND lower(body) = lower(?)
			ORDER BY status = ? DESC, seq DESC LIMIT 1`,
			scope, FactActive, FactQuarantined, body, FactActive).Scan(&duplicate)
		if err != nil && err != sql.ErrNoRows {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}

	payload := factPayload{NodeID: nodeID, Scope: scope, Kind: kind, Body: body,
		Unsettled: unsettled, Status: status, Artifact: artifact}
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
		Status: status, StatusSeq: seq, Unsettled: unsettled, Artifact: artifact}, nil
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

// SupersedeFact retires one active or quarantined fact in favour of another,
// journaled.
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
	seq, _, err := appendEvent(tx, "", EventFactSuperseded, payload)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	if err := applyFactSupersession(tx, payload, seq); err != nil {
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
			SELECT f.seq, f.ts, f.node_id, f.scope, f.kind, f.body, f.unsettled, f.status, f.artifact, f.status_note,
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
		SELECT seq, ts, node_id, scope, kind, body, unsettled, status, artifact, status_note,
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
		var timestamp, unsettled, lastUsed string
		if err := rows.Scan(&fact.Seq, &timestamp, &fact.NodeID, &fact.Scope, &fact.Kind,
			&fact.Body, &unsettled, &fact.Status, &fact.Artifact, &fact.StatusNote,
			&fact.StatusSeq, &fact.EvidenceSeq, &fact.StatusOrigin, &fact.Uses, &lastUsed); err != nil {
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
	status := payload.Status
	if !validFactStatus(status) {
		// fact_learned events predating skill candidacy have no status field.
		status = FactActive
	}

	if _, err := tx.Exec(`
		INSERT INTO facts (seq, ts, node_id, scope, kind, body, unsettled, status, artifact, status_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.NodeID, scope, kind, payload.Body, string(encoded), status, payload.Artifact, seq); err != nil {
		return err
	}
	if status != FactActive {
		return nil
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
func applyFactSupersession(tx *sql.Tx, payload factSupersededPayload, seq int64) error {
	result, err := tx.Exec(`
		UPDATE facts
		SET status = ?, status_note = ?, status_seq = ?, evidence_seq = ?, status_origin = ?
		WHERE seq = ? AND status <> ?`,
		FactSuperseded, payload.Reason, seq, payload.BySeq, FactOriginSupersession,
		payload.FactSeq, FactSuperseded)
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
	case FactPreference, FactQuirk, FactLesson, FactPlain, FactUnsettled, FactSkill:
		return true
	}
	return false
}

func validFactStatus(status string) bool {
	switch status {
	case FactCandidate, FactActive, FactSuperseded, FactQuarantined:
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
	hasScope, hasUnsettled, hasArtifact, hasStatusNote := false, false, false, false
	hasStatusSeq, hasEvidenceSeq, hasStatusOrigin := false, false, false
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
		case "unsettled":
			hasUnsettled = true
		case "artifact":
			hasArtifact = true
		case "status_note":
			hasStatusNote = true
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
	var createSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'facts'`).Scan(&createSQL); err != nil {
		return err
	}
	if hasScope && hasUnsettled && hasArtifact && hasStatusNote &&
		hasStatusSeq && hasEvidenceSeq && hasStatusOrigin &&
		strings.Contains(createSQL, "'unsettled'") &&
		strings.Contains(createSQL, "'skill'") && strings.Contains(createSQL, "'candidate'") &&
		strings.Contains(createSQL, "'quarantined'") {
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
		case EventFactLearned, EventFactActivated, EventFactSuperseded,
			EventFactInjected, EventFactQuarantined, EventFactRestored:
		default:
			continue
		}
		if err := replayEvent(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}
