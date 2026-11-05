package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract (a), (c), (d): a priced provider call reaches both tool phases
// during a live turn without deadlocking, while open and list keep saved spend.
func TestTurnToolCarriesLiveSpendDuringARunningTurn(t *testing.T) {
	testTurnToolSpend(t, .125, .25)
}

// Contract (b), (d): a real turn with no spend omits the JSON key at both
// tool phases and completes under the same deadline as a priced turn.
func TestTurnToolOmitsSpendWhenNothingWasSpent(t *testing.T) {
	testTurnToolSpend(t, 0, 0)
}

// Contract (a), (d): failed calls, approval updates and output refreshes carry
// the live reading at each emission, without holding the call-log lock.
func TestTurnToolRestatementsCarryTheCurrentSpend(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	conv := &fakeConversation{cost: .375, hold: hold, script: []session.Event{
		{Kind: session.EventToolBegin, CallID: "failed", Tool: "bash"},
		{Kind: session.EventToolFailed, CallID: "failed", Tool: "bash", Text: "failed"},
		{Kind: session.EventToolBegin, CallID: "long", Tool: "bash"},
	}}
	backend, rec, agent := workBackend(t, conv)
	backend.toolQuiet = 0
	if _, err := backend.Send(agent.ID, "read the output", ""); err != nil {
		t.Fatal(err)
	}
	failed := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "failed" && row.Phase == appwire.PhaseEnd })
	if failed.SpentUSD == nil || *failed.SpentUSD != .375 || failed.OK == nil || *failed.OK {
		t.Fatalf("failed tool row = %+v", failed)
	}
	toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "long" && row.Phase == appwire.PhaseBegin })
	conv.mu.Lock()
	conv.cost = .5
	conv.mu.Unlock()
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &session.Question{
		ID: 7, Kind: session.QuestionConsent, Head: "Allow the command?",
		Subject: session.SubjectRef{Kind: session.SubjectCall, CallID: "long", Name: "bash"},
		Options: session.AnswerOptions(session.QuestionConsent),
	}})
	asked := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "long" && row.Ask != "" })
	if asked.SpentUSD == nil || *asked.SpentUSD != .5 {
		t.Fatalf("approval tool row = %+v", asked)
	}
	conv.mu.Lock()
	conv.cost = .75
	conv.mu.Unlock()
	a, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend.callOutput(a, agent.ID, backend.liveOf(a), session.Event{Kind: session.EventToolOutput, CallID: "long", Text: "still running\n"})
	fresh := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "long" && len(row.Tail) == 1 })
	if fresh.SpentUSD == nil || *fresh.SpentUSD != .75 {
		t.Fatalf("refreshed tool row = %+v", fresh)
	}
}

func testTurnToolSpend(t *testing.T, saved, cost float64) {
	t.Helper()
	d := newUnreadDisk(t)
	meta, err := session.LoadMeta(d.dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.SpentUSD = saved
	if err := session.SaveMeta(d.dir, meta); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(d.dir, "transcript.jsonl")
	journal := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n{\"type\":\"usage\",\"usage\":{\"costUsd\":%g}}\n", d.id, saved)
	if err := os.WriteFile(transcript, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(meta.Workspace, "receipt.txt")
	if err := os.WriteFile(path, []byte("the tool ran\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	finish := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answered := false
		for _, message := range request.Messages {
			answered = answered || message.Role == "tool"
		}
		if answered {
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if answered {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"cost":0}}`+"\n\n")
		} else {
			fmt.Fprintf(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"live-spend","type":"function","function":{"name":"read","arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"cost":%g}}`+"\n\n", string(args), cost)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	profile := t.TempDir()
	var engine *session.Agent
	d.backend.open = func(ask appOpenAsk) (*appConn, error) {
		var err error
		engine, err = session.New(session.Config{
			Workspace: ask.Workspace, Place: session.Place{Dir: d.dir}, SessionFile: ask.Transcript,
			ProfileDir: profile, Model: "test/model", APIKey: "fixture", BaseURL: server.URL,
			OneModel: true, Routing: provider.RoutingOff, DeterministicNames: true,
		})
		if err != nil {
			return nil, err
		}
		return &appConn{Conv: engine, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = engine.Close() }}, nil
	}
	// Release the held response before either the server or backend is closed.
	t.Cleanup(release)
	d.listed()
	if _, err := d.backend.OpenAgent(d.id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.backend.Send(d.id, "read receipt.txt, then finish", ""); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{appwire.PhaseBegin, appwire.PhaseEnd} {
		row := toolRow(d.rec, t, func(row appwire.TurnTool) bool { return row.Call == "live-spend" && row.Phase == phase })
		live := engine.Usage().CostUSD
		if live != saved+cost {
			t.Fatalf("live meter = %v, want %v", live, saved+cost)
		}
		if live > 0 && (row.SpentUSD == nil || *row.SpentUSD != live) {
			t.Fatalf("%s tool row = %+v, want live spend %v", phase, row, live)
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if _, present := fields["spent_usd"]; present != (live > 0) {
			t.Fatalf("%s tool row has wrong spend presence: %s", phase, raw)
		}
		if phase == appwire.PhaseEnd && (row.OK == nil || !*row.OK || !strings.Contains(strings.Join(row.Tail, "\n"), "the tool ran")) {
			t.Fatalf("the tool did not run successfully: %+v", row)
		}
		opened, err := d.backend.OpenAgent(d.id)
		if err != nil || opened.SpentUSD != saved || opened.State != appwire.StateWorking {
			t.Fatalf("running agents.open = %+v, %v; want saved spend %v", opened, err, saved)
		}
		if listed := d.listed(); listed.SpentUSD != saved {
			t.Fatalf("running agents.list spend = %v, want saved spend %v", listed.SpentUSD, saved)
		}
	}
	release()
	d.rec.waitFor(t, 5*time.Second, appTurnDone)
	entries, _, err := d.backend.Entries(d.id, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Kind != appwire.EntryTool {
			continue
		}
		found = true
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if _, present := fields["spent_usd"]; present {
			t.Fatalf("history tool row carries live spend: %s", raw)
		}
	}
	if !found {
		t.Fatal("history did not replay the tool call")
	}
}
