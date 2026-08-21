package store

import (
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

// QuestionClass separates a question that needs a human's consent from one
// that merely informs — the axis that will later gate which questions an
// orchestrator may answer on its own. The conservative default is law:
// nothing constructs a question this package will read back as
// QuestionInformational unless a producer explicitly says so. An unlabeled
// question is a consent question, everywhere — the zero value, the schema
// default, and every read path agree on that, so silence never widens
// autonomy.
type QuestionClass string

const (
	QuestionConsent       QuestionClass = "consent"
	QuestionInformational QuestionClass = "informational"
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
	Class            QuestionClass
	Status           AgentQuestionStatus
	Options          []QuestionOption
	Category         QuestionCategory
	DefaultAnswer    string
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
    class               TEXT NOT NULL DEFAULT 'consent' CHECK (class IN ('consent', 'informational')),
    status              TEXT NOT NULL CHECK (status IN ('pending', 'asked', 'answered', 'expired')),
	options             JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options)),
	category            TEXT NOT NULL DEFAULT 'generic',
	default_answer      TEXT NOT NULL DEFAULT '',
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

func migrateAgentQuestionSchema(db *sql.DB) error {
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{"category", `ALTER TABLE agent_questions ADD COLUMN category TEXT NOT NULL DEFAULT 'generic'`},
		{"default_answer", `ALTER TABLE agent_questions ADD COLUMN default_answer TEXT NOT NULL DEFAULT ''`},
		// Conservative default at the schema layer: a row from before this column
		// existed reads back as 'consent', the same as an unlabeled new row — a
		// legacy question never silently becomes eligible for informational
		// autonomy just because it predates the axis.
		{"class", `ALTER TABLE agent_questions ADD COLUMN class TEXT NOT NULL DEFAULT 'consent' CHECK (class IN ('consent', 'informational'))`},
	} {
		found, err := tableHasColumn(db, "agent_questions", column.name)
		if err != nil {
			return err
		}
		if !found {
			if _, err := db.Exec(column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

type agentQuestionPayload struct {
	SessionID        string           `json:"session_id"`
	Text             string           `json:"text"`
	OriginNodeID     string           `json:"origin_node_id,omitempty"`
	OriginCharterID  string           `json:"origin_charter_id,omitempty"`
	OriginCommandSeq int64            `json:"origin_command_seq,omitempty"`
	Urgency          QuestionUrgency  `json:"urgency"`
	Class            QuestionClass    `json:"class"`
	Options          []QuestionOption `json:"options,omitempty"`
	Category         QuestionCategory `json:"category,omitempty"`
	DefaultAnswer    string           `json:"default,omitempty"`
	ExpiresAt        time.Time        `json:"expires_at,omitempty"`
}

type agentQuestionSurfacedPayload struct {
	QuestionSeq int64 `json:"question_seq"`
	MessageSeq  int64 `json:"message_seq"`
	// SessionID re-homes a question in the conversation that can now answer it.
	// Empty — every event written before this existed, and every ordinary
	// surfacing — leaves the question where it was. It is set only when an
	// unanswered blocking question is carried into a live session because the
	// one that asked it is gone: the answer lookup is session-filtered, so
	// re-posting the words without moving the question would show the user a
	// question their reply could not reach.
	SessionID string `json:"session_id,omitempty"`
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
	question.DefaultAnswer = strings.TrimSpace(question.DefaultAnswer)
	if question.Category == "" {
		question.Category = QuestionCategoryGeneric
	}
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
	// Conservative default at the Go layer: a producer that says nothing about
	// class gets the class that always escalates to a human. This runs before
	// validation so an unlabeled question can never be rejected as "unknown
	// class" — silence is a valid, and the safest, answer.
	if question.Class == "" {
		question.Class = QuestionConsent
	}
	if !validQuestionClass(question.Class) {
		return AgentQuestion{}, fmt.Errorf("ask question: %w: unknown class %q", ErrInvalid, question.Class)
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

	tx, err := s.beginWrite()
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
		OriginCommandSeq: question.OriginCommandSeq, Urgency: question.Urgency, Class: question.Class,
		Options: options, ExpiresAt: question.ExpiresAt,
		Category: question.Category, DefaultAnswer: question.DefaultAnswer,
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
	question.Category = payload.Category
	question.DefaultAnswer = payload.DefaultAnswer
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
		where += ` AND (session_id = ? OR session_id = '')`
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

// OpenQuestions returns every question in one session the user still owes an
// answer to, quiet and already-surfaced alike, oldest first.
//
// PendingQuestions answers "what has nobody been shown yet", which is a
// surfacing decision. This answers "what is still waiting on you", which is the
// only honest basis for a lens that lists open questions: a blocking question
// crosses into the thread the instant it is asked, so anything built on the
// unsurfaced set is structurally blind to exactly the questions a running job
// is stuck behind.
func (s *Store) OpenQuestions(sessionID string, limit int) ([]AgentQuestion, error) {
	where := `status IN (?, ?)`
	args := []any{QuestionPending, QuestionAsked}
	if sessionID != "" {
		where += ` AND (session_id = ? OR session_id = '')`
		args = append(args, sessionID)
	}
	return s.queryAgentQuestions(where+` ORDER BY seq LIMIT ?`, append(args, questionLimit(limit)))
}

// QuestionsForNode returns every question one node has asked, newest first,
// whatever became of each. UnresolvedQuestions answers "what is outstanding",
// which is a live queue; this answers "what has this node already asked and
// what was said back", which is a durable record. A stop that must be put to
// the user exactly once — and that means something different once they have
// answered it — can only be built on the second: a question drops out of the
// unresolved set the moment it is answered, and a marker that disappears when
// the answer arrives is a question that gets asked forever.
func (s *Store) QuestionsForNode(nodeID string, limit int) ([]AgentQuestion, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, nil
	}
	return s.queryAgentQuestions(`origin_node_id = ? ORDER BY seq DESC LIMIT ?`,
		[]any{nodeID, questionLimit(limit)})
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
	return s.surfaceQuestion(seq, "")
}

// SurfaceQuestionForSession surfaces a neutral queued question into the live
// session that chose it. Session-bound questions keep their original session.
func (s *Store) SurfaceQuestionForSession(seq int64, sessionID string) (Message, error) {
	return s.surfaceQuestion(seq, strings.TrimSpace(sessionID))
}

// ResurfaceQuestion carries an unanswered question into a live session and
// re-posts it there. It is the recovery path for a blocking question whose
// original session is gone: the request behind it was never dropped by anyone's
// decision, it simply stopped being visible, and a question nobody can see is a
// request that was silently abandoned.
//
// Unlike SurfaceQuestion this moves the question's own session, because
// QuestionForAnswer is session-filtered — showing the words without moving the
// question would render a prompt the user's reply could not reach.
func (s *Store) ResurfaceQuestion(seq int64, sessionID string) (Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Message{}, fmt.Errorf("resurface question: %w: empty session", ErrInvalid)
	}
	return s.surfaceQuestionInto(seq, sessionID, true)
}

func (s *Store) surfaceQuestion(seq int64, neutralSessionID string) (Message, error) {
	return s.surfaceQuestionInto(seq, neutralSessionID, false)
}

func (s *Store) surfaceQuestionInto(seq int64, neutralSessionID string, rehome bool) (Message, error) {
	tx, err := s.beginWrite()
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
	if question.Status != QuestionPending && !(rehome && question.Status == QuestionAsked) {
		return Message{}, fmt.Errorf("surface question: %w: question %d is already %s", ErrInvalid, seq, question.Status)
	}
	messageSessionID := question.SessionID
	if messageSessionID == "" || rehome {
		messageSessionID = neutralSessionID
	}
	// The blocks are attached HERE rather than by each producer, because the
	// smuggling this ends (Part 2.11, 13.3 bug 1) was never one caller's habit:
	// every durable question in the product reaches a transcript through this
	// one function, so this is the one place that can make all of them typed.
	// Nothing is invented — PartsForQuestion says only what the row and the body
	// it already holds say — and the body is left exactly as written, because
	// the chat that has never heard of parts reads the body and only the body.
	parts, err := normalizeMessageParts(PartsForQuestion(question))
	if err != nil {
		// A question that cannot describe itself still has to reach the person.
		// Dropping the blocks costs the new renderer its structure; dropping the
		// question costs them the request.
		parts = nil
	}
	payload := messagePayload{
		SessionID: messageSessionID, Role: RoleAgent, Body: question.Text,
		NodeID: question.OriginNodeID, CommandSeq: question.OriginCommandSeq,
		QuestionSeq: question.Seq, Options: question.Options, Parts: parts,
	}
	messageSeq, messageAt, err := appendEvent(tx, question.OriginNodeID, EventMessagePosted, payload)
	if err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	if err := applyMessageView(tx, payload, messageSeq, messageAt); err != nil {
		return Message{}, fmt.Errorf("surface question: %w", err)
	}
	surfaced := agentQuestionSurfacedPayload{QuestionSeq: seq, MessageSeq: messageSeq}
	if rehome {
		surfaced.SessionID = messageSessionID
	}
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
		Seq: messageSeq, Time: messageAt, SessionID: messageSessionID,
		Role: RoleAgent, Body: question.Text, NodeID: question.OriginNodeID,
		CommandSeq: question.OriginCommandSeq, QuestionSeq: question.Seq,
		Options: append([]QuestionOption(nil), question.Options...),
		Parts:   parts,
	}, nil
}

// ResolveQuestion terminally settles a question exactly once. Answered
// questions must have been surfaced or already linked to an agent message;
// expiry may retire either a pending or asked question. messageSeq optionally
// points at the user reply.
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

	tx, err := s.beginWrite()
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
	if status == QuestionAnswered {
		if err := validateQuestionAnswerTx(tx, question, answerSeq); err != nil {
			return fmt.Errorf("resolve question: %w", err)
		}
	}
	payload := agentQuestionResolvedPayload{
		QuestionSeq: seq, Status: status, Resolution: resolution, MessageSeq: answerSeq,
	}
	eventSeq, at, err := appendEvent(tx, agentQuestionAnchor(question), EventAgentQuestionResolved, payload)
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

// ResolveQuestionWithCommand atomically settles one selectable question and
// enqueues its continuation command. The conditional no-op update takes the
// SQLite write lock before reading the question, so racing message and dock
// answers serialize: exactly one transaction journals both events and later
// answers observe an already-settled question without creating another
// command.
func (s *Store) ResolveQuestionWithCommand(seq int64, resolution string, answerSeq int64, command Command) (Command, bool, error) {
	resolution = bounded(strings.TrimSpace(resolution), MaxDigestBytes)
	if resolution == "" {
		return Command{}, false, fmt.Errorf("resolve question with command: %w: empty resolution", ErrInvalid)
	}
	if answerSeq < 0 {
		return Command{}, false, fmt.Errorf("resolve question with command: %w: invalid answer message", ErrInvalid)
	}
	if err := validateCommandRequest(command); err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}

	tx, err := s.beginWrite()
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`UPDATE agent_questions SET updated_seq = updated_seq
		WHERE seq = ? AND status IN (?, ?)`, seq, QuestionPending, QuestionAsked)
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	if changed == 0 {
		var status AgentQuestionStatus
		if err := tx.QueryRow(`SELECT status FROM agent_questions WHERE seq = ?`, seq).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Command{}, false, fmt.Errorf("resolve question with command: %w: no question at seq %d", ErrNotFound, seq)
			}
			return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
		}
		if status == QuestionAnswered {
			return Command{}, false, nil
		}
		return Command{}, false, fmt.Errorf("resolve question with command: %w: question %d is already %s", ErrInvalid, seq, status)
	}

	question, found, err := queryAgentQuestionTx(tx, seq)
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	if !found {
		return Command{}, false, fmt.Errorf("resolve question with command: %w: no question at seq %d", ErrNotFound, seq)
	}
	if err := validateQuestionAnswerTx(tx, question, answerSeq); err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	payload := agentQuestionResolvedPayload{
		QuestionSeq: seq, Status: QuestionAnswered, Resolution: resolution, MessageSeq: answerSeq,
	}
	eventSeq, at, err := appendEvent(tx, agentQuestionAnchor(question), EventAgentQuestionResolved, payload)
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	if err := applyAgentQuestionResolution(tx, payload, eventSeq, at); err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	command, err = requestCommandTx(tx, command)
	if err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Command{}, false, fmt.Errorf("resolve question with command: %w", err)
	}
	return command, true, nil
}

