package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	EventContextualEvidence          EventKind = "contextual_evidence"
	EventContextualSuppression       EventKind = "contextual_suppression"
	EventContextualMemorySuppression EventKind = "contextual_memory_suppression"
	ContextualEvidenceLimit                    = 128
)

// ContextualEvidence preserves observations separately from the claim derived
// from them. The existing event journal is its only durable representation.
type ContextualEvidence struct {
	ID            string
	MemoryID      string
	Owner         string
	SessionID     string
	TurnID        string
	Actor         string
	Tool          string
	ReceiptIDs    []string
	Applicability []string
	Rationale     string
	Rejected      []string
	Reconsider    string
	Observation   string
	Revision      string
	Verification  string
	Derivations   []int64
	Authority     string
	ValidFrom     time.Time
	ValidUntil    time.Time
	Conditions    map[string]string
	SourceKey     string
	SourceHash    string
	Seq           int64
	At            time.Time
}

type contextualSuppression struct{ Owner, SourceKey, SourceHash, Reason string }

func contextualNode(owner string) string { return "contextual:" + owner }

// validateContextualEvidence is the whole admission table for one evidence row:
// required fields, bounds, the authority/actor pairs that may speak, and the
// interval. It answers one reason per refusal, and it is deliberately a table
// rather than a growing if-ladder — a new guard is a row, and the append
// writers below share the same rows.
func validateContextualEvidence(e ContextualEvidence) error {
	if strings.TrimSpace(e.Owner) == "" || strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.MemoryID) == "" || strings.TrimSpace(e.Observation) == "" {
		return errors.New("contextual evidence requires owner, id, memory and observation")
	}
	if err := validateContextualEvidenceBounds(e); err != nil {
		return err
	}
	return validateContextualEvidenceAuthority(e)
}

// validateContextualEvidenceBounds is the size and interval half of the table,
// split into its shape (what a row may weigh) and its metadata (how long any one
// field may be) so neither road holds more endings than a reader can follow.
func validateContextualEvidenceBounds(e ContextualEvidence) error {
	if err := validateContextualEvidenceShape(e); err != nil {
		return err
	}
	return validateContextualEvidenceMetadata(e)
}

// validateContextualEvidenceShape is the count and interval half.
func validateContextualEvidenceShape(e ContextualEvidence) error {
	switch {
	case len(e.ReceiptIDs) > 32 || len(e.Derivations) > 16 || len(e.Conditions) > 16 || len(e.Observation) > 16384:
		return errors.New("contextual evidence exceeds bounds")
	case len(e.Applicability) > 8 || len(e.Rejected) > 8:
		return errors.New("contextual reasoning exceeds bounds")
	case !e.ValidUntil.IsZero() && !e.ValidUntil.After(e.ValidFrom):
		return errors.New("invalid contextual validity interval")
	}
	return nil
}

// validateContextualEvidenceMetadata is the per-field length half: identifiers,
// conditions, receipts and the reasoning lines.
func validateContextualEvidenceMetadata(e ContextualEvidence) error {
	for _, v := range []string{e.ID, e.MemoryID, e.Owner, e.SessionID, e.TurnID, e.Actor, e.Tool, e.Revision, e.Verification, e.Authority, e.SourceKey, e.SourceHash} {
		if len(v) > 1024 {
			return errors.New("contextual metadata exceeds bounds")
		}
	}
	for k, v := range e.Conditions {
		if len(k) > 256 || len(v) > 1024 {
			return errors.New("contextual condition exceeds bounds")
		}
	}
	for _, v := range e.ReceiptIDs {
		if len(v) > 1024 {
			return errors.New("contextual receipt exceeds bounds")
		}
	}
	for _, v := range append(append([]string{e.Rationale, e.Reconsider}, e.Applicability...), e.Rejected...) {
		if utf8.RuneCountInString(v) > 240 {
			return errors.New("contextual reasoning exceeds bounds")
		}
	}
	return nil
}

