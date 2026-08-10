package head

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// blockingClient is a provider call that never returns on its own. It is the
// only shape in which the interrupt is a real thing: a turn stopped after the
// request went out and before any answer came back.
type blockingClient struct {
	entered chan struct{}
	mutex   sync.Mutex
	calls   int
}

func (client *blockingClient) CompleteWithMessages(ctx context.Context, _ []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	client.calls++
	client.mutex.Unlock()
	select {
	case client.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (client *blockingClient) callCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.calls
}

func interruptDuringTurn(t *testing.T, partial string) (store.Message, *blockingClient, *store.Store, int64) {
	t.Helper()
	graphStore := openHeadStore(t)
	client := &blockingClient{entered: make(chan struct{}, 1)}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-interrupt", Role: store.RoleUser, Body: "how is the report coming along?",
	})
	if err != nil {
		t.Fatal(err)
	}
	conversationalHead := New(client, graphStore)
	type outcome struct {
		cursor int64
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		cursor, pollErr := conversationalHead.poll(context.Background(), 0)
		done <- outcome{cursor: cursor, err: pollErr}
	}()
	select {
	case <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the head never reached the provider")
	}
	if !conversationalHead.Interrupt(partial) {
		t.Fatal("interrupt found no turn in flight")
	}
	var result outcome
	select {
	case result = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the interrupted turn never ended")
	}
	if result.err != nil {
		t.Fatalf("poll after an interrupt returned %v, want the head still serving", result.err)
	}
	if result.cursor < user.Seq {
		t.Fatalf("cursor = %d, want past the interrupted message at %d", result.cursor, user.Seq)
	}
	if conversationalHead.Interrupt("") {
		t.Fatal("interrupt reported a turn in flight after the turn ended")
	}
	return user, client, graphStore, result.cursor
}

func TestInterruptedTurnKeepsThePartialAndStopsAnswering(t *testing.T) {
	user, client, graphStore, cursor := interruptDuringTurn(t, "the report is ")
	reply := waitForAgentReply(t, graphStore, user.SessionID, user.Seq)
	if reply.Body != "the report is"+interruptedTail {
		t.Fatalf("interrupted reply = %q, want the partial marked interrupted", reply.Body)
	}
	if cursor < reply.Seq {
		t.Fatalf("cursor = %d, want past the reply at %d", cursor, reply.Seq)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want the stopped turn tried exactly once", calls)
	}
}

func TestInterruptedTurnWithNothingSaidStillSpeaks(t *testing.T) {
	user, _, graphStore, _ := interruptDuringTurn(t, "   ")
	reply := waitForAgentReply(t, graphStore, user.SessionID, user.Seq)
	if reply.Body != interruptedReply {
		t.Fatalf("interrupted reply = %q, want the standalone interrupted line", reply.Body)
	}
	if strings.TrimSpace(reply.Body) == "" {
		t.Fatal("an interrupted turn went quiet")
	}
}

func TestInterruptWithoutATurnReportsNothingStopped(t *testing.T) {
	if New(&fakeClient{}, openHeadStore(t)).Interrupt("anything") {
		t.Fatal("interrupt claimed to stop a turn that was never running")
	}
}