// QuestionForAnswer matches an explicit reference first, otherwise the most
// recent surfaced question with no intervening user turn.
func (s *Store) QuestionForAnswer(sessionID string, beforeSeq, referenceSeq int64) (AgentQuestion, bool, error) {
	if beforeSeq <= 0 {
		return AgentQuestion{}, false, nil
	}
	if referenceSeq != 0 {
		questions, err := s.queryAgentQuestions(`seq = ? AND (session_id = ? OR session_id = '')
			AND seq < ? AND EXISTS (
				SELECT 1 FROM messages m WHERE m.question_seq = agent_questions.seq
				AND m.role = ? AND m.seq < ?
			)`, []any{referenceSeq, sessionID, beforeSeq, RoleAgent, beforeSeq})
		if err != nil || len(questions) == 0 {
			return AgentQuestion{}, false, err
		}
		return questions[0], true, nil
	}
	questions, err := s.QuestionsForAnswer(sessionID, beforeSeq)
	if err != nil || len(questions) == 0 {
		return AgentQuestion{}, false, err
	}
	return questions[0], true, nil
}

// QuestionsForAnswer returns every surfaced question this reply could plausibly
// be answering, newest first. QuestionForAnswer takes the first of these and
// that is right whenever there is only one; when there are several, "newest
// wins" is a coin toss dressed as a rule, and the caller needs to see the whole
// set before it silently spends one of them.
func (s *Store) QuestionsForAnswer(sessionID string, beforeSeq int64) ([]AgentQuestion, error) {
	if beforeSeq <= 0 {
		return nil, nil
	}
	return s.queryAgentQuestions(`(session_id = ? OR session_id = '') AND status = ? AND asked_message_seq < ?
		AND NOT EXISTS (
			SELECT 1 FROM messages u WHERE u.session_id = ?
			AND u.role = ? AND u.seq > agent_questions.asked_message_seq AND u.seq < ?
		) ORDER BY asked_message_seq DESC`,
		[]any{sessionID, QuestionAsked, beforeSeq, sessionID, RoleUser, beforeSeq})
}

