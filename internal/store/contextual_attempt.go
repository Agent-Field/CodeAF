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

// EventContextualAttempt is the kind of the observed-outcome rows in the one
// canonical journal. An attempt is written from the executeTool boundary for a
// call that FAILED or was BLOCKED — never for an ordinary success — so a real
// failure outlives the turn that produced it without waiting for a model to
// extract it.
const EventContextualAttempt EventKind = "contextual_attempt"
const ContextualAttemptLimit = 64

// RETENTION IS LOGICAL, NOT PHYSICAL, AND THAT IS THE JOURNAL'S LAW RATHER THAN
// A CHOICE HERE. Attempts live in the one canonical journal, whose
// events_no_delete trigger refuses every DELETE — an attempt cannot be pruned
// without breaking the append-only guarantee the whole store rests on. What
// bounds automatic outcome churn therefore is the READ: every attempt path
// reads at most one [ContextualAttemptLimit]-sized newest window (see
// [Store.ContextualAttemptsApplicable] and [Store.ContextualAttemptsRecent]),
// so growth is on disk and never in the work a turn does. The scan that window
// pays for is covered by events_node_kind_seq, and a suppression tombstone is
// a separate event that no read can lose.

// The three status words an attempt may carry. "unknown" is the honest reading
// of a call the HARNESS blocked — a refused door, a withdrawn hand — because a
// refusal is not a demonstration that the approach cannot work.
const (
	AttemptFailed    = "failed"
	AttemptSucceeded = "succeeded"
	AttemptUnknown   = "unknown"
)

// ContextualAttempt is one independently observed command outcome. It keeps the
// original action, goal, observation and circumstances separate from any
// inferred cause and from the current claim: InferredCause is advisory text and
// is NEVER promoted to authority by this store.
//
// THE SOURCE KEY IS THE FORGET LINK. An attempt carries the same SourceKey and
// SourceHash as the evidence row extracted from the same tool receipt, so an
// explicit forget of that claim retires the attempt with it — and nothing else.
// A fresh observation is a different call and a different key, so it survives.
type ContextualAttempt struct {
	ID            string
	Owner         string
	SessionID     string
	TurnID        string
	Tool          string
	Action        string
	Goal          string
	Status        string
	ReceiptIDs    []string
	Observation   string
	InferredCause string
	Reconsider    string
	Snapshot      string
	Conditions    map[string]string
	SourceKey     string
	SourceHash    string
	// AlternativeOf, on an AttemptSucceeded row, is the SourceKey of the
	// DEMONSTRATED failure this success was independently observed as a later
	// alternative to. It is the whole association: the pairing rests on the
	// same turn and goal plus the shared action, never on an inferred cause.
	// Empty on a failure or a block, which carry their own history only.
	AlternativeOf string
	ValidFrom     time.Time
	ValidUntil    time.Time
	Seq           int64
	At            time.Time
}

