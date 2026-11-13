package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
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

// C1, C5: omitting a new chat's model inherits the configured selection of a
// temporary parent; an explicit model still names the person's chosen model.
func TestStartChatInheritsConfiguredModelFromTemporaryParent(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited", true: "explicit"}[explicit], func(t *testing.T) {
			args, want := `{"brief":"Read README"}`, "test/model"
			if explicit {
				args, want = `{"brief":"Read README","model":"chosen/model"}`, "chosen/model"
			}
			c := chained(nil,
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("new-chat", "start_chat", args), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Requested."), nil },
			)
			a, _ := newTestAgent(t, c, func(cfg *Config) {
				cfg.Interactive = true
				cfg.Place = Place{Dir: t.TempDir()}
				cfg.TaskModels = func() []string { return []string{"chosen/model"} }
			})
			if err := a.UseModelForNow("available/model"); err != nil {
				t.Fatal(err)
			}
			_, _ = a.loadCapability("chats")
			stream, err := a.Submit(t.Context(), "start another chat")
			if err != nil {
				t.Fatal(err)
			}
			var request *ChatStart
			for _, event := range collect(t, stream) {
				if event.Kind == EventChatStart {
					request = event.ChatStart
				}
			}
			if request == nil || request.Model != want || a.Model() != "available/model" || a.ConfiguredModel() != "test/model" {
				t.Fatalf("request=%+v live=%q configured=%q", request, a.Model(), a.ConfiguredModel())
			}
		})
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

func TestChatStartClaimHasOneWinner(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("new-chat", "start_chat", `{"brief":"Read README"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Requested."), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.Interactive = true; c.Place = Place{Dir: t.TempDir()} })
	_, _ = agent.loadCapability("chats")
	stream, err := agent.Submit(t.Context(), "Start another chat")
	if err != nil {
		t.Fatal(err)
	}
	var request *ChatStart
	for _, event := range collect(t, stream) {
		if event.Kind == EventChatStart {
			request = event.ChatStart
		}
	}
	if request == nil {
		t.Fatal("no request")
	}
	door, ok := any(agent).(interface{ ClaimChatStart(string) (bool, error) })
	if !ok {
		t.Fatal("no shared chat-start claim door")
	}
	var winners atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			claimed, err := door.ClaimChatStart(request.ID)
			if err != nil {
				t.Error(err)
			}
			if claimed {
				winners.Add(1)
			}
		})
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatalf("launch winners=%d", winners.Load())
	}
	if claimed, err := door.ClaimChatStart("unknown"); err == nil || claimed {
		t.Fatal("unknown request claimed")
	}
}

func TestStartChatIsUnavailableToAppOnlySessions(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Interactive = true
		c.HandOverOnAsk = true
		c.Place = Place{Dir: t.TempDir()}
	})
	if answer, failed := agent.loadCapability("chats"); !failed {
		t.Fatalf("app-only session offered terminal chat launches: %s", answer)
	}
}
