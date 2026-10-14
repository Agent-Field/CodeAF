package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// olderBuildsCharterSchema is the table every build before the v1 scheduler's
// removal created at open, spelled exactly as they spelled it. A store one of
// them made still has it, with whatever rows it held, and this build has to
// open that store, leave the table alone, and rebuild around it.
const olderBuildsCharterSchema = `
CREATE TABLE IF NOT EXISTS charters (
    id               TEXT PRIMARY KEY,
    invariant        TEXT NOT NULL,
    watch             JSON NOT NULL CHECK (json_valid(watch)),
    sentinel_hint     TEXT NOT NULL DEFAULT '',
    action            JSON NOT NULL CHECK (json_valid(action)),
    rails             JSON NOT NULL CHECK (json_valid(rails)),
    status            TEXT NOT NULL CHECK (status IN ('proposed', 'active', 'paused', 'retired')),
	autonomy          TEXT NOT NULL DEFAULT 'probation' CHECK (autonomy IN ('probation', 'tenured')),
	green_firings     INTEGER NOT NULL DEFAULT 0 CHECK (green_firings >= 0),
	demotions         INTEGER NOT NULL DEFAULT 0 CHECK (demotions >= 0),
    ratification      JSON NOT NULL CHECK (json_valid(ratification)),
	session_id        TEXT NOT NULL DEFAULT '',
	spec              JSON NOT NULL DEFAULT '{}' CHECK (json_valid(spec)),
	source_command_seq INTEGER NOT NULL DEFAULT 0,
    proposal_shape    TEXT NOT NULL DEFAULT '',
    last_wake         TEXT,
    next_due          TEXT,
    wake_seq          INTEGER NOT NULL DEFAULT 0,
    wake_pending      INTEGER NOT NULL DEFAULT 0 CHECK (wake_pending IN (0, 1)),
    sentinel_yes      INTEGER NOT NULL DEFAULT 0 CHECK (sentinel_yes IN (0, 1)),
    wake_evidence     TEXT NOT NULL DEFAULT '',
    last_checked      TEXT,
    last_check_line   TEXT NOT NULL DEFAULT '',
    file_fingerprint  TEXT NOT NULL DEFAULT '',
    graph_cursor      INTEGER NOT NULL DEFAULT 0,
    graph_day         TEXT NOT NULL DEFAULT '',
    graph_triggered   INTEGER NOT NULL DEFAULT 0 CHECK (graph_triggered IN (0, 1)),
    created_seq       INTEGER NOT NULL REFERENCES events(seq),
    updated_seq       INTEGER NOT NULL REFERENCES events(seq),
	created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS charters_due ON charters (status, next_due, created_seq);
CREATE VIRTUAL TABLE IF NOT EXISTS charters_fts USING fts5(
    charter_id UNINDEXED, invariant, tokenize='porter unicode61'
);
`

