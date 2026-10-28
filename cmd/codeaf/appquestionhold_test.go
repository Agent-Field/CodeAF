package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract 3 and 5: app Hold reaches the terminal engine's question clock;
// unheld clocks still take their suggestion through that same engine.
func TestAppModelQuestionHoldStopsTheEngineClock(t *testing.T) {
	for _, hold := range []bool{true, false} {
		t.Run(fmt.Sprintf("hold=%v", hold), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEAF_HOME", root)
			t.Setenv(config.ProfileDirEnv, root)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"data":[]}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				var delta any
				finish := "tool_calls"
				switch calls.Add(1) {
				case 1:
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "load", "type": "function", "function": map[string]string{"name": "load_capability", "arguments": `{"group":"questions"}`}}}}
				case 2:
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "choose", "type": "function", "function": map[string]string{"name": "ask", "arguments": `{"head":"Which store?","kind":"choice","reason":"Both fit","stakes":"reversible","options":[{"key":"sqlite","label":"SQLite"},{"key":"jsonl","label":"JSONL"}],"pick":{"key":"sqlite","reason":"Existing readers"}}`}}}}
				default:
					delta, finish = map[string]string{"content": "done"}, "stop"
				}
				chunk, _ := json.Marshal(map[string]any{"id": "clock", "choices": []any{map[string]any{"index": 0, "delta": delta}}})
				fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"clock\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", chunk, finish)
			}))
			defer server.Close()
			engine, err := session.New(session.Config{Workspace: root, Place: session.Place{Dir: filepath.Join(root, "chat"), Workspace: root}, ProfileDir: root, Model: "test/model", APIKey: "dummy", BaseURL: server.URL, Interactive: true, OneModel: true, DeterministicNames: true, ChoiceClockSeconds: 1})
			if err != nil {
				t.Fatal(err)
			}
			loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: engine}, nil }})
			if err != nil {
				engine.Close()
				t.Fatal(err)
			}
			defer loop.Close()
			rec := newAppRecorder()
			backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: loop.Client.Agent(), Workspace: root, Transcript: appFakeTranscript, Release: func() {}}, nil
			})
			backend.profileDir = root
			defer backend.Close()
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: root})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Send(agent.ID, "Choose a store with the ask tool.", ""); err != nil {
				t.Fatal(err)
			}
			i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
			rec.mu.Lock()
			ask := rec.data[i].(appwire.AskOpened).Ask
			rec.mu.Unlock()
			if ask.ClockUntil == nil {
				t.Fatal("the question has no clock")
			}
			if !hold {
				rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskClosed))
				decisions := engine.Decisions()
				if len(decisions) != 1 || len(decisions[0].Picked) != 1 || decisions[0].Picked[0] != "1" || len(decisions[0].Labels) != 1 || decisions[0].Labels[0] != "SQLite" {
					t.Fatalf("unheld clock decisions = %+v", decisions)
				}
				return
			}
			if err := backend.HoldAsk(agent.ID, ask.ID); err != nil {
				t.Fatal(err)
			}
			rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
				v, ok := data.(appwire.AskOpened)
				return ok && v.Ask.ID == ask.ID && v.Ask.ClockUntil == nil
			})
			time.Sleep(time.Until(*ask.ClockUntil) + 100*time.Millisecond)
			if open := engine.OpenQuestions(); len(open) != 1 || !open[0].Deadline.IsZero() || len(engine.Decisions()) != 0 {
				t.Fatalf("held engine question = %+v; decisions = %+v", open, engine.Decisions())
			}
			if open, err := backend.ListAsks(agent.ID); err != nil || len(open) != 1 || open[0].ID != ask.ID || open[0].ClockUntil != nil {
				t.Fatalf("held asks.list = %+v %v", open, err)
			}
			if waiting, _, err := backend.ListInbox(appwire.InboxWaiting, ""); err != nil || len(waiting) != 1 || waiting[0].Ask.ClockUntil != nil {
				t.Fatalf("held Inbox = %+v %v", waiting, err)
			}
			// An identical lane replay must not publish another opening.
			a := mustFind(t, backend, agent.ID)
			q := engine.OpenQuestions()[0]
			backend.questionEvent(a, agent.ID, backend.liveOf(a), session.Event{Kind: session.EventQuestion, Question: &q})
			rec.mu.Lock()
			defer rec.mu.Unlock()
			opened := 0
			for _, event := range rec.events {
				if event == appwire.EventAskOpened {
					opened++
				}
			}
			if opened != 2 {
				t.Fatalf("initial + Hold + identical replay emitted %d openings, want 2", opened)
			}
		})
	}
}

// Contract 3: task proposal Hold retains its existing task action.
func TestAppTaskQuestionHoldKeepsTheTaskDoor(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	backend.profileDir = t.TempDir()
	q := session.Question{ID: 4, Kind: session.QuestionTask, Head: "Start the task?", Deadline: time.Now().Add(time.Hour)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	if err := backend.HoldAsk(agent.ID, "task:4"); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if len(conv.intents) != 1 || conv.intents[0].ID != "4" || conv.intents[0].Action != "hold" || len(conv.answers) != 0 {
		t.Fatalf("task Hold = %+v; answers = %+v", conv.intents, conv.answers)
	}
}

// Contract 3: replaying either consent sibling preserves grouping silently.
func TestAppQuestionReplayDoesNotRepeatTheBatch(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	backend, rec, agent := workBackend(t, &fakeConversation{})
	backend.profileDir = t.TempDir()
	a := mustFind(t, backend, agent.ID)
	live := backend.liveOf(a)
	questions := []session.Question{
		{ID: 1, Kind: session.QuestionConsent, Batch: "tools", Head: "First call", Options: session.AnswerOptions(session.QuestionConsent)},
		{ID: 2, Kind: session.QuestionConsent, Batch: "tools", Head: "Second call", Options: session.AnswerOptions(session.QuestionConsent)},
	}
	for range 2 {
		for _, q := range questions {
			backend.questionEvent(a, agent.ID, live, session.Event{Kind: session.EventQuestion, Question: &q})
		}
	}
	// Drain the ordered event queue before counting the observable openings.
	live.box.Close()
	openings, batchless := 0, 0
	rec.mu.Lock()
	for _, data := range rec.data {
		if v, ok := data.(appwire.AskOpened); ok {
			openings++
			if len(v.Ask.Batch) == 0 {
				batchless++
			}
		}
	}
	rec.mu.Unlock()
	if openings != 4 || batchless != 2 {
		t.Fatalf("two initial asks + grouping + identical replays = %d openings (%d batchless); want 4 (2 batchless)", openings, batchless)
	}
	asks, err := backend.ListAsks(agent.ID)
	if err != nil || len(asks) != 2 || len(asks[0].Batch) != 2 || len(asks[1].Batch) != 2 {
		t.Fatalf("replayed asks = %+v %v", asks, err)
	}
}
