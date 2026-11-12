package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

func harnessWire(t *testing.T, run func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error)) (*appBackend, *appRecorder, *actionWire, string, *session.Agent) {
	t.Helper()
	engine, err := session.New(session.Config{
		Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "transcript.jsonl"),
		Model: "test/model", APIKey: "test-key", BaseURL: "http://127.0.0.1:1",
		Harnesses:  []subharness.Entry{{Name: "research", Description: "Research a question", Revision: 1}},
		RunHarness: run,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: engine}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	b, rec, agent := workBackend(t, &fakeConversation{})
	b.locked(func() { b.agents[agent.ID].live.conn.Conv = loop.Client.Agent() })
	return b, rec, newActionWire(t, b), agent.ID, engine
}

// The real registered wire reaches the real session through the same remote
// handle the app uses, with no model endpoint and no copied harness runner.
func TestAppRunHarnessTextAndClientCorrelation(t *testing.T) {
	var runs atomic.Int32
	b, rec, w, id, engine := harnessWire(t, func(_ context.Context, name, text, model string, _ func(subharness.Trail)) (string, subharness.Usage, error) {
		if name != "research" || text != "tidy the changelog" || model != "" {
			t.Errorf("runner input: %q %q %q", name, text, model)
		}
		runs.Add(1)
		return "the report", subharness.Usage{}, nil
	})
	if !slices.Contains(w.hello, "agents.run_harness") {
		t.Fatal("hello lacks agents.run_harness")
	}
	b.locked(func() { b.agents[id].attach = map[string]appAttach{"waiting": {}} })
	if _, err := w.call("agents.run_harness", appwire.RunHarnessParams{ID: id, Name: "research", Text: "tidy the changelog", ClientID: "bad\u0001id"}); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("invalid client_id: %v", err)
	}
	for i := 1; i <= 2; i++ {
		got := machineCall[appwire.TurnResult](t, w, "agents.run_harness", appwire.RunHarnessParams{ID: id, Name: "research", Text: "tidy the changelog", ClientID: "same-id"})
		if got.Turn == 0 {
			t.Fatal("no turn")
		}
		rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
			done, ok := data.(appwire.TurnDone)
			return ok && done.Turn == got.Turn
		})
		rec.waitFor(t, time.Second, func(event string, data any) bool {
			added, ok := data.(appwire.EntryAdded)
			return ok && added.Entry.Turn == got.Turn && added.Entry.Kind == appwire.EntryUser && added.Entry.ClientID == "same-id" && added.Entry.Text == "tidy the changelog" && len(added.Entry.Attachments) == 0
		})
		rec.waitFor(t, time.Second, func(event string, data any) bool {
			added, ok := data.(appwire.EntryAdded)
			return ok && added.Entry.Turn == got.Turn && added.Entry.Text == "the report"
		})
		// The pump publishes done before it releases its busy flag.
		a, _ := b.find(id)
		a.gate.Lock()
		live := b.liveOf(a)
		a.gate.Unlock()
		<-live.pumpDone
	}
	b.locked(func() {
		if _, kept := b.agents[id].attach["waiting"]; !kept {
			t.Error("harness consumed a staged attachment")
		}
	})
	if runs.Load() != 2 {
		t.Fatalf("client_id was deduplicated: %d runs", runs.Load())
	}
	var users int
	for _, entry := range engine.Transcript() {
		if entry.Role == "user" {
			users++
			if entry.Person == nil || entry.Person.ClientID != "same-id" {
				t.Fatalf("accepted message lost client_id: %+v", entry)
			}
		}
	}
	if users != 2 {
		t.Fatalf("accepted messages: %d", users)
	}
}

func TestAppRunHarnessUnknownAndBusyRefusals(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	_, rec, w, id, _ := harnessWire(t, func(_ context.Context, _, _, _ string, _ func(subharness.Trail)) (string, subharness.Usage, error) {
		close(started)
		<-finish
		return "done", subharness.Usage{}, nil
	})
	defer close(finish)
	ask := appwire.RunHarnessParams{ID: id, Name: "missing", Text: "request"}
	if _, err := w.call("agents.run_harness", ask); err == nil || !strings.Contains(err.Message, "no harness named") {
		t.Fatalf("unknown name: %v", err)
	}
	rec.mu.Lock()
	for _, event := range rec.events {
		if event == appwire.EventTurnStarted || event == appwire.EventEntryAdded {
			t.Errorf("unknown harness emitted %s", event)
		}
	}
	rec.mu.Unlock()
	ask.Name = "research"
	machineCall[appwire.TurnResult](t, w, "agents.run_harness", ask)
	<-started
	if _, err := w.call("agents.run_harness", ask); err == nil || err.Code != appwire.CodeBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestAppRunHarnessUnavailableAndAttachments(t *testing.T) {
	b, _, agent := workBackend(t, &fakeConversation{})
	w := newActionWire(t, b)
	ask := map[string]any{"id": agent.ID, "name": "research", "text": "request"}
	if _, err := w.call("agents.run_harness", ask); err == nil || err.Message != "harnesses are unavailable here" {
		t.Fatalf("unavailable: %v", err)
	}
	for _, field := range []string{"attachments", "submission", "reads"} {
		ask[field] = json.RawMessage("[]")
		if _, err := w.call("agents.run_harness", ask); err == nil || err.Code != appwire.CodeBadParams {
			t.Fatalf("%s accepted: %v", field, err)
		}
		delete(ask, field)
	}
}
