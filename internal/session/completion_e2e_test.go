//go:build e2e

package session

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// This is a real completion-reader check over controlled evidence, alongside
// the organization suite's full Submit journeys. Incorrect inputs stay visible:
// accepting every finished write would hide the same bug in the other direction.
func TestRealCompletionEvidence(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		if os.Getenv("AFORGE_E2E_REQUIRE_LIVE") != "" {
			t.Fatal("live completion check requires OPENROUTER_API_KEY")
		}
		t.Skip("no OPENROUTER_API_KEY")
	}
	const model = "deepseek/deepseek-v4-flash"
	const correct = `{"value":"receipt_id","record_id":"059f308d57830a036ed0b080c97491ae","revision":1,"source_id":"805ee9f1df5f4ef0","draft":"Use receipt_id."}`
	for _, tc := range []struct {
		name, content, outcome string
		done                   bool
	}{
		{"correct", correct, "Successfully wrote report.json", true},
		{"wrong_type", strings.Replace(correct, `"revision":1`, `"revision":"1"`, 1), "Successfully wrote report.json", false},
		{"wrong_source", strings.Replace(correct, "805ee9f1df5f4ef0", "stale-source", 1), "Successfully wrote report.json", false},
		{"failed_write", correct, "Error: permission denied; file was not written", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := provider.NewClient(provider.Config{APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: model})
			if err != nil {
				t.Fatal(err)
			}
			a := checkpointAgent(t, client, func(cfg *Config) {
				cfg.Model = model
				cfg.OneModel = true
				cfg.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierMastermind): model})
			})
			workedTurn(a, `Write report.json with value receipt_id, record_id 059f308d57830a036ed0b080c97491ae, revision as the JSON number 1 (not a string), source_id 805ee9f1df5f4ef0, and a one-sentence draft.`, 0)
			args, _ := json.Marshal(map[string]string{"path": strings.Repeat("long-directory/", 10) + "report.json", "content": tc.content})
			a.record(toolCallMessage("write-report", "write", string(args)))
			a.record(ai.Message{Role: "tool", ToolCallID: "write-report", Content: []ai.ContentPart{{Type: "text", Text: tc.outcome}}})
			a.record(textMessage("assistant", "The report is written."))
			read := a.readRemains(context.Background())
			t.Logf("READER model=%s done=%v answered=%v unreachable=%v observation=%q spend=$%.6f", model, read.nothingLeft, read.answered, read.unreachable, read.said, a.Usage().CostUSD)
			if !read.answered || read.unreachable || read.nothingLeft != tc.done || (!tc.done && read.said == "") {
				t.Fatalf("completion judgment does not match actual write evidence: %+v", read)
			}
			if a.Usage().CostUSD > 0.05 {
				t.Fatal("completion fixture exceeded $0.05")
			}
		})
	}
}