// validateContextualEvidenceAuthority is the WHO MAY SPEAK half: an authority
// word this build knows, and the actor that must stand behind it.
func validateContextualEvidenceAuthority(e ContextualEvidence) error {
	switch {
	case !validContextualAuthority(e.Authority):
		return errors.New("unknown contextual authority")
	case (e.Authority == "user" || e.Authority == "approved_rule" || e.Authority == "confirmed_decision") && e.Actor != "user":
		return errors.New("user authority requires a user assertion")
	case e.Authority == "observation" && e.Actor != "user" && e.Actor != "tool" && e.Actor != "system":
		return errors.New("observation requires a user, tool or system source")
	case len(e.ReceiptIDs) > 0 && e.Actor != "tool":
		return errors.New("receipts require a tool observation")
	case len(e.Derivations) > 0 && e.Authority != "inference":
		return errors.New("derived evidence must retain inference authority")
	}
	return nil
}

func validContextualAuthority(authority string) bool {
	switch authority {
	case "user", "observation", "inference", "approved_rule", "confirmed_decision", "proposal":
		return true
	}
	return false
}

// appendContextualEvidenceTx admits one evidence row inside an ALREADY-OPEN
// write transaction. It is the shared body of [Store.AppendContextualEvidence]
// and [Store.WriteContextual]: a caller that must land a memory row and its
// provenance together uses this, so the two cannot be split by a crash.
func appendContextualEvidenceTx(tx *sql.Tx, e ContextualEvidence) (ContextualEvidence, error) {
	e.Seq = 0
	e.At = time.Time{}
	var duplicate int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.ID')=?`, contextualNode(e.Owner), EventContextualEvidence, e.ID).Scan(&duplicate); err != nil {
		return e, err
	}
	if duplicate != 0 {
		return e, errors.New("contextual evidence id already exists")
	}
	if e.SourceKey != "" {
		var suppressed int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'')='' OR json_extract(payload,'$.SourceHash')=?)`, contextualNode(e.Owner), EventContextualSuppression, e.SourceKey, e.SourceHash).Scan(&suppressed); err != nil {
			return e, err
		}
		if suppressed > 0 {
			return e, errors.New("contextual source was suppressed")
		}
	}
	if invalid, err := contextualMemorySuppressed(tx, e); err != nil {
		return e, err
	} else if invalid {
		return e, errors.New("contextual memory or source was suppressed")
	}
	// Parent validation uses this write transaction, so suppression cannot race it.
	for _, seq := range e.Derivations {
		parent, err := readContextualEvidence(tx, e.Owner, seq)
		if err != nil {
			return e, fmt.Errorf("contextual parent: %w", err)
		}
		budget := ContextualEvidenceLimit
		if ok, err := contextualUsable(contextualDB{tx}, parent, time.Now(), map[int64]bool{}, &budget); err != nil || !ok {
			if err != nil {
				return e, err
			}
			return e, errors.New("contextual parent is no longer applicable")
		}
	}
	seq, at, err := appendEvent(tx, contextualNode(e.Owner), EventContextualEvidence, e)
	if err != nil {
		return e, err
	}
	e.Seq = seq
	e.At = at
	return e, nil
}

// AppendContextualEvidence rejects a derived claim unless every parent still
// belongs to its owner and remains usable. Receipts cannot confer authority.
func (s *Store) AppendContextualEvidence(e ContextualEvidence) (ContextualEvidence, error) {
	if err := validateContextualEvidence(e); err != nil {
		return e, err
	}
	tx, err := s.beginWrite()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	committed, err := appendContextualEvidenceTx(tx, e)
	if err != nil {
		return e, err
	}
	if err = tx.Commit(); err != nil {
		return e, err
	}
	return committed, nil
}

// SuppressContextualSource prevents relearning the same source content, and
// excludes every descendant when the evidence projection next reads it.
func (s *Store) SuppressContextualSource(owner, key, hash, reason string) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(key) == "" {
		return errors.New("suppression requires owner and source")
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, _, err = appendEvent(tx, contextualNode(owner), EventContextualSuppression, contextualSuppression{owner, key, hash, reason})
	if err != nil {
		return err
	}
	return tx.Commit()
}

type contextualReader interface{ QueryRow(string, ...any) *sql.Row }

// contextualRecords answers one canonical evidence row at a fixed sequence and
// the two owner-journal states the guard consults: whether a suppression retires
// it, and whether a later correction or a direct suppression retired its source.
// A database-backed reader loads each answer on demand; the batched guard
// ([Store.ContextualEvidenceEligibleBatch]) preloads the whole ancestry in a
// couple of owner-scoped reads, so the recursion below never issues one query
// per candidate row.
type contextualRecords interface {
	record(owner string, seq int64) (ContextualEvidence, error)
	suppressed(e ContextualEvidence) (bool, error)
	sourceRetired(e ContextualEvidence) (bool, error)
}

type contextualDB struct{ q contextualReader }

func (d contextualDB) record(owner string, seq int64) (ContextualEvidence, error) {
	return readContextualEvidence(d.q, owner, seq)
}

func (d contextualDB) suppressed(e ContextualEvidence) (bool, error) {
	return contextualMemorySuppressed(d.q, e)
}

// sourceRetired is the older of the two journal questions: a direct source
// suppression, or a later evidence row that shares the source key with a
// different hash or revision. It consults the whole owner journal, never only the
// window the caller read, so a truncated window cannot resurrect a retired source.
func (d contextualDB) sourceRetired(e ContextualEvidence) (bool, error) {
	if e.SourceKey == "" {
		return false, nil
	}
	var invalid int
	err := d.q.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND ((kind=? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'' )='' OR json_extract(payload,'$.SourceHash')=?)) OR (kind=? AND seq>? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'' )<>? OR COALESCE(json_extract(payload,'$.Revision'),'' )<>?)))`, contextualNode(e.Owner), EventContextualSuppression, e.SourceKey, e.SourceHash, EventContextualEvidence, e.Seq, e.SourceKey, e.SourceHash, e.Revision).Scan(&invalid)
	return invalid != 0, err
}

