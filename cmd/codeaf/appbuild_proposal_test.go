package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

type proposalTestModel struct{ sent bool }

func (m *proposalTestModel) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	reply := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Added the renewal date field."}}}
	if !m.sent {
		m.sent = true
		args, _ := json.Marshal(map[string]string{"command": `codeaf app add-field {"type":"deal","name":"renewal_on","definition":{"type":"date"}}`})
		reply.Content = nil
		reply.ToolCalls = []ai.ToolCall{{ID: "field", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}}
	}
	return &ai.Response{Choices: []ai.Choice{{Message: reply}}}, nil
}

func TestAppChangeCardSurvivesEngineRestart(t *testing.T) {
	for _, source := range []string{"marked", "assistant"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(home.EnvVar, root)
			app := installAppAssistantCRM(t)
			record := newAppRecorder()
			b := newAppBackend(record, refuseAppOpen)
			t.Cleanup(func() { _ = b.Close() })
			jobs, err := builder.OpenJobs(builder.Options{
				Home: root, Store: b.AppDefinitions(),
				Model:    func() (builder.Completer, error) { return &proposalTestModel{}, nil },
				Proposed: b.appAssistantProposed, Changed: b.appBuildChanged,
			})
			if err != nil {
				t.Fatal(err)
			}
			b.builds.jobs = jobs
			wire := wireOver(t, b)
			var opened struct{ Draft string }
			if source == "marked" {
				wire.must("apps.change", map[string]string{"app": app.ID, "request": ""}, &opened)
				wire.must("apps.say", map[string]any{"draft": opened.Draft, "text": "Add a renewal date to Deals",
					"target": map[string]any{"screen": "deals", "path": []any{"body", "children", 1, "children", 0, "children", 1}, "node_id": "deals"}}, nil)
			} else {
				request, _ := json.Marshal(map[string]string{"app": app.ID, "request": "Add a renewal date to Deals"})
				if _, err := b.runAppCLICommand(t.Context(), appwire.AppContentPropose, request); err == nil {
					t.Fatal("the assistant proposed a change without a grant")
				}
				if err := b.GrantAppAgent(appwire.AppAgentGrant{App: app.ID, Agent: app.ID, Group: "actions", ID: "propose_change", Allowed: true}); err != nil {
					t.Fatal(err)
				}
				model := httptest.NewServer(http.HandlerFunc(serveAppProposalTestModel))
				t.Cleanup(model.Close)
				t.Setenv("CODEAF_BASE_URL", model.URL)
				t.Setenv("OPENROUTER_API_KEY", "test-model-key")
				var assistant struct{ Conversation string }
				wire.must("apps.agent_open", map[string]string{"app": app.ID}, &assistant)
				wire.must("agents.send", map[string]string{"id": assistant.Conversation,
					"text": "Please change the CRM app itself by adding a renewal date field to Deals."}, nil)
				record.waitFor(t, 10*time.Second, appTurnDone)
				var tasks struct{ Tasks []appwire.Task }
				wire.must("tasks.list", map[string]string{"agent": assistant.Conversation}, &tasks)
				if len(tasks.Tasks) != 0 {
					t.Fatalf("the assistant started generic tasks: %+v", tasks.Tasks)
				}

			}
			var card appwire.AppChangeProposed
			record.waitFor(t, 5*time.Second, func(name string, payload any) bool {
				if name != appwire.EventAppChangeProposed {
					return false
				}
				card = payload.(appwire.AppChangeProposed)
				return true
			})
			if card.App != app.ID || (opened.Draft != "" && card.Draft != opened.Draft) || card.ID == "" ||
				!strings.Contains(strings.Join(card.SummaryLines, " "), "renewal") {
				t.Fatalf("proposal = %+v", card)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}

			fresh := newAppBackend(newAppRecorder(), refuseAppOpen)
			t.Cleanup(func() { _ = fresh.Close() })
			reopened := wireOver(t, fresh)
			var listed struct {
				Builds []struct{ Proposal *appwire.AppChangeProposed }
			}
			reopened.must("apps.list", struct{}{}, &listed)
			if len(listed.Builds) != 1 || !reflect.DeepEqual(listed.Builds[0].Proposal, &card) {
				t.Fatalf("reopened builds = %+v, want the saved proposal %+v", listed.Builds, card)
			}
			var kept store.Definition
			reopened.must("apps.get", map[string]string{"app": app.ID}, &kept)
			if kept.Meta.Version != 1 {
				t.Fatalf("version before Apply = %d", kept.Meta.Version)
			}
			var applied struct{ Meta store.Meta }
			reopened.must("apps.apply", map[string]string{"draft": card.Draft}, &applied)
			if applied.Meta.Version != 2 {
				t.Fatalf("version after Apply = %d", applied.Meta.Version)
			}
			reopened.must("apps.list", struct{}{}, &listed)
			if len(listed.Builds) != 0 {
				t.Fatal("the applied card is still pending")
			}
		})
	}
}

func serveAppProposalTestModel(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		_, _ = fmt.Fprint(w, `{"data":[]}`)
		return
	}
	var request struct {
		Stream   bool
		Messages []struct {
			Role    string
			Content json.RawMessage
		}
		Tools []json.RawMessage
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	message := map[string]any{"role": "assistant", "content": "The change is ready for review."}
	finish := "stop"
	switch {
	case len(request.Messages) > 0 && strings.Contains(string(request.Messages[0].Content), "Classify the user's request"):
		message["content"] = `{"change_app":true}`
	case len(request.Tools) > 0 && len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "user":
		delete(message, "content")
		message["tool_calls"] = []any{map[string]any{
			"index": 0, "id": "change", "type": "function",
			"function": map[string]string{"name": "app", "arguments": `{"verb":"propose_change"}`},
		}}
		finish = "tool_calls"
	}
	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		raw, _ := json.Marshal(map[string]any{"id": "proposal-test", "choices": []any{map[string]any{"index": 0, "delta": message, "finish_reason": finish}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
}
