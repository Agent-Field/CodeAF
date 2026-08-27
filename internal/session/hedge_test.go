package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const hedgeTestFloor = 10 * time.Millisecond

// watchedHedgeContext is runTurn's observed context in miniature. Keeping the
// floor on the capability makes these tests exercise the real timer without
// turning the production eight-second promise into a mutable package variable.
func watchedHedgeContext(partial *partialBuffer) context.Context {
	observer := func(event provider.StreamEvent) {
		if event.Kind == provider.StreamDelta {
			partial.write(event.Delta)
		}
	}
	ctx := provider.WithStreamObserver(context.Background(), observer)
	return withInteractiveHedge(ctx, observer, hedgeTestFloor)
}

func completeHedgeTest(t *testing.T, agent *Agent, ctx context.Context, hub *eventHub, partial *partialBuffer) (*ai.Response, error) {
	t.Helper()
	response, _, err := agent.completeWithRetry(ctx, hub, "hedge/test-model", effort.None,
		partial, &warmBatch{}, &formingBatch{})
	return response, err
}

func hedgeResponseText(t *testing.T, response *ai.Response) string {
	t.Helper()
	if response == nil || len(response.Choices) == 0 {
		t.Fatalf("response has no answer: %+v", response)
	}
	return messageText(response.Choices[0].Message)
}

func TestTTFTHedgeLeavesAFastPrimaryAlone(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "primary")
			return textResponse("primary"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	partial := &partialBuffer{}

	response, err := completeHedgeTest(t, agent, watchedHedgeContext(partial), newEventHub(), partial)
	if err != nil {
		t.Fatal(err)
	}
	if got := hedgeResponseText(t, response); got != "primary" {
		t.Fatalf("answer = %q, want primary", got)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want one fast primary", got)
	}
}

func TestTTFTHedgeKeepsTheSecondaryWinnerAndCancelsThePrimary(t *testing.T) {
	primaryCancelled := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			<-ctx.Done()
			close(primaryCancelled)
			return nil, ctx.Err()
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "hedge")
			return textResponse("hedge"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	partial := &partialBuffer{}
	hub := newEventHub()

	response, err := completeHedgeTest(t, agent, watchedHedgeContext(partial), hub, partial)
	if err != nil {
		t.Fatal(err)
	}
	if got := hedgeResponseText(t, response); got != "hedge" {
		t.Fatalf("answer = %q, want hedge", got)
	}
	if got := partial.take(); got != "hedge" {
		t.Fatalf("visible stream = %q, want only the hedge winner", got)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("requests = %d, want primary plus one hedge", got)
	}
	if !reflect.DeepEqual(completer.request(0), completer.request(1)) {
		t.Fatal("the hedge did not receive the primary's identical transcript")
	}
	select {
	case <-primaryCancelled:
	case <-time.After(time.Second):
		t.Fatal("the losing primary was not cancelled promptly")
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	var notice string
	for _, event := range hub.backlog {
		if event.Kind == EventRetrying {
			notice = event.Text
		}
	}
	if !strings.Contains(notice, "no first token in 10ms") || !strings.Contains(notice, "in parallel") {
		t.Fatalf("hedge notice = %q", notice)
	}
}

func TestTTFTHedgeWaitsForPrimaryWhenTheSecondaryErrors(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			timer := time.NewTimer(4 * hedgeTestFloor)
			defer timer.Stop()
			select {
			case <-timer.C:
				provider.Emit(ctx, provider.StreamDelta, "primary")
				return textResponse("primary"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, errors.New("secondary failed")
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	partial := &partialBuffer{}

	response, err := completeHedgeTest(t, agent, watchedHedgeContext(partial), newEventHub(), partial)
	if err != nil {
		t.Fatal(err)
	}
	if got := hedgeResponseText(t, response); got != "primary" {
		t.Fatalf("answer = %q, want primary", got)
	}
	if got := partial.take(); got != "primary" {
		t.Fatalf("visible stream = %q, want primary", got)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("requests = %d, want two", got)
	}
}

func TestTTFTHedgeCountsTwoFailuresAsOneRetryAttempt(t *testing.T) {
	hedgeStarted := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			<-hedgeStarted
			return nil, errors.New("503 primary unavailable")
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(hedgeStarted)
			return nil, errors.New("503 hedge unavailable")
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "retry")
			return textResponse("retry"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	partial := &partialBuffer{}

	response, err := completeHedgeTest(t, agent, watchedHedgeContext(partial), newEventHub(), partial)
	if err != nil {
		t.Fatal(err)
	}
	if got := hedgeResponseText(t, response); got != "retry" {
		t.Fatalf("answer = %q, want retry", got)
	}
	if got := completer.requests(); got != 3 {
		t.Fatalf("requests = %d, want two failed racers and one retry", got)
	}
}

func TestTTFTHedgeIsAbsentOutsideTheInteractiveTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			timer := time.NewTimer(4 * hedgeTestFloor)
			defer timer.Stop()
			select {
			case <-timer.C:
				provider.Emit(ctx, provider.StreamDelta, "single")
				return textResponse("single"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.InTask = true
	})
	// Hand the task a hedge capability deliberately: runTurn must remove it,
	// because a task can inherit a context from the conversation that started it.
	inherited := &partialBuffer{}

	events, err := agent.Submit(watchedHedgeContext(inherited), "go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := messageText(lastMessage(agent)); got != "single" {
		t.Fatalf("transcript answer = %q, want single", got)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want one on the task path", got)
	}
}
