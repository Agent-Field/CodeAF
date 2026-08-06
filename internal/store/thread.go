package store

import (
	"context"
	"database/sql"
	"fmt"
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
	Seq       int64
	Time      time.Time
	SessionID string
	Role      Role
	Body      string
	// NodeID optionally anchors the message to a graph node (a fold
	// announcement, an ask, a completion report).
	NodeID string
	// CommandSeq optionally links the message to the command it acknowledges
	// or reports on.
	CommandSeq int64
}

// Command is one materialized mutation request.
type Command struct {
	Seq       int64
	Time      time.Time
	SessionID string
	Kind      CommandKind
	// Reflex asks the reconciler to admit exactly one verbatim micro-leaf
	// without compiling or planning it. It remains a splice command so a
	// promotion can enqueue the ordinary path with the same instruction.
	Reflex      bool
	Target      string
	Instruction string
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
    node_id     TEXT NOT NULL DEFAULT '',
    command_seq INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_session_seq ON messages (session_id, seq);

CREATE TABLE IF NOT EXISTS commands (
    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
    ts          TEXT NOT NULL,
    session_id  TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('splice', 'amend', 'cancel')),
    reflex      INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1)),
    target      TEXT NOT NULL DEFAULT '',
    instruction TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
    result      TEXT NOT NULL DEFAULT '',
    updated_seq INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS commands_status_seq ON commands (status, seq);
`

type messagePayload struct {
	SessionID  string `json:"session_id,omitempty"`
	Role       Role   `json:"role"`
	Body       string `json:"body"`
	NodeID     string `json:"node_id,omitempty"`
	CommandSeq int64  `json:"command_seq,omitempty"`
}

type commandPayload struct {
	SessionID   string      `json:"session_id,omitempty"`
	Kind        CommandKind `json:"kind"`
	Reflex      bool        `json:"reflex,omitempty"`
	Target      string      `json:"target,omitempty"`
	Instruction string      `json:"instruction"`
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

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Message{}, fmt.Errorf("post message: %w", err)
	}
	defer tx.Rollback()

	if message.NodeID != "" {
		if err := requireNode(tx, message.NodeID); err != nil {
			return Message{}, fmt.Errorf("post message: %w", err)
		}
	}
	payload := messagePayload{
		SessionID:  message.SessionID,
		Role:       message.Role,
		Body:       message.Body,
		NodeID:     message.NodeID,
		CommandSeq: message.CommandSeq,
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
	return message, nil
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
		SELECT seq, ts, session_id, role, body, node_id, command_seq
		FROM messages WHERE `+where+` ORDER BY seq LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var timestamp string
		if err := rows.Scan(&message.Seq, &timestamp, &message.SessionID,
			&message.Role, &message.Body, &message.NodeID, &message.CommandSeq); err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("list messages: parse time: %w", err)
		}
		message.Time = at
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return messages, nil
}

// NodeMessages returns the messages anchored to one node after a journal
// sequence, oldest first — a running worker's steering mailbox, and a node
// view's conversation trail.
func (s *Store) NodeMessages(nodeID string, afterSeq int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, session_id, role, body, node_id, command_seq
		FROM messages WHERE node_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		nodeID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("node messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var timestamp string
		if err := rows.Scan(&message.Seq, &timestamp, &message.SessionID,
			&message.Role, &message.Body, &message.NodeID, &message.CommandSeq); err != nil {
			return nil, fmt.Errorf("node messages: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("node messages: parse time: %w", err)
		}
		message.Time = at
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
	if !validCommandKind(command.Kind) {
		return Command{}, fmt.Errorf("request command: %w: unknown kind %q", ErrInvalid, command.Kind)
	}
	if strings.TrimSpace(command.Instruction) == "" {
		return Command{}, fmt.Errorf("request command: %w: empty instruction", ErrInvalid)
	}
	if command.Reflex && (command.Kind != CommandSplice || strings.TrimSpace(command.Target) != "") {
		return Command{}, fmt.Errorf("request command: %w: reflex must be an untargeted splice", ErrInvalid)
	}
	if command.Kind != CommandSplice && strings.TrimSpace(command.Target) == "" {
		return Command{}, fmt.Errorf("request command: %w: %s requires a target node", ErrInvalid, command.Kind)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	defer tx.Rollback()

	if command.Target != "" {
		if err := requireNode(tx, command.Target); err != nil {
			return Command{}, fmt.Errorf("request command: %w", err)
		}
	}
	payload := commandPayload{
		SessionID:   command.SessionID,
		Kind:        command.Kind,
		Reflex:      command.Reflex,
		Target:      command.Target,
		Instruction: command.Instruction,
	}
	seq, at, err := appendEvent(tx, command.Target, EventCommandRequested, payload)
	if err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	if err := applyCommandView(tx, payload, seq, at); err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Command{}, fmt.Errorf("request command: %w", err)
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
		SELECT seq, ts, session_id, kind, reflex, target, instruction, status, result, updated_seq
		FROM commands WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	defer rows.Close()

	commands := make([]Command, 0)
	for rows.Next() {
		var command Command
		var reflex int
		var timestamp string
		if err := rows.Scan(&command.Seq, &timestamp, &command.SessionID, &command.Kind,
			&reflex, &command.Target, &command.Instruction, &command.Status, &command.Result,
			&command.UpdatedSeq); err != nil {
			return nil, fmt.Errorf("list commands: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("list commands: parse time: %w", err)
		}
		command.Reflex = reflex != 0
		command.Time = at
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	return commands, nil
}

func applyMessageView(tx *sql.Tx, payload messagePayload, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO messages (seq, ts, session_id, role, body, node_id, command_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), payload.SessionID, payload.Role, payload.Body,
		payload.NodeID, payload.CommandSeq)
	return err
}

func applyCommandView(tx *sql.Tx, payload commandPayload, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO commands (seq, ts, session_id, kind, reflex, target, instruction, status, result, updated_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?)`,
		seq, formatTime(at), payload.SessionID, payload.Kind, payload.Reflex, payload.Target,
		payload.Instruction, CommandPending, seq)
	return err
}

// migrateThreadSchema keeps command events from older stores replayable after
// reflex routing was added. The event payload defaults to false, so adding the
// materialized column is the whole migration.
func migrateThreadSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(commands)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "reflex" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE commands ADD COLUMN reflex INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1))`)
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
	return kind == CommandSplice || kind == CommandAmend || kind == CommandCancel
}