func validateQuestionAnswerTx(tx *sql.Tx, question AgentQuestion, answerSeq int64) error {
	if question.Status == QuestionPending {
		var linked bool
		if err := tx.QueryRow(`SELECT EXISTS(
			SELECT 1 FROM messages WHERE question_seq = ? AND role = ?
		)`, question.Seq, RoleAgent).Scan(&linked); err != nil {
			return err
		}
		if !linked {
			return fmt.Errorf("%w: question %d has not been asked", ErrInvalid, question.Seq)
		}
	} else if question.Status != QuestionAsked {
		return fmt.Errorf("%w: question %d is already %s", ErrInvalid, question.Seq, question.Status)
	}
	if answerSeq == 0 {
		return nil
	}
	var sessionID string
	var role Role
	if err := tx.QueryRow(`SELECT session_id, role FROM messages WHERE seq = ?`, answerSeq).Scan(&sessionID, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: answer message %d", ErrNotFound, answerSeq)
		}
		return err
	}
	if role != RoleUser || (question.SessionID != "" && sessionID != question.SessionID) {
		return fmt.Errorf("%w: answer message does not belong to the question session", ErrInvalid)
	}
	return nil
}

func agentQuestionAnchor(question AgentQuestion) string {
	if question.OriginNodeID != "" {
		return question.OriginNodeID
	}
	return question.OriginCharterID
}

