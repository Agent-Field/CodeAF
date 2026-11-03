package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func TestWakeB_OneModelCallWithCostAndNoTools(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known", false: "unknown"}[known], func(t *testing.T) {
			usd := .04
			client := &scriptedCompleter{steps: []step{func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
				if len(messages) != 2 || provider.RoleFrom(ctx) != lane.RoleStanding || provider.RoutingIntentFrom(ctx) != provider.IntentBackground {
					t.Fatalf("wake call: %+v", messages)
				}
				if !strings.Contains(messages[0].Content[0].Text, "person's pages") || !strings.Contains(messages[1].Content[0].Text, "Answer format:") {
					t.Fatal("wake rules absent")
				}
				r := textResponse(`{"summary":"Next steps","items":[]}`)
				if known {
					r.Usage = &ai.Usage{Cost: &usd, PromptTokens: 10, CompletionTokens: 5}
				} else {
					r.Usage = nil
				}
				return r, nil
			}}}
			var lines []UsageLine
			answer, err := workstreamWakeCall(context.Background(), client, "cheap/model", WorkstreamWakeAsk{ID: "wake", AgentID: "agent", Event: workstream.WakeEvent{Kind: "morning"}, Snapshot: json.RawMessage(`{}`)}, func(line UsageLine) { lines = append(lines, line) })
			if err != nil || client.requests() != 1 || len(lines) != 1 || answer.Model != "cheap/model" {
				t.Fatal(answer, lines, err)
			}
			if known && (answer.CostUSD == nil || *answer.CostUSD != .04 || lines[0].USD != .04) {
				t.Fatal("cost lost")
			}
			if !known && (answer.CostUSD != nil || !lines[0].Unbilled) {
				t.Fatal("unknown cost fabricated")
			}
		})
	}
}
func TestWakeB_SnapshotUsesReadCutRule(t *testing.T) {
	text := workstreamWakeQuestion(WorkstreamWakeAsk{Snapshot: json.RawMessage(`{"words":"` + strings.Repeat("é", 40000) + `"}`)})
	start := strings.Index(text, "Numbers absent below")
	end := strings.Index(text, "\nAnswer format:")
	if start < 0 || end < start || end-start > 60000 || !utf8.ValidString(text) || !strings.HasSuffix(text[start:end], "\nThe workstream read was cut.") {
		t.Fatal("snapshot cut drifted")
	}
}
