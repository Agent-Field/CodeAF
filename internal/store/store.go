// Package store owns Aforge's durable, append-only task graph.
//
// Events are the source of truth. Nodes and edges are queryable materialized
// views updated in the same SQLite transaction as the event that changed them.
// Any process may open the database: WAL keeps readers independent, and claim
// tokens make worker ownership a compare-and-swap rather than process state.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// RootID is the one permanent spine root. It is created with a new store and
	// is never itself scheduled or folded.
	RootID = "root"

	// MaxDigestBytes keeps summaries small enough to route through the graph.
	// Large results belong in the content-addressed store and are referenced by
	// pointers instead.
	MaxDigestBytes = 4 << 10
)

// Status is the scheduling state of a node.
type Status string

const (
	Pending   Status = "pending"
	Claimed   Status = "claimed"
	Running   Status = "running"
	Done      Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Origin says who introduced a subtree onto the spine.
type Origin string

const (
	OriginUser    Origin = "user"
	OriginTrigger Origin = "trigger"
	OriginSelf    Origin = "self"
)

// EdgeKind says how one node bears on another. FeedsInto and Blocks are hard
// scheduling dependencies; Suggests routes a soft hint and never delays work.
type EdgeKind string

const (
	FeedsInto EdgeKind = "feeds_into"
	Blocks    EdgeKind = "blocks"
	Suggests  EdgeKind = "suggests"
)

// EventKind names state transitions in the append-only journal.
type EventKind string

const (
	EventSpineCreated   EventKind = "spine_created"
	EventSubtreeSpliced EventKind = "subtree_spliced"
	EventNodeClaimed    EventKind = "node_claimed"
	EventNodeStarted    EventKind = "node_started"
	EventNodeCompleted  EventKind = "node_completed"
	EventNodeFailed     EventKind = "node_failed"
	EventNodeReleased   EventKind = "node_released"
	EventSubtreeFolded  EventKind = "subtree_folded"

	// Thread events: the conversation and its asynchronous mutation requests
	// live in the same journal as the graph they act on.
	EventMessagePosted    EventKind = "message_posted"
	EventCommandRequested EventKind = "command_requested"
	EventCommandResolved  EventKind = "command_resolved"

	// EventUsageRecorded is one executed node's spend.
	EventUsageRecorded EventKind = "usage_recorded"

	// EventFactLearned is one durable fact distilled from finished work.
	EventFactLearned EventKind = "fact_learned"
)

var (
	ErrNotFound    = errors.New("node not found")
	ErrClaimLost   = errors.New("claim is stale or no longer owned")
	ErrNotReady    = errors.New("node is not ready")
	ErrInvalid     = errors.New("invalid graph mutation")
	ErrOpenChild   = errors.New("node has an open child")
	ErrOpenSubtree = errors.New("subtree is not complete")
)

// Provenance is stamped onto every node admitted by one splice. Intent is
// deliberately stored verbatim: later planning and folding may interpret it,
// but the store never rewrites what was asked.
type Provenance struct {
	Origin    Origin `json:"origin"`
	SessionID string `json:"session_id,omitempty"`
	Intent    string `json:"intent"`
}

// Need is one incoming edge named by a node specification.
type Need struct {
	NodeID string   `json:"node_id"`
	Kind   EdgeKind `json:"kind"`
}

// NodeSpec is one node to admit. Exactly one node in a Subtree has an empty
// Parent; Splice attaches that node to the parent argument. Every other Parent
// names another node in the same subtree.
type NodeSpec struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Brief  string `json:"brief"`
	Stage  int    `json:"stage"`
	Needs  []Need `json:"needs,omitempty"`
}

// Subtree is the atomic unit of admission.
type Subtree struct {
	Nodes []NodeSpec `json:"nodes"`
}

