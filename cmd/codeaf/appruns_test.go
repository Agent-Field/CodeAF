package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppRunsUsesInboxApproval(t *testing.T) {
	t.Run("Inbox", func(t *testing.T) { testAppRunApproval(t, false) })
	t.Run("Desk card", func(t *testing.T) { testAppRunApproval(t, true) })
}

func testAppRunApproval(t *testing.T, deskCard bool) {
	rec := newAppRecorder()
	backend := newAppBackend(rec, nil)
	fake := runs.NewFakeConnector(map[string]runs.Reply{"gmail.send": {Output: "sent"}})
	e, err := runs.Open(runs.Config{
		Root: t.TempDir(), Ask: backend.openRunApproval, Connector: fake,
		Load: func(_, _ string) (runs.Definition, error) {
			return runs.Parse([]byte("run: example\nsteps:\n  - id: send\n    tool: gmail.send"))
		},
		Changed: func(event runs.ChangeEvent) { backend.emit.Emit(appwire.EventRunsChanged, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	backend.appRuns.Store(e)
	r, err := backend.StartRun("app", "example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _, err := backend.ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(items) != 1 || items[0].Ask.Title != "Approval needed: send" {
		t.Fatalf("inbox = %+v, %v", items, err)
	}
	item := items[0]
	if len(item.Ask.Path) != 1 || item.Ask.Path[0] != "App" {
		t.Fatalf("approval should name its source in plain words, got %v", item.Ask.Path)
	}
	if _, err := backend.AnswerInbox(item.ID, appwire.OptionAllowOnce, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := backend.UndoInbox(item.ID); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != 0 {
		t.Fatal("a held answer released the connector before its hold ended")
	}
	if deskCard {
		wire := wireOver(t, backend)
		answer := map[string]string{
			"agent": item.Ask.Agent, "id": item.Ask.ID + "-stale", "option": appwire.OptionAllowOnce,
		}
		wire.mustFail("asks.answer", answer, appwire.CodeNotFound)
		if got, err := backend.GetRun("app", r.ID); err != nil || got.State != runs.YourCall || len(fake.Calls()) != 0 {
			t.Fatalf("stale approval changed the run: %+v, %v, calls = %v", got, err, fake.Calls())
		}
		answer["id"] = item.Ask.ID
		wire.must("asks.answer", answer, nil)
	} else if err := backend.answerInboxItem(item.ID, item.Ask.Agent, item.Ask.ID, appwire.AskAnswer{Option: appwire.OptionAllowOnce}); err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := backend.GetRun("app", r.ID); err != nil || got.State != runs.Done || len(fake.Calls()) != 1 {
		t.Fatalf("run = %+v, %v, calls = %v", got, err, fake.Calls())
	}
	items, _, err = backend.ListInbox(appwire.InboxDone, "")
	if err != nil || len(items) != 1 || items[0].Answered.Option != appwire.OptionAllowOnce {
		t.Fatalf("done = %+v, %v", items, err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data {
		if receipt, ok := data.(appwire.InboxResolvedEvent); ok && receipt.ID == item.ID {
			if receipt.Receipt == "" {
				t.Fatal("run approval closed without an Inbox receipt")
			}
			return
		}
	}
	t.Fatal("run approval did not publish its Inbox receipt")
}

func TestAppRunApprovalReopensFromItsJournal(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, time.November, 26, 14, 0, 0, 0, time.UTC)
	open := func() (*appBackend, *runs.Engine) {
		b := newAppBackend(newAppRecorder(), nil)
		e, err := runs.Open(runs.Config{
			Root: root, Ask: b.openRunApproval, Now: func() time.Time { return now },
			Load: func(_, _ string) (runs.Definition, error) {
				return runs.Parse([]byte("run: follow-up\ntitle: Follow up\nsteps:\n  - id: save\n    gate: {approval: proof}\n    records.create: {type: note, set: {body: Call Acme}}"))
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.appRuns.Store(e)
		b.restoreRunApprovals(e)
		return b, e
	}
	b, engine := open()
	r, err := b.StartRun("app", "follow-up", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	items, _, err := b.ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("inbox before restart = %v, %v", items, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Hour)
	b, engine = open()
	defer engine.Close()
	wire := wireOver(t, b)
	var page appwire.ItemsCountsResult
	wire.must("inbox.list", map[string]string{"list": "waiting"}, &page)
	if len(page.Items) != 1 || page.Items[0].ID != items[0].ID || !page.Items[0].At.Equal(items[0].At) {
		t.Fatalf("restored inbox = %+v, want original card %+v", page, items[0])
	}
	var target struct {
		AppRun string `json:"app_run"`
	}
	wire.must("inbox.open", map[string]string{"id": items[0].ID}, &target)
	if target.AppRun != r.ID {
		t.Fatalf("inbox.open = %+v, want run %s", target, r.ID)
	}
	var detail struct {
		Steps    []struct{ ID, Kind, State string }
		Approval struct {
			RecordType string `json:"record_type"`
			Args       map[string]any
		}
	}
	wire.must("runs.get", map[string]string{"id": r.ID}, &detail)
	if len(detail.Steps) != 1 || detail.Steps[0].ID != "save" || detail.Steps[0].Kind != "records_create" || detail.Steps[0].State != runs.YourCall || detail.Approval.RecordType != "note" || detail.Approval.Args["body"] != "Call Acme" {
		t.Fatalf("run detail = %+v", detail)
	}
}
