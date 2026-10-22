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