func readContextualEvidence(q contextualReader, owner string, seq int64) (ContextualEvidence, error) {
	var e ContextualEvidence
	var payload, ts string
	err := q.QueryRow(`SELECT payload,ts FROM events WHERE seq=? AND node_id=? AND kind=?`, seq, contextualNode(owner), EventContextualEvidence).Scan(&payload, &ts)
	if err != nil {
		return e, err
	}
	if err = json.Unmarshal([]byte(payload), &e); err != nil {
		return e, err
	}
	e.Seq = seq
	e.At, err = parseTime(ts)
	return e, err
}

// contextualUsable consults global source state even when the caller reads a
// narrow window. Truncating a window must never resurrect an invalidated source.
func contextualUsable(r contextualRecords, e ContextualEvidence, at time.Time, seen map[int64]bool, budget *int) (bool, error) {
	*budget--
	if *budget < 0 || seen[e.Seq] {
		return false, nil
	}
	seen[e.Seq] = true
	defer delete(seen, e.Seq)
	if !contextualWithinValidity(e, at) {
		return false, nil
	}
	if invalid, err := r.suppressed(e); err != nil || invalid {
		return false, err
	}
	if invalid, err := r.sourceRetired(e); err != nil || invalid {
		return false, err
	}
	for _, seq := range e.Derivations {
		parent, err := r.record(e.Owner, seq)
		if err != nil {
			return false, err
		}
		ok, err := contextualUsable(r, parent, at, seen, budget)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// contextualWithinValidity is the interval half of the guard, kept apart so the
// road above reads as its phases.
func contextualWithinValidity(e ContextualEvidence, at time.Time) bool {
	if !e.ValidFrom.IsZero() && at.Before(e.ValidFrom) {
		return false
	}
	if !e.ValidUntil.IsZero() && !at.Before(e.ValidUntil) {
		return false
	}
	return true
}

// ContextualEvidenceWindow bounds the amount read as well as the result. The
// caller advances with the last returned sequence; this is an audit projection.
func (s *Store) ContextualEvidenceWindow(owner string, after int64, limit int) ([]ContextualEvidence, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("contextual evidence requires owner")
	}
	if limit <= 0 || limit > ContextualEvidenceLimit {
		limit = ContextualEvidenceLimit
	}
	rows, err := s.db.Query(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND seq>? ORDER BY seq LIMIT ?`, contextualNode(owner), EventContextualEvidence, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContextualEvidence, 0)
	for rows.Next() {
		var e ContextualEvidence
		var ts, payload string
		var seq int64
		if err = rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		e.Seq = seq
		e.At, err = parseTime(ts)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ContextualEvidenceByID(owner, id string) (ContextualEvidence, error) {
	var seq int64
	err := s.db.QueryRow(`SELECT seq FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.ID')=? ORDER BY seq DESC LIMIT 1`, contextualNode(owner), EventContextualEvidence, id).Scan(&seq)
	if err != nil {
		return ContextualEvidence{}, err
	}
	return readContextualEvidence(s.db, owner, seq)
}

// ContextualEvidenceApplicable reads at most one bounded newest window. It
// deliberately returns fewer results when excluded evidence fills that window.
func (s *Store) ContextualEvidenceApplicable(owner string, conditions map[string]string, at time.Time, limit int) ([]ContextualEvidence, error) {
	if limit <= 0 || limit > ContextualEvidenceLimit {
		limit = ContextualEvidenceLimit
	}
	if at.IsZero() {
		at = time.Now()
	}
	var floor int64
	err := s.db.QueryRow(`SELECT COALESCE(MIN(seq),0)-1 FROM (SELECT seq FROM events WHERE node_id=? AND kind=? ORDER BY seq DESC LIMIT ?)`, contextualNode(owner), EventContextualEvidence, ContextualEvidenceLimit).Scan(&floor)
	if err != nil {
		return nil, err
	}
	all, err := s.ContextualEvidenceWindow(owner, floor, ContextualEvidenceLimit)
	if err != nil {
		return nil, err
	}
	return contextualFilterEvidence(s.db, all, false, conditions, at, limit)
}

// ContextualEvidenceApproved reads the newest bounded window of BINDING
// authority — approved rules and confirmed decisions — for one owner.
//
// IT EXISTS SO NOISE CANNOT STARVE A LIVE CONSTRAINT. The general projection
// reads a newest window of every observation, so a burst of newer incidental
// evidence would push a rare approved rule past the window and out of the
// results. A rule a person explicitly approved is not the newest thing; it is
// the thing that must keep binding. This projection filters the window by
// authority at the query, so the window is spent on rules, and the same
// validity, suppression, correction and condition guards still decide whether
// each one is live. The window and the result are both bounded.
func (s *Store) ContextualEvidenceApproved(owner string, conditions map[string]string, at time.Time, limit int) ([]ContextualEvidence, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("contextual evidence requires owner")
	}
	if limit <= 0 || limit > ContextualEvidenceLimit {
		limit = ContextualEvidenceLimit
	}
	if at.IsZero() {
		at = time.Now()
	}
	// ONE INDEXED READ, NEWEST FIRST. The partial index
	// events_contextual_approved holds only binding rows and covers seq, so the
	// newest-window read is a seek over the rule count rather than a walk of
	// the owner's whole evidence partition. The window is still bounded: it
	// carries at most [ContextualEvidenceLimit] rows, newest first, and the
	// result is bounded by limit.
	rows, err := s.db.Query(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.Authority') IN ('approved_rule','confirmed_decision') ORDER BY seq DESC LIMIT ?`, contextualNode(owner), EventContextualEvidence, ContextualEvidenceLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	newest, err := scanContextualEvidenceRows(rows)
	if err != nil {
		return nil, err
	}
	return contextualFilterEvidence(s.db, newest, true, conditions, at, limit)
}

// scanContextualEvidenceRows reads a (seq, ts, payload) evidence window.
func scanContextualEvidenceRows(rows *sql.Rows) ([]ContextualEvidence, error) {
	out := make([]ContextualEvidence, 0)
	for rows.Next() {
		var e ContextualEvidence
		var ts, payload string
		var seq int64
		if err := rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		e.Seq = seq
		at, err := parseTime(ts)
		if err != nil {
			return nil, err
		}
		e.At = at
		out = append(out, e)
	}
	return out, rows.Err()
}

// contextualFilterEvidence applies the live guards to one already-read window,
// newest first when newestFirst is set and oldest first otherwise. The budget is
// shared across the window, so an ancestry that exhausts it stops the walk, not
// one row of it.
func contextualFilterEvidence(q contextualReader, window []ContextualEvidence, newestFirst bool, conditions map[string]string, at time.Time, limit int) ([]ContextualEvidence, error) {
	out := make([]ContextualEvidence, 0, limit)
	budget := ContextualEvidenceLimit * ContextualEvidenceLimit
	guard := contextualDB{q}
	for step := 0; step < len(window) && len(out) < limit; step++ {
		i := step
		if !newestFirst {
			i = len(window) - 1 - step
		}
		e := window[i]
		ok, err := contextualUsable(guard, e, at, map[int64]bool{}, &budget)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if ok, err = contextualConditionsBounded(guard, e, conditions); err != nil {
			return nil, err
		}
		if ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// contextualRevisionUnknown answers whether a snapshot identity is one the
// capture could not prove. The empty string is NOT unknown here: it is the
// absent revision of a user-authored rule, which the caller treats as a
// wildcard before this ever runs.
func contextualRevisionUnknown(s string) bool {
	switch s {
	case "unknown", "source-snapshot-unavailable":
		return true
	}
	return false
}

func contextualConditionsMatch(r contextualRecords, e ContextualEvidence, conditions map[string]string, remaining *int) (bool, error) {
	// AN UNKNOWN SNAPSHOT PROVES NOTHING. An EMPTY revision is user-authored
	// intent — a rule the person wrote with no source attached — and applies on
	// any source. Any other revision must name a KNOWN current snapshot and
	// equal it, so "unknown" never matches "unknown": two captures that each
	// failed to identify the source are not the same source.
	if e.Revision != "" {
		cur := conditions["revision"]
		if contextualRevisionUnknown(cur) || contextualRevisionUnknown(e.Revision) || cur != e.Revision {
			return false, nil
		}
	}
	*remaining--
	if *remaining < 0 {
		return false, nil
	}
	for k, v := range e.Conditions {
		if actual, ok := conditions[k]; !ok || actual != v {
			return false, nil
		}
	}
	for _, seq := range e.Derivations {
		parent, err := r.record(e.Owner, seq)
		if err != nil {
			return false, err
		}
		ok, err := contextualConditionsMatch(r, parent, conditions, remaining)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// ContextualEvidenceForMemory exposes the latest raw record for explanation;
// callers must ask ContextualEvidenceEligible before using it as a claim.
func (s *Store) ContextualEvidenceForMemory(owner, memoryID string) (ContextualEvidence, error) {
	var seq int64
	err := s.db.QueryRow(`SELECT seq FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.MemoryID')=? ORDER BY seq DESC LIMIT 1`, contextualNode(owner), EventContextualEvidence, memoryID).Scan(&seq)
	if err != nil {
		return ContextualEvidence{}, err
	}
	return readContextualEvidence(s.db, owner, seq)
}

// ContextualEvidenceEligible reloads the canonical record, so a caller cannot
// substitute an altered owner, condition or authority into the projection.
func (s *Store) ContextualEvidenceEligible(e ContextualEvidence, conditions map[string]string, at time.Time) (bool, error) {
	canonical, err := readContextualEvidence(s.db, e.Owner, e.Seq)
	if err != nil {
		return false, err
	}
	if at.IsZero() {
		at = time.Now()
	}
	budget := ContextualEvidenceLimit
	ok, err := contextualUsable(contextualDB{s.db}, canonical, at, map[int64]bool{}, &budget)
	if err != nil || !ok {
		return false, err
	}
	return contextualConditionsBounded(contextualDB{s.db}, canonical, conditions)
}

func contextualConditionsBounded(r contextualRecords, e ContextualEvidence, conditions map[string]string) (bool, error) {
	budget := ContextualEvidenceLimit
	return contextualConditionsMatch(r, e, conditions, &budget)
}

// SuppressContextualMemorySources retires all provenance of a forgotten claim
// with one canonical event. New evidence cannot relearn any retired source.
func (s *Store) SuppressContextualMemorySources(owner, memoryID, reason string) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(memoryID) == "" {
		return errors.New("memory suppression requires owner and memory")
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, _, err = appendEvent(tx, contextualNode(owner), EventContextualMemorySuppression, struct{ MemoryID, Reason string }{memoryID, reason})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// The source join includes every historical provenance entry of a suppressed
// memory, rather than only its latest entry or a bounded retrieval window.
func contextualMemorySuppressed(q contextualReader, e ContextualEvidence) (bool, error) {
	var invalid int
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM events AS suppression WHERE suppression.node_id=? AND suppression.kind=? AND (json_extract(suppression.payload,'$.MemoryID')=? OR EXISTS(SELECT 1 FROM events AS source WHERE source.node_id=suppression.node_id AND source.kind=? AND json_extract(source.payload,'$.MemoryID')=json_extract(suppression.payload,'$.MemoryID') AND json_extract(source.payload,'$.SourceKey')=? AND json_extract(source.payload,'$.SourceHash')=? AND ?<>'')))`, contextualNode(e.Owner), EventContextualMemorySuppression, e.MemoryID, EventContextualEvidence, e.SourceKey, e.SourceHash, e.SourceKey).Scan(&invalid)
	return invalid != 0, err
}
