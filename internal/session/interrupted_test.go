package session

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// cutOff is an executor.Interrupted with a fixed note that counts its closes.
type cutOff struct {
	mu     sync.Mutex
	note   string
	closed int
}

func (c *cutOff) Lines() []string { return nil }

func (c *cutOff) Note() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed > 0 {
		return ""
	}
	return c.note
}

func (c *cutOff) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func TestTheModelIsToldWhichCallsWereCutOffOnceAndTheyAreThenClosed(t *testing.T) {
	cut := &cutOff{note: "- bash: sleep 150 (local)"}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok again"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.Interrupted = cut })
	for _, said := range []string{"where were we?", "and now?"} {
		events, err := agent.Submit(context.Background(), said)
		if err != nil {
			t.Fatal(err)
		}
		collect(t, events)
	}
	first := userTextIn(completer.request(0))
	if !strings.Contains(first, interruptedNoteOpening) || !strings.Contains(first, "bash: sleep 150") {
		t.Fatalf("the first request did not carry the cut-off note:\n%s", first)
	}
	if cut.closed != 1 {
		t.Fatalf("closed %d times, want once", cut.closed)
	}
	if again := strings.Count(userTextIn(completer.request(1)), interruptedNoteOpening); again != 1 {
		t.Fatalf("the note rode the second request %d times, want the one from before", again)
	}
}

func TestTheCutOffNoteIsNeverThePersonsWords(t *testing.T) {
	if !isVolatileNote(interruptedNoteOpening + "\n\nanything") {
		t.Fatal("the cut-off note would be drawn as something the person typed")
	}
}
