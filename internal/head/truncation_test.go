package head

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Session bd3c78ed asked for a diagram, got 1,611 characters of one, and the
// journal recorded it as an answer. The turn had ended at exactly 600
// completion tokens — the cap this route still sets — and the provider said so
// in finish_reason, which every consumer then discarded by calling Text().
//
// These tests drive the real Serve path through the real streaming adapter,
// because the whole defect lived between the wire and the posted message. What
// they assert is a mark, not a change: the body posted must be byte for byte
// what it was before parts existed, and the fact that it was cut must ride
// beside it where a renderer can find it.
//
// The one thing that moved with the tool loop is what the wire carries. There is
// no router envelope any more: a turn that calls no tool speaks in plain prose
// and that prose IS the reply, so these fixtures stream words rather than JSON.
// The finish_reason seam they exercise is untouched by that.

// finishReasonServer answers every answering call with the same reply and the
// finish reason under test.
func finishReasonServer(reply, finishReason string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		frame := `data: {"choices":[{"index":0,"delta":{"content":"` + reply + `"}`
		if finishReason != "" {
			frame += `,"finish_reason":"` + finishReason + `"`
		}
		frame += `}]}`
		_, _ = writer.Write([]byte(frame + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
}

// servedReply runs one whole turn through Serve and returns the head's message.
func servedReply(t *testing.T, session, ask string, handler http.Handler) store.Message {
	t.Helper()
	graph := openHeadStore(t)
	client := streamedProviderClient(t, handler)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = provider.WithStreamObserver(ctx, func(provider.StreamEvent) {})
	done := make(chan error, 1)
	go func() { done <- New(client, graph).Serve(ctx) }()

	user, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleUser, Body: ask})
	if err != nil {
		t.Fatalf("post user message: %v", err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("serve returned %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
	return reply
}

func TestATurnCutByTheOutputCapJournalsThatItWasCut(t *testing.T) {
	const half = "The architecture has three layers: the journal, the projections and the"
	reply := servedReply(t, "truncated",
		"draw me the architecture",
		finishReasonServer(half, "length"))

	// The words are untouched. A mark that also edits the message is a second
	// behaviour change hiding inside a record.
	if reply.Body != half {
		t.Fatalf("the posted body changed:\n got %q\nwant %q", reply.Body, half)
	}
	if len(reply.Parts) != 1 {
		t.Fatalf("a capped turn carried %d parts, want one end mark: %+v", len(reply.Parts), reply.Parts)
	}
	part := reply.Parts[0]
	if part.Kind != store.PartEnded || part.Ended == nil {
		t.Fatalf("the part is not an end mark: %+v", part)
	}
	if part.Ended.How != store.EndLength || part.Ended.FinishReason != "length" {
		t.Fatalf("end mark = %+v, want length with the provider's own word", part.Ended)
	}
}

func TestATurnThatFinishedCarriesNoMarkAtAll(t *testing.T) {
	reply := servedReply(t, "complete", "hello",
		finishReasonServer("hi there", "stop"))
	if reply.Body != "hi there" {
		t.Fatalf("reply body = %q", reply.Body)
	}
	if reply.Parts != nil {
		t.Fatalf("an ordinary turn grew parts: %+v", reply.Parts)
	}
}

// A stream that stops without ever saying why is the other half of the law: no
// terminal frame is not the same as a clean ending, and the journal has to be
// able to tell them apart.
func TestAStreamThatStopsWithoutSayingWhyIsMarkedAsDropped(t *testing.T) {
	reply := servedReply(t, "dropped", "hello",
		finishReasonServer("partway thro", ""))
	if reply.Body != "partway thro" {
		t.Fatalf("reply body = %q", reply.Body)
	}
	if len(reply.Parts) != 1 || reply.Parts[0].Ended == nil ||
		reply.Parts[0].Ended.How != store.EndStreamDrop {
		t.Fatalf("a dropped stream posted %+v", reply.Parts)
	}
	if reply.Parts[0].Ended.FinishReason != "" {
		t.Fatalf("a drop invented a finish reason: %q", reply.Parts[0].Ended.FinishReason)
	}
}

// The turn the person stopped. postInterrupted is the one reply route that
// never reaches the routing call, and the law names interruption explicitly.
func TestAnInterruptedTurnCarriesTheInterruptedMark(t *testing.T) {
	graph := openHeadStore(t)
	if err := New(&fakeClient{}, graph).postInterrupted("stopped", "half a sen"); err != nil {
		t.Fatalf("post interrupted: %v", err)
	}
	replies := agentReplies(t, graph, "stopped")
	if len(replies) != 1 {
		t.Fatalf("posted %d replies", len(replies))
	}
	if !strings.HasPrefix(replies[0].Body, "half a sen") {
		t.Fatalf("the partial words were not kept: %q", replies[0].Body)
	}
	if len(replies[0].Parts) != 1 || replies[0].Parts[0].Ended == nil ||
		replies[0].Parts[0].Ended.How != store.EndInterrupted {
		t.Fatalf("interrupted reply carried %+v", replies[0].Parts)
	}
}
