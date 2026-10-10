package store

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// PracticeGroup is the scheduler marker for disposable, lowest-priority
// curriculum work. It is deliberately not an organizational group.
//
// NOTHING ADMITS PRACTICE WORK ANY MORE. The practice loop that spliced these
// jobs went with the rest of the v1 resident's scheduler (see
// docs/design/automations/DESIGN.md), but a store written before that still
// holds practice jobs, and every reader that keeps them out of the person's
// own work — the runner's priority, the fold, the history and territory
// walks — has to go on recognising them by this mark.
const PracticeGroup = "practice"

// The question lifecycle's journal spellings. Practice rounds were the only
// writer of the last two, and that writer is gone; they stay because a journal
// that already holds them must replay to the same question status it read as
// when it was written (see replayEvent and the facts migration).
const (
	EventQuestionStatusChanged     EventKind = "question_status_changed"
	EventQuestionPracticeStarted   EventKind = "question_practice_started"
	EventQuestionPracticeCompleted EventKind = "question_practice_completed"
)

type questionStatusPayload struct {
	QuestionSeq int64  `json:"question_seq"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// questionPracticeStarted is the durable link between one question and the
// self-origin subtree that was admitted to exercise it. It is read only on
// replay now.
type questionPracticeStarted struct {
	QuestionSeq      int64   `json:"question_seq"`
	JobID            string  `json:"job_id"`
	BaselineSurprise float64 `json:"baseline_surprise"`
	ExpectedTokens   int     `json:"expected_tokens"`
}

// questionPracticeCompleted is one measured round's landing, read only on
// replay now.
type questionPracticeCompleted struct {
	QuestionSeq    int64   `json:"question_seq"`
	JobID          string  `json:"job_id"`
	ResultSurprise float64 `json:"result_surprise"`
	Reduced        bool    `json:"reduced"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason"`
}

// RecordQuestion journals one open knowledge gap. A scope has at most one
// question over its lifetime: resolved and retired gaps are hysteresis, not an
// invitation for a periodic scan to manufacture the same curriculum again.
func (s *Store) RecordQuestion(nodeID, scope, body string) (Fact, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fact{}, fmt.Errorf("record question: %w: empty gap", ErrInvalid)
	}
	if strings.ContainsAny(body, "\r\n") {
		return Fact{}, fmt.Errorf("record question: %w: gap must be one line", ErrInvalid)
	}
	scope = normalizeScope(scope)
	resolved, err := resolveScope(s.db, scope)
	if err != nil {
		return Fact{}, fmt.Errorf("record question: %w", err)
	}
	existing, err := s.factsWhere(`kind = ? AND scope = ? ORDER BY seq DESC LIMIT 1`, FactQuestion, resolved)
	if err != nil {
		return Fact{}, fmt.Errorf("record question: %w", err)
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	return s.recordFact(FactWriterOther, nodeID, resolved, FactQuestion, body, nil, 0, QuestionOpen, "", "", false)
}

func applyQuestionStatus(tx *sql.Tx, payload questionStatusPayload, seq int64) error {
	if !validFactStatusForKind(FactQuestion, payload.Status) {
		return fmt.Errorf("invalid question status %q", payload.Status)
	}
	result, err := tx.Exec(`UPDATE facts SET status=?, status_note=?, status_seq=?
		WHERE seq=? AND kind=?`, payload.Status, payload.Reason, seq,
		payload.QuestionSeq, FactQuestion)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("question %d is missing", payload.QuestionSeq)
	}
	return nil
}

func applyQuestionPracticeStarted(tx *sql.Tx, payload questionPracticeStarted, seq int64) error {
	if payload.QuestionSeq <= 0 || strings.TrimSpace(payload.JobID) == "" ||
		payload.BaselineSurprise <= 0 || math.IsNaN(payload.BaselineSurprise) ||
		math.IsInf(payload.BaselineSurprise, 0) || payload.ExpectedTokens < 0 {
		return fmt.Errorf("invalid question practice start")
	}
	var status string
	if err := tx.QueryRow(`SELECT status FROM facts WHERE seq=? AND kind=?`,
		payload.QuestionSeq, FactQuestion).Scan(&status); err != nil {
		return err
	}
	if status != QuestionOpen {
		return fmt.Errorf("question %d is %s, not open", payload.QuestionSeq, status)
	}
	if err := requireNode(tx, payload.JobID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO question_practices
		(start_seq, question_seq, job_id, baseline_surprise, expected_tokens)
		VALUES (?, ?, ?, ?, ?)`, seq, payload.QuestionSeq, payload.JobID,
		payload.BaselineSurprise, payload.ExpectedTokens); err != nil {
		return err
	}
	payloadStatus := questionStatusPayload{QuestionSeq: payload.QuestionSeq,
		Status: QuestionPracticing, Reason: "practice job " + payload.JobID}
	return applyQuestionStatus(tx, payloadStatus, seq)
}

func applyQuestionPracticeCompleted(tx *sql.Tx, payload questionPracticeCompleted, seq int64) error {
	if !validFactStatusForKind(FactQuestion, payload.Status) ||
		payload.Status == QuestionPracticing || payload.ResultSurprise < 0 {
		return fmt.Errorf("invalid question practice completion")
	}
	result, err := tx.Exec(`UPDATE question_practices SET result_surprise=?, reduced=?,
		completion_seq=?, reason=? WHERE question_seq=? AND job_id=? AND completion_seq IS NULL`,
		payload.ResultSurprise, payload.Reduced, seq, payload.Reason,
		payload.QuestionSeq, payload.JobID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("practice job %q is missing or complete", payload.JobID)
	}
	return applyQuestionStatus(tx, questionStatusPayload{QuestionSeq: payload.QuestionSeq,
		Status: payload.Status, Reason: payload.Reason}, seq)
}

// UserIdle is the cheap reconciler gate: one indexed materialized-view check
// for live user work and one journal timestamp lookup for the quiet period.
func (s *Store) UserIdle(now time.Time, quietFor time.Duration) (bool, error) {
	if quietFor < 0 {
		return false, fmt.Errorf("user idle: %w: negative quiet period", ErrInvalid)
	}
	var inFlight bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM nodes
		WHERE origin=? AND folded=0 AND status IN (?, ?, ?))`, OriginUser,
		Pending, Claimed, Running).Scan(&inFlight); err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	if inFlight {
		return false, nil
	}
	if quietFor == 0 {
		return true, nil
	}
	var latest sql.NullString
	if err := s.db.QueryRow(`SELECT MAX(ts) FROM events WHERE kind=?
		AND json_extract(payload, '$.provenance.origin')=?`,
		EventSubtreeSpliced, OriginUser).Scan(&latest); err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	if !latest.Valid {
		return true, nil
	}
	at, err := parseTime(latest.String)
	if err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	return !now.Before(at.Add(quietFor)), nil
}
