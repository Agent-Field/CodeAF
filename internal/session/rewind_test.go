package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A rewind drops the last thing the person said and the whole turn it started —
// the reply, the tool calls, the results — and leaves everything before it
// exactly as it was.
func TestRewindDropsTheLastTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "how does the planner work?"))
	collect(t, mustSubmit(t, agent, "now delete it all"))

	removed, err := agent.Rewind()
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	// What came back is what the surface must un-draw: the person's message,
	// the assistant turn it started, the tool row inside it, and the result.
	if len(removed) == 0 {
		t.Fatal("Rewind reported nothing removed")
	}
	if removed[0].Role != "user" || removed[0].Text != "now delete it all" {
		t.Fatalf("first removed entry = %+v, want the message that started the turn", removed[0])
	}
	sawToolRow := false
	for _, entry := range removed {
		if entry.Role == "tool" && entry.Tool == "ls" {
			sawToolRow = true
		}
	}
	if !sawToolRow {
		t.Fatalf("the dropped turn's tool call was not reported: %+v", removed)
	}

	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript after rewind = %v, want the first turn alone", got)
	}
	if countMessages(agent, "now delete it all") != 0 {
		t.Fatal("the rewound message is still in the transcript")
	}

	// And the model never hears it again: the next request carries the first
	// turn and the new question, and nothing of the turn that was taken back.
	collect(t, mustSubmit(t, agent, "never mind — explain the executor"))
	sent := completer.request(3)
	for _, message := range sent {
		if strings.Contains(messageText(message), "now delete it all") {
			t.Fatalf("the rewound turn rode the next request: %v", rolesOf(sent))
		}
	}
	if got, want := rolesOf(sent), []string{"system", "user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("next request = %v, want %v", got, want)
	}
}

// The journal is rewound too, and a session that rewinds and keeps working
// resumes as itself: the dropped turn is gone, and everything said AFTER the
// rewind is ordinary conversation that replays.
func TestRewindSurvivesAResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the executor runs leaves"), nil
		},
	}}
	first, workspace := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, first, "how does the planner work?"))
	collect(t, mustSubmit(t, first, "now delete it all"))
	if _, err := first.Rewind(); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	collect(t, mustSubmit(t, first, "explain the executor instead"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	second.mu.Lock()
	restored := append([]ai.Message(nil), second.messages...)
	second.mu.Unlock()

	if got, want := rolesOf(restored), []string{"system", "user", "assistant", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("resumed transcript = %v, want the rewound turn gone and the new one kept", got)
	}
	for _, message := range restored {
		if strings.Contains(messageText(message), "now delete it all") {
			t.Fatal("the rewound turn came back on resume")
		}
	}
	if messageText(restored[3]) != "explain the executor instead" {
		t.Fatalf("post-rewind message = %q", messageText(restored[3]))
	}
	if messageText(restored[4]) != "the executor runs leaves" {
		t.Fatalf("post-rewind reply = %q", messageText(restored[4]))
	}
}

// A rewind under a running turn would cut the transcript the turn is writing.
// It refuses instead, and says which door to use.
func TestRewindRefusesWhileATurnRuns(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "working on it")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "start something long")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never started")
	}

	if _, err := agent.Rewind(); !errors.Is(err, ErrTurnInFlight) {
		t.Fatalf("Rewind mid-turn = %v, want ErrTurnInFlight", err)
	}

	agent.Interrupt()
	collect(t, events)

	// Once the turn is over the same rewind lands: the refusal was about
	// timing, not about the request.
	if _, err := agent.Rewind(); err != nil {
		t.Fatalf("Rewind after the turn ended: %v", err)
	}
	if got, want := transcriptRoles(agent), []string{"system"}; !equalStrings(got, want) {
		t.Fatalf("transcript = %v, want the interrupted turn dropped", got)
	}
}

// Nothing said, nothing to take back. The sentinel is what lets the surface say
// so instead of reporting a success that removed nothing.
func TestRewindWithNothingToRewind(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.Rewind(); !errors.Is(err, ErrNothingToRewind) {
		t.Fatalf("Rewind on a fresh session = %v, want ErrNothingToRewind", err)
	}
}

// A compaction summary is a user-role message this package injected, not
// something anybody said. Rewinding to it would drop a whole resumed
// conversation and leave the summary that replaced its beginning.
func TestRewindSkipsTheCompactionNote(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", compactionNote("## Goal\nship the thing")),
		textMessage("user", "so where were we?"),
		textMessage("assistant", "here is where we were"))
	agent.mu.Unlock()

	if _, err := agent.Rewind(); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user"}; !equalStrings(got, want) {
		t.Fatalf("transcript = %v, want the summary note kept", got)
	}

	// And the note itself is not a turn: a second rewind has nothing left.
	if _, err := agent.Rewind(); !errors.Is(err, ErrNothingToRewind) {
		t.Fatalf("second Rewind = %v, want ErrNothingToRewind", err)
	}
}
