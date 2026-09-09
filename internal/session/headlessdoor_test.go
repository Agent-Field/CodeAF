package session

import (
	"context"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The screen and budget choose who owns real execution state; neither one
// inserts a classifier or completion reviewer into the model's conversation.
func TestHeadlessAndWatchedTurnsKeepTheSameMainContext(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		interactive, unattended, budget, steward bool
	}{
		{"watched", true, false, false, false},
		{"headless bounded", false, true, true, true},
		{"headless ordinary", false, false, false, false},
		{"headless unbounded", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const ask = "Read input.txt and report its exact contents."
			c := &scriptedCompleter{steps: []step{
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					found := false
					for _, m := range messages {
						if m.Role == "user" && messageContentText(m) == ask {
							found = true
						}
					}
					if !found {
						t.Error("working model did not receive the original ask")
					}
					return toolResponse("call_0", "read", `{"path":"input.txt"}`), nil
				},
				func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
					found := false
					for _, m := range messages {
						if m.Role == "tool" && strings.Contains(messageContentText(m), "exact input") {
							found = true
						}
					}
					if !found {
						t.Error("working model lost its tool result")
					}
					return textResponse("The file says exact input."), nil
				},
			}}
			a, dir := newTestAgent(t, c, func(config *Config) {
				config.Interactive = tc.interactive
				config.Unattended = tc.unattended
				if tc.budget {
					config.Budget = Budget{Wall: time.Hour}
				}
			})
			if (a.steward() != nil) != tc.steward {
				t.Fatal("door chose the wrong owner for concrete execution state")
			}
			if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("exact input"), 0600); err != nil {
				t.Fatal(err)
			}
			collect(t, mustSubmit(t, a, ask))
			if c.requests() != 2 {
				t.Fatalf("door added another model call: %d", c.requests())
			}
			if !strings.Contains(messageContentText(lastMessage(a)), "file says exact input") {
				t.Fatal("normal answer was replaced")
			}
		})
	}
}