// Node is the durable scheduling view of one graph node.
type Node struct {
	ID         string
	Parent     string
	Brief      string
	Stage      int
	Status     Status
	Owner      string
	ClaimToken uint64
	Attempt    uint64
	Summary    string
	Error      string

	Provenance   Provenance
	CreatedSeq   int64
	CreatedOrder int
	UpdatedSeq   int64
	StartedAt    time.Time
	FinishedAt   time.Time

	// Folded marks historical nodes replaced in the active view. FoldRoot is
	// the compact representative that remains visible in place of its subtree.
	Folded       bool
	FoldRoot     bool
	FoldDigest   string
	FoldPointers []string
}

// Edge points from an input to the node that consumes or is constrained by it.
type Edge struct {
	From         string
	To           string
	Kind         EdgeKind
	CreatedSeq   int64
	CreatedOrder int
}

// Event is one immutable journal entry.
type Event struct {
	Seq     int64
	Time    time.Time
	NodeID  string
	Kind    EventKind
	Payload json.RawMessage
}

// Claim is the complete authority a worker needs to mutate one claimed node.
// Both owner and token must continue to match; release and reassignment make an
// older Claim permanently unusable.
type Claim struct {
	ID    string
	Owner string
	Token uint64
}

// Snapshot is a deterministic copy of the full materialized views, including
// folded historical nodes. It is useful for inspection and rebuild checks.
type Snapshot struct {
	Nodes []Node
	Edges []Edge
}

// Store is one handle onto the shared SQLite graph.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
    seq       INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        TEXT NOT NULL,
    node_id   TEXT NOT NULL,
    kind      TEXT NOT NULL,
    payload   JSON NOT NULL CHECK (json_valid(payload))
);

CREATE TABLE IF NOT EXISTS nodes (
    id             TEXT PRIMARY KEY,
    parent_id      TEXT REFERENCES nodes(id),
    brief          TEXT NOT NULL,
    stage          INTEGER NOT NULL CHECK (stage >= 0),
    status         TEXT NOT NULL CHECK (status IN ('pending', 'claimed', 'running', 'done', 'failed', 'cancelled')),
    owner          TEXT NOT NULL DEFAULT '',
    claim_token    INTEGER NOT NULL DEFAULT 0 CHECK (claim_token >= 0),
    attempt        INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    summary        TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    origin         TEXT NOT NULL CHECK (origin IN ('user', 'trigger', 'self')),
    session_id     TEXT,
    intent         TEXT NOT NULL,
    created_seq    INTEGER NOT NULL REFERENCES events(seq),
    created_order  INTEGER NOT NULL CHECK (created_order >= 0),
    updated_seq    INTEGER NOT NULL REFERENCES events(seq),
    started_at     TEXT,
    finished_at    TEXT,
    folded         INTEGER NOT NULL DEFAULT 0 CHECK (folded IN (0, 1)),
    fold_root      INTEGER NOT NULL DEFAULT 0 CHECK (fold_root IN (0, 1)),
    fold_digest    TEXT NOT NULL DEFAULT '',
    fold_pointers  JSON NOT NULL DEFAULT '[]' CHECK (json_valid(fold_pointers)),
    CHECK (fold_root = 0 OR folded = 1)
);

CREATE TABLE IF NOT EXISTS edges (
    from_id      TEXT NOT NULL REFERENCES nodes(id),
    to_id        TEXT NOT NULL REFERENCES nodes(id),
    kind         TEXT NOT NULL CHECK (kind IN ('feeds_into', 'blocks', 'suggests')),
    created_seq  INTEGER NOT NULL REFERENCES events(seq),
    created_order INTEGER NOT NULL CHECK (created_order >= 0),
    PRIMARY KEY (from_id, to_id, kind)
);

CREATE UNIQUE INDEX IF NOT EXISTS nodes_one_spine_root
    ON nodes ((1)) WHERE parent_id IS NULL;
CREATE INDEX IF NOT EXISTS nodes_parent ON nodes (parent_id);
CREATE INDEX IF NOT EXISTS nodes_ready ON nodes (status, folded, created_seq, created_order);
CREATE INDEX IF NOT EXISTS edges_to_kind ON edges (to_id, kind);
CREATE INDEX IF NOT EXISTS events_node_seq ON events (node_id, seq);

