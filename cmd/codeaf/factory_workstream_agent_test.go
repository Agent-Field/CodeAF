package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/session"
)

type agentTestDoor struct {
	mu    sync.Mutex
	pages []string
}

func (d *agentTestDoor) Workstream(context.Context) (factory.OrgWorkstream, error) {
	return factory.OrgWorkstream{Name: "Webhook reliability", Stage: "building"}, nil
}
func (d *agentTestDoor) WritePage(_ context.Context, page, title, body string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pages = append(d.pages, title+":"+body)
	return "made the page \"" + title + "\"", nil
}
func (d *agentTestDoor) Decide(context.Context, string) (string, error) { return "proposed", nil }

// THE SERVER'S AGENT IS THE ENGINE'S OWN CONVERSATION, run with the
// organization's key and the workstream's one tool. The model is asked with
// that key and offered that tool alone; its call reaches the workstream through
// the door; its steps and answer are reported; and what it spent is answered.
func TestTheServerAgentIsASessionWithTheOrgKeyAndOneTool(t *testing.T) {
	var mu sync.Mutex
	var auth []string
	var tools [][]string
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		answered := false
		for _, m := range req.Messages {
			answered = answered || m.Role == "tool"
		}
		mu.Lock()
		calls++
		auth = append(auth, r.Header.Get("Authorization"))
		var names []string
		for _, tool := range req.Tools {
			names = append(names, tool.Function.Name)
		}
		if len(names) > 0 {
			tools = append(tools, names)
		}
		mu.Unlock()
		delta, finish := map[string]any{"content": "Made the runbook."}, "stop"
		if len(req.Tools) > 0 && !answered {
			args := `{"ask":"new","title":"Runbook","body":"Page someone."}`
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function",
				"function": map[string]string{"name": session.WorkstreamPagesTool, "arguments": args}}}}
			finish = "tool_calls"
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"cost\":0.02}}\n\ndata: [DONE]\n\n", chunk, finish)
	}))
	t.Cleanup(model.Close)
	saved := factoryWorkstreamAgentBase
	factoryWorkstreamAgentBase = model.URL
	t.Cleanup(func() { factoryWorkstreamAgentBase = saved })

	door := &agentTestDoor{}
	var steps []string
	run := factoryWorkstreamAgent(nil, nil)
	end, err := run(t.Context(), serve.AgentTurn{Workstream: "ws_1", Name: "Webhook reliability", Turn: "turn_1", Asker: "mia",
		Text: "Write a runbook.", Key: "sk-or-v1-organization", Dir: t.TempDir(), Door: door,
		Step: func(kind, text, tool string, failed bool) { steps = append(steps, kind+"|"+tool+"|"+text) }})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// Besides the turn's own two calls, the conversation makes its small
	// side calls (one model for every call); every one carries the key.
	if len(tools) != 2 {
		t.Fatalf("the turn called the model with tools %d times (%v) of %d calls", len(tools), tools, calls)
	}
	for i, got := range auth {
		if got != "Bearer sk-or-v1-organization" {
			t.Fatalf("call %d authorized as %q", i, got)
		}
	}
	for i, names := range tools {
		if strings.Join(names, ",") != session.WorkstreamPagesTool {
			t.Fatalf("call %d offered %v (all %v, door %v, steps %q)", i, names, tools, door.pages, steps)
		}
	}
	if strings.Join(door.pages, ";") != "Runbook:Page someone." {
		t.Fatalf("the door saw %v", door.pages)
	}
	if len(steps) != 2 || !strings.HasPrefix(steps[0], api.AgentEntryStep+"|"+session.WorkstreamPagesTool+"|") || steps[1] != api.AgentEntryAgent+"||Made the runbook." {
		t.Fatalf("steps %q", steps)
	}
	if end.USD < 0.039 || end.USD > 0.041 {
		t.Fatalf("spent %v, want 0.04", end.USD)
	}
}
