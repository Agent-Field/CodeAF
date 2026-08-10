package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// The thread is the conversational face of the graph, stored in the same
// journal. A chat message is an event; a requested mutation is an event; both
// are materialized for fast rendering. The chat loop writes messages and
// command requests and returns immediately — the reconciler applies commands
// asynchronously and posts system messages back into the thread.

// MaxMessageBytes bounds one chat message. Larger content belongs in the
// content-addressed store, referenced from the message body.
const MaxMessageBytes = 16 << 10

// Role says who authored a thread message. System messages are emitted by the
// graph itself (folds landing, commands applying, budgets tripping).
type Role string

const (
	RoleUser   Role = "user"
	RoleAgent  Role = "agent"
	RoleSystem Role = "system"
)

// QuestionOption is one ordered, selectable answer carried beside an askback.
// Value is machine-facing continuation data; Label is the user's wording.
type QuestionOption struct {
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// BriefItemKind says what one slim row in an arrival brief represents.
// The body remains resident-written prose; the kind gives every lens a stable
// glyph and lets it summarize the fold without parsing that prose.
type BriefItemKind string

const (
	BriefDone      BriefItemKind = "done"
	BriefFailure   BriefItemKind = "failure"
	BriefCancelled BriefItemKind = "cancelled"
	BriefQuestion  BriefItemKind = "question"
	BriefCharter   BriefItemKind = "charter"
	BriefFact      BriefItemKind = "fact"
	BriefSkill     BriefItemKind = "skill"
	BriefSpend     BriefItemKind = "spend"
	// BriefWaiting is the only kind that is not an event in the window. Every
	// other row answers "what happened while you were away"; this one answers
	// "what is still true now" — work stopped on an unanswered question, work
	// that has not moved. A returning employer's first question is what is
	// waiting on them, and until this kind existed the brief structurally could
	// not say it.
	BriefWaiting BriefItemKind = "waiting"
)

// BriefItem is one composed row in a morning-brief fold. Ref is optional
// durable provenance (a node id, charter id, or fact sequence).
type BriefItem struct {
	Kind BriefItemKind `json:"kind"`
	Body string        `json:"body"`
	Ref  string        `json:"ref,omitempty"`
}

// Brief marks one agent message as an arrival fold. Counts and cost are
// deterministic journal facts; Items and the containing message Body are the
// resident's composed voice. SinceSeq and ThroughSeq make the interval auditable.
type Brief struct {
	SinceSeq      int64 `json:"since_seq"`
	ThroughSeq    int64 `json:"through_seq"`
	Done          int   `json:"done,omitempty"`
	Failed        int   `json:"failed,omitempty"`
	Cancelled     int   `json:"cancelled,omitempty"`
	Questions     int   `json:"questions,omitempty"`
	CharterFired  int   `json:"charter_fired,omitempty"`
	FactsLearned  int   `json:"facts_learned,omitempty"`
	SkillsLearned int   `json:"skills_learned,omitempty"`
	// Waiting counts the standing rows — what is stopped on the user now, not
	// what happened in the window. Every other total is a fact about the past.
	Waiting int         `json:"waiting,omitempty"`
	CostUSD float64     `json:"cost_usd,omitempty"`
	Items   []BriefItem `json:"items"`
}

// SeenState is one edge of a human-facing session. Both edges are journaled:
// a clean detach is the precise watermark, while a lone attach still gives a
// useful conservative watermark after a crash.
type SeenState string

const (
	SeenAttached SeenState = "attached"
	SeenDetached SeenState = "detached"
)

// Seen is one journaled observation that a user-facing lens was present.
type Seen struct {
	Seq       int64
	Time      time.Time
	Surface   string
	SessionID string
	State     SeenState
}

// CommandKind names an asynchronous graph mutation requested from the thread.
type CommandKind string

const (
	// CommandSplice asks for new work: Instruction carries the verbatim user
	// intent; the reconciler compiles and splices it.
	CommandSplice CommandKind = "splice"
	// CommandAmend redirects existing work: Target names the node whose
	// subtree the instruction amends.
	CommandAmend CommandKind = "amend"
	// CommandCancel withdraws work: Target names the subtree root to cancel.
	CommandCancel CommandKind = "cancel"
	// CommandRedirect carries the user's own words into a job already in
	// flight: Target names its root, Instruction is what they said, verbatim.
	// The remaining plan is revised against it and the running leaves are told.
	CommandRedirect CommandKind = "redirect"
	// CommandExpedite is redirection's impatient sibling: Target names a live
	// job's root and the user wants it sooner, not different. It is a separate
	// kind rather than a flag on redirect so replay stays a switch on the verb
	// and no command row grows a column.
	CommandExpedite CommandKind = "expedite"
	// CommandPause/Resume operate on a journal-derived scheduler hold rather
	// than inventing a presentation-only node status.
	CommandPause        CommandKind = "pause"
	CommandResume       CommandKind = "resume"
	CommandReprioritize CommandKind = "reprioritize"
	CommandRestart      CommandKind = "restart"
	// CommandSetModel re-points what a job's remaining work runs on without
	// touching the work itself: Target names the subtree, Instruction names the
	// model. It is a surgery verb rather than a setting because the pin is
	// provenance — it lives on the nodes, it is journaled, and it survives a
	// restart — and because the change has to be as revisable and as visible as
	// every other mid-run correction. It never interrupts anything: a leaf
	// already handed to a model finishes there, and the new pin is read by the
	// next one to be claimed.
	CommandSetModel CommandKind = "set_model"

	// Charter commands are requested through the same durable reconciler queue
	// as graph mutations. Their target names a charter rather than a node.
	CommandCharterRatify  CommandKind = "charter_ratify"
	CommandCharterPause   CommandKind = "charter_pause"
	CommandCharterRetire  CommandKind = "charter_retire"
	CommandCharterCadence CommandKind = "charter_cadence"
	// CommandCharterWording changes what a standing rule says or does without
	// touching when it runs. It is the other half of editing a rule by talking
	// about it: "change it to Tuesday" retimes, "make it say take the bins out
	// too" rewords, and before this the second sentence had nowhere to land.
	CommandCharterWording   CommandKind = "charter_wording"
	CommandCharterOnce      CommandKind = "charter_once"
	CommandCharterFire      CommandKind = "charter_fire"
	CommandCharterDecline   CommandKind = "charter_decline"
	CommandCharterAlways    CommandKind = "charter_always"
	CommandCharterNever     CommandKind = "charter_never"
	CommandCharterProbation CommandKind = "charter_probation"

	CommandServiceStop        CommandKind = "service_stop"
	CommandServiceRestart     CommandKind = "service_restart"
	CommandServiceAutoRestart CommandKind = "service_auto_restart"

	// Standing-watch commands carry the one global unattended-presence
	// decision. They deliberately have no graph-node or charter target.
	CommandStandingWatchEnable  CommandKind = "standing_watch_enable"
	CommandStandingWatchDecline CommandKind = "standing_watch_decline"

	// CommandHandover asks whoever currently holds the resident role to give it
	// up; Instruction says who is asking and why, in plain words. It is a
	// command rather than a new event because the journal already has exactly
	// the shape this needs: a durable request only the resident drains,
	// resolved exactly once, replayed like everything else — and because a
	// resident too old to know the verb rejects it in words instead of
	// ignoring it, which is how a requester learns it must wait for the
	// heartbeat to go stale instead.
	CommandHandover CommandKind = "handover"
)

// CommandStatus is the lifecycle of a requested command. Commands are durable
// the moment they are requested and are resolved exactly once.
type CommandStatus string

const (
	CommandPending  CommandStatus = "pending"
	CommandApplied  CommandStatus = "applied"
	CommandRejected CommandStatus = "rejected"
)

// Message is one materialized thread entry. Seq is the journal sequence, so
// message order is total and shared with every other event in the store.
type Message struct {
	Seq         int64
	Time        time.Time
	SessionID   string
	Role        Role
	Body        string
	Attachments []string
	// Model is chat-lane metadata. On a user message it requests the model for
	// that one conversational turn; on an agent message it records the model
	// that actually produced the reply. Empty preserves the ordinary talk lane.
	Model string
	// NodeID optionally anchors the message to a graph node (a fold
	// announcement, an ask, a completion report).
	NodeID string
	// CommandSeq optionally links the message to the command it acknowledges
	// or reports on.
	CommandSeq int64
	// QuestionSeq links an agent ask or the user's answer to the durable
	// agent-question lifecycle it belongs to. It is separate from Options:
	// free-text questions need the same unambiguous answer routing.
	QuestionSeq int64
	// Answers is the newest user turn this line answers, and it exists because
	// one reply is no longer one message. A run of messages typed in one breath
	// is folded into a single turn and answered once, so a surface counting what
	// it is still owed cannot read that count off the replies alone: it would
	// wait forever for answers that were never going to come separately. Zero
	// everywhere except the head's own replies, where it is the seq of the last
	// user row the turn covered.
	Answers int64
	// Options is the ordered set of selectable answers for an askback.
	// Nil means the question accepts free text only.
	Options []QuestionOption
	// Brief is non-nil only for the resident's folded arrival summary.
	Brief *Brief
	// Progress is non-nil only for a replaceable compile-progress post. The
	// body remains a readable journal line; these fields let surfaces present
	// the current phase and real generated titles without parsing prose.
	Progress *MessageProgress
	// Parts is the ordered list of typed blocks this message carries — see
	// message_parts.go. Nil is a legacy prose message and is what almost every
	// message in a real database is; Body remains the whole rendered line
	// either way, so a surface that has never heard of parts renders exactly
	// what it rendered before.
	Parts []MessagePart
}

// MessageProgress is the durable, user-facing shape of compile progress.
type MessageProgress struct {
	Phase  string `json:"phase"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Latest string `json:"latest"`
}

// Command is one materialized mutation request.
type Command struct {
	Seq       int64
	Time      time.Time
	SessionID string
	Kind      CommandKind
	// Issuer names who asked — see issuer.go. Empty is the legacy value and
	// reads as IssuerUser, which is what every command written before the axis
	// existed actually was.
	Issuer CommandIssuer
	// Reflex asks the reconciler to admit exactly one verbatim micro-leaf
	// without compiling or planning it. It remains a splice command so a
	// promotion can enqueue the ordinary path with the same instruction.
	Reflex bool
	// Fresh is the person asking for this one to be worked out from scratch
	// rather than the way it has been done before. It is a reading of what they
	// meant — "don't use the template this time", "plan this one properly" —
	// made where every other reading of a message is made, and carried here
	// because the engine that would reach for learned know-how runs long after
	// the sentence is gone.
	Fresh       bool
	Target      string
	Instruction string
	Attachments []string
	Status      CommandStatus
	Result      string
	UpdatedSeq  int64
}

const threadSchema = `
CREATE TABLE IF NOT EXISTS messages (
    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
    ts          TEXT NOT NULL,
    session_id  TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL CHECK (role IN ('user', 'agent', 'system')),
	body        TEXT NOT NULL,
	attachments JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
    model       TEXT NOT NULL DEFAULT '',
    node_id     TEXT NOT NULL DEFAULT '',
    command_seq INTEGER NOT NULL DEFAULT 0,
    question_seq INTEGER NOT NULL DEFAULT 0,
    answers_seq INTEGER NOT NULL DEFAULT 0,
	options     JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options)),
	brief       JSON NOT NULL DEFAULT 'null' CHECK (json_valid(brief)),
	progress    JSON NOT NULL DEFAULT 'null' CHECK (json_valid(progress)),
	parts       JSON NOT NULL DEFAULT 'null' CHECK (json_valid(parts))
);
CREATE INDEX IF NOT EXISTS messages_session_seq ON messages (session_id, seq);

