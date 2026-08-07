package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// QuestionUrgency controls when the resident may move a queued question into
// the conversation. Whenever questions remain ambient until a user chooses
// one from a lens such as the TUI dock.
type QuestionUrgency string

const (
	QuestionBlocking          QuestionUrgency = "blocking"
	QuestionNextNaturalMoment QuestionUrgency = "next-natural-moment"
	QuestionWhenever          QuestionUrgency = "whenever"
)

// AgentQuestionStatus is the durable lifecycle of an agent-to-user question.
// Pending is quiet, Asked has entered a thread, and Answered/Expired are
// terminal resolutions.
type AgentQuestionStatus string

const (
	QuestionPending  AgentQuestionStatus = "pending"
	QuestionAsked    AgentQuestionStatus = "asked"
	QuestionAnswered AgentQuestionStatus = "answered"
	QuestionExpired  AgentQuestionStatus = "expired"
)

// AgentQuestion is one materialized agent-to-user question. Seq is the
// question's queue event. OriginCommandSeq is retained for compiler askbacks;
// ordinary resident questions should name either their node or charter.
type AgentQuestion struct {
	Seq              int64
	SessionID        string
	Text             string
	OriginNodeID     string
	OriginCharterID  string
	OriginCommandSeq int64
	Urgency          QuestionUrgency
	Status           AgentQuestionStatus
	Options          []QuestionOption
	CreatedAt        time.Time
	AskedAt          time.Time
	ResolvedAt       time.Time
	ExpiresAt        time.Time
	Resolution       string
	AskedMessageSeq  int64
	AnswerMessageSeq int64
	UpdatedSeq       int64
}

const agentQuestionSchema = `
CREATE TABLE IF NOT EXISTS agent_questions (
    seq                 INTEGER PRIMARY KEY REFERENCES events(seq),
    session_id          TEXT NOT NULL,
    text                TEXT NOT NULL,
    origin_node_id      TEXT NOT NULL DEFAULT '',
    origin_charter_id   TEXT NOT NULL DEFAULT '',
    origin_command_seq  INTEGER NOT NULL DEFAULT 0,
    urgency             TEXT NOT NULL CHECK (urgency IN ('blocking', 'next-natural-moment', 'whenever')),
    status              TEXT NOT NULL CHECK (status IN ('pending', 'asked', 'answered', 'expired')),
    options             JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options)),
    created_at          TEXT NOT NULL,
    asked_at            TEXT,
    resolved_at         TEXT,
    expires_at          TEXT,
    resolution          TEXT NOT NULL DEFAULT '',
    asked_message_seq   INTEGER NOT NULL DEFAULT 0,
    answer_message_seq  INTEGER NOT NULL DEFAULT 0,
    updated_seq         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_questions_session_status_seq
    ON agent_questions (session_id, status, seq);
CREATE INDEX IF NOT EXISTS agent_questions_status_seq
    ON agent_questions (status, seq);
`

type agentQuestionPayload struct {
	SessionID        string           `json:"session_id"`
	Text             string           `json:"text"`
	OriginNodeID     string           `json:"origin_node_id,omitempty"`
	OriginCharterID  string           `json:"origin_charter_id,omitempty"`
	OriginCommandSeq int64            `json:"origin_command_seq,omitempty"`
	Urgency          QuestionUrgency  `json:"urgency"`
	Options          []QuestionOption `json:"options,omitempty"`
	ExpiresAt        time.Time        `json:"expires_at,omitempty"`
}

type agentQuestionSurfacedPayload struct {
	QuestionSeq int64 `json:"question_seq"`
	MessageSeq  int64 `json:"message_seq"`
}

type agentQuestionResolvedPayload struct {
	QuestionSeq int64               `json:"question_seq"`
	Status      AgentQuestionStatus `json:"status"`
	Resolution  string              `json:"resolution,omitempty"`
	MessageSeq  int64               `json:"message_seq,omitempty"`
}

