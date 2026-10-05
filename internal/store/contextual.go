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

// AppendContextualEvidence rejects a derived claim unless every parent still
// belongs to its owner and remains usable. Receipts cannot confer authority.
func (s *Store) AppendContextualEvidence(e ContextualEvidence) (ContextualEvidence, error) {
	if strings.TrimSpace(e.Owner) == "" || strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.MemoryID) == "" || strings.TrimSpace(e.Observation) == "" {
		return e, errors.New("contextual evidence requires owner, id, memory and observation")
	}
	if len(e.ReceiptIDs) > 32 || len(e.Derivations) > 16 || len(e.Conditions) > 16 || len(e.Observation) > 16384 {
		return e, errors.New("contextual evidence exceeds bounds")
	}
	if e.Authority != "user" && e.Authority != "observation" && e.Authority != "inference" && e.Authority != "approved_rule" && e.Authority != "confirmed_decision" && e.Authority != "proposal" {
		return e, errors.New("unknown contextual authority")
	}
	if !e.ValidUntil.IsZero() && !e.ValidUntil.After(e.ValidFrom) {
		return e, errors.New("invalid contextual validity interval")
	}
	if (e.Authority == "user" || e.Authority == "approved_rule" || e.Authority == "confirmed_decision") && e.Actor != "user" {
		return e, errors.New("user authority requires a user assertion")
	}
	if e.Authority == "observation" && e.Actor != "user" && e.Actor != "tool" && e.Actor != "system" {
		return e, errors.New("observation requires a user, tool or system source")
	}
	if len(e.ReceiptIDs) > 0 && e.Actor != "tool" {
		return e, errors.New("receipts require a tool observation")
	}
	if len(e.Derivations) > 0 && e.Authority != "inference" {
		return e, errors.New("derived evidence must retain inference authority")
	}
	for _, v := range []string{e.ID, e.MemoryID, e.Owner, e.SessionID, e.TurnID, e.Actor, e.Tool, e.Revision, e.Verification, e.Authority, e.SourceKey, e.SourceHash} {
		if len(v) > 1024 {
			return e, errors.New("contextual metadata exceeds bounds")
		}
	}
	for k, v := range e.Conditions {
		if len(k) > 256 || len(v) > 1024 {
			return e, errors.New("contextual condition exceeds bounds")
		}
	}
	for _, v := range e.ReceiptIDs {
		if len(v) > 1024 {
			return e, errors.New("contextual receipt exceeds bounds")
		}
	}
	if len(e.Applicability) > 8 || len(e.Rejected) > 8 {
		return e, errors.New("contextual reasoning exceeds bounds")
	}
	for _, v := range append(append([]string{e.Rationale, e.Reconsider}, e.Applicability...), e.Rejected...) {
		if utf8.RuneCountInString(v) > 240 {
			return e, errors.New("contextual reasoning exceeds bounds")
		}
	}
	e.Seq = 0
	e.At = time.Time{}
	tx, err := s.beginWrite()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	var duplicate int
	err = tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.ID')=?`, contextualNode(e.Owner), EventContextualEvidence, e.ID).Scan(&duplicate)
	if err != nil {
		return e, err
	}
	if duplicate != 0 {
		return e, errors.New("contextual evidence id already exists")
	}
	if e.SourceKey != "" {
		var suppressed int
		err = tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'')='' OR json_extract(payload,'$.SourceHash')=?)`, contextualNode(e.Owner), EventContextualSuppression, e.SourceKey, e.SourceHash).Scan(&suppressed)
		if err != nil {
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
		if ok, err := contextualUsable(tx, parent, time.Now(), map[int64]bool{}, &budget); err != nil || !ok {
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
	if err = tx.Commit(); err != nil {
		return e, err
	}
	e.Seq = seq
	e.At = at
	return e, nil
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
func contextualUsable(q contextualReader, e ContextualEvidence, at time.Time, seen map[int64]bool, budget *int) (bool, error) {
	*budget--
	if *budget < 0 || seen[e.Seq] {
		return false, nil
	}
	seen[e.Seq] = true
	defer delete(seen, e.Seq)
	if !e.ValidFrom.IsZero() && at.Before(e.ValidFrom) || !e.ValidUntil.IsZero() && !at.Before(e.ValidUntil) {
		return false, nil
	}
	if invalid, err := contextualMemorySuppressed(q, e); err != nil || invalid {
		return false, err
	}
	var invalid int
	if e.SourceKey != "" {
		err := q.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND ((kind=? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'' )='' OR json_extract(payload,'$.SourceHash')=?)) OR (kind=? AND seq>? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'' )<>? OR COALESCE(json_extract(payload,'$.Revision'),'' )<>?)))`, contextualNode(e.Owner), EventContextualSuppression, e.SourceKey, e.SourceHash, EventContextualEvidence, e.Seq, e.SourceKey, e.SourceHash, e.Revision).Scan(&invalid)
		if err != nil {
			return false, err
		}
		if invalid > 0 {
			return false, nil
		}
	}
	for _, seq := range e.Derivations {
		p, err := readContextualEvidence(q, e.Owner, seq)
		if err != nil {
			return false, err
		}
		ok, err := contextualUsable(q, p, at, seen, budget)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
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
	out := make([]ContextualEvidence, 0)
	budget := ContextualEvidenceLimit * ContextualEvidenceLimit
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		ok, err := contextualUsable(s.db, e, at, map[int64]bool{}, &budget)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		ok, err = contextualConditionsBounded(s.db, e, conditions)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func contextualConditionsMatch(q contextualReader, e ContextualEvidence, conditions map[string]string, remaining *int) (bool, error) {
	if e.Revision != "" && conditions["revision"] != e.Revision {
		return false, nil
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
		parent, err := readContextualEvidence(q, e.Owner, seq)
		if err != nil {
			return false, err
		}
		ok, err := contextualConditionsMatch(q, parent, conditions, remaining)
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
	ok, err := contextualUsable(s.db, canonical, at, map[int64]bool{}, &budget)
	if err != nil || !ok {
		return false, err
	}
	return contextualConditionsBounded(s.db, canonical, conditions)
}

func contextualConditionsBounded(q contextualReader, e ContextualEvidence, conditions map[string]string) (bool, error) {
	budget := ContextualEvidenceLimit
	return contextualConditionsMatch(q, e, conditions, &budget)
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
