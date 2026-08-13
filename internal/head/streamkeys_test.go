package head

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHICH OF THE HEAD'S TURNS STREAM, AND UNDER WHAT KEY.
//
// The head talks to a model from more places than the conversation, and each one
// answers a different question about the same law: model-authored text that will
// render VISIBLY on the surface a person has open renders as its deltas arrive;
// text that lands folded, or that is not prose at all, arrives whole.
//
// The key is what carries that decision across the process boundary. A turn that
// stamps the room's own session shares the room's live region; a turn that stamps
// a companion suffix gets its own admission test on the surface
// (internal/tui2/chat/companion.go); a turn that takes the observer OFF says it
// must never be drawn as speech at all.
//
// These are pinned here, at the emitter, because the surface cannot tell the
// difference between a key it was never sent and a key it decided to drop — and
// the failure looks identical either way: the answer appears whole.

// streamProbe is a client that reports what the context it was called on would
// do with a delta. It is the only honest way to ask the question: the observer
// and the session are both context values, and Emit is the door every real
// emitter goes through.
type streamProbe struct {
	mutex     sync.Mutex
	responses []string
	model     string

	// observed is the events that actually escaped, in order. A call made on a
	// context with the observer taken off produces none, which is the whole of
	// what "this turn does not stream" means.
	observed []provider.StreamEvent
}

func (probe *streamProbe) CompleteWithMessages(ctx context.Context, _ []ai.Message,
	_ ...ai.Option) (*ai.Response, error) {
	// Exactly what internal/provider's adapter emits around a real streamed
	// completion, minus the bytes: the boundary and the key are the facts under
	// test, and the text is not.
	provider.Emit(ctx, provider.StreamStarted, "")
	provider.Emit(ctx, provider.StreamDelta, "…")
	provider.Emit(ctx, provider.StreamFinished, "")

	probe.mutex.Lock()
	defer probe.mutex.Unlock()
	if len(probe.responses) == 0 {
		return nil, errors.New("no probe response left")
	}
	text := probe.responses[0]
	probe.responses = probe.responses[1:]
	response := textResponse(text)
	response.Model = probe.model
	return response, nil
}

func (probe *streamProbe) Model() string { return probe.model }

// keys is the distinct stream sessions this probe saw, in first-seen order.
func (probe *streamProbe) keys() []string {
	probe.mutex.Lock()
	defer probe.mutex.Unlock()
	var out []string
	for _, event := range probe.observed {
		if len(out) == 0 || out[len(out)-1] != event.Session {
			out = append(out, event.Session)
		}
	}
	return out
}

// watched installs the surface's observer on a context, exactly as
// cmd/aforge/chat.go's serveHead does for the life of a window.
func (probe *streamProbe) watched(ctx context.Context) context.Context {
	return provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		probe.mutex.Lock()
		defer probe.mutex.Unlock()
		probe.observed = append(probe.observed, event)
	})
}