// AskQuestion queues a question without surfacing it. The resident decides
// when policy permits a separate SurfaceQuestion transition.
func (s *Store) AskQuestion(question AgentQuestion) (AgentQuestion, error) {
	question.Text = strings.TrimSpace(question.Text)
	question.SessionID = strings.TrimSpace(question.SessionID)
	question.OriginNodeID = strings.TrimSpace(question.OriginNodeID)
	question.OriginCharterID = strings.TrimSpace(question.OriginCharterID)
	if question.Text == "" {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: empty text", ErrInvalid)
	}
	if len(question.Text) > MaxMessageBytes {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: text is %d bytes (limit %d)",
			ErrInvalid, len(question.Text), MaxMessageBytes)
	}
	if !validQuestionUrgency(question.Urgency) {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: unknown urgency %q", ErrInvalid, question.Urgency)
	}
	if question.OriginNodeID != "" && question.OriginCharterID != "" {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: node and charter origins are mutually exclusive", ErrInvalid)
	}
	if question.OriginCommandSeq < 0 {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: invalid origin command", ErrInvalid)
	}
	options, err := normalizeQuestionOptions(question.Options)
	if err != nil {
		return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
	}
	defer tx.Rollback()

	if question.OriginNodeID != "" {
		if err := requireNode(tx, question.OriginNodeID); err != nil {
			return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
		}
		if question.SessionID == "" {
			_ = tx.QueryRow(`SELECT COALESCE(session_id, '') FROM nodes WHERE id = ?`,
				question.OriginNodeID).Scan(&question.SessionID)
		}
	}
	if question.OriginCharterID != "" {
		if err := requireCharter(tx, question.OriginCharterID); err != nil {
			return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
		}
		if question.SessionID == "" {
			_ = tx.QueryRow(`SELECT session_id FROM charters WHERE id = ?`,
				question.OriginCharterID).Scan(&question.SessionID)
		}
	}
	if question.OriginCommandSeq != 0 {
		var commandSession string
		if err := tx.QueryRow(`SELECT session_id FROM commands WHERE seq = ?`,
			question.OriginCommandSeq).Scan(&commandSession); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return AgentQuestion{}, fmt.Errorf("ask question: %w: origin command %d", ErrNotFound, question.OriginCommandSeq)
			}
			return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
		}
		if question.SessionID == "" {
			question.SessionID = commandSession
		} else if commandSession != "" && question.SessionID != commandSession {
			return AgentQuestion{}, fmt.Errorf("ask question: %w: origin command belongs to another session", ErrInvalid)
		}
	}
	if question.SessionID == "" {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: empty session", ErrInvalid)
	}

	payload := agentQuestionPayload{
		SessionID: question.SessionID, Text: question.Text,
		OriginNodeID: question.OriginNodeID, OriginCharterID: question.OriginCharterID,
		OriginCommandSeq: question.OriginCommandSeq, Urgency: question.Urgency,
		Options: options, ExpiresAt: question.ExpiresAt,
	}
	anchor := question.OriginNodeID
	if anchor == "" {
		anchor = question.OriginCharterID
	}
	seq, at, err := appendEvent(tx, anchor, EventAgentQuestionQueued, payload)
	if err != nil {
		return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
	}
	if err := applyAgentQuestionView(tx, payload, seq, at); err != nil {
		return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AgentQuestion{}, fmt.Errorf("ask question: %w", err)
	}
	question.Seq = seq
	question.Status = QuestionPending
	question.Options = options
	question.CreatedAt = at
	question.UpdatedSeq = seq
	return question, nil
}

