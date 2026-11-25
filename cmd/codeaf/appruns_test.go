package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppRunsUsesInboxApproval(t *testing.T) {
	backend := newAppBackend(newAppRecorder(), nil)
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
	backend.appRuns = e
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
	if err := backend.answerInboxItem(item.ID, item.Ask.Agent, item.Ask.ID, appwire.AskAnswer{Option: appwire.OptionAllowOnce}); err != nil {
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
}