CREATE TABLE IF NOT EXISTS commands (
    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
    ts          TEXT NOT NULL,
    session_id  TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    issuer      TEXT NOT NULL DEFAULT '',
    reflex      INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1)),
    fresh       INTEGER NOT NULL DEFAULT 0 CHECK (fresh IN (0, 1)),
    target      TEXT NOT NULL DEFAULT '',
	instruction TEXT NOT NULL,
	attachments JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
    status      TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
    result      TEXT NOT NULL DEFAULT '',
    updated_seq INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS commands_status_seq ON commands (status, seq);
`

type messagePayload struct {
	SessionID   string           `json:"session_id,omitempty"`
	Role        Role             `json:"role"`
	Body        string           `json:"body"`
	Attachments []string         `json:"attachments,omitempty"`
	Model       string           `json:"model,omitempty"`
	NodeID      string           `json:"node_id,omitempty"`
	CommandSeq  int64            `json:"command_seq,omitempty"`
	QuestionSeq int64            `json:"question_seq,omitempty"`
	Answers     int64            `json:"answers_seq,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
	Brief       *Brief           `json:"brief,omitempty"`
	Progress    *MessageProgress `json:"progress,omitempty"`
	// Parts is omitempty so a message without them journals the byte-identical
	// payload it journaled before this field existed. Replay of an old event is
	// therefore not merely compatible, it is the same decode.
	Parts []MessagePart `json:"parts,omitempty"`
}