// AppendContextualAttempt writes one observed outcome. It mirrors the evidence
// writer's guards: one transaction, bounded fields, a suppressed source refused
// BEFORE anything is returned, and a duplicate receipt made IDEMPOTENT — a
// re-sent observation with the same SourceKey and SourceHash returns the row
// already held, while a genuinely fresh observation (a different hash) is new.
func (s *Store) AppendContextualAttempt(e ContextualAttempt) (ContextualAttempt, error) {
	if strings.TrimSpace(e.Owner) == "" || !ValidOwner(e.Owner) || strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Action) == "" || strings.TrimSpace(e.Observation) == "" {
		return e, errors.New("contextual attempt requires a valid owner, id, action and observation")
	}
	switch e.Status {
	case AttemptFailed, AttemptSucceeded:
		// A DEMONSTRATED OUTCOME NEEDS A REAL RECEIPT. An empty receipt is the
		// absent evidence that contract 1 refuses to dress up as a proven failure.
		if len(e.ReceiptIDs) == 0 {
			return e, errors.New("a demonstrated attempt requires a receipt")
		}
	case AttemptUnknown:
		// BLOCKED OR UNPROVEN: no receipt is required and none is invented.
	default:
		return e, errors.New("unknown contextual attempt status")
	}
	if len(e.ReceiptIDs) > 8 || len(e.Conditions) > 16 || len(e.Observation) > 16384 || len(e.Action) > 4096 || len(e.Goal) > 4096 {
		return e, errors.New("contextual attempt exceeds bounds")
	}
	if !e.ValidUntil.IsZero() && !e.ValidUntil.After(e.ValidFrom) {
		return e, errors.New("invalid contextual attempt validity interval")
	}
	for _, v := range []string{e.ID, e.Owner, e.SessionID, e.TurnID, e.Tool, e.Reconsider, e.Snapshot, e.SourceKey, e.SourceHash, e.AlternativeOf} {
		if len(v) > 1024 {
			return e, errors.New("contextual attempt metadata exceeds bounds")
		}
	}
	if utf8.RuneCountInString(e.InferredCause) > 240 || utf8.RuneCountInString(e.Reconsider) > 240 {
		return e, errors.New("contextual attempt reasoning exceeds bounds")
	}
	for k, v := range e.Conditions {
		if len(k) > 256 || len(v) > 1024 {
			return e, errors.New("contextual attempt condition exceeds bounds")
		}
	}
	for _, v := range e.ReceiptIDs {
		if len(v) > 1024 {
			return e, errors.New("contextual attempt receipt exceeds bounds")
		}
	}
	e.Seq = 0
	e.At = time.Time{}
	tx, err := s.beginWrite()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	// SUPPRESSION IS ANSWERED BEFORE ANY ROW IS RETURNED. A source the journal
	// has retired, or a claim it was provenance of and has been forgotten, must
	// never come back authoritative — even to a caller re-sending the same
	// receipt.
	if e.SourceKey != "" {
		suppressed, err := contextualSourceSuppressed(tx, e.Owner, e.SourceKey, e.SourceHash)
		if err != nil {
			return e, err
		}
		if suppressed {
			return e, errors.New("contextual attempt source was suppressed")
		}
	}
	// A DUPLICATE RECEIPT IS IDEMPOTENT. The same source key and content hash is
	// the same observed fact delivered twice; returning it changes nothing and
	// creates no new row.
	if e.SourceKey != "" && e.SourceHash != "" {
		var payload, ts string
		var seq int64
		err = tx.QueryRow(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.SourceKey')=? AND json_extract(payload,'$.SourceHash')=? ORDER BY seq DESC LIMIT 1`, contextualNode(e.Owner), EventContextualAttempt, e.SourceKey, e.SourceHash).Scan(&seq, &ts, &payload)
		if err == nil {
			var existing ContextualAttempt
			if err = decodeAttempt(payload, &existing); err != nil {
				return e, err
			}
			existing.Seq = seq
			if existing.At, err = parseTime(ts); err != nil {
				return e, err
			}
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return e, err
		}
	}
	var duplicate int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.ID')=?`, contextualNode(e.Owner), EventContextualAttempt, e.ID).Scan(&duplicate); err != nil {
		return e, err
	}
	if duplicate != 0 {
		return e, errors.New("contextual attempt id already exists")
	}
	seq, at, err := appendEvent(tx, contextualNode(e.Owner), EventContextualAttempt, e)
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