// PendingQuestions returns quiet, unsurfaced questions oldest first. An empty
// sessionID returns all sessions for resident policy work.
func (s *Store) PendingQuestions(sessionID string, limit int) ([]AgentQuestion, error) {
	where := `status = ?`
	args := []any{QuestionPending}
	if sessionID != "" {
		where += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	return s.queryAgentQuestions(where+` ORDER BY seq LIMIT ?`, append(args, questionLimit(limit)))
}

// UnresolvedQuestions includes both quiet and already-surfaced questions. It
// lets the resident expire stale work without conflating expiry with display.
func (s *Store) UnresolvedQuestions(limit int) ([]AgentQuestion, error) {
	return s.queryAgentQuestions(`status IN (?, ?) ORDER BY seq LIMIT ?`,
		[]any{QuestionPending, QuestionAsked, questionLimit(limit)})
}

// AgentQuestionBySeq returns one materialized question.
func (s *Store) AgentQuestionBySeq(seq int64) (AgentQuestion, bool, error) {
	questions, err := s.queryAgentQuestions(`seq = ?`, []any{seq})
	if err != nil {
		return AgentQuestion{}, false, err
	}
	if len(questions) == 0 {
		return AgentQuestion{}, false, nil
	}
	return questions[0], true, nil
}

// SurfaceQuestion moves one pending question into its thread. The message and
// status transition are separate journal events committed atomically.
func (s *Store) SurfaceQuestion(seq int64) (Message, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	defer tx.Rollback()

	question, found, err := queryAgentQuestionTx(tx, seq)
	if err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	if !found {
		return Message{}, fmt.Errorf("surface question: %w: no question at seq %d", ErrNotFound, seq)
	}
	if question.Status != QuestionPending {
		return Message{}, fmt.Errorf("surface question: %w: question %d is already %s", ErrInvalid, seq, question.Status)
	}
	payload := messagePayload{
		SessionID: question.SessionID, Role: RoleAgent, Body: question.Text,
		NodeID: question.OriginNodeID, CommandSeq: question.OriginCommandSeq,
		QuestionSeq: question.Seq, Options: question.Options,
	}
	messageSeq, messageAt, err := appendEvent(tx, question.OriginNodeID, EventMessagePosted, payload)
	if err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	if err := applyMessageView(tx, payload, messageSeq, messageAt); err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	surfaced := agentQuestionSurfacedPayload{QuestionSeq: seq, MessageSeq: messageSeq}
	eventSeq, at, err := appendEvent(tx, question.OriginNodeID, EventAgentQuestionSurfaced, surfaced)
	if err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	if err := applyAgentQuestionSurfaced(tx, surfaced, eventSeq, at); err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	return Message{
		Seq: messageSeq, Time: messageAt, SessionID: question.SessionID,
		Role: RoleAgent, Body: question.Text, NodeID: question.OriginNodeID,
		CommandSeq: question.OriginCommandSeq, QuestionSeq: question.Seq,
		Options: append([]QuestionOption(nil), question.Options...),
	}, nil
}

// ResolveQuestion terminally settles a question exactly once. Answered
// questions must have been surfaced; expiry may retire either a pending or
// asked question. messageSeq optionally points at the user reply.
func (s *Store) ResolveQuestion(seq int64, status AgentQuestionStatus, resolution string, messageSeq ...int64) error {
	if status != QuestionAnswered && status != QuestionExpired {
		return fmt.Errorf("resolve question: %w: status %q is not a resolution", ErrInvalid, status)
	}
	if len(messageSeq) > 1 || (len(messageSeq) == 1 && messageSeq[0] < 0) {
		return fmt.Errorf("resolve question: %w: invalid answer message", ErrInvalid)
	}
	answerSeq := int64(0)
	if len(messageSeq) == 1 {
		answerSeq = messageSeq[0]
	}
	resolution = bounded(strings.TrimSpace(resolution), MaxDigestBytes)
	if resolution == "" {
		return fmt.Errorf("resolve question: %w: empty resolution", ErrInvalid)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	defer tx.Rollback()
	question, found, err := queryAgentQuestionTx(tx, seq)
	if err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	if !found {
		return fmt.Errorf("resolve question: %w: no question at seq %d", ErrNotFound, seq)
	}
	if question.Status != QuestionPending && question.Status != QuestionAsked {
		return fmt.Errorf("resolve question: %w: question %d is already %s", ErrInvalid, seq, question.Status)
	}
	if status == QuestionAnswered && question.Status != QuestionAsked {
		return fmt.Errorf("resolve question: %w: question %d has not been asked", ErrInvalid, seq)
	}
	if status == QuestionAnswered && answerSeq != 0 {
		var sessionID string
		var role Role
		if err := tx.QueryRow(`SELECT session_id, role FROM messages WHERE seq = ?`, answerSeq).Scan(&sessionID, &role); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("resolve question: %w: answer message %d", ErrNotFound, answerSeq)
			}
			return fmt.Errorf("resolve question: %w", err)
		}
		if role != RoleUser || sessionID != question.SessionID {
			return fmt.Errorf("resolve question: %w: answer message does not belong to the question session", ErrInvalid)
		}
	}
	payload := agentQuestionResolvedPayload{
		QuestionSeq: seq, Status: status, Resolution: resolution, MessageSeq: answerSeq,
	}
	eventSeq, at, err := appendEvent(tx, question.OriginNodeID, EventAgentQuestionResolved, payload)
	if err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	if err := applyAgentQuestionResolution(tx, payload, eventSeq, at); err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	return nil
}

// QuestionForAnswer matches an explicit reference first, otherwise the most
// recent surfaced question with no intervening user turn.
func (s *Store) QuestionForAnswer(sessionID string, beforeSeq, referenceSeq int64) (AgentQuestion, bool, error) {
	if beforeSeq <= 0 {
		return AgentQuestion{}, false, nil
	}
	if referenceSeq != 0 {
		questions, err := s.queryAgentQuestions(`seq = ? AND session_id = ? AND status = ? AND asked_message_seq < ?`,
			[]any{referenceSeq, sessionID, QuestionAsked, beforeSeq})
		if err != nil || len(questions) == 0 {
			return AgentQuestion{}, false, err
		}
		return questions[0], true, nil
	}
	questions, err := s.queryAgentQuestions(`session_id = ? AND status = ? AND asked_message_seq < ?
		AND NOT EXISTS (
			SELECT 1 FROM messages u WHERE u.session_id = agent_questions.session_id
			AND u.role = ? AND u.seq > agent_questions.asked_message_seq AND u.seq < ?
		) ORDER BY asked_message_seq DESC LIMIT 1`,
		[]any{sessionID, QuestionAsked, beforeSeq, RoleUser, beforeSeq})
	if err != nil || len(questions) == 0 {
		return AgentQuestion{}, false, err
	}
	return questions[0], true, nil
}