CREATE TRIGGER IF NOT EXISTS events_no_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TRIGGER IF NOT EXISTS events_no_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;
`

type spinePayload struct {
	ID         string     `json:"id"`
	Brief      string     `json:"brief"`
	Provenance Provenance `json:"provenance"`
}

// Open opens or creates the store at path. WAL is persistent database state;
// busy_timeout and foreign keys are connection-local and therefore live in the
// DSN so every pooled connection receives them.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("open store: %w: empty path", ErrInvalid)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	u := url.URL{Scheme: "file", Path: absolute}
	query := u.Query()
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	closeOnError := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return closeOnError(fmt.Errorf("open store: %w", err))
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&journalMode); err != nil {
		return closeOnError(fmt.Errorf("enable WAL: %w", err))
	}
	if !strings.EqualFold(journalMode, "wal") {
		return closeOnError(fmt.Errorf("enable WAL: SQLite selected %q", journalMode))
	}
	if _, err := db.Exec(schema); err != nil {
		return closeOnError(fmt.Errorf("initialize store schema: %w", err))
	}
	if _, err := db.Exec(threadSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize thread schema: %w", err))
	}
	if _, err := db.Exec(usageSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize usage schema: %w", err))
	}
	if _, err := db.Exec(factsSchema); err != nil {
		return closeOnError(fmt.Errorf("initialize facts schema: %w", err))
	}

	store := &Store{db: db}
	if err := store.ensureSpine(); err != nil {
		return closeOnError(err)
	}
	return store, nil
}

// Close releases this process's connections. The database remains immediately
// resumable by any other handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSpine() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	defer tx.Rollback()

	var eventCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	if eventCount == 0 {
		payload := spinePayload{
			ID:    RootID,
			Brief: "Permanent Aforge spine",
			Provenance: Provenance{
				Origin: OriginSelf,
				Intent: "permanent spine root",
			},
		}
		seq, at, err := appendEvent(tx, RootID, EventSpineCreated, payload)
		if err != nil {
			return fmt.Errorf("initialize spine: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO nodes (
			    id, parent_id, brief, stage, status, origin, session_id,
			    intent, created_seq, created_order, updated_seq, started_at
			) VALUES (?, NULL, ?, 0, ?, ?, NULL, ?, ?, 0, ?, ?)`,
			RootID, payload.Brief, Running, payload.Provenance.Origin,
			payload.Provenance.Intent, seq, seq, formatTime(at)); err != nil {
			return fmt.Errorf("initialize spine view: %w", err)
		}
	} else {
		var roots, spine int
		if err := tx.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE id = ?) FROM nodes WHERE parent_id IS NULL`, RootID).Scan(&roots, &spine); err != nil {
			return fmt.Errorf("validate spine: %w", err)
		}
		if roots != 1 || spine != 1 {
			return fmt.Errorf("validate spine: materialized view has %d roots (%d permanent); run Rebuild", roots, spine)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("initialize spine: %w", err)
	}
	return nil
}

func appendEvent(tx *sql.Tx, nodeID string, kind EventKind, payload any) (int64, time.Time, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("encode %s event: %w", kind, err)
	}
	at := time.Now().UTC()
	result, err := tx.Exec(`INSERT INTO events (ts, node_id, kind, payload) VALUES (?, ?, ?, ?)`,
		formatTime(at), nodeID, kind, string(encoded))
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("append %s event: %w", kind, err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("read %s sequence: %w", kind, err)
	}
	return seq, at, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func terminal(status Status) bool {
	return status == Done || status == Failed || status == Cancelled
}

func validOrigin(origin Origin) bool {
	return origin == OriginUser || origin == OriginTrigger || origin == OriginSelf
}

func validEdgeKind(kind EdgeKind) bool {
	return kind == FeedsInto || kind == Blocks || kind == Suggests
}
