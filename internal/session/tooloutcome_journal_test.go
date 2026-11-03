package session

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestToolOutcomeJournalPreservesProducerResult(t *testing.T) {
	_, path, _, _, _ := rejectionTurn(t, []rejectionCall{
		{id: "success", tool: "bash", args: `{"command":"printf 'PASS\\nCommand aborted\\n'"}`},
		{id: "failure", tool: "bash", args: `{"command":"exit 3"}`},
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"success": "ok", "failure": "failed"}
	for _, line := range strings.Split(string(body), "\n") {
		var row struct {
			Role         string `json:"role"`
			Call         string `json:"toolCallId"`
			Presentation struct {
				Outcome string `json:"outcome"`
			} `json:"presentation"`
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if expected, ok := want[row.Call]; ok && row.Role == "tool" {
			if row.Presentation.Outcome != expected {
				t.Errorf("%s: outcome=%q want %q", row.Call, row.Presentation.Outcome, expected)
			}
			delete(want, row.Call)
		}
	}
	if len(want) != 0 {
		t.Fatal("missing tool results", want)
	}
}

func TestToolOutcomeJournalPreservesInterruptions(t *testing.T) {
	for _, outcome := range []string{"cancelled", "timed out", "deadline"} {
		t.Run(outcome, func(t *testing.T) {
			path := t.TempDir() + "/journal.jsonl"
            args := `{"command":"sleep 5", "timeout":5}`
            if outcome == "timed out" {
                args = `{"command":"sleep 5", "timeout":0.05}`
            }
			agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("cut", "bash", args), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
			}}, func(c *Config) { c.SessionFile = path })
			tool := bareBashTool(t, agent)
			execute := tool.Execute
			tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
				if outcome == "cancelled" {
					ctx, cancel := context.WithCancel(ctx)
					timer := time.AfterFunc(20*time.Millisecond, cancel)
					defer timer.Stop()
					defer cancel()
					return execute(ctx, args)
				}
				if outcome == "deadline" {
					ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
					defer cancel()
					return execute(ctx, args)
				}
				return execute(ctx, args)
			}
			for i := range agent.tools {
				if agent.tools[i].Name == "bash" {
					agent.tools[i] = tool
				}
			}
			collect(t, mustSubmit(t, agent, "run the check"))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := outcome
			if want == "deadline" {
				want = "timed out"
			}
			found := false
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var row struct {
					Role         string `json:"role"`
					Presentation struct {
						Outcome string `json:"outcome"`
					} `json:"presentation"`
				}
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				if row.Role == "tool" {
					found = true
					if row.Presentation.Outcome != want {
						t.Errorf("producer outcome=%q want %q", row.Presentation.Outcome, want)
					}
				}
			}
			if !found {
				t.Fatal("missing tool receipt")
			}
		})
	}
}