func applyAgentQuestionView(tx *sql.Tx, payload agentQuestionPayload, seq int64, at time.Time) error {
	options, err := encodeQuestionOptions(payload.Options)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO agent_questions (
		seq, session_id, text, origin_node_id, origin_charter_id, origin_command_seq,
		urgency, status, options, created_at, expires_at, updated_seq
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, payload.SessionID, payload.Text, payload.OriginNodeID, payload.OriginCharterID,
		payload.OriginCommandSeq, payload.Urgency, QuestionPending, options, formatTime(at),
		nullableQuestionTime(payload.ExpiresAt), seq)
	return err
}

func applyAgentQuestionSurfaced(tx *sql.Tx, payload agentQuestionSurfacedPayload, eventSeq int64, at time.Time) error {
	result, err := tx.Exec(`UPDATE agent_questions SET status = ?, asked_at = ?,
		asked_message_seq = ?, updated_seq = ? WHERE seq = ? AND status = ?`,
		QuestionAsked, formatTime(at), payload.MessageSeq, eventSeq, payload.QuestionSeq, QuestionPending)
	if err != nil {
		return err
	}
	return requireOneQuestionChange(result, "surface", payload.QuestionSeq)
}

func applyAgentQuestionResolution(tx *sql.Tx, payload agentQuestionResolvedPayload, eventSeq int64, at time.Time) error {
	result, err := tx.Exec(`UPDATE agent_questions SET status = ?, resolved_at = ?,
		resolution = ?, answer_message_seq = ?, updated_seq = ?
		WHERE seq = ? AND status IN (?, ?)`,
		payload.Status, formatTime(at), payload.Resolution, payload.MessageSeq, eventSeq,
		payload.QuestionSeq, QuestionPending, QuestionAsked)
	if err != nil {
		return err
	}
	return requireOneQuestionChange(result, "resolve", payload.QuestionSeq)
}

func requireOneQuestionChange(result sql.Result, action string, seq int64) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%s targets missing or settled question %d", action, seq)
	}
	return nil
}

func (s *Store) queryAgentQuestions(where string, args []any) ([]AgentQuestion, error) {
	rows, err := s.db.Query(`SELECT seq, session_id, text, origin_node_id, origin_charter_id,
		origin_command_seq, urgency, status, options, created_at, asked_at, resolved_at,
		expires_at, resolution, asked_message_seq, answer_message_seq, updated_seq
		FROM agent_questions WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent questions: %w", err)
	}
	defer rows.Close()
	questions := make([]AgentQuestion, 0)
	for rows.Next() {
		question, err := scanAgentQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("list agent questions: %w", err)
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent questions: %w", err)
	}
	return questions, nil
}

type questionScanner interface{ Scan(...any) error }

func scanAgentQuestion(scanner questionScanner) (AgentQuestion, error) {
	var question AgentQuestion
	var options, created string
	var asked, resolved, expires sql.NullString
	if err := scanner.Scan(&question.Seq, &question.SessionID, &question.Text,
		&question.OriginNodeID, &question.OriginCharterID, &question.OriginCommandSeq,
		&question.Urgency, &question.Status, &options, &created, &asked, &resolved,
		&expires, &question.Resolution, &question.AskedMessageSeq,
		&question.AnswerMessageSeq, &question.UpdatedSeq); err != nil {
		return AgentQuestion{}, err
	}
	if err := decodeQuestionOptions(options, &question.Options); err != nil {
		return AgentQuestion{}, err
	}
	var err error
	if question.CreatedAt, err = parseTime(created); err != nil {
		return AgentQuestion{}, err
	}
	if question.AskedAt, err = parseTime(asked.String); err != nil {
		return AgentQuestion{}, err
	}
	if question.ResolvedAt, err = parseTime(resolved.String); err != nil {
		return AgentQuestion{}, err
	}
	if question.ExpiresAt, err = parseTime(expires.String); err != nil {
		return AgentQuestion{}, err
	}
	return question, nil
}

func queryAgentQuestionTx(tx *sql.Tx, seq int64) (AgentQuestion, bool, error) {
	row := tx.QueryRow(`SELECT seq, session_id, text, origin_node_id, origin_charter_id,
		origin_command_seq, urgency, status, options, created_at, asked_at, resolved_at,
		expires_at, resolution, asked_message_seq, answer_message_seq, updated_seq
		FROM agent_questions WHERE seq = ?`, seq)
	question, err := scanAgentQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentQuestion{}, false, nil
	}
	return question, err == nil, err
}

func validQuestionUrgency(urgency QuestionUrgency) bool {
	switch urgency {
	case QuestionBlocking, QuestionNextNaturalMoment, QuestionWhenever:
		return true
	default:
		return false
	}
}

func questionLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func encodeQuestionOptions(options []QuestionOption) (string, error) {
	encoded, err := json.Marshal(options)
	return string(encoded), err
}

func nullableQuestionTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}