// TestAStoreTheRemovedSchedulerWroteStillOpensAndRebuilds is the data law for
// the v1 scheduler's removal: everything it journaled is still in the log and
// still in its table, nothing reads either, and `codeaf rebuild` replays the
// rest of the store exactly as it did — its events fall through replay as the
// unknown kinds they now are.
func TestAStoreTheRemovedSchedulerWroteStillOpensAndRebuilds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "older.db")
	graph := openTestStore(t, path)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "summarise the changelog", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "s", Intent: "summarise the changelog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.db.Exec(olderBuildsCharterSchema); err != nil {
		t.Fatal(err)
	}

	// What the scheduler left in the log, written the way it wrote it: a rule
	// stood up, woken, judged and fired, the offer to keep watching and its
	// answer, a wake pass's receipt — and, from the edges of the queue, one
	// change to the rule nobody applied and one question asked on its behalf.
	tx, err := graph.beginWrite()
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := appendEvent(tx, "charter-7", EventKind("charter_created"), map[string]any{
		"id": "charter-7", "invariant": "every monday, summarise the changelog", "status": "active",
	})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO charters (id, invariant, watch, action, rails, status, ratification,
		created_seq, updated_seq, created_at) VALUES (?, ?, '{}', '{}', '{}', 'active', '{}', ?, ?, ?)`,
		"charter-7", "every monday, summarise the changelog", created, created, "2026-09-01T09:00:00Z"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	for _, kind := range []string{"charter_woken", "sentinel_checked", "charter_fired", "charter_promoted",
		"charter_status_changed", "standing_watch_offered", "standing_watch_enabled", "standing_watch_pass"} {
		if _, _, err := appendEvent(tx, "charter-7", EventKind(kind), map[string]any{"wake_seq": 1}); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	command := commandPayload{SessionID: "s", Kind: "charter_ratify", Target: "charter-7", Instruction: "yes, stand this up"}
	commandSeq, commandAt, err := appendEvent(tx, "charter-7", EventCommandRequested, command)
	if err == nil {
		err = applyCommandView(tx, command, commandSeq, commandAt)
	}
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	asked := agentQuestionPayload{SessionID: "s", Text: "Stand this rule up?", OriginCharterID: "charter-7",
		Urgency: QuestionBlocking, Class: QuestionConsent,
		Options: []QuestionOption{{Label: "yes, stand this up", Value: "charter:ratify:charter-7"}}}
	questionSeq, questionAt, err := appendEvent(tx, "charter-7", EventAgentQuestionQueued, asked)
	if err == nil {
		err = applyAgentQuestionView(tx, asked, questionSeq, questionAt)
	}
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	if err := reopened.Rebuild(); err != nil {
		t.Fatalf("an older build's journal no longer rebuilds: %v", err)
	}
	if node, found, err := reopened.Node("job"); err != nil || !found || node.Provenance.Intent != "summarise the changelog" {
		t.Fatalf("the rebuild lost the person's own work: %+v found=%t err=%v", node, found, err)
	}
	var invariant string
	if err := reopened.db.QueryRow(`SELECT invariant FROM charters WHERE id = 'charter-7'`).Scan(&invariant); err != nil {
		t.Fatalf("the older build's table was not left alone: %v", err)
	}
	events, err := reopened.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	kept := 0
	for _, event := range events {
		if event.NodeID == "charter-7" {
			kept++
		}
	}
	if kept != 11 {
		t.Fatalf("the journal holds %d of the scheduler's 11 events; nothing may be thrown away", kept)
	}

	pending, err := reopened.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !RetiredSchedulerCommand(pending[0].Kind) {
		t.Fatalf("the unapplied rule change did not replay as a retired verb: %+v", pending)
	}
	questions, err := reopened.UnresolvedQuestions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || questions[0].OriginCharterID != "charter-7" {
		t.Fatalf("the question asked for the rule did not replay with its origin: %+v", questions)
	}
	var raw string
	if err := reopened.db.QueryRow(`SELECT payload FROM events WHERE seq = ?`, questionSeq).Scan(&raw); err != nil ||
		!json.Valid([]byte(raw)) || !strings.Contains(raw, "charter-7") {
		t.Fatalf("the question's queue event is gone or changed: %q %v", raw, err)
	}
}

// Nothing may start a new piece of the scheduler: a fresh store does not grow
// its table, the queue refuses its verbs, and a question cannot be asked on a
// standing rule's behalf.
func TestANewStoreHasNoPlaceForTheRemovedScheduler(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "fresh.db"))
	var tables int
	if err := graph.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('charters', 'charters_fts')`).
		Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("a fresh store created %d of the removed scheduler's tables", tables)
	}
	for _, kind := range []CommandKind{"charter_ratify", "charter_fire", "standing_watch_enable"} {
		if !RetiredSchedulerCommand(kind) {
			t.Fatalf("%s is not recognised as a retired verb", kind)
		}
		_, err := graph.RequestCommand(Command{SessionID: "s", Kind: kind, Target: "charter-1", Instruction: "yes"})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s was queued: err = %v", kind, err)
		}
	}
	if RetiredSchedulerCommand(CommandSplice) || RetiredSchedulerCommand(CommandHandover) {
		t.Fatal("a live verb reads as retired")
	}
	_, err := graph.AskQuestion(AgentQuestion{SessionID: "s", Text: "keep watching?",
		OriginCharterID: "charter-1", Urgency: QuestionBlocking})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a question was asked on a standing rule's behalf: err = %v", err)
	}
}
