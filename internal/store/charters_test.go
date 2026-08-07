package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCharterSpecDissolvesIntoCanonicalLifecycle(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "charter-spec.db"))
	spec := CharterSpec{
		Invariant: "Whenever a backend PR opens, review it.",
		Watch: CharterWatch{
			Kind: WatchCron, Cadence: "every morning", Schedule: "0 9 * * *",
		},
		Sentinel: "Is there a new backend PR?",
		Action:   "Review the new backend PR.",
		Rails: CharterSpecRails{
			EstimatedCostUSD: 0.08, MaxPerDay: 10,
			MaxPerDayJustification: "caps the default worst day at about $0.80",
			Expiry:                 "never",
		},
	}
	charter, err := graph.DraftCharter("backend-prs", "standing", 17, spec)
	if err != nil {
		t.Fatal(err)
	}
	if charter.Status != CharterProposed {
		t.Fatalf("draft status = %q, want %q", charter.Status, CharterProposed)
	}
	if charter.Watch.Kind != WatchCron || charter.Watch.Cron == nil ||
		charter.Watch.Cron.Kind != CronDaily || charter.Watch.Cron.Hour != 9 ||
		charter.Watch.Cadence != "every morning" {
		t.Fatalf("cadence words did not compile to a typed schedule: %+v", charter.Watch)
	}
	if rails := charter.Rails(); rails.PerFiringBudgetUSD != 0.08 || rails.MaxFiringsPerDay != 10 ||
		rails.ExpiresAt != nil {
		t.Fatalf("spec rails did not dissolve: %+v", charter.Rails())
	}
	again, err := graph.DraftCharter("backend-prs", "standing", 17, spec)
	if err != nil || again.CreatedSeq != charter.CreatedSeq {
		t.Fatalf("retried draft did not converge: %+v err=%v", again, err)
	}
	active, err := graph.ActiveCharters()
	if err != nil || len(active) != 0 {
		t.Fatalf("draft silently armed: active=%+v err=%v", active, err)
	}
	if err := graph.SetCharterStatus(charter.ID, CharterActive, Ratification{
		Origin: OriginUser, SessionID: "standing", Evidence: "yes, stand this up",
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := graph.SearchActiveCharters("backend reviews")
	if err != nil || len(matches) != 1 || matches[0].ID != charter.ID {
		t.Fatalf("BM25 matches = %+v err=%v", matches, err)
	}
	armed := matches[0]
	if err := graph.ReviseCharter(charter.ID, armed.Invariant,
		RetimeWatch(armed.Watch, "hourly", time.Now()), armed.SentinelHint,
		armed.Action, armed.Rails()); err != nil {
		t.Fatal(err)
	}
	before, found, err := graph.Charter(charter.ID)
	if err != nil || !found {
		t.Fatalf("charter before rebuild = %+v found=%t err=%v", before, found, err)
	}
	if before.Watch.Cron == nil || before.Watch.Cron.Kind != CronEveryHours ||
		before.Watch.Cron.Interval != 1 || before.Watch.Cadence != "hourly" {
		t.Fatalf("cadence edit did not produce a typed hourly schedule: %+v", before.Watch)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, found, err := graph.Charter(charter.ID)
	if err != nil || !found || !reflect.DeepEqual(after, before) {
		t.Fatalf("charter after rebuild = %+v, want %+v found=%t err=%v", after, before, found, err)
	}
	matches, err = graph.SearchActiveCharters("backend reviews")
	if err != nil || len(matches) != 1 || matches[0].ID != charter.ID {
		t.Fatalf("BM25 matches after rebuild = %+v err=%v", matches, err)
	}

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []EventKind
	for _, event := range events {
		switch event.Kind {
		case EventCharterCreated, EventCharterStatusChanged, EventCharterRevised:
			lifecycle = append(lifecycle, event.Kind)
		}
	}
	wantEvents := []EventKind{EventCharterCreated, EventCharterStatusChanged, EventCharterRevised}
	if !reflect.DeepEqual(lifecycle, wantEvents) {
		t.Fatalf("charter lifecycle events = %v, want %v", lifecycle, wantEvents)
	}
}

func TestReminderSpecCompilesToAtScheduleWithExpiry(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "reminder-spec.db"))
	charter, err := graph.DraftCharter("call-mom", "chat", 3, CharterSpec{
		Invariant: "Remind me tomorrow at 9 to call Mom.",
		Watch:     CharterWatch{Kind: WatchCron, Cadence: "tomorrow at 9"},
		Sentinel:  "Is it time for the reminder?",
		Action:    "Say: call Mom.",
		SayOnly:   true,
		Rails: CharterSpecRails{
			EstimatedCostUSD: 0.02, MaxPerDay: 1,
			MaxPerDayJustification: "one firing matches the reminder's once expiry",
			Expiry:                 "once",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if charter.Watch.Cron == nil || charter.Watch.Cron.Kind != CronAt {
		t.Fatalf("reminder watch = %+v, want a cron at-schedule", charter.Watch)
	}
	at := charter.Watch.Cron.At
	tomorrow := time.Now().AddDate(0, 0, 1)
	if at.Hour() != 9 || at.Minute() != 0 || at.Day() != tomorrow.Day() {
		t.Fatalf("reminder instant = %v, want tomorrow 09:00", at)
	}
	rails := charter.Rails()
	if rails.ExpiresAt == nil || !rails.ExpiresAt.After(at) || rails.MaxFiringsPerDay != 1 {
		t.Fatalf("reminder rails = %+v, want expiry after one firing", rails)
	}
	if !charter.Action.SayOnly || charter.Action.Template != "call Mom." {
		t.Fatalf("reminder action = %+v, want say-only template", charter.Action)
	}
	next, err := NextCronDue(*charter.Watch.Cron, at)
	if err != nil || !next.After(rails.ExpiresAt.Add(24*time.Hour)) {
		t.Fatalf("post-firing due = %v err=%v, want parked beyond expiry", next, err)
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
		Rails: CharterSpecRails{EstimatedCostUSD: 0.02, MaxPerDay: 1,
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

func TestUnlimitedTodayRailAdmitsCharterFirings(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "unlimited-firings.db"))
	charter := mustTestCharter(t, "charter-unlimited", CharterActive, CharterRails{
		PerFiringBudgetUSD: 5, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	// The rail is what this test exercises; tenure the charter so the autonomy
	// gate does not intercept the firing first.
	if err := graph.PromoteCharter(charter.ID, "test tenure", true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	wakeSeq, err := graph.BeginCharterWake(charter.ID, now, "poll due", CharterWatchState{
		NextDue: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSentinelCheck(charter.ID, SentinelCheck{
		WakeSeq: wakeSeq, Yes: true, Line: "condition occurred",
	}); err != nil {
		t.Fatal(err)
	}
	subtree := Subtree{Nodes: []NodeSpec{{ID: "firing-unlimited-1", Brief: "Update docs", Stage: 1}}}
	provenance := Provenance{Origin: OriginTrigger, SessionID: "charter-session",
		Intent: "Update docs", CharterID: charter.ID}

	// A $1 daily rail cannot admit a $5 firing…
	disposition, err := graph.FireCharter(charter.ID, wakeSeq, subtree, provenance, 1, now)
	if err != nil || disposition != FireRailWait {
		t.Fatalf("firing under a tiny rail = %q err=%v, want %q", disposition, err, FireRailWait)
	}
	// …until the user raises today to unlimited; then projected spend never
	// defers a firing for the rest of the day.
	if err := graph.RaiseDailyRailUnlimited("slash:/budget unlimited today"); err != nil {
		t.Fatal(err)
	}
	disposition, err = graph.FireCharter(charter.ID, wakeSeq, subtree, provenance, 1, now)
	if err != nil || disposition != FireAdmitted {
		t.Fatalf("firing under an unlimited rail = %q err=%v, want %q", disposition, err, FireAdmitted)
	}
}
