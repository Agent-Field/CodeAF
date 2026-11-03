package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/session"
)

// refusingFactoryEffort answers only requests without thinking controls.
func refusingFactoryEffort(t *testing.T) (*httptest.Server, func() []bool) {
	t.Helper()
	var mu sync.Mutex
	var requests []bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		knobs := body["reasoning"] != nil || body["reasoning_effort"] != nil || body["thinking"] != nil
		mu.Lock()
		requests = append(requests, knobs)
		mu.Unlock()
		if knobs {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"This model does not support reasoning","code":400}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server, func() []bool { mu.Lock(); defer mu.Unlock(); return append([]bool(nil), requests...) }
}

func TestFactoryLiveManagerRecoversEffortOnlyForItsTurn(t *testing.T) {
	server, requests := refusingFactoryEffort(t)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), Model: "fixture/manager", APIKey: "fixture", BaseURL: server.URL, Effort: effort.High, OneModel: true, DeterministicNames: true})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	// This is the live road: the Factory runner borrows an existing agent.
	manager := liveManagerTurn{agent: agent, door: runDoor(newRunRig(t).st, t.TempDir(), t.TempDir())}
	events, err := manager.SubmitRunnerNote(ctx, "Report done.")
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("live manager refused: %v", event.Err)
		}
	}
	if err := manager.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	events, err = agent.Submit(ctx, "An ordinary question.")
	if err != nil {
		t.Fatal(err)
	}
	refused := false
	for event := range events {
		if event.Kind == session.EventError {
			refused = true
		}
	}
	got := requests()
	if !refused || len(got) != 3 || !got[0] || got[1] || !got[2] {
		t.Fatalf("ordinary refusal=%v requests=%v; want true [true false true]", refused, got)
	}
}

func TestFactoryPageErrandsRecoverEffortOnExistingAgent(t *testing.T) {
	for _, path := range []struct{ purpose, role string }{
		{"repo_ask", "insight_ask"}, {"repo_notes", "insight_notes"},
		{"repo_themes", "insight_themes"}, {"repo_eval", "insight"},
		{"pipeline_ai", "insight_pipeline"}, {"view_topic", "insight_views"},
		{"factory_brief", "brief"},
	} {
		t.Run(path.purpose, func(t *testing.T) {
			s := newLocalSetup(t)
			server, requests := refusingFactoryEffort(t)
			t.Setenv("OPENROUTER_API_KEY", "fixture")
			t.Setenv("CODEAF_BASE_URL", server.URL)
			s.start()
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				agent, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: t.TempDir(), Model: "fixture/page", APIKey: "fixture", BaseURL: server.URL, OneModel: true, DeterministicNames: true, SupportsParameter: func(string, string) (bool, bool) { return true, true }})
				if err != nil {
					return nil, err
				}
				return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
			}
			s.backend.mint = appMint
			var created struct{ Agent appwire.Agent }
			s.must("agents.create", map[string]any{"workspace": t.TempDir()}, &created)
			if _, err := s.backend.OpenAgent(created.Agent.ID); err != nil {
				t.Fatalf("open page agent: %v", err)
			}
			params := map[string]any{"agent": created.Agent.ID, "purpose": path.purpose, "role": path.role, "effort": "high", "messages": []appwire.ErrandMessage{{Role: "user", Text: "Answer done."}}, "max_tokens": 100}
			var answer appwire.Errand
			deadline := time.Now().Add(5 * time.Second)
			for {
				answer = appwire.Errand{}
				s.must("agents.errand", params, &answer)
				if answer.Reason != appwire.ErrandNotOpen {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("agent did not open")
				}
				if _, err := s.backend.OpenAgent(created.Agent.ID); err != nil {
					t.Fatalf("open page agent: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if answer.Reason != "" || answer.Text != "done" {
				t.Fatalf("Factory errand: %+v", answer)
			}
			// The same model, role and agent still refuse outside Factory work.
			params["purpose"] = "ordinary"
			s.must("agents.errand", params, &answer)
			got := requests()
			if answer.Reason != session.ErrandModelFailed || len(got) != 3 || !got[0] || got[1] || !got[2] {
				t.Fatalf("ordinary reason=%s requests=%v; want model_failed [true false true]", answer.Reason, got)
			}
		})
	}
}
