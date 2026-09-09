package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAnInvalidShellCallIsRepairedWithoutAskingThePerson(t *testing.T) {
	for name, args := range map[string]string{
		"truncated":          "{",
		"missing command":    "{}",
		"null":               "null",
		"wrong command type": `{"command":17}`,
		"empty command":      `{"command":"   "}`,
	} {
		t.Run(name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("invalid-shell", "bash", args), nil
				},
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					if !strings.Contains(roleText(messages, "tool"), invalidArgumentsPrefix) {
						t.Error("the model did not receive an actionable argument error")
					}
					return toolResponse("repaired-shell", "bash", `{"command":"printf RECOVERED"}`), nil
				},
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					recovered := false
					for _, message := range messages {
						if message.Role == "tool" && strings.Contains(messageText(message), "RECOVERED") {
							recovered = true
						}
					}
					if !recovered {
						t.Error("the model did not receive the repaired command's result")
					}
					return textResponse("Recovered and verified."), nil
				},
			}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
				c.AskConsent = true
			})
			events := drainAnswering(t, mustSubmit(t, agent, "Run the check."), func(request Event) {
				t.Error("an invalid shell call asked the person for permission")
				agent.ResolveConsent(request.ID, false)
			})
			if countKind(events, EventConsentRequest) != 0 {
				t.Fatal("argument recovery required user intervention")
			}
		})
	}
}