// THE DELIVERY ANSWER streams, under the room's key plus `#delivered`. Its text
// is posted as an ordinary agent row in the room the person asked in, so it is
// the plainest case of the law there is.
func TestTheDeliveryAnswerStreamsUnderItsOwnCompanionKey(t *testing.T) {
	graph := openHeadStore(t)
	ask := postUser(t, graph, "room", "which region grew fastest?")
	delivered := settledJob(t, graph, "task-9", "Regional growth read",
		"which region grew fastest?", "North grew 14%.")

	probe := &streamProbe{model: "test/model", responses: []string{"North, at 14%."}}
	head := New(probe, graph)
	cursors := newSessionCursors(ask.Seq)
	cursors.mark("room", ask.Seq)
	if err := head.poll(probe.watched(context.Background()), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	waitForAgentReply(t, graph, "room", delivered.Seq)

	want := AbsorbStreamSession("room")
	if got := probe.keys(); len(got) != 1 || got[0] != want {
		t.Fatalf("the delivery answer streamed under %v, want exactly [%q] — "+
			"a key the surface cannot attribute is a key it drops, and the answer appears whole",
			got, want)
	}
}

// THE EPHEMERAL ASIDE streams under `#aside`, and that key is the surface's
// instruction NOT to draw it: what an aside journals is a collapsed stub, and a
// fold does not stream. The key must still be stamped — an aside that arrived
// unkeyed would read as the room's own turn and type itself into the transcript,
// which is the bug the key was invented for (8.2.9).
func TestAnAsideIsKeyedApartSoItIsNeverDrawnAsTheRoomsOwnTurn(t *testing.T) {
	graph := openHeadStore(t)
	postUser(t, graph, "room", "what is it doing?")

	probe := &streamProbe{model: "test/model", responses: []string{"Reading the plan."}}
	head := New(probe, graph)
	if _, err := head.Ask(probe.watched(context.Background()), "room", "why that one?"); err != nil {
		t.Fatalf("ask: %v", err)
	}

	want := AsideStreamSession("room")
	if got := probe.keys(); len(got) != 1 || got[0] != want {
		t.Fatalf("the aside streamed under %v, want exactly [%q]", got, want)
	}
	if want == "room" {
		t.Fatal("the aside's key is the room's own: its deltas would be drawn as the reply")
	}
}

// THE ROOM NAMING CLERK does not stream at all, and this is the exclusion stated
// as a measurement rather than as a comment. A title is a LABEL: four words that
// name a room in a list. Typing them into the transcript would be the surface
// showing a label as though somebody had said it, and streaming three words is
// noise even where it is harmless.
func TestTheRoomNamingClerkNeverStreams(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "how do I read a plan file?",
		"open it with the plan tool — it is JSON with a nodes array")

	probe := &streamProbe{model: "test/model", responses: []string{"Plan file reading"}}
	head := New(probe, graph).WithRoomNaming(true)
	head.nameRoomLater(probe.watched(context.Background()), "room")
	waitForRoomName(t, graph, "room")

	if got := probe.observed; len(got) != 0 {
		t.Fatalf("naming a room put %d stream boundaries on the surface's feed (%v) — "+
			"a label is not a reply and must not be typed into a transcript",
			len(got), probe.keys())
	}
}

// The same clerk, reached through the launch backfill rather than through a
// turn. Both doors take the observer off, and a door that forgot would be the
// louder of the two: eight rooms named at once, into whichever room is open.
func TestTheRoomNameBackfillNeverStreams(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "room", "how do I read a plan file?",
		"open it with the plan tool")

	probe := &streamProbe{model: "test/model", responses: []string{"Plan file reading"}}
	head := New(probe, graph).WithRoomNaming(true)
	head.BackfillRoomNames(probe.watched(context.Background()))
	waitForRoomName(t, graph, "room")

	if got := probe.observed; len(got) != 0 {
		t.Fatalf("the naming backfill put %d stream boundaries on the surface's feed (%v)",
			len(got), probe.keys())
	}
}

// Every companion key is the room's id with a suffix on it, and nothing else.
// The surface splits them on that shape (internal/tui2/chat's streamRoom), so a
// key built any other way is a key it attributes to the wrong room or to no
// room at all.
func TestEveryCompanionKeyIsTheRoomPlusASuffix(t *testing.T) {
	const room = "chat-20260813-101500.000000"
	for name, key := range map[string]string{
		"the delivery answer": AbsorbStreamSession(room),
		"the receipt wake":    ReceiptStreamSession(room),
		"the ephemeral aside": AsideStreamSession(room),
	} {
		if !strings.HasPrefix(key, room) {
			t.Errorf("%s keys as %q, which does not begin with the room %q",
				name, key, room)
		}
		suffix := strings.TrimPrefix(key, room)
		if !strings.HasPrefix(suffix, "#") || strings.Contains(suffix[1:], "#") {
			t.Errorf("%s keys as %q: the suffix %q is not a single #-marked word",
				name, key, suffix)
		}
	}
}

// waitForRoomName blocks until the clerk has titled a room, because both naming
// doors hand the caller its turn back and do the work on a goroutine.
func waitForRoomName(t *testing.T, graph *store.Store, session string) {
	t.Helper()
	for range 400 {
		if room, found, err := graph.Session(session); err == nil && found &&
			strings.TrimSpace(room.Title) != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the room was never named")
}
