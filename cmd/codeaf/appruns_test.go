package main

import (
	"context"
	"testing"

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
