package remote

import (
	"context"
	"testing"
	"time"
)

type replyAgent struct {
	*fakeAgent
	calls int
}

func (a *replyAgent) SuggestReply(ctx context.Context, in string) (string, error) {
	a.calls++
	if d, ok := ctx.Deadline(); !ok || time.Until(d) > time.Second {
		return "", context.DeadlineExceeded
	}
	return "Explain the second option.", nil
}
func TestReplySuggestionRunsOnOwningEngine(t *testing.T) {
	far := &replyAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	agent := &Agent{c: loop.Client}
	text, err := agent.SuggestReply(ctx, "The plan has two options.")
	if err != nil || text != "Explain the second option." || far.calls != 1 {
		t.Fatalf("text=%q err=%v calls=%d", text, err, far.calls)
	}
}

type heldReplySuggestion struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
}

func (a *heldReplySuggestion) SuggestReply(ctx context.Context, _ string) (string, error) {
	close(a.entered)
	select {
	case <-a.release:
		return "Explain the second option.", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// A suggestion is background model work, not a conversation turn. Holding its
// response must never hold a person's message behind the suggestion's deadline.
func TestASendIsNotHeldBehindAReplySuggestion(t *testing.T) {
	if classify(methodReplySuggestion) != classWork || classify(methodReplySuggestion).road() != onItsOwn {
		t.Fatal("reply suggestions must use the independent work lane")
	}
	far := &heldReplySuggestion{
		fakeAgent: &fakeAgent{model: "m"},
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	loop := askLoop(t, far)
	t.Cleanup(func() {
		select {
		case <-far.release:
		default:
			close(far.release)
		}
	})

	type suggestionResult struct {
		text string
		err  error
	}
	suggested := make(chan suggestionResult, 1)
	go func() {
		text, err := loop.Client.Agent().SuggestReply(t.Context(), "The plan has two options.")
		suggested <- suggestionResult{text, err}
	}()
	select {
	case <-far.entered:
	case <-time.After(time.Second):
		t.Fatal("the suggestion never reached the engine")
	}

	sent := make(chan error, 1)
	go func() {
		_, err := loop.Client.Agent().Submit(t.Context(), "Use the second option.")
		sent <- err
	}()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("the send was refused: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the send is waiting behind a reply suggestion")
	}
	select {
	case got := <-suggested:
		t.Fatalf("the held suggestion answered early: text=%q err=%v", got.text, got.err)
	default:
	}

	far.mu.Lock()
	if len(far.sent) != 1 || far.sent[0] != "Use the second option." {
		far.mu.Unlock()
		t.Fatal("the engine did not receive the person's message")
	}
	far.mu.Unlock()
	close(far.release)
	select {
	case got := <-suggested:
		if got.err != nil || got.text != "Explain the second option." {
			t.Fatalf("the released suggestion failed: text=%q err=%v", got.text, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("the suggestion did not answer after release")
	}
}