type seenPayload struct {
	Surface   string    `json:"surface"`
	SessionID string    `json:"session_id,omitempty"`
	State     SeenState `json:"state"`
}

type commandPayload struct {
	SessionID   string        `json:"session_id,omitempty"`
	Kind        CommandKind   `json:"kind"`
	Issuer      CommandIssuer `json:"issuer,omitempty"`
	Reflex      bool          `json:"reflex,omitempty"`
	Fresh       bool          `json:"fresh,omitempty"`
	Target      string        `json:"target,omitempty"`
	Instruction string        `json:"instruction"`
	Attachments []string      `json:"attachments,omitempty"`
}

type commandResolvedPayload struct {
	CommandSeq int64         `json:"command_seq"`
	Status     CommandStatus `json:"status"`
	Result     string        `json:"result,omitempty"`
}

// PostMessage appends one thread message. Seq and Time on the argument are
// ignored; the returned Message carries the assigned values.
func (s *Store) PostMessage(message Message) (Message, error) {
	if !validRole(message.Role) {
		return Message{}, fmt.Errorf("post message: %w: unknown role %q", ErrInvalid, message.Role)
	}
	if strings.TrimSpace(message.Body) == "" {
		return Message{}, fmt.Errorf("post message: %w: empty body", ErrInvalid)
	}
	if len(message.Body) > MaxMessageBytes {
		return Message{}, fmt.Errorf("post message: %w: body is %d bytes (limit %d); spill to the blob store and reference it",
			ErrInvalid, len(message.Body), MaxMessageBytes)
	}
	options, err := normalizeQuestionOptions(message.Options)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	brief, err := normalizeBrief(message.Brief)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	if brief != nil && message.Role != RoleAgent {
		return Message{}, fmt.Errorf("post message: %w: a brief must use the agent role", ErrInvalid)
	}
	progress, err := normalizeMessageProgress(message.Progress)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	if progress != nil && message.Role != RoleSystem {
		return Message{}, fmt.Errorf("post message: %w: progress must use the system role", ErrInvalid)
	}
	parts, err := normalizeMessageParts(message.Parts)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	// The size gate is here rather than in the normalizer because it costs an
	// encode, and the read path calls the normalizer on every parts-bearing
	// message it decodes. A write is rare; a read is a poll tick.
	if len(parts) > 0 {
		encoded, encodeErr := encodeMessageParts(parts)
		if encodeErr != nil {
			return Message{}, fmt.Errorf("post message: %w: %s", ErrInvalid, encodeErr)
		}
		if len(encoded) > MaxMessagePartsBytes {
			return Message{}, fmt.Errorf("post message: %w: parts are %d bytes (limit %d); reference the content instead of carrying it",
				ErrInvalid, len(encoded), MaxMessagePartsBytes)
		}
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	defer tx.Rollback()

	if message.NodeID != "" {
		if err := requireNode(tx, message.NodeID); err != nil {
			if !errors.Is(err, ErrNotFound) || !pendingMessageAnchor(tx, message) {
				return Message{}, fmt.Errorf("post message: %w", err)
			}
		}
	}
	payload := messagePayload{
		SessionID:   message.SessionID,
		Role:        message.Role,
		Body:        message.Body,
		Attachments: append([]string(nil), message.Attachments...),
		Model:       strings.TrimSpace(message.Model),
		NodeID:      message.NodeID,
		CommandSeq:  message.CommandSeq,
		QuestionSeq: message.QuestionSeq,
		Answers:     message.Answers,
		Options:     options,
		Brief:       brief,
		Progress:    progress,
		Parts:       parts,
	}
	seq, at, err := appendEvent(tx, message.NodeID, EventMessagePosted, payload)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	if err := applyMessageView(tx, payload, seq, at); err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	message.Seq = seq
	message.Time = at
	message.Model = payload.Model
	message.Options = options
	message.Brief = brief
	message.Progress = progress
	message.Parts = parts
	return message, nil
}

// LastSeen returns the newest attach or detach watermark across user-facing
// lenses. A resident database represents one person's attention stream, so a
// TUI close followed by a web open is one continuous seen history.
func (s *Store) LastSeen() (Seen, bool, error) {
	row := s.db.QueryRow(`
		SELECT seq, ts, payload FROM events
		WHERE kind = ? ORDER BY seq DESC LIMIT 1`, EventSeenTouched)
	var seen Seen
	var timestamp, encoded string
	if err := row.Scan(&seen.Seq, &timestamp, &encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Seen{}, false, nil
		}
		return Seen{}, false, fmt.Errorf("last seen: %w", err)
	}
	var payload seenPayload
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return Seen{}, false, fmt.Errorf("last seen: decode event %d: %w", seen.Seq, err)
	}
	at, err := parseTime(timestamp)
	if err != nil {
		return Seen{}, false, fmt.Errorf("last seen: parse time: %w", err)
	}
	seen.Time = at
	seen.Surface = payload.Surface
	seen.SessionID = payload.SessionID
	seen.State = payload.State
	return seen, true, nil
}

