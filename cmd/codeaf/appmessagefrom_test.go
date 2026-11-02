package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A MESSAGE SENT ON THE PERSON'S BEHALF SAYS WHO SENT IT. The model reads the
// words under one attribution line; the person's entry shows the words alone
// with from set, live and after a fresh backend reads the record; a message
// with no from carries none.
func TestAppAttributedMessageKeepsItsSender(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	rec := newAppRecorder()
	first := newAppBackend(rec, appOpenInProcess)
	closed := false
	defer func() {
		if !closed {
			first.Close()
		}
	}()
	agent, err := mustCreate(t, first, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	before := len(appModelRequests(model))
	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: "tidy the docs", From: "afi"}))
	rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Kind == appwire.EntryUser && added.Entry.From == "afi" && added.Entry.Text == "tidy the docs"
	})
	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: "my own words"}))
	attributed := false
	for _, request := range appModelRequests(model)[before:] {
		joined := strings.Join(request.texts(), "\n")
		attributed = attributed || strings.Contains(joined, appAttribution("afi")+"\ntidy the docs")
		if strings.Contains(joined, appAttribution("afi")+"\nmy own words") {
			t.Fatal("the person's own message was attributed")
		}
	}
	if !attributed {
		t.Fatal("the model was not told who sent the message")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	second := newAppBackend(newAppRecorder(), appOpenInProcess)
	defer second.Close()
	entries, _, err := second.Entries(agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	users := appUsersOf(entries)
	if len(users) != 2 || users[0].Text != "tidy the docs" || users[0].From != "afi" || users[1].From != "" {
		t.Fatalf("the reopened record holds %+v", users)
	}
}
