package session

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Concurrent calls made after a stop share the existing auxiliary-call guard.
// The obsolete automatic checkpoint participants are gone; exercise the common
// role call path that explicit work still uses.
func TestOneEscProducesAtMostOnePlannerPassAndOneTitleCall(t *testing.T) {
	var calls atomic.Int32
	completer := &scriptedCompleter{aside: func([]ai.Message) (*ai.Response, bool) {
		calls.Add(1)
		return textResponse("error handling"), true
	}}
	agent, _ := newTestAgent(t, completer, nil)
	agent.Interrupt()
	agent.interrupt.note("finish the error handling")
	var wg sync.WaitGroup
	for _, role := range []roles.Role{roles.RolePlanner, roles.RolePlanner, roles.RolePlanner, roles.RoleTaskName, roles.RoleTaskName} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = agent.callRole(context.Background(), role, "test/model", []ai.Message{textMessage("user", "finish the error handling")})
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 2 {
		t.Fatalf("concurrent stop handlers made %d calls, want one planner and one name", got)
	}
	agent.interrupt.finishTurn()
	if err := agent.interrupt.allow(roles.RolePlanner); err != nil {
		t.Fatalf("a later turn still refuses explicit planning: %v", err)
	}
	if err := agent.interrupt.allow(roles.RoleTaskName); err != nil {
		t.Fatalf("a later turn still refuses task naming: %v", err)
	}
}

// The real Submit boundary cancels the in-flight provider call. A redirect then
// receives its own answer without buying a replacement orchestration pass.
func TestAnInterruptCancelsTheWorkingCallAndAllowsTheRedirect(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	c := &simpleLoopCompleter{answer: func(ctx context.Context, _ []ai.Message, round int) (*ai.Response, error) {
		if round == 1 {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return textResponse("The redirected answer."), nil
	}}
	agent, _ := simpleLoopAgent(t, c)
	events := mustSubmit(t, agent, "Inspect the outstanding work and explain what remains to be done.")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("working call never started")
	}
	agent.Interrupt()
	collect(t, events)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupt did not cancel the provider call")
	}
	collect(t, mustSubmit(t, agent, "Instead, answer this replacement request directly."))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rounds != 2 || c.sideCalls != 0 {
		t.Fatalf("redirect used %d working and %d auxiliary calls", c.rounds, c.sideCalls)
	}
	if messageContentText(lastMessage(agent)) != "The redirected answer." {
		t.Fatal("redirect did not answer")
	}
}

func isSessionTitleCall(messages []ai.Message) bool {
	if len(messages) == 0 || messages[0].Role != "system" {
		return false
	}
	return messageContentText(messages[0]) == titleSystem
}

func TestLiveInterruptFanoutSpendsAtMostOnePlannerAndTitle(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY missing")
	}
	model := strings.TrimSpace(os.Getenv("AFORGE_LIVE_MODEL"))
	if model == "" {
		model = "deepseek/deepseek-v4-flash-0731"
	}
	inner, err := provider.NewClient(provider.Config{
		APIKey:  key,
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   model,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	watch := &interruptCallWatch{inner: inner}
	agent, err := newAgent(Config{
		Workspace:  t.TempDir(),
		Model:      model,
		APIKey:     key,
		BaseURL:    "https://openrouter.ai/api/v1",
		AskConsent: true,
		System:     "SYSTEM",
		RolesSource: tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): model,
			roles.TierKey(roles.TierLow):        model,
		}),
	}, watch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	inFlight := make(chan struct{})
	watch.onFirst = func() { close(inFlight) }
	events, err := agent.Submit(context.Background(), "count from one to twenty, one number per line")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	select {
	case <-inFlight:
	case <-time.After(20 * time.Second):
		t.Fatal("the first live call never started")
	}
	agent.Interrupt()
	collect(t, events)

	// A real provider shares neither the fixture collector's scheduling nor
	// its ten-second budget. Bound the request itself, so expiry cancels the
	// paid work instead of merely abandoning its event stream. The assertions
	// below count fanout and require a completed answer, never a fast answer.
	ctx, cancel := context.WithTimeout(context.Background(), liveInterruptWindow)
	defer cancel()
	redirect, err := agent.Submit(ctx, "never mind the count. Reply with the single word ok")
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	collectLiveInterrupt(t, ctx, redirect)

	if got := watch.planner.Load(); got > 1 {
		t.Fatalf("live Esc produced %d planner-shaped calls, want at most one", got)
	}
	if got := watch.title.Load(); got > 1 {
		t.Fatalf("live Esc produced %d title-shaped calls, want at most one", got)
	}
}

type interruptCallWatch struct {
	inner   Completer
	once    sync.Once
	onFirst func()
	planner atomic.Int32
	title   atomic.Int32
}

func (w *interruptCallWatch) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	w.once.Do(func() {
		if w.onFirst != nil {
			w.onFirst()
		}
	})
	switch {
	case isNameCall(messages) || isSessionTitleCall(messages):
		w.title.Add(1)
	default:
		var request ai.Request
		for _, option := range options {
			_ = option(&request)
		}
		if len(request.Tools) == 0 {
			w.planner.Add(1)
		}
	}
	return w.inner.CompleteWithMessages(ctx, messages, options...)
}

func seedLongConversation(agent *Agent, n int) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			agent.messages = append(agent.messages, textMessage("user", "please do the numbered part and the error handling"))
		} else {
			agent.messages = append(agent.messages, textMessage("assistant", "working through the numbered part"))
		}
	}
}

// liveInterruptWindow is a network safety bound, matching the adjacent live
// compact-history probe. It is not a latency gate; PERF.md records the distinction.
const liveInterruptWindow = 45 * time.Second

// collectLiveInterrupt requires the redirected turn to answer and close. The
// ordinary collector deliberately stays strict for deterministic fixtures and
// for the first turn's cancellation; only this actual provider call needs a
// network budget. Exact model wording is not the fanout law; errors, an empty
// answer, and a closed stream without completion are failures,
// even if neither condition spent an extra planner or title call.
func collectLiveInterrupt(t *testing.T, ctx context.Context, events <-chan Event) {
	t.Helper()
	var answer strings.Builder
	var done bool
	var seen []EventKind
	for {
		select {
		case event, open := <-events:
			if !open {
				if !done {
					t.Fatal("live redirect closed without completing")
				}
				if strings.TrimSpace(answer.String()) == "" {
					t.Fatalf("live redirect completed without an answer; events: %v", seen)
				}
				return
			}
			seen = append(seen, event.Kind)
			switch event.Kind {
			case EventTextDelta:
				answer.WriteString(event.Text)
			case EventRetrying:
				// The surface replaces a discarded attempt with the next one.
				// Its partial text is not part of the completed answer.
				answer.Reset()
			case EventTurnDone:
				done = true
			case EventError:
				t.Fatalf("live redirect failed: %v", event.Err)
			}
		case <-ctx.Done():
			t.Fatalf("live redirect did not finish: %v; answer so far: %q", ctx.Err(), answer.String())
		}
	}
}
