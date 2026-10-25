package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestStartChatResolvesModelAndEmitsOnlyARequest(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("new-chat", "start_chat", `{"brief":"Read README","model":"kimi-k3"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Requested."), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Interactive = true
		c.Place = Place{Dir: t.TempDir()}
		c.TaskModels = func() []string { return []string{"moonshotai/kimi-k3", "deepseek/deepseek-v4.1-flash"} }
	})
	if _, failed := agent.loadCapability("chats"); failed {
		t.Fatal("chat capability missing")
	}
	stream, err := agent.Submit(t.Context(), "Start a chat on kimi-k3")
	if err != nil {
		t.Fatal(err)
	}
	var request *ChatStart
	for _, event := range collect(t, stream) {
		if event.Kind == EventChatStart {
			request = event.ChatStart
		}
	}
	if request == nil || request.Model != "moonshotai/kimi-k3" || request.Brief != "Read README" {
		t.Fatalf("request=%+v", request)
	}
	if agent.Model() != "test/model" {
		t.Fatal("request changed parent model")
	}
}

func TestStartChatRefusesUnknownAndAmbiguousModels(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.TaskModels = func() []string { return []string{"moonshotai/kimi-k3", "moonshotai/kimi-k3.1-pro"} }
	})
	for _, word := range []string{"missing/model", "kimi"} {
		args, _ := json.Marshal(map[string]string{"brief": "Read README", "model": word})
		answer, failed, err := agent.startChatTool(t.Context(), args)
		if err != nil || !failed || strings.Contains(answer, "Requested a new chat") {
			t.Fatalf("%s: %s %v", word, answer, err)
		}
	}
}
