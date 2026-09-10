package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type boundaryForkCompleter struct {
	*forkCompleter
	boundaryMu   sync.Mutex
	callerRounds int
	wakeRequest  chan []ai.Message
	releaseWake  chan struct{}
}

func (c *boundaryForkCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range opts {
		_ = option(&request)
	}
	if handIndexOf(messages) == 0 && len(request.Tools) > 0 {
		c.boundaryMu.Lock()
		c.callerRounds++
		round := c.callerRounds
		c.boundaryMu.Unlock()
		if round == 3 {
			c.wakeRequest <- append([]ai.Message(nil), messages...)
			select {
			case <-c.releaseWake:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return c.forkCompleter.CompleteWithMessages(ctx, messages, opts...)
}

// The hand finishes after the caller's final request was assembled. The old
// Submit stream can end before the wake reaches the scripted provider. This
// schedule asserts the report is retained and reaches that next request.
func TestForkReportAcrossFinalRequestBoundary(t *testing.T) {
	base := newForkCompleter(0)
	c := &boundaryForkCompleter{forkCompleter: base, wakeRequest: make(chan []ai.Message, 1), releaseWake: make(chan struct{})}
	releaseHands := make(chan struct{})
	var once sync.Once
	base.caller = []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("fork", "fork", forkCall(forkPartJSON("the adapters", "adapters"), forkReadingPartJSON("read the docs"))), nil
	}}
	base.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		<-releaseHands
		if index == 1 && turn == 1 {
			return toolResponse("write", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
		}
		return textResponse("hand done"), nil
	}
	base.callerTail = func(context.Context, []ai.Message) (*ai.Response, error) {
		once.Do(func() { close(releaseHands) })
		deadline := time.Now().Add(2 * time.Second)
		for base.driven().jobs.handsOutstanding() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return textResponse("done"), nil
	}
	agent, _ := newTestAgent(t, c, nil)
	base.drive(agent)
	wakes := agent.Wakes()
	collect(t, mustSubmit(t, agent, "split it"))
	handsAreHome(t, agent)
	// The original test inspects this request too early: neither report had
	// arrived when the request was assembled, although both hands are now home.
	if reports := theHandReports(t, base); len(reports) != 0 {
		t.Fatalf("barrier failed: old request already had reports: %v", reports)
	}
	var next []ai.Message
	select {
	case next = <-c.wakeRequest:
	case <-time.After(2 * time.Second):
		t.Fatal("unread report did not wake the caller")
	}
	found := false
	for _, m := range next {
		if strings.Contains(messageContentText(m), "wrote adapters/mine.md") {
			found = true
		}
	}
	if !found {
		t.Fatal("writing report was lost before wake request")
	}
	close(c.releaseWake)
	select {
	case events := <-wakes:
		collect(t, events)
	case <-time.After(2 * time.Second):
		t.Fatal("wake stream missing")
	}
	t.Log("Old request had no reports after hands returned; next wake request retained the writing report.")
}