func applyAgentQuestionView(tx *sql.Tx, payload agentQuestionPayload, seq int64, at time.Time) error {
	options, err := encodeQuestionOptions(payload.Options)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO agent_questions (
		seq, session_id, text, origin_node_id, origin_charter_id, origin_command_seq,
		urgency, class, status, options, category, default_answer, created_at, expires_at, updated_seq
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, payload.SessionID, payload.Text, payload.OriginNodeID, payload.OriginCharterID,
		payload.OriginCommandSeq, payload.Urgency, defaultQuestionClass(payload.Class), QuestionPending, options,
		defaultQuestionCategory(payload.Category), strings.TrimSpace(payload.DefaultAnswer), formatTime(at),
		nullableQuestionTime(payload.ExpiresAt), seq)
	return err
}

func applyAgentQuestionSurfaced(tx *sql.Tx, payload agentQuestionSurfacedPayload, eventSeq int64, at time.Time) error {
	if session := strings.TrimSpace(payload.SessionID); session != "" {
		result, err := tx.Exec(`UPDATE agent_questions SET status = ?, asked_at = ?,
			asked_message_seq = ?, session_id = ?, updated_seq = ?
			WHERE seq = ? AND status IN (?, ?)`,
			QuestionAsked, formatTime(at), payload.MessageSeq, session, eventSeq,
			payload.QuestionSeq, QuestionPending, QuestionAsked)
		if err != nil {
			return err
		}
		return requireOneQuestionChange(result, "resurface", payload.QuestionSeq)
	}
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
		origin_command_seq, urgency, class, status, options, category, default_answer, created_at, asked_at, resolved_at,
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
		&question.Urgency, &question.Class, &question.Status, &options, &question.Category, &question.DefaultAnswer, &created, &asked, &resolved,
		&expires, &question.Resolution, &question.AskedMessageSeq,
		&question.AnswerMessageSeq, &question.UpdatedSeq); err != nil {
		return AgentQuestion{}, err
	}
	// Read-path enforcement of the conservative default: whatever the column
	// actually holds, an unrecognized or empty class is never handed back as
	// anything but consent. This is belt-and-suspenders against the DB-layer
	// default (schema + migration both default to 'consent'), not a
	// substitute for it — a defense that only existed in Go would not protect
	// a raw SQL reader of this table.
	question.Class = defaultQuestionClass(question.Class)
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
		origin_command_seq, urgency, class, status, options, category, default_answer, created_at, asked_at, resolved_at,
		expires_at, resolution, asked_message_seq, answer_message_seq, updated_seq
		FROM agent_questions WHERE seq = ?`, seq)
	question, err := scanAgentQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentQuestion{}, false, nil
	}
	return question, err == nil, err
}

func defaultQuestionCategory(category QuestionCategory) QuestionCategory {
	if category == "" {
		return QuestionCategoryGeneric
	}
	return category
}

func validQuestionUrgency(urgency QuestionUrgency) bool {
	switch urgency {
	case QuestionBlocking, QuestionNextNaturalMoment, QuestionWhenever:
		return true
	default:
		return false
	}
}

// defaultQuestionClass is the last-line defense for the conservative-default
// law: anything that reaches this function with an empty or otherwise
// unrecognized class — a legacy row, a hand-built payload, a future caller
// that forgets to normalize — reads back as QuestionConsent rather than
// falling through to whatever SQLite's column default happens to be.
func defaultQuestionClass(class QuestionClass) QuestionClass {
	if !validQuestionClass(class) {
		return QuestionConsent
	}
	return class
}

func validQuestionClass(class QuestionClass) bool {
	switch class {
	case QuestionConsent, QuestionInformational:
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
