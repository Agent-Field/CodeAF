//go:build linux

package session

import (
	"context"
	"fmt"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/executor"
	"strings"
	"testing"
)

// TestC18DegradedJailIsSaidOnceInTheConversation proves C18 through the real
// session and shell tool: setup work runs without namespaces, and its reduced
// limits are one dim notice across multiple calls and multiple turns.
func TestC18DegradedJailIsSaidOnceInTheConversation(t *testing.T) {
	t.Cleanup(executor.UseNamespaceProbeInTests(func() bool { return false }))
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprint("background=", background), func(t *testing.T) {
			bash := func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("shell", "bash", fmt.Sprintf(`{"command":"printf ran","background":%v}`, background)), nil
			}
			script := &scriptedCompleter{steps: []step{bash, finalText("done"), bash, finalText("done again")}}
			agent, _ := newTestAgent(t, script, func(c *Config) { c.Seat = executor.Stance{Class: executor.Sandboxed, Setup: true} })
			notices := 0
			for _, said := range []string{"run one", "run another"} {
				events := collect(t, mustSubmit(t, agent, said))
				for _, event := range events {
					if event.Kind == EventNotice && event.Text == executor.DegradedLine {
						notices++
						if event.Told {
							t.Fatal("the reduced-limit line is not a dim notice")
						}
					}
					if event.Kind == EventToolFailed {
						t.Fatalf("command failed: %+v", event)
					}
					if !background && event.Kind == EventToolEnd && !strings.Contains(event.Output, "ran") {
						t.Fatalf("setup did not run: %+v", event)
					}
				}
			}
			if notices != 1 {
				t.Fatalf("reduced limits were announced %d times", notices)
			}
		})
	}
}