// ContextualAttemptsApplicable reads at most one bounded newest window and
// returns the attempts still usable under the supplied conditions. It matches
// only the conditions an attempt actually carries — the project — and NEVER the
// source revision, because contract 4 keeps a prior failure retrievable across
// a revision change instead of erasing it. The caller labels currency.
func (s *Store) ContextualAttemptsApplicable(owner string, conditions map[string]string, at time.Time, limit int) ([]ContextualAttempt, error) {
	if strings.TrimSpace(owner) == "" || !ValidOwner(owner) {
		return nil, errors.New("contextual attempts require a valid owner")
	}
	if limit <= 0 || limit > ContextualAttemptLimit {
		limit = ContextualAttemptLimit
	}
	if at.IsZero() {
		at = time.Now()
	}
	var floor int64
	err := s.db.QueryRow(`SELECT COALESCE(MIN(seq),0)-1 FROM (SELECT seq FROM events WHERE node_id=? AND kind=? ORDER BY seq DESC LIMIT ?)`, contextualNode(owner), EventContextualAttempt, ContextualAttemptLimit).Scan(&floor)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND seq>? ORDER BY seq`, contextualNode(owner), EventContextualAttempt, floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := make([]ContextualAttempt, 0)
	for rows.Next() {
		var a ContextualAttempt
		var payload, ts string
		var seq int64
		if err = rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err = decodeAttempt(payload, &a); err != nil {
			return nil, err
		}
		a.Seq = seq
		if a.At, err = parseTime(ts); err != nil {
			return nil, err
		}
		all = append(all, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ContextualAttempt, 0)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		a := all[i]
		ok, err := contextualAttemptUsable(s.db, a, at)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if !contextualAttemptConditions(a, conditions) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// contextualAttemptUsable consults global suppression even when the caller
// reads a narrow window, so truncating a window can never resurrect a retired
// source or a forgotten claim's provenance.
func contextualAttemptUsable(q contextualReader, a ContextualAttempt, at time.Time) (bool, error) {
	if !a.ValidFrom.IsZero() && at.Before(a.ValidFrom) || !a.ValidUntil.IsZero() && !at.Before(a.ValidUntil) {
		return false, nil
	}
	if a.SourceKey == "" {
		return true, nil
	}
	suppressed, err := contextualSourceSuppressed(q, a.Owner, a.SourceKey, a.SourceHash)
	if err != nil || suppressed {
		return false, err
	}
	return true, nil
}

// contextualSourceSuppressed answers whether a source was directly retired, or
// whether it is provenance of a memory that has since been forgotten. The
// provenance join is exactly the evidence row's own: an attempt that shares the
// SourceKey and SourceHash of a forgotten claim's evidence is retired with it,
// while an unrelated attempt in the same project is untouched.
func contextualSourceSuppressed(q contextualReader, owner, key, hash string) (bool, error) {
	var suppressed int
	err := q.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND ((kind=? AND json_extract(payload,'$.SourceKey')=? AND (COALESCE(json_extract(payload,'$.SourceHash'),'')='' OR json_extract(payload,'$.SourceHash')=?)) OR (kind=? AND EXISTS(SELECT 1 FROM events AS source WHERE source.node_id=events.node_id AND source.kind=? AND json_extract(source.payload,'$.MemoryID')=json_extract(events.payload,'$.MemoryID') AND json_extract(source.payload,'$.SourceKey')=? AND json_extract(source.payload,'$.SourceHash')=?)))`, contextualNode(owner), EventContextualSuppression, key, hash, EventContextualMemorySuppression, EventContextualEvidence, key, hash).Scan(&suppressed)
	if err != nil {
		return false, err
	}
	return suppressed != 0, nil
}

// contextualAttemptConditions matches a supplied condition only when the
// attempt carries that key. An absent key is not a mismatch, so a project read
// cannot drop an attempt for naming no revision.
func contextualAttemptConditions(a ContextualAttempt, conditions map[string]string) bool {
	for k, v := range a.Conditions {
		if actual, ok := conditions[k]; !ok || actual != v {
			return false
		}
	}
	return true
}

// ContextualAttemptsRecent reads a bounded newest window of an owner's usable
// attempts WITHOUT condition filtering. It exists for ONE caller — the explicit
// forget path — where the person named the work in words and the attempt that
// recorded it must be reachable even when it carried a condition the caller's
// read set cannot reconstruct. It never widens what a model is shown: the
// retrieval path keeps its conditions, and only the forget path uses this.
func (s *Store) ContextualAttemptsRecent(owner string, at time.Time, limit int) ([]ContextualAttempt, error) {
	if strings.TrimSpace(owner) == "" || !ValidOwner(owner) {
		return nil, errors.New("contextual attempts require a valid owner")
	}
	if limit <= 0 || limit > ContextualAttemptLimit {
		limit = ContextualAttemptLimit
	}
	if at.IsZero() {
		at = time.Now()
	}
	var floor int64
	err := s.db.QueryRow(`SELECT COALESCE(MIN(seq),0)-1 FROM (SELECT seq FROM events WHERE node_id=? AND kind=? ORDER BY seq DESC LIMIT ?)`, contextualNode(owner), EventContextualAttempt, ContextualAttemptLimit).Scan(&floor)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND seq>? ORDER BY seq`, contextualNode(owner), EventContextualAttempt, floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := make([]ContextualAttempt, 0)
	for rows.Next() {
		var a ContextualAttempt
		var payload, ts string
		var seq int64
		if err = rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err = decodeAttempt(payload, &a); err != nil {
			return nil, err
		}
		a.Seq = seq
		if a.At, err = parseTime(ts); err != nil {
			return nil, err
		}
		all = append(all, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ContextualAttempt, 0)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		a := all[i]
		ok, err := contextualAttemptUsable(s.db, a, at)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func decodeAttempt(payload string, a *ContextualAttempt) error {
	if err := json.Unmarshal([]byte(payload), a); err != nil {
		return fmt.Errorf("decode contextual attempt: %w", err)
	}
	return nil
}
