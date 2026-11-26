package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
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
// organization's key and the workstream's one tool, and the web's two reading
// hands beside it (finding #38). The model is asked with that key and offered
// those tools alone, and told it can look things up; its call reaches the
// workstream through the door; its steps and answer are reported; and what it
// spent is answered.
func TestTheServerAgentIsASessionWithTheOrgKeyAndOneTool(t *testing.T) {
	var mu sync.Mutex
	var auth []string
	var tools [][]string
	var toldWeb []bool
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
			toldWeb = append(toldWeb, strings.Contains(string(raw), "You can look things up on the web") && !strings.Contains(string(raw), "use the web"))
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
		if strings.Join(names, ",") != session.WorkstreamPagesTool+",web_search,web_fetch" {
			t.Fatalf("call %d offered %v (all %v, door %v, steps %q)", i, names, tools, door.pages, steps)
		}
		if !toldWeb[i] {
			t.Fatalf("call %d offered the web but its prompt did not say so, or still said it cannot use the web", i)
		}
	}
	if strings.Join(door.pages, ";") != "Runbook:Page someone." {
		t.Fatalf("the door saw %v", door.pages)
	}
	if len(steps) != 2 || !strings.HasPrefix(steps[0], api.AgentEntryStep+"|"+session.WorkstreamPagesTool+"|made the page Runbook") || steps[1] != api.AgentEntryAgent+"||Made the runbook." {
		t.Fatalf("steps %q", steps)
	}
	if end.USD < 0.039 || end.USD > 0.041 {
		t.Fatalf("spent %v, want 0.04", end.USD)
	}
}

// THE SERVER AGENT ALWAYS NAMES A MODEL SOURCE. An unset CODEAF_BASE_URL used
// to hand the provider client an empty base URL, which it refuses, so every
// server turn failed with "provider base URL is required" on a server started
// without the variable (film defect D9). Unset means OpenRouter; set means the
// operator's own proxy.
func TestTheServerAgentDefaultsToOpenRouterWhenNoBaseURLIsSet(t *testing.T) {
	t.Setenv("CODEAF_BASE_URL", "")
	if got := factoryWorkstreamAgentBaseURL(); got != config.DefaultBaseURL || got == "" {
		t.Fatalf("base URL with no variable = %q, want %q", got, config.DefaultBaseURL)
	}
	t.Setenv("CODEAF_BASE_URL", " https://proxy.example/v1 ")
	if got := factoryWorkstreamAgentBaseURL(); got != "https://proxy.example/v1" {
		t.Fatalf("base URL with the variable = %q, want the operator's proxy", got)
	}
}

// agentWorkTestDoor is a server door that also keeps the workstream's tasks
// and files, as the real one has since #2687.
type agentWorkTestDoor struct {
	agentTestDoor
	work []string
}

func (d *agentWorkTestDoor) Work(_ context.Context, tool string, args json.RawMessage) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.work = append(d.work, tool+" "+string(args))
	return `{"said":"The Ask \"Which tagline?\" waits on santosh."}`, nil
}

// THE ORGANIZATION'S AFI CAN RAISE AN ASK (live2 E6). On 073b2a469 the server's
// agent carried workstream_pages alone, so a conflict on an organization could
// not ask anyone. The real runner, given a door that keeps work, offers the
// model every ws_* tool beside workstream_pages (never ws_chats, which only a
// laptop that holds the chats answers), its ws_task_update with an ask reaches
// the door, and the prompt carries the workstream rules with their wording
// rule. Who may answer that Ask, one person and not another, is the server's
// own rule, pinned in serve's TestServerAgentKeepsWorkAsTasks with three people.
func TestTheServerAgentWithWorkCarriesTheTaskToolsAndRaisesAnAsk(t *testing.T) {
	var mu sync.Mutex
	var tools [][]string
	var system string
	ask := `{"task":"tk_1","ask":{"title":"Which tagline?","options":[{"label":"Fresh daily","recommended":true},{"label":"Picked this morning"}]}}`
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		answered := false
		mu.Lock()
		for _, m := range req.Messages {
			answered = answered || m.Role == "tool"
			if m.Role == "system" && len(req.Tools) > 0 {
				system += string(m.Content)
			}
		}
		var names []string
		for _, tool := range req.Tools {
			names = append(names, tool.Function.Name)
		}
		if len(names) > 0 {
			tools = append(tools, names)
		}
		mu.Unlock()
		delta, finish := map[string]any{"content": "Asked santosh which tagline."}, "stop"
		if len(req.Tools) > 0 && !answered {
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function",
				"function": map[string]string{"name": session.WSTaskUpdateTool, "arguments": ask}}}}
			finish = "tool_calls"
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"cost\":0.01}}\n\ndata: [DONE]\n\n", chunk, finish)
	}))
	t.Cleanup(model.Close)
	saved := factoryWorkstreamAgentBase
	factoryWorkstreamAgentBase = model.URL
	t.Cleanup(func() { factoryWorkstreamAgentBase = saved })

	door := &agentWorkTestDoor{}
	run := factoryWorkstreamAgent(nil, nil)
	if _, err := run(t.Context(), serve.AgentTurn{Workstream: "ws_1", Name: "Pantry", Turn: "turn_1", Asker: "mia",
		Text: "Ask santosh which tagline the launch post uses.", Key: "sk-or-v1-organization", Dir: t.TempDir(), Door: door,
		Step: func(string, string, string, bool) {}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(tools) == 0 {
		t.Fatal("the model was never offered a tool")
	}
	offered := strings.Join(tools[0], ",")
	for _, want := range []string{session.WorkstreamPagesTool, session.WSTasksTool, session.WSTaskCreateTool, session.WSTaskUpdateTool, session.WSTaskCommentTool} {
		if !strings.Contains(","+offered+",", ","+want+",") {
			t.Errorf("the server's Afi is not offered %s (offered %s)", want, offered)
		}
	}
	if strings.Contains(offered, "ws_chats") {
		t.Errorf("the server's Afi is offered ws_chats, which no server answers")
	}
	if len(door.work) != 1 || door.work[0] != session.WSTaskUpdateTool+" "+ask {
		t.Fatalf("the door saw %q", door.work)
	}
	if !strings.Contains(system, "Decisions is the first row in Files") || !strings.Contains(system, "Never write an id") {
		t.Errorf("the server's prompt lacks the wording rule")
	}
}
