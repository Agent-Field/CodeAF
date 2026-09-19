package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE EVENT SAYS WHICH KIND OF ANSWER THE HARNESS WROTE, AND IT SAYS SO WHERE THE
// ATTEMPTED ACTION IS KNOWN. One worker on the bash belt, through the real turn
// loop, draws both kinds and then a call that runs:
//
//   - two calls in one response is a fault in the FORM of the reply. The harness
//     answers it, nothing was attempted on the world, and the event is
//     HarnessMade without Refused;
//   - one well-formed call that a door refuses (here the guard that keeps a
//     worker from reaching a remote) is AN ACTION THE WORKER ATTEMPTED. The event
//     is HarnessMade AND Refused, which is the fact the run's record and the task
//     page draw a refused action from;
//   - a call that ran carries neither.
//
// The test reads the facts and never the answers' words, which is the law the
// readers downstream are held to as well.
func TestARefusedActionIsToldApartFromACorrectionOnTheEvent(t *testing.T) {
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q", "-b", "main")
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{
					{ID: "one", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo form-first"}`}},
					{ID: "two", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo form-second"}`}},
				},
			}}}}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("three", "bash", `{"command":"git push origin HEAD"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("four", "bash", `{"command":"echo it-ran"}`), nil
		},
		finalText("done"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})

	type answer struct{ harness, refused, failed bool }
	seen := map[string]answer{}
	for _, event := range collect(t, mustSubmit(t, agent, "go")) {
		if event.Kind != EventToolFailed && event.Kind != EventToolEnd {
			continue
		}
		for _, mark := range []string{"form-first", "form-second", "git push", "it-ran"} {
			if strings.Contains(event.Args, mark) {
				seen[mark] = answer{harness: event.HarnessMade, refused: event.Refused, failed: event.Kind == EventToolFailed}
			}
		}
	}
	for mark, want := range map[string]answer{
		"form-first":  {harness: true, refused: false, failed: true},
		"form-second": {harness: true, refused: false, failed: true},
		"git push":    {harness: true, refused: true, failed: true},
		"it-ran":      {harness: false, refused: false, failed: false},
	} {
		got, ok := seen[mark]
		if !ok {
			t.Errorf("no ended event for the call carrying %q", mark)
			continue
		}
		if got != want {
			t.Errorf("the call carrying %q ended as %+v, want %+v", mark, got, want)
		}
	}
}