// TouchSeen appends one attach/detach edge and returns its journal watermark.
func (s *Store) TouchSeen(surface, sessionID string, state SeenState) (Seen, error) {
	surface = strings.TrimSpace(surface)
	sessionID = strings.TrimSpace(sessionID)
	if surface == "" || (state != SeenAttached && state != SeenDetached) {
		return Seen{}, fmt.Errorf("touch seen: %w: surface and valid state are required", ErrInvalid)
	}
	payload := seenPayload{Surface: surface, SessionID: sessionID, State: state}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Seen{}, fmt.Errorf("touch seen: %w", err)
	}
	defer tx.Rollback()
	seq, at, err := appendEvent(tx, "", EventSeenTouched, payload)
	if err != nil {
		return Seen{}, fmt.Errorf("touch seen: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Seen{}, fmt.Errorf("touch seen: %w", err)
	}
	return Seen{Seq: seq, Time: at, Surface: surface, SessionID: sessionID, State: state}, nil
}

// pendingMessageAnchor permits planning narration to name the root that a
// pending splice will admit. The command link is the proof that the unknown ID
// is provisional rather than a dangling message.
func pendingMessageAnchor(tx *sql.Tx, message Message) bool {
	if message.CommandSeq == 0 {
		return false
	}
	var sessionID string
	var kind CommandKind
	var status CommandStatus
	err := tx.QueryRow(`
		SELECT session_id, kind, status FROM commands WHERE seq = ?`,
		message.CommandSeq).Scan(&sessionID, &kind, &status)
	return err == nil && kind == CommandSplice && status == CommandPending &&
		sessionID == message.SessionID
}

