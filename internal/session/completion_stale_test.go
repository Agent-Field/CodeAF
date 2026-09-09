package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A direction received while the model is finishing reaches the request that
// writes the actual final answer. Removing a completion reader must not make
// the response already in flight authoritative over newer user input.
func TestQueuedDirectionReachesTheActualFinalRequest(t *testing.T) {
	const revision = "Use CSV, and sort the rows by port."
	entered, release := make(chan struct{}), make(chan struct{})
	c := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return textResponse("The old report is ready."), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			found := false
			for _, m := range messages {
				if m.Role == "user" && strings.Contains(messageContentText(m), revision) {
					found = true
				}
			}
			if !found {
				t.Error("final answer was requested without the newer direction")
			}
			return textResponse("The CSV rows are sorted by port."), nil
		},
	}}
	a, _ := newTestAgent(t, c, nil)
	events := mustSubmit(t, a, "Write the report in Markdown.")
	<-entered
	mustSteer(t, a, revision)
	close(release)
	collect(t, events)
	if c.requests() != 2 {
		t.Fatalf("revision used %d calls, want the interrupted response and one corrected answer", c.requests())
	}
	if got := messageContentText(lastMessage(a)); !strings.Contains(got, "CSV rows are sorted") {
		t.Fatalf("stale final answer won: %q", got)
	}
}

// A normal completed response does not consume or cancel an outstanding job.
// Its later receipt must still wake the same conversation and reach the model.
func TestOrdinaryCompletionKeepsTheBackgroundResultOwed(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "release")
	command := "while [ ! -f " + flag + " ]; do sleep 0.02; done; echo built"
	args, _ := json.Marshal(map[string]any{"command": command, "background": true})
	var answered atomic.Int32
	c := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("job-start", "bash", string(args)), nil
		},
	}}
	for range 8 {
		c.steps = append(c.steps, func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			for _, m := range messages {
				if m.Role == "user" && strings.Contains(messageContentText(m), "job 1 exited 0") {
					answered.Add(1)
					return textResponse("The build finished successfully."), nil
				}
			}
			return textResponse("The command is running."), nil
		})
	}
	a, _ := newTestAgent(t, c, nil)
	wakes := a.Wakes()
	t.Cleanup(func() { _ = os.WriteFile(flag, []byte("go"), 0600) })
	collect(t, mustSubmit(t, a, "Run the command and report its result."))
	if !jobStillRunning(t, a, 1) {
		t.Fatal("ordinary completion stopped the running command")
	}
	if err := os.WriteFile(flag, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case stream := <-wakes:
		collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("completed background job did not wake the conversation")
	}
	if got := answered.Load(); got != 1 {
		t.Fatalf("actual job receipt answered %d times, want once", got)
	}
	if !strings.Contains(messageContentText(lastMessage(a)), "build finished successfully") {
		t.Fatal("background result was not answered")
	}
}
