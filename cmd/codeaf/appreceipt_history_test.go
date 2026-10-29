package main

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// AN ANSWERED ASK LEAVES THE SAME RECEIPT AFTER A RELAUNCH, once, in the
// place it appeared live, including its original identity and instant.
func TestAppAnsweredReceiptReadsBackAfterARelaunch(t *testing.T) {
	for _, name := range []string{"choice", "choice without a call", "saved scope"} {
		t.Run(name, func(t *testing.T) {
			appDayProfile(t)
			at := time.Date(2026, 10, 29, 19, 46, 12, 345678900, time.UTC)
			q := session.Question{
				ID: 7, Kind: session.QuestionAsk, Ask: session.AskChoice, Head: "Which database?",
				Options: []session.AnswerOption{{Key: "sqlite", Label: "SQLite"}, {Key: "jsonl", Label: "JSONL"}},
				Subject: session.SubjectRef{Kind: session.SubjectCall, CallID: "choose", Name: "ask"},
			}
			tool, option, scope, wantText := "ask", "sqlite", "", "Answered · you · "
			if name == "choice without a call" {
				q.Subject = session.SubjectRef{}
			}
			if name == "saved scope" {
				tool, option, scope, wantText = "bash", appwire.OptionAlways, "printf receipt", "Always allowed · printf receipt · you · "
				q.Kind, q.Ask = session.QuestionConsent, ""
				q.Options = session.AnswerOptions(session.QuestionConsent)
				q.ConsentScopes = []string{scope}
				q.Subject.Name = tool
			}
			hold := make(chan struct{})
			var release sync.Once
			releaseTurn := func() { release.Do(func() { close(hold) }) }
			conv := &fakeConversation{hold: hold, script: []session.Event{
				{Kind: session.EventConsentRequest, ID: q.ID, Tool: tool, Args: `{"command":"printf receipt"}`},
				{Kind: session.EventToolBegin, CallID: "choose", Tool: tool, Args: `{"command":"printf receipt"}`},
			}}
			conv.onAnswer = func(answer session.Answer) {
				conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
			}
			rec := newAppRecorder()
			var transcript string
			first := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				transcript = ask.Transcript
				return &appConn{Conv: conv, Transcript: ask.Transcript, Workspace: ask.Workspace, Release: func() {}}, nil
			})
			first.now = func() time.Time { return at }
			t.Cleanup(func() { _ = first.Close() })
			// Release the scripted turn even if an assertion fails before shutdown.
			defer releaseTurn()
			agent, err := mustCreate(t, first, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := first.Send(agent.ID, "choose a database", ""); err != nil {
				t.Fatal(err)
			}
			rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
				row, ok := data.(appwire.TurnTool)
				return event == appwire.EventTurnTool && ok && row.Call == "choose" && row.Phase == appwire.PhaseBegin
			})
			// The engine writes the call before publishing its question. Keep a
			// real journal so a fresh backend must discover and read it from disk.
			journal := `{"type":"session","version":1,"id":"` + agent.ID + `"}` + "\n" +
				`{"type":"message","role":"user","content":"choose a database"}` + "\n" +
				`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"choose","function":{"name":"` + tool + `","arguments":"{}"}}]}` + "\n"
			if err := os.WriteFile(transcript, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
			if err := first.AnswerAskWith(agent.ID, appAskID(q), appwire.AskAnswer{Option: option, Scope: scope}); err != nil {
				t.Fatal(err)
			}
			if scope != "" {
				conv.raise(session.Event{Kind: session.EventQuestionWithdrawn, Question: &q})
			}
			i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
				added, ok := data.(appwire.EntryAdded)
				return event == appwire.EventEntryAdded && ok && added.Entry.Kind == appwire.EntryReceipt
			})
			rec.mu.Lock()
			live := rec.data[i].(appwire.EntryAdded).Entry
			previous := ""
			for _, data := range rec.data[:i] {
				switch row := data.(type) {
				case appwire.EntryAdded:
					previous = row.Entry.ID
				case appwire.TurnTool:
					previous = "t" + row.Call
				}
			}
			rec.mu.Unlock()
			if live.ID != "r"+appAskID(q) || !live.At.Equal(at) || !strings.HasPrefix(live.Text, wantText) || previous != "tchoose" {
				t.Fatalf("live receipt = %+v, after %q", live, previous)
			}
			if scope != "" && (live.Link == nil || live.Link.Target != "settings:permissions") {
				t.Fatalf("saved scope receipt has no settings link: %+v", live)
			}
			// Later journal entries must not pull the receipt away from its call.
			journal += `{"type":"message","role":"assistant","content":"SQLite it is."}` + "\n"
			if err := os.WriteFile(transcript, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			// Finish the scripted turn before closing the backend.
			releaseTurn()
			rec.waitFor(t, 5*time.Second, appTurnDone)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			fresh := newAppBackend(newAppRecorder(), nil)
			defer fresh.Close()
			entries, more, err := fresh.Entries(agent.ID, "", appwire.MaxEntries)
			if err != nil || more {
				t.Fatalf("reopened entries = %+v, more %v, %v", entries, more, err)
			}
			found := 0
			for i, entry := range entries {
				if entry.ID != live.ID {
					continue
				}
				found++
				if !reflect.DeepEqual(entry, live) || i == 0 || entries[i-1].ID != previous || i+1 == len(entries) || entries[i+1].Text != "SQLite it is." {
					t.Fatalf("reopened receipt or position differs: live = %+v, entries = %+v", live, entries)
				}
			}
			if found != 1 {
				t.Fatalf("receipt %q appears %d times after relaunch: %+v", live.ID, found, entries)
			}
		})
	}
}
