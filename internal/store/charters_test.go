package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCharterLifecycleIsJournaledAndRebuildable(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "charters.db"))
	spec := CharterSpec{
		Invariant: "Whenever a backend PR opens, review it.",
		Watch: CharterWatch{
			Kind: WatchCron, Cadence: "every morning", Schedule: "0 9 * * *",
		},
		Sentinel: "Is there a new backend PR?",
		Action:   "Review the new backend PR.",
		Rails: CharterRails{
			EstimatedCostUSD: 0.08, MaxPerDay: 10,
			MaxPerDayJustification: "caps the default worst day at about $0.80",
			Expiry:                 "never",
		},
	}
	charter, err := graph.DraftCharter("backend-prs", "standing", 17, spec)
	if err != nil {
		t.Fatal(err)
	}
	if charter.Status != CharterDraft {
		t.Fatalf("draft status = %q, want %q", charter.Status, CharterDraft)
	}
	active, err := graph.ActiveCharters()
	if err != nil || len(active) != 0 {
		t.Fatalf("draft silently armed: active=%+v err=%v", active, err)
	}
	if err := graph.RatifyCharter(charter.ID); err != nil {
		t.Fatal(err)
	}
	matches, err := graph.SearchActiveCharters("backend reviews")
	if err != nil || len(matches) != 1 || matches[0].ID != charter.ID {
		t.Fatalf("BM25 matches = %+v err=%v", matches, err)
	}
	if err := graph.EditCharterCadence(charter.ID, "hourly", "0 * * * *"); err != nil {
		t.Fatal(err)
	}
	before, found, err := graph.CharterByID(charter.ID)
	if err != nil || !found {
		t.Fatalf("charter before rebuild = %+v found=%t err=%v", before, found, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, found, err := graph.CharterByID(charter.ID)
	if err != nil || !found || !reflect.DeepEqual(after, before) {
		t.Fatalf("charter after rebuild = %+v, want %+v found=%t err=%v", after, before, found, err)
	}

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []EventKind
	for _, event := range events {
		switch event.Kind {
		case EventCharterDrafted, EventCharterRatified, EventCharterCadenceEdited:
			lifecycle = append(lifecycle, event.Kind)
		}
	}
	wantEvents := []EventKind{EventCharterDrafted, EventCharterRatified, EventCharterCadenceEdited}
	if !reflect.DeepEqual(lifecycle, wantEvents) {
		t.Fatalf("charter lifecycle events = %v, want %v", lifecycle, wantEvents)
	}
}

func TestQuestionOptionsPersistInOrderAndExpireAfterAUserTurn(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "questions.db"))
	want := []QuestionOption{
		{Label: "first choice", Value: "first"},
		{Label: "second choice", Value: "second"},
	}
	question, err := graph.PostMessage(Message{
		SessionID: "question", Role: RoleAgent, Body: "Pick one?", Options: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := graph.Messages("question", 0, 0)
	if err != nil || len(read) != 1 || !reflect.DeepEqual(read[0].Options, want) {
		t.Fatalf("persisted question = %+v err=%v", read, err)
	}
	user, err := graph.PostMessage(Message{SessionID: "question", Role: RoleUser, Body: "2"})
	if err != nil {
		t.Fatal(err)
	}
	pending, found, err := graph.PendingQuestion("question", user.Seq)
	if err != nil || !found || pending.Seq != question.Seq || !reflect.DeepEqual(pending.Options, want) {
		t.Fatalf("pending question = %+v found=%t err=%v", pending, found, err)
	}
	later, err := graph.PostMessage(Message{SessionID: "question", Role: RoleUser, Body: "another turn"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := graph.PendingQuestion("question", later.Seq); err != nil || found {
		t.Fatalf("consumed question remained pending: found=%t err=%v", found, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	read, err = graph.Messages("question", 0, 0)
	if err != nil || len(read) != 3 || !reflect.DeepEqual(read[0].Options, want) {
		t.Fatalf("rebuilt question options = %+v err=%v", read, err)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var encoded []QuestionOption
	for _, event := range events {
		if event.Kind != EventMessagePosted || event.Seq != question.Seq {
			continue
		}
		var payload messagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		encoded = payload.Options
	}
	if !reflect.DeepEqual(encoded, want) {
		t.Fatalf("journaled options = %+v, want %+v", encoded, want)
	}
}

func TestLegacyThreadSchemaMigratesForOptionsAndCharterCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-thread.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = graph.db.Exec(`
		DROP INDEX messages_session_seq;
		ALTER TABLE messages RENAME TO messages_current;
		CREATE TABLE messages (
		    seq INTEGER PRIMARY KEY REFERENCES events(seq), ts TEXT NOT NULL,
		    session_id TEXT NOT NULL DEFAULT '',
		    role TEXT NOT NULL CHECK (role IN ('user', 'agent', 'system')),
		    body TEXT NOT NULL, node_id TEXT NOT NULL DEFAULT '',
		    command_seq INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO messages SELECT seq, ts, session_id, role, body, node_id,
		    command_seq FROM messages_current;
		DROP TABLE messages_current;
		CREATE INDEX messages_session_seq ON messages (session_id, seq);

		DROP INDEX commands_status_seq;
		ALTER TABLE commands RENAME TO commands_current;
		CREATE TABLE commands (
		    seq INTEGER PRIMARY KEY REFERENCES events(seq), ts TEXT NOT NULL,
		    session_id TEXT NOT NULL DEFAULT '',
		    kind TEXT NOT NULL CHECK (kind IN ('splice', 'amend', 'cancel')),
		    reflex INTEGER NOT NULL DEFAULT 0 CHECK (reflex IN (0, 1)),
		    target TEXT NOT NULL DEFAULT '', instruction TEXT NOT NULL,
		    status TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
		    result TEXT NOT NULL DEFAULT '', updated_seq INTEGER NOT NULL
		);
		INSERT INTO commands SELECT seq, ts, session_id, kind, reflex, target,
		    instruction, status, result, updated_seq FROM commands_current;
		DROP TABLE commands_current;
		CREATE INDEX commands_status_seq ON commands (status, seq);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if found, err := tableHasColumn(reopened.db, "messages", "options"); err != nil || !found {
		t.Fatalf("message options migration: found=%t err=%v", found, err)
	}
	charter, err := reopened.DraftCharter("legacy-charter", "legacy", 0, CharterSpec{
		Invariant: "Every day verify the backup.",
		Watch:     CharterWatch{Kind: WatchCron, Cadence: "every day", Schedule: "0 9 * * *"},
		Sentinel:  "Is today's backup verified?", Action: "Verify the backup.",
		Rails: CharterRails{EstimatedCostUSD: 0.02, MaxPerDay: 1,
			MaxPerDayJustification: "one daily check", Expiry: "never"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.RequestCommand(Command{
		SessionID: "legacy", Kind: CommandCharterRatify, Target: charter.ID,
		Instruction: "yes, stand this up",
	}); err != nil {
		t.Fatalf("charter command after migration: %v", err)
	}
}