// Messages returns thread messages after a journal sequence, oldest first.
// An empty sessionID returns every session's messages; afterSeq zero starts
// from the beginning. This is the tailing primitive: a lens remembers the
// last Seq it rendered and asks for what came after.
func (s *Store) Messages(sessionID string, afterSeq int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	where := `seq > ?`
	args := []any{afterSeq}
	if sessionID != "" {
		where += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT seq, ts, session_id, role, body, attachments, model, node_id, command_seq, question_seq, answers_seq, options, brief, progress, parts
		FROM messages WHERE `+where+` ORDER BY seq LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var timestamp, attachments, options, brief, progress string
		// RawBytes, not string: the driver hands the column's own buffer over
		// and the legacy check below reads it without copying it. It is valid
		// only until the next row, which is exactly as long as it is used.
		var parts sql.RawBytes
		if err := rows.Scan(&message.Seq, &timestamp, &message.SessionID,
			&message.Role, &message.Body, &attachments, &message.Model, &message.NodeID, &message.CommandSeq, &message.QuestionSeq, &message.Answers, &options, &brief, &progress, &parts); err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		if err := decodeQuestionOptions(options, &message.Options); err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		if err := decodeBrief(brief, &message.Brief); err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		if err := decodeMessageProgress(progress, &message.Progress); err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		decodeMessageParts(parts, message.Seq, &message.Parts)
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("list messages: parse time: %w", err)
		}
		message.Time = at
		if err := json.Unmarshal([]byte(attachments), &message.Attachments); err != nil {
			return nil, fmt.Errorf("list messages: decode attachments: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return messages, nil
}

// LastNonUserMessageSeq returns the sequence of the newest message the user did
// not write, or zero when the thread has only ever heard from them.
//
// It answers the head's resume question — where does the trailing run of
// unanswered user messages begin — without reading the thread. Tailing pages
// the whole history to keep one number, and that history only grows.
//
// Deprecated: one number cannot answer that question for two rooms — a reply in
// either carries it past the other's unanswered rows. Use
// SessionLastNonUserMessageSeq, or SessionMessageCursors for every room at once.
func (s *Store) LastNonUserMessageSeq() (int64, error) {
	var seq int64
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE role <> ?`, string(RoleUser)).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read last non-user message sequence: %w", err)
	}
	return seq, nil
}

// NodeMessages returns the messages anchored to one node after a journal
// sequence, oldest first — a running worker's steering mailbox, and a node
// view's conversation trail.
func (s *Store) NodeMessages(nodeID string, afterSeq int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, session_id, role, body, attachments, model, node_id, command_seq, question_seq, answers_seq, options, brief, progress, parts
		FROM messages WHERE node_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		nodeID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("node messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var timestamp, attachments, options, brief, progress string
		// RawBytes, not string: the driver hands the column's own buffer over
		// and the legacy check below reads it without copying it. It is valid
		// only until the next row, which is exactly as long as it is used.
		var parts sql.RawBytes
		if err := rows.Scan(&message.Seq, &timestamp, &message.SessionID,
			&message.Role, &message.Body, &attachments, &message.Model, &message.NodeID, &message.CommandSeq, &message.QuestionSeq, &message.Answers, &options, &brief, &progress, &parts); err != nil {
			return nil, fmt.Errorf("node messages: %w", err)
		}
		if err := decodeQuestionOptions(options, &message.Options); err != nil {
			return nil, fmt.Errorf("node messages: %w", err)
		}
		if err := decodeBrief(brief, &message.Brief); err != nil {
			return nil, fmt.Errorf("node messages: %w", err)
		}
		if err := decodeMessageProgress(progress, &message.Progress); err != nil {
			return nil, fmt.Errorf("node messages: %w", err)
		}
		decodeMessageParts(parts, message.Seq, &message.Parts)
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("node messages: parse time: %w", err)
		}
		message.Time = at
		if err := json.Unmarshal([]byte(attachments), &message.Attachments); err != nil {
			return nil, fmt.Errorf("node messages: decode attachments: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("node messages: %w", err)
	}
	return messages, nil
}

// RequestCommand records one asynchronous mutation request and returns
// immediately. The reconciler picks it up via PendingCommands and settles it
// with ResolveCommand; the requester never blocks on the mutation itself.
func (s *Store) RequestCommand(command Command) (Command, error) {
	if err := validateCommandRequest(command); err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	defer tx.Rollback()

	command, err = requestCommandTx(tx, command)
	if err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	return command, nil
}

func validateCommandRequest(command Command) error {
	if !validCommandKind(command.Kind) {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, command.Kind)
	}
	if strings.TrimSpace(command.Instruction) == "" {
		return fmt.Errorf("%w: empty instruction", ErrInvalid)
	}
	if command.Reflex && (command.Kind != CommandSplice || strings.TrimSpace(command.Target) != "") {
		return fmt.Errorf("%w: reflex must be an untargeted splice", ErrInvalid)
	}
	if command.Kind != CommandSplice && !isGlobalCommand(command.Kind) && strings.TrimSpace(command.Target) == "" {
		return fmt.Errorf("%w: %s requires a target", ErrInvalid, command.Kind)
	}
	return nil
}

// requestCommandTx appends and materializes one already-validated command in
// the caller's transaction. Keeping this primitive shared lets a question
// resolution and its continuation command commit as one journaled decision.
func requestCommandTx(tx *sql.Tx, command Command) (Command, error) {
	// Who asked is checked here rather than in validateCommandRequest because
	// this is the primitive both entry paths share, and an unauthorized command
	// must be refused whichever door it arrived through.
	command.Issuer = command.Issuer.normalized()
	if !command.Issuer.valid() {
		return Command{}, fmt.Errorf("request command: %w: unknown issuer %q", ErrInvalid, command.Issuer)
	}
	if command.Target != "" {
		if isCharterCommand(command.Kind) {
			if err := requireCharter(tx, command.Target); err != nil {
				return Command{}, err
			}
		} else if isServiceCommand(command.Kind) {
			var status ServiceStatus
			if err := tx.QueryRow(`SELECT status FROM services WHERE id=?`, command.Target).Scan(&status); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return Command{}, fmt.Errorf("request command: %w: service %q", ErrNotFound, command.Target)
				}
				return Command{}, fmt.Errorf("request command: %w", err)
			}
			if status == ServiceStopped && command.Kind != CommandServiceRestart {
				return Command{}, fmt.Errorf("request command: %w: service %q is stopped", ErrInvalid, command.Target)
			}
		} else {
			if err := requireNode(tx, command.Target); err != nil {
				return Command{}, err
			}
			if err := validateNodeCommand(tx, command.Kind, command.Target); err != nil {
				return Command{}, err
			}
		}
	}
	// Outside the block above on purpose: an untargeted command is inside
	// nobody's subtree, so a task that writes one must be refused rather than
	// skipped.
	if err := authorizeCommandIssuer(tx, command); err != nil {
		return Command{}, err
	}
	payload := commandPayload{
		SessionID:   command.SessionID,
		Kind:        command.Kind,
		Issuer:      command.Issuer,
		Reflex:      command.Reflex,
		Fresh:       command.Fresh,
		Target:      command.Target,
		Instruction: command.Instruction,
		Attachments: append([]string(nil), command.Attachments...),
	}
	seq, at, err := appendEvent(tx, command.Target, EventCommandRequested, payload)
	if err != nil {
		return Command{}, err
	}
	if err := applyCommandView(tx, payload, seq, at); err != nil {
		return Command{}, err
	}
	command.Seq = seq
	command.Time = at
	command.Status = CommandPending
	command.UpdatedSeq = seq
	return command, nil
}

// PendingCommands returns unapplied commands oldest first — the reconciler's
// work queue.
func (s *Store) PendingCommands(limit int) ([]Command, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.queryCommands(`status = ? ORDER BY seq LIMIT ?`, []any{CommandPending, limit})
}

// CommandBySeq returns one command by its journal sequence.
func (s *Store) CommandBySeq(seq int64) (Command, bool, error) {
	commands, err := s.queryCommands(`seq = ?`, []any{seq})
	if err != nil {
		return Command{}, false, err
	}
	if len(commands) == 0 {
		return Command{}, false, nil
	}
	return commands[0], true, nil
}

// HasCommandTarget reports whether the journaled command view contains a
// command of kind aimed at target. A targeted splice is the durable marker for
// a reflex promotion.
func (s *Store) HasCommandTarget(target string, kind CommandKind) (bool, error) {
	var found bool
	if err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM commands WHERE target = ? AND kind = ?
	)`, target, kind).Scan(&found); err != nil {
		return false, fmt.Errorf("find targeted command: %w", err)
	}
	return found, nil
}

// TargetedCommands returns what was aimed at one node, oldest first.
// HasCommandTarget answers whether it happened; this answers what was said,
// which is what a redirect's own words are — the strongest correction signal in
// the system, and one that survived downstream only as a boolean.
func (s *Store) TargetedCommands(target string, kind CommandKind, limit int) ([]Command, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	return s.queryCommands(`target = ? AND kind = ? ORDER BY seq LIMIT ?`, []any{target, kind, limit})
}

// ResolveCommand settles a pending command exactly once. Status must be
// CommandApplied or CommandRejected; result says what actually happened and
// belongs in the system message reported back to the thread.
func (s *Store) ResolveCommand(seq int64, status CommandStatus, result string) error {
	if status != CommandApplied && status != CommandRejected {
		return fmt.Errorf("resolve command: %w: status %q is not a resolution", ErrInvalid, status)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	defer tx.Rollback()

	var current CommandStatus
	err = tx.QueryRow(`SELECT status FROM commands WHERE seq = ?`, seq).Scan(&current)
	if err == sql.ErrNoRows {
		return fmt.Errorf("resolve command: %w: no command at seq %d", ErrNotFound, seq)
	}
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	if current != CommandPending {
		return fmt.Errorf("resolve command: %w: command %d is already %s", ErrInvalid, seq, current)
	}
	payload := commandResolvedPayload{CommandSeq: seq, Status: status, Result: bounded(result, MaxDigestBytes)}
	eventSeq, _, err := appendEvent(tx, "", EventCommandResolved, payload)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	if err := applyCommandResolution(tx, payload, eventSeq); err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	return nil
}

func (s *Store) queryCommands(where string, args []any) ([]Command, error) {
	rows, err := s.db.Query(`
		SELECT seq, ts, session_id, kind, issuer, reflex, fresh, target, instruction, attachments, status, result, updated_seq
		FROM commands WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	defer rows.Close()

	commands := make([]Command, 0)
	for rows.Next() {
		var command Command
		var reflex, fresh int
		var timestamp, attachments string
		if err := rows.Scan(&command.Seq, &timestamp, &command.SessionID, &command.Kind, &command.Issuer,
			&reflex, &fresh, &command.Target, &command.Instruction, &attachments, &command.Status, &command.Result,
			&command.UpdatedSeq); err != nil {
			return nil, fmt.Errorf("list commands: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("list commands: parse time: %w", err)
		}
		command.Reflex = reflex != 0
		command.Fresh = fresh != 0
		if err := json.Unmarshal([]byte(attachments), &command.Attachments); err != nil {
			return nil, fmt.Errorf("list commands: decode attachments: %w", err)
		}
		command.Time = at
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	return commands, nil
}

func applyMessageView(tx *sql.Tx, payload messagePayload, seq int64, at time.Time) error {
	attachments, err := json.Marshal(payload.Attachments)
	if err != nil {
		return err
	}
	options, err := json.Marshal(payload.Options)
	if err != nil {
		return err
	}
	brief, err := json.Marshal(payload.Brief)
	if err != nil {
		return err
	}
	progress, err := json.Marshal(payload.Progress)
	if err != nil {
		return err
	}
	// Replay reaches here too, so the parts a journaled event carries are
	// normalized on the way into the projection exactly as they were on the way
	// into the journal — including the pass-through of a kind this build does
	// not know, which is how a rebuild by an older binary stops being lossy.
	parts, err := normalizeMessageParts(payload.Parts)
	if err != nil {
		return err
	}
	encodedParts, err := encodeMessageParts(parts)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO messages (seq, ts, session_id, role, body, attachments, model, node_id, command_seq, question_seq, answers_seq, options, brief, progress, parts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.SessionID, payload.Role, payload.Body,
		string(attachments), payload.Model, payload.NodeID, payload.CommandSeq, payload.QuestionSeq,
		payload.Answers, string(options), string(brief), string(progress), encodedParts)
	if err != nil {
		return err
	}
	// The searchable copy is written by the same transaction as the row it
	// indexes. Every message write in this package goes through here, so there
	// is exactly one seam to keep honest.
	if err := refreshMessageFTS(tx, seq); err != nil {
		return err
	}
	// And for the same reason, this is where a session becomes a thing rather
	// than a string: the row is minted by the first message that names it and
	// its activity mark is raised by every one after, in the message's own
	// transaction, so the projection cannot drift from the messages it is a
	// projection of.
	return ensureSessionTx(tx, payload.SessionID, "", at)
}

func normalizeMessageProgress(progress *MessageProgress) (*MessageProgress, error) {
	if progress == nil {
		return nil, nil
	}
	normalized := *progress
	normalized.Phase = strings.TrimSpace(normalized.Phase)
	normalized.Latest = strings.TrimSpace(normalized.Latest)
	if normalized.Phase == "" || normalized.Done < 0 || normalized.Total < 0 ||
		normalized.Done > normalized.Total || (normalized.Total == 0 && normalized.Done != 0) {
		return nil, fmt.Errorf("%w: invalid message progress", ErrInvalid)
	}
	return &normalized, nil
}

func decodeMessageProgress(encoded string, target **MessageProgress) error {
	if encoded == "" || encoded == "null" {
		*target = nil
		return nil
	}
	var progress MessageProgress
	if err := json.Unmarshal([]byte(encoded), &progress); err != nil {
		return fmt.Errorf("decode progress: %w", err)
	}
	normalized, err := normalizeMessageProgress(&progress)
	if err != nil {
		return fmt.Errorf("decode progress: %w", err)
	}
	*target = normalized
	return nil
}

func normalizeBrief(brief *Brief) (*Brief, error) {
	if brief == nil {
		return nil, nil
	}
	if brief.SinceSeq < 0 || brief.ThroughSeq < brief.SinceSeq ||
		brief.Done < 0 || brief.Failed < 0 || brief.Cancelled < 0 || brief.Questions < 0 || brief.CharterFired < 0 ||
		brief.FactsLearned < 0 || brief.SkillsLearned < 0 || brief.Waiting < 0 || brief.CostUSD < 0 ||
		math.IsNaN(brief.CostUSD) || math.IsInf(brief.CostUSD, 0) {
		return nil, fmt.Errorf("%w: invalid brief totals", ErrInvalid)
	}
	if len(brief.Items) == 0 {
		return nil, fmt.Errorf("%w: brief has no items", ErrInvalid)
	}
	normalized := *brief
	normalized.Items = make([]BriefItem, 0, len(brief.Items))
	for _, item := range brief.Items {
		item.Body = strings.TrimSpace(item.Body)
		item.Ref = strings.TrimSpace(item.Ref)
		if item.Body == "" || !validBriefItemKind(item.Kind) {
			return nil, fmt.Errorf("%w: invalid brief item", ErrInvalid)
		}
		normalized.Items = append(normalized.Items, item)
	}
	return &normalized, nil
}

func decodeBrief(raw string, target **Brief) error {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" {
		*target = nil
		return nil
	}
	var brief Brief
	if err := json.Unmarshal([]byte(raw), &brief); err != nil {
		return err
	}
	normalized, err := normalizeBrief(&brief)
	if err != nil {
		return err
	}
	*target = normalized
	return nil
}

func validBriefItemKind(kind BriefItemKind) bool {
	switch kind {
	case BriefDone, BriefFailure, BriefCancelled, BriefQuestion, BriefCharter,
		BriefFact, BriefSkill, BriefSpend, BriefWaiting:
		return true
	default:
		return false
	}
}

func applyCommandView(tx *sql.Tx, payload commandPayload, seq int64, at time.Time) error {
	attachments, err := json.Marshal(payload.Attachments)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO commands (seq, ts, session_id, kind, issuer, reflex, fresh, target, instruction, attachments, status, result, updated_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)`,
		seq, formatTime(at), payload.SessionID, payload.Kind, payload.Issuer, payload.Reflex, payload.Fresh, payload.Target,
		payload.Instruction, string(attachments), CommandPending, seq)
	return err
}

func applyCommandResolution(tx *sql.Tx, payload commandResolvedPayload, eventSeq int64) error {
	result, err := tx.Exec(`
		UPDATE commands SET status = ?, result = ?, updated_seq = ?
		WHERE seq = ? AND status = ?`,
		payload.Status, payload.Result, eventSeq, payload.CommandSeq, CommandPending)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("resolution targets missing or settled command %d", payload.CommandSeq)
	}
	return nil
}

func requireNode(tx *sql.Tx, id string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	return nil
}

func validRole(role Role) bool {
	return role == RoleUser || role == RoleAgent || role == RoleSystem
}

func validCommandKind(kind CommandKind) bool {
	switch kind {
	case CommandSplice, CommandAmend, CommandCancel, CommandRedirect, CommandExpedite, CommandPause, CommandResume,
		CommandReprioritize, CommandRestart, CommandSetModel, CommandCharterRatify,
		CommandCharterPause, CommandCharterRetire, CommandCharterCadence, CommandCharterWording,
		CommandCharterOnce,
		CommandCharterFire, CommandCharterDecline, CommandCharterAlways, CommandCharterNever, CommandCharterProbation,
		CommandServiceStop, CommandServiceRestart, CommandServiceAutoRestart,
		CommandStandingWatchEnable, CommandStandingWatchDecline, CommandHandover:
		return true
	default:
		return false
	}
}

func isServiceCommand(kind CommandKind) bool {
	switch kind {
	case CommandServiceStop, CommandServiceRestart, CommandServiceAutoRestart:
		return true
	default:
		return false
	}
}

func validateNodeCommand(tx *sql.Tx, kind CommandKind, target string) error {
	if kind == CommandSplice {
		return nil
	}
	if target == RootID {
		return fmt.Errorf("%s cannot target the permanent spine: %w", kind, ErrInvalid)
	}
	var status Status
	var held bool
	var folded bool
	var group string
	if err := tx.QueryRow(`SELECT status, held, folded, grp FROM nodes WHERE id = ?`, target).
		Scan(&status, &held, &folded, &group); err != nil {
		return err
	}
	if folded || group == TerritoryGroup || group == "charter" {
		return fmt.Errorf("%s target %q is not executable work: %w", kind, target, ErrInvalid)
	}
	switch kind {
	// A model change is a claim about work still to be done, so it wants the
	// same live target the other mid-run verbs do: a settled subtree has
	// nothing left to re-point, and saying so at the funnel is cheaper than
	// letting the reconciler discover it.
	case CommandCancel, CommandPause, CommandAmend, CommandRedirect, CommandExpedite, CommandSetModel:
		if status != Pending && status != Claimed && status != Running {
			return fmt.Errorf("%s target %q is %s: %w", kind, target, status, ErrInvalid)
		}
	case CommandResume:
		if !held || (status != Pending && status != Claimed && status != Running) {
			return fmt.Errorf("resume target %q is not paused: %w", target, ErrInvalid)
		}
	case CommandReprioritize:
		if status != Pending {
			return fmt.Errorf("reprioritize target %q is %s: %w", target, status, ErrInvalid)
		}
	case CommandRestart:
		if status != Failed && status != Cancelled {
			return fmt.Errorf("restart target %q is %s: %w", target, status, ErrInvalid)
		}
	}
	return nil
}

func isCharterCommand(kind CommandKind) bool {
	switch kind {
	case CommandCharterRatify, CommandCharterPause, CommandCharterRetire,
		CommandCharterCadence, CommandCharterWording, CommandCharterOnce, CommandCharterFire,
		CommandCharterDecline, CommandCharterAlways, CommandCharterNever, CommandCharterProbation:
		return true
	default:
		return false
	}
}

// isGlobalCommand names the kinds that address the whole store rather than a
// node in the graph. A handover joins them: it is about which process is
// serving, and there is no node it could point at.
func isGlobalCommand(kind CommandKind) bool {
	return kind == CommandStandingWatchEnable || kind == CommandStandingWatchDecline ||
		kind == CommandHandover
}
