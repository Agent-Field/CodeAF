package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// Resolve through the session binding, so switching chat moves the actual
// request while tier and role choices remain above the conversation floor.
func TestReflexClientFollowsTheLiveConversationUnlessWritten(t *testing.T) {
	for _, state := range []string{"unset", "cleared", "written", "role pin"} {
		t.Run(state, func(t *testing.T) {
			rows := map[string]string{}
			if state == "cleared" {
				rows[config.KeyTierReflexModel] = ""
			}
			if state == "written" {
				rows[config.KeyTierReflexModel] = "vendor/pinned"
			}
			source := profileTiers(t, rows)
			if state == "role pin" {
				source = func(key string) (string, bool) { return "vendor/role-pin", key == roles.PinKey(roles.RoleReflex) }
			}
			asked := &modelAsked{}
			agent, _ := brainAgent(t, asked, func(c *Config) { c.RolesSource = source })
			for _, chat := range []string{"vendor/chat", "vendor/changed-chat"} {
				agent.SetModel(chat)
				client := agent.reflexClient()
				if client == nil {
					t.Fatal("no reflex client")
				}
				if _, err := client.CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "route this memory")}); err != nil {
					t.Fatal(err)
				}
				want := chat
				if state == "written" {
					want = "vendor/pinned"
				}
				if state == "role pin" {
					want = "vendor/role-pin"
				}
				if got := asked.model(); got != want {
					t.Fatalf("%s called %s, want %s", state, got, want)
				}
			}
		})
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
