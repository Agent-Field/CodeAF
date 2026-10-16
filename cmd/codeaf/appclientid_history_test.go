package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A CLIENT ID IS A FACT OF THE RECORD, NOT OF THE PROCESS. A real conversation
// against a scripted model is sent ordinary words with the app's id and no mark
// beside them; the backend is closed and a NEW one, whose process-local map is
// empty, reads the entries back off the disk. The id is on the person's line;
// the words are exactly what was typed; a message sent with none has none; and
// the model is sent the same words either way.
func TestAppOrdinaryClientIDSurvivesABackendRestart(t *testing.T) {
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
	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: "named words", ClientID: "p-1"}))
	appTurnDoneFor(t, rec, appSendMessageWhenIdle(t, first, agent.ID, appwire.AgentMessage{Text: "anonymous words"}))
	// An id the record would not keep refuses the message before the model or
	// the record hears of it.
	asked := len(appModelRequests(model))
	_, err = first.SendMessage(agent.ID, appwire.AgentMessage{Text: "too named", ClientID: strings.Repeat("i", session.MaxClientIDBytes+1)})
	if wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("an oversize client id answered %v", err)
	}
	if got := len(appModelRequests(model)); got != asked {
		t.Fatalf("a refused message asked the model %d times", got-asked)
	}
	named, anonymous := false, false
	for _, request := range appModelRequests(model)[before:] {
		joined := strings.Join(request.texts(), "\n")
		named = named || strings.Contains(joined, "named words")
		anonymous = anonymous || strings.Contains(joined, "anonymous words")
		if strings.Contains(joined, "p-1") {
			t.Fatalf("the client id reached the model: %q", joined)
		}
	}
	if !named || !anonymous {
		t.Fatal("the model was not sent both messages' words")
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
	if len(users) != 2 {
		t.Fatalf("the reopened record holds %d messages of the person's: %+v", len(users), users)
	}
	if users[0].Text != "named words" || users[0].ClientID != "p-1" {
		t.Fatalf("the named message reopened as %+v", users[0])
	}
	if users[1].Text != "anonymous words" || users[1].ClientID != "" {
		t.Fatalf("the anonymous message reopened as %+v", users[1])
	}
}

// THE RECORD'S ID IS READ FIRST; THE PROCESS-LOCAL ONE ONLY WHERE THE RECORD
// HAS NONE. Nothing is matched up by turn, time or words.
func TestAppEntryShapesPreferTheRecordedClientID(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	marks := map[int]appTurnMark{0: {turn: 7, clientID: "local-0"}, 1: {turn: 8, clientID: "local-1"}}
	items := []session.DisplayEntry{
		{Role: "user", Text: "a", Person: &session.PersonMark{ClientID: "recorded-0"}},
		{Role: "user", Text: "b", Person: &session.PersonMark{}},
		{Role: "user", Text: "c"},
	}
	entries, _ := appEntryShapes("ag", items, at, marks)
	if len(entries) != 3 || entries[0].ClientID != "recorded-0" || entries[0].Turn != 7 || entries[1].ClientID != "local-1" || entries[2].ClientID != "" {
		t.Fatalf("entries = %+v", entries)
	}
}
