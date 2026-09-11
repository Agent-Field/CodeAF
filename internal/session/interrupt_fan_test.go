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

// ONE ESC PRODUCES AT MOST ONE PLANNER PASS AND ONE TITLE CALL.
//
// F13/F17: one Escape, then a redirect, fired two mastermind replans, two
// identical title calls, and a 57-message handoff re-read in the same second.
// The handlers that used to race — the mark reader, the brief writer, two
// namers, the conversation-wide draft — now share one "what changed" decision
// (interrupt_fan.go). This drives those handlers concurrently after one Esc
// and a typed redirect, which is the measured shape.

func TestOneEscProducesAtMostOnePlannerPassAndOneTitleCall(t *testing.T) {
	var planner, title, handoffMsgs atomic.Int32
	completer := &scriptedCompleter{
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			switch {
			case isNameCall(messages) || isSessionTitleCall(messages):
				title.Add(1)
				return textResponse("error handling"), true
			case askedForSketch(messages) || askedToWriteHandoff(messages):
				planner.Add(1)
				if askedForSketch(messages) {
					return textResponse("A | B"), true
				}
				return textResponse("finish the error handling the person just asked for"), true
			default:
				return nil, false
			}
		},
		steps: []step{
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				if askedForHandoff(messages) {
					handoffMsgs.Store(int32(len(messages)))
					planner.Add(1)
					return textResponse("finish the error handling"), nil
				}
				return textResponse("(unscripted)"), nil
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): "test/planner",
			roles.TierKey(roles.TierLow):        "test/namer",
		})
	})
	seedLongConversation(agent, 57)

	const asked = "wait, never mind the big-number part. Just do the error handling"
	agent.Interrupt()
	agent.interrupt.note(asked)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); _ = agent.readMark(context.Background()) }()
	go func() {
		defer wg.Done()
		_, _ = agent.writeHandoff(context.Background(), asked, "", "")
	}()
	go func() { defer wg.Done(); _ = agent.nameAhead(asked) }()
	go func() { defer wg.Done(); _ = agent.nameAhead(asked) }()
	go func() {
		defer wg.Done()
		_, _, _, _ = agent.checkpointBrief(context.Background(), &Usage{}, "test/model")
	}()
	wg.Wait()
	// nameAhead lands on a goroutine; give the one allowed call a moment to
	// finish so the count is of completed requests, not of launches.
	waitFor(t, "the one allowed title call to land", func() bool {
		return title.Load() > 0 || completer.asideRequests() > 0
	})
	time.Sleep(50 * time.Millisecond)

	if got := planner.Load(); got > 1 {
		t.Fatalf("one Esc produced %d planner passes, want at most one", got)
	}
	if got := title.Load(); got > 1 {
		t.Fatalf("one Esc produced %d title calls, want at most one", got)
	}
	if got := handoffMsgs.Load(); got > 3 {
		t.Fatalf("a redirect re-read %d messages, want the conversation left unread", got)
	}

	t.Run("a later turn is not capped", func(t *testing.T) {
		agent.interrupt.finishTurn()
		if err := agent.interrupt.allow(roles.RoleMarkReader); err != nil {
			t.Fatalf("a later turn was still refused a planner pass: %v", err)
		}
		if err := agent.interrupt.allow(roles.RoleTaskName); err != nil {
			t.Fatalf("a later turn was still refused a title call: %v", err)
		}
	})
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
	case askedForSketch(messages) || askedToWriteHandoff(messages):
		w.planner.Add(1)
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
