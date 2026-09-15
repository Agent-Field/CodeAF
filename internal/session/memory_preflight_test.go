package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// stalledMemoryProvider holds recall until its actual operation deadline, while
// the main answer remains healthy. This reproduces the missing preflight bound
// without a live account, a two-minute transport timeout, or a private log.
type stalledMemoryProvider struct {
	fallback reflexScript
	// checked carries what was true of the lookup's own request AT THE MOMENT IT
	// ARRIVED, and it is written there rather than when the call ends because the
	// call no longer ends before the turn does: the lookup runs beside the answer
	// now (loop.go's law), so a fixture that waited for its context would be
	// waiting for the eighteen seconds it is allowed rather than for the turn.
	checked chan error
}

func (c *stalledMemoryProvider) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if len(messages) > 0 && strings.Contains(messageText(messages[0]), "memory router") {
		if provider.RoleFrom(ctx) != lane.RoleRecall {
			c.checked <- errors.New("foreground recall was demoted to background memory")
			return nil, context.DeadlineExceeded
		}
		// THE BOUND IS THE ROLE'S GIVE-UP AND NOT ITS CEILING. The ceiling is when
		// a silence is ACTED on — two seconds, and the lookup moves to another
		// machine; the give-up is when it stops. They used to be the same instant,
		// which is why no lookup was ever rescued off a slow machine.
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > lane.RoleRecall.GiveUp() {
			c.checked <- errors.New("recall lacks its own bounded deadline")
			return nil, context.DeadlineExceeded
		}
		c.checked <- nil
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.fallback.CompleteWithMessages(ctx, messages, options...)
}

// A LOOKUP THAT NEVER ANSWERS COSTS THE PERSON NOTHING AT ALL.
//
// It used to cost them the whole of its window, because the turn was held behind
// it; it now runs beside the answer (loop.go's law), so what this proves is
// stronger than it was — the answer arrives while the lookup is still stalled,
// not merely before its deadline — and the lookup is still ASKED, correctly, on
// its own role and inside its own bound.
func TestAStalledMemoryLookupReleasesTheHealthyMainAnswer(t *testing.T) {
	c := &stalledMemoryProvider{checked: make(chan error, 1), fallback: reflexScript{answer: "the main answer arrived"}}
	agent, brain := brainAgent(t, c, nil)
	remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	began := time.Now()
	events := mustSubmit(t, agent, "reformat this file for me")
	limit := time.NewTimer(lane.RoleRecall.GiveUp())
	defer limit.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				// THE ANSWER CAME BACK WHILE THE LOOKUP WAS STILL STALLED, which is
				// the whole claim: the turn is over and the reading beside it has not
				// been released by anything but its own deadline, which has not
				// arrived.
				if waited := time.Since(began); waited >= lane.RoleRecall.Ceiling() {
					t.Fatalf("the answer took %s with a stalled lookup beside it, want it back "+
						"before the lookup had even been acted on", waited)
				}
				var text strings.Builder
				for _, message := range agent.snapshot() {
					if message.Role == "assistant" {
						text.WriteString(messageText(message))
					}
				}
				if !strings.Contains(text.String(), "the main answer arrived") {
					t.Fatalf("main answer missing: %q", text.String())
				}
				// AND THE LOOKUP WAS REALLY ASKED, on its own role and inside its own
				// bound. It is waited for HERE rather than read without waiting,
				// because nothing orders it against the turn any more — that is the
				// point of the change — and a fixture that read the channel the
				// instant the turn ended would be asserting a scheduling accident.
				select {
				case err := <-c.checked:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(lane.RoleRecall.GiveUp()):
					t.Fatal("memory route was not exercised")
				}
				return
			}
			if event.Kind == EventError {
				t.Fatalf("optional memory failure ended the turn: %+v", event)
			}
		case <-limit.C:
			t.Fatal("a stalled lookup held the main answer")
		}
	}
}
