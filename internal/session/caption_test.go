package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestTheNarratorDoesNotFireOnABatchThatFinishesFast(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	hub := newEventHub()
	defer hub.close()

	results := agent.runToolsWarm(context.Background(), agent.newEpisode(),
		[]ai.ToolCall{fixBash("true")}, hub, nil)
	if len(results) != 1 || results[0].isError {
		t.Fatalf("the instant batch did not finish cleanly: %+v", results)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, event := range hub.backlog {
		if event.Kind == EventCaption {
			t.Fatalf("a fast batch grew a narrator caption: %q", event.Text)
		}
	}
}

func TestTheNarratorStopsAfterThreeCallsInOneTurn(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("checking the first silence"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("checking the second silence"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("checking the third silence"), nil
		},
	}}
	agent, _ := newTestAgent(t, client, nil)
	hub := newEventHub()
	defer hub.close()
	ep := agent.newEpisode()
	ctx := withEpisode(context.Background(), ep)
	calls := []ai.ToolCall{fixBash("sleep 30")}

	for range captionCalls + 1 {
		agent.maybeCaption(ctx, hub, calls, []string{`{"command":"sleep 30"}`})
	}

	if got := client.requests(); got != captionCalls {
		t.Fatalf("narrator calls = %d, want the per-turn cap %d", got, captionCalls)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if got := countKind(hub.backlog, EventCaption); got != captionCalls {
		t.Fatalf("caption events = %d, want %d", got, captionCalls)
	}
}

func TestANarratorAnswerThatIsTheInstructionIsRefused(t *testing.T) {
	for _, echoed := range []string{
		captionPrompt,
		"What is this work trying to find out?",
		"Sure: " + captionPrompt,
	} {
		if got := cleanCaption(echoed); got != "" {
			t.Errorf("cleanCaption(%q) = %q, want the instruction refused", echoed, got)
		}
	}
	if got := cleanCaption("Caption: checking where the fold is minted"); got != "checking where the fold is minted" {
		t.Fatalf("an ordinary caption cleaned to %q", got)
	}
	got := cleanCaption("Good leads. Fetching the key pages to confirm which are open.")
	if got != "Fetching the key pages to confirm which are open" {
		t.Fatalf("cleanCaption did not keep one short sentence: %q", got)
	}
	long := "fetching the key pages to confirm which are actually still open tonight in toronto"
	if got := cleanCaption(long); got != "fetching the key pages to confirm" {
		t.Fatalf("cleanCaption left a mid-clause cut: %q", got)
	}
	if strings.Contains(cleanCaption(long), "…") {
		t.Fatal("cleanCaption appended an ellipsis")
	}
}

func TestTheNarratorInstructionComesLast(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if got := messageContentText(messages[0]); got != captionSystem {
				t.Errorf("system message = %q, want character only", got)
			}
			user := messageContentText(messages[1])
			if !strings.HasSuffix(user, captionPrompt) {
				t.Errorf("instruction is not last:\n%s", user)
			}
			return textResponse("checking the fold"), nil
		},
	}}
	agent, _ := newTestAgent(t, client, nil)
	agent.maybeCaption(withEpisode(context.Background(), agent.newEpisode()),
		newEventHub(), []ai.ToolCall{fixBash("true")}, []string{`{"command":"true"}`})
}
