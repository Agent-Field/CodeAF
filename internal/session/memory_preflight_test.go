package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// stalledMemoryProvider holds recall until its actual operation deadline, while
// the main answer remains healthy. This reproduces the missing preflight bound
// without a live account, a two-minute transport timeout, or a private log.
type stalledMemoryProvider struct {
	fallback reflexScript
	checked  chan error
}

func (c *stalledMemoryProvider) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if len(messages) > 0 && strings.Contains(messageText(messages[0]), "memory router") {
		if provider.RoleFrom(ctx) != lane.RoleRecall {
			c.checked <- errors.New("foreground recall was demoted to background memory")
			return nil, context.DeadlineExceeded
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > lane.VisiblePatience {
			c.checked <- errors.New("recall lacks its interactive deadline")
			return nil, context.DeadlineExceeded
		}
		<-ctx.Done()
		c.checked <- nil
		return nil, ctx.Err()
	}
	return c.fallback.CompleteWithMessages(ctx, messages, options...)
}

func TestAStalledMemoryLookupReleasesTheHealthyMainAnswer(t *testing.T) {
	c := &stalledMemoryProvider{checked: make(chan error, 1), fallback: reflexScript{answer: "the main answer arrived"}}
	agent, brain := brainAgent(t, c, nil)
	remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	events := mustSubmit(t, agent, "reformat this file for me")
	limit := time.NewTimer(lane.VisiblePatience + 5*time.Second)
	defer limit.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				select {
				case err := <-c.checked:
					if err != nil {
						t.Fatal(err)
					}
				default:
					t.Fatal("memory route was not exercised")
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
				return
			}
			if event.Kind == EventError {
				t.Fatalf("optional memory failure ended the turn: %+v", event)
			}
		case <-limit.C:
			t.Fatal("memory timeout did not release the main answer")
		}
	}
}
