package pool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// hangingClient is the provider from the incident: it takes the call and never
// answers. Everything below is about who, if anybody, eventually stops waiting.
type hangingClient struct {
	calls atomic.Int64
	// saw records the deadline the inner call was actually given, which is the
	// difference between a wall that exists and a wall that is applied.
	saw atomic.Bool
}

func (h *hangingClient) Model() string { return "hanging/model" }

func (h *hangingClient) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	h.calls.Add(1)
	if _, ok := ctx.Deadline(); ok {
		h.saw.Store(true)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// answeringClient returns immediately, which is what every honest call does.
type answeringClient struct{ calls atomic.Int64 }

func (a *answeringClient) Model() string { return "answering/model" }

func (a *answeringClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	a.calls.Add(1)
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}}},
		FinishReason: "stop",
	}}}, nil
}

// TestAWalledCallStopsWaitingAndSaysWhy is the whole first layer. The contract
// call that hung had no deadline anywhere on its path; this is that call, and
// it now ends.
func TestAWalledCallStopsWaitingAndSaysWhy(t *testing.T) {
	hanging := &hangingClient{}
	client := Adopt(config.Config{}, "hanging/model", hanging).WithCallWall(50 * time.Millisecond)

	started := time.Now()
	_, err := client.CompleteWithMessages(context.Background(), nil)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a call that never answered returned no error")
	}
	if !errors.Is(err, ErrCallWall) {
		t.Fatalf("error = %v, want the call wall", err)
	}
	// The reconciler's watchdog asks exactly this question, and asking it must
	// not require importing this package.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a wall expiry is not legible as a deadline: %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("the wall took %s to fire", elapsed)
	}
	if !hanging.saw.Load() {
		t.Fatal("the inner call was never given a deadline")
	}
	// The layer above owns the retry. One call in, one call out.
	if got := hanging.calls.Load(); got != 1 {
		t.Fatalf("inner calls = %d, want exactly 1 — the wall must not retry", got)
	}
}

// TestTheWallTravelsWithASnapshot is the part that decides whether any of this
// reaches the call that actually hung. plan.Build and plan.Contracts do not
// hold the slot — they take a snapshot once and call it for the rest of the
// pass — so a wall that lived only on the slot's own method would have bounded
// every structuring call except the expensive ones.
func TestTheWallTravelsWithASnapshot(t *testing.T) {
	hanging := &hangingClient{}
	client := Adopt(config.Config{}, "hanging/model", hanging).WithCallWall(50 * time.Millisecond)

	model, structuring := client.Snapshot()
	if model != "hanging/model" {
		t.Fatalf("snapshot model = %q", model)
	}
	if structuring.Model() != "hanging/model" {
		t.Fatalf("the walled client forgot which model it serves: %q", structuring.Model())
	}

	_, err := structuring.CompleteWithMessages(context.Background(), nil)
	if !errors.Is(err, ErrCallWall) {
		t.Fatalf("a snapshot taken from a walled slot was unbounded: %v", err)
	}
}

// TestAnUnwalledSlotIsUntouched keeps the work slot exactly as it was. A leaf
// is an agent loop with the executor's own deadline over it, and a second,
// dumber governor there would cut honest work in half.
func TestAnUnwalledSlotIsUntouched(t *testing.T) {
	hanging := &hangingClient{}
	client := Adopt(config.Config{}, "hanging/model", hanging)

	if got := client.CallWall(); got != 0 {
		t.Fatalf("an unconfigured slot has a wall of %s", got)
	}
	_, unwalled := client.Snapshot()
	if _, walled := unwalled.(walled); walled {
		t.Fatal("an unwalled slot handed back a decorator")
	}

	// The caller's own context is the only thing that ends this call, which is
	// the behaviour every leaf has always had.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.CompleteWithMessages(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the caller's own deadline", err)
	}
	if errors.Is(err, ErrCallWall) {
		t.Fatal("an unwalled slot invented a wall")
	}
}

// TestTheWallDoesNotSpeakForACancellingCaller keeps an interrupt an interrupt.
// A person pressing escape is not a provider failure, and reporting it as one
// would put it on the retry path built for wedged sockets.
func TestTheWallDoesNotSpeakForACancellingCaller(t *testing.T) {
	hanging := &hangingClient{}
	client := Adopt(config.Config{}, "hanging/model", hanging).WithCallWall(time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := client.CompleteWithMessages(ctx, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the caller's cancellation", err)
	}
	if errors.Is(err, ErrCallWall) {
		t.Fatal("a cancelled call was reported as a stalled model")
	}
}

// TestAnHonestCallIsNeverTouched is the constraint the whole design is under:
// none of this may cost a call that answers.
func TestAnHonestCallIsNeverTouched(t *testing.T) {
	answering := &answeringClient{}
	client := Adopt(config.Config{}, "answering/model", answering).WithCallWall(50 * time.Millisecond)

	// Three calls in a row, each taking most of a wall's worth of time between
	// them: the wall is per completion, so a long sequence is never bounded by
	// it. This is what makes four minutes safe for an agent loop.
	for attempt := 0; attempt < 3; attempt++ {
		response, err := client.CompleteWithMessages(context.Background(), nil)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if response == nil || response.Text() != "done" {
			t.Fatalf("attempt %d returned %+v", attempt, response)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if got := answering.calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}
