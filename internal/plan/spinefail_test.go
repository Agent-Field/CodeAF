package plan

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// spineDownClient refuses every spine sample at once and answers the grounding
// call only when its context is cancelled — the shape of a planner whose
// spine was reset by the peer while the grounding reply was still hours off.
type spineDownClient struct {
	groundCancelled chan struct{}
}

func (c *spineDownClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	for _, m := range messages {
		if m.Role == "system" && textOf(m) == groundPrompt {
			<-ctx.Done()
			close(c.groundCancelled)
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("read response: connection reset by peer")
}

// A failed spine ends the build now, not when the grounding call it no longer
// needs has run out its own ceiling.
func TestAFailedSpineDoesNotWaitOnTheGrounding(t *testing.T) {
	client := &spineDownClient{groundCancelled: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := Build(context.Background(), client, "build seven tools", Options{Ensemble: EnsembleNever, SpineSamples: 3})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a build with no spine should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the build waited on a grounding call it had no use for")
	}
	select {
	case <-client.groundCancelled:
	case <-time.After(time.Second):
		t.Fatal("the grounding call was left running after the spine failed")
	}
}
