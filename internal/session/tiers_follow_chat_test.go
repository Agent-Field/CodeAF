package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// Read a real mark through the profile-backed ladder, rather than resolving a
// role in isolation: the caller used to withhold the conversation's floor.
func TestAnUnconfiguredMarkReaderCallsTheLiveConversationModel(t *testing.T) {
	const model = "deepseek/deepseek-v4.1-flash"
	for _, state := range []string{"unset", "pinned", "cleared"} {
		t.Run(state, func(t *testing.T) {
			rows := map[string]string{}
			want := model
			if state == "pinned" {
				rows[config.KeyTierMastermindModel], want = "vendor/reader", "vendor/reader"
			}
			if state == "cleared" {
				rows[config.KeyTierMastermindModel] = ""
			}
			completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse(checkpointNothingLeft), nil
			}}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Model = "vendor/original"
				c.RolesSource = profileTiers(t, rows)
			})
			agent.SetModel(model)
			workedTurn(agent, "explain the history of the internet", 3)
			read := agent.readMark(context.Background())
			if !read.asked || read.failed {
				t.Fatalf("mark reading = %+v", read)
			}
			completer.mu.Lock()
			defer completer.mu.Unlock()
			if len(completer.models) != 1 || completer.models[0] != want {
				t.Fatalf("wire models = %v, want [%s]", completer.models, want)
			}
		})
	}
}

// Programs and saved unrouted tasks read tiers directly, without the call
// ladder. They must freeze the same conversation floor rather than a worker.
func TestUnroutedPlannerReadersKeepTheConversationModel(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.SetModel("vendor/live-chat")
	if got := agent.unroutedCrewRecord().Plan; got != "vendor/live-chat" {
		t.Fatalf("saved planner = %q", got)
	}
	run := &beltRun{delegate: &delegate.Delegate{}}
	if got := agent.delegateCrew(run).Brain; got != "vendor/live-chat" {
		t.Fatalf("program planner = %q", got)
	}
	if got := agent.beltRunSpec(run, "do the work").PlanModel; got != "vendor/live-chat" {
		t.Fatalf("run planner = %q", got)
	}
	run.recoveredCrew = &TaskCrewRecord{Version: 1}
	if got := agent.beltRunSpec(run, "resume the work").PlanModel; got != "vendor/live-chat" {
		t.Fatalf("restored planner = %q", got)
	}

}

// Exercise the memory client's binding as well as config's contextual read:
// a saved paid default cannot replace the free model this session switched to.
func TestLowCreditReflexCallsRespectTheLiveConversation(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	asked := &modelAsked{}
	agent, _ := brainAgent(t, asked, func(c *Config) { c.ProfileDir = dir })
	for _, chat := range []string{"vendor/paid", config.FreeChatModel} {
		agent.SetModel(chat)
		client := agent.reflexClient()
		if client == nil {
			t.Fatal("no reflex client")
		}
		if _, err := client.CompleteWithMessages(context.Background(), []ai.Message{textMessage("user", "route this memory")}); err != nil {
			t.Fatal(err)
		}
		got := asked.model()
		if chat == config.FreeChatModel && got != chat {
			t.Fatalf("free chat %s called %s", chat, got)
		}
		if chat == "vendor/paid" && got != "nvidia/nemotron-3.5-lightning:free" {
			t.Fatalf("low paid chat called %s", got)
		}
	}
}
