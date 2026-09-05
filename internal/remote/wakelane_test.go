package remote

// wakelane_test.go is the engine's own turns on the wire: the turn a landed
// task starts must reach every attached surface like a turn somebody submitted,
// because the journal alone is not an answer a person can see.
//
// It drives the same scripted agents and raw frame links server_test.go does,
// for the reason that file gives: the question is whether a wake is minted,
// told, numbered and closed like any other stream, and none of it is a
// question about a model.

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// wakingAgent is the scripted agent plus the one lane a *session.Agent has and
// a scripted one never did: its own turns, handed over before their first
// event. The hand-over is what internal/session's [Agent.wakeLocked] does — a
// stream per woken turn, on a buffered lane, closed by the stop.
type wakingAgent struct {
	*fakeAgent
	lane chan (<-chan session.Event)
	stop func()
}

func newWakingAgent() *wakingAgent {
	agent := &wakingAgent{
		fakeAgent: &fakeAgent{},
		lane:      make(chan (<-chan session.Event), 8),
	}
	var once sync.Once
	agent.stop = func() { once.Do(func() { close(agent.lane) }) }
	return agent
}

func (w *wakingAgent) WatchWakes() (<-chan (<-chan session.Event), func()) {
	return w.lane, w.stop
}

// wake hands over one turn of the conversation's own, the way a landed task
// does on the real agent: the stream arrives on the lane BEFORE the turn's
// first event, and events is what the engine pumps from.
func (w *wakingAgent) wake(t *testing.T) chan session.Event {
	t.Helper()
	events := make(chan session.Event, 8)
	select {
	case w.lane <- events:
	case <-time.After(5 * time.Second):
		t.Fatal("the wake never reached the conversation's own lane")
	}
	return events
}

// wakingSession is one persistent conversation on the waking agent, held the
// way a host holds it: the session exists before any surface arrives and
// outlives every one of them.
func wakingSession(agent *wakingAgent) *Session {
	return NewSession(&Engine{Agent: agent, Workspace: "/srv/app"}, true)
}

// THE SHAPE IS THE SEAM, stated as a compile-time fact. The whole fix stands
// on the hosted engine being able to subscribe to a *session.Agent's own turns
// the way a local surface does; an agent that stopped offering the lane would
// strand every hosted conversation's wakes back in the journal, and this fails
// the build the day it happens rather than the day somebody runs a terminal.
var _ wakeLaneAgent = (*session.Agent)(nil)

// The turn the conversation started on its own crosses to every attached
// window exactly as a submitted one does: told before its first event, drawn
// event by event, closed with the seq of its last. This is the failure the
// hosted run photographed — the answer in the journal, the one window sitting
// in front of it reading idle.
func TestATurnTheConversationStartsOnItsOwnCrossesToEveryWindow(t *testing.T) {
	agent := newWakingAgent()
	sess := wakingSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})

	events := agent.wake(t)
	events <- session.Event{Kind: session.EventTextDelta, Text: "task 1 done"}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)

	for _, window := range []*link{desk, away} {
		told := decode[Turn](t, window.await(func(f Frame) bool { return f.Kind == "turn" }).Payload)
		if told.Stream == 0 {
			t.Fatalf("the window was told about stream 0: %+v", told)
		}
		// A WAKE OPENS ON NOTHING. The note that woke the turn is drained and
		// journaled inside it, so there is no sentence to carry and the
		// surface draws the turn's work and its answer — the same picture a
		// local surface watching its own session gets.
		if told.Said != "" {
			t.Fatalf("the wake was told it opened on %q — nothing typed it", told.Said)
		}
		for want := uint64(1); want <= 2; want++ {
			frame := window.await(func(f Frame) bool { return f.Kind == "event" && f.ID == told.Stream })
			if frame.Seq != want {
				t.Fatalf("event %d carried seq %d", want, frame.Seq)
			}
			if want == 1 {
				if text := decode[EventWire](t, frame.Payload).Unwire().Text; text != "task 1 done" {
					t.Fatalf("the wake's first event was %q", text)
				}
			}
		}
		closed := window.await(func(f Frame) bool { return f.Kind == "closed" && f.ID == told.Stream })
		if closed.Seq != 2 {
			t.Fatalf("the wake ended with seq %d, want the last event's 2", closed.Seq)
		}
	}

	// The stream is gone with the turn: a cursor naming it is answered with
	// nothing, because the transcript is the authority on a finished turn.
	waitForEngine(t, func() bool { return sess.liveSeq(1) == 0 })
}

// A window that arrives while the conversation's own turn is running picks it
// up part-way: the welcome names the live stream and the replay hands over the
// gap, and only the gap. A wake that began while nobody was attached is the
// same case one minute earlier, and before the lane existed it was the case
// that could not happen at all.
func TestAWindowArrivingMidWakeReplaysTheGapAndOnlyTheGap(t *testing.T) {
	agent := newWakingAgent()
	sess := wakingSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version})

	events := agent.wake(t)
	events <- session.Event{Kind: session.EventTextDelta, Text: "the first part"}
	waitForEngine(t, func() bool { return sess.liveSeq(1) == 1 })

	late := dialSession(t, sess)
	welcome := decode[Welcome](t, late.hello(Hello{Version: Version}).Payload)
	if welcome.Live != 1 {
		t.Fatalf("a window arriving mid-wake was told stream %d is live, want 1", welcome.Live)
	}

	// The gap the arrival missed, then the rest — each event once, and never
	// the gap a second time.
	events <- session.Event{Kind: session.EventTextDelta, Text: " and the rest"}
	close(events)

	seen := map[uint64]string{}
	for {
		frame := late.recvAny()
		if frame.Kind == "event" && frame.ID == welcome.Live {
			seen[frame.Seq] = decode[EventWire](t, frame.Payload).Unwire().Text
			continue
		}
		if frame.Kind == "closed" && frame.ID == welcome.Live {
			break
		}
	}
	if len(seen) != 2 || seen[1] != "the first part" || seen[2] != " and the rest" {
		t.Fatalf("the late window drew %v, want each half exactly once", seen)
	}
}

// The client hands a wake to the surface through the same door a watched turn
// takes ([Client.Follow]), which is the whole fix on the far side: a hosted
// surface has no Wakes of its own to subscribe to, so the turn some other
// party started — a window, or the conversation itself — must arrive as a
// Following or not at all.
func TestTheClientHandsAWakeToTheSurfaceAsAFollowing(t *testing.T) {
	agent := newWakingAgent()
	loop, err := Loopback(Hello{Version: Version, Workspace: "/srv/app"}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: agent, Workspace: "/srv/app"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	following := loop.Client.Follow()
	events := agent.wake(t)
	events <- session.Event{Kind: session.EventTextDelta, Text: "the answer about the result"}
	close(events)

	select {
	case turn := <-following:
		if turn.Said != "" {
			t.Fatalf("the wake was handed over as opening on %q", turn.Said)
		}
		first := <-turn.Events
		if first.Kind != session.EventTextDelta || first.Text != "the answer about the result" {
			t.Fatalf("the first handed-over event was %+v", first)
		}
		if _, open := <-turn.Events; open {
			t.Fatal("the wake's stream did not end with its turn")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wake never crossed to the surface")
	}
}

// The lane ends with the conversation, and no wake after the end mints a
// stream nobody will ever close — the same discipline the task lanes are
// closed under, because a lane left open is a goroutine and a subscription
// the sweep has already decided is over.
func TestTheWakeLaneEndsWithTheConversation(t *testing.T) {
	agent := newWakingAgent()
	sess := wakingSession(agent)

	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case _, open := <-agent.lane:
		if open {
			t.Fatal("the lane outlived the conversation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the conversation left its wake lane open")
	}
}

// A swap retakes the lane on the conversation that replaced the old one, so a
// wake on the new agent crosses and a straggler from the old one cannot. The
// retake is [Session.swap]'s law for every rail in the room, and this is the
// one rail that was missing from it.
func TestASwapRetakesTheWakeLaneOnTheNewConversation(t *testing.T) {
	old := newWakingAgent()
	next := newWakingAgent()
	sess := NewSession(&Engine{
		Agent:     old,
		Workspace: "/srv/app",
		// The swap goes through the door a surface uses, because that is the
		// only road a conversation is replaced on.
		Fresh: func() (WrappedAgent, string, error) {
			return next, "/srv/next.jsonl", nil
		},
	}, true)

	window := dialSession(t, sess)
	window.hello(Hello{Version: Version})
	window.ok(1, MethodSessionNew, nil)

	sess.mu.Lock()
	agent, generation := sess.agent, sess.generation
	sess.mu.Unlock()
	if agent != WrappedAgent(next) || generation != 1 {
		t.Fatalf("the swap left agent %v generation %d", agent, generation)
	}

	events := next.wake(t)
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)

	told := decode[Turn](t, window.await(func(f Frame) bool { return f.Kind == "turn" }).Payload)
	if told.Stream == 0 {
		t.Fatal("a wake on the new conversation never crossed to the window")
	}
	window.await(func(f Frame) bool { return f.Kind == "closed" && f.ID == told.Stream })

	// And the old conversation's lane is ended, so nothing of it can follow.
	select {
	case _, open := <-old.lane:
		if open {
			t.Fatal("the previous conversation's wake lane is still open")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the swap left the previous conversation's wake lane open")
	}
}

// A wake queued on the OLD conversation's lane is drained and never minted
// after a swap, because a stream named under the new generation would draw the
// previous conversation's answer into the one that replaced it — the same lie
// [Session.pump] refuses by generation for a turn somebody submitted.
//
// THE STALE QUEUE IS STAGED BY HAND, on a lane handed to the loop with the
// generation it was opened under. Real life stages the same thing through the
// lane's buffer — a stream sitting on it while the swap closes the
// conversation around it — but that window is a scheduling race, and a test
// that won it by timing would prove nothing on the day it mattered. The loop,
// the generation and the lane are the seam; this drives them as they really
// are wired.
func TestAStaleQueuedWakeIsDrainedAndNeverMinted(t *testing.T) {
	old := newWakingAgent()
	next := newWakingAgent()
	sess := NewSession(&Engine{
		Agent:     old,
		Workspace: "/srv/app",
		Fresh:     func() (WrappedAgent, string, error) { return next, "/srv/next.jsonl", nil },
	}, true)
	window := dialSession(t, sess)
	window.hello(Hello{Version: Version})

	// The stream is queued before the conversation is swapped out from under
	// it, on the old generation's lane.
	stale := make(chan (<-chan session.Event), 1)
	events := make(chan session.Event, 8)
	stale <- events
	close(stale)
	window.ok(1, MethodSessionNew, nil)

	sess.mu.Lock()
	generation := sess.generation
	sess.mu.Unlock()
	if generation != 1 {
		t.Fatalf("the swap left generation %d, want 1", generation)
	}
	sess.pumps.Add(1)
	go sess.pumpOwnTurns(stale, 0)

	// The stale wake runs to its end, and the loop answers it by draining —
	// the channel has a session writing into it — and by minting nothing. The
	// drain IS the proof the decision was made, because the loop spawns it
	// only after refusing the mint.
	events <- session.Event{Kind: session.EventTextDelta, Text: "from the old one"}
	close(events)
	waitForEngine(t, func() bool { return len(events) == 0 })

	sess.mu.Lock()
	minted := len(sess.rings)
	sess.mu.Unlock()
	if minted != 0 {
		t.Fatalf("a stale wake minted %d stream(s) in the conversation that replaced its own", minted)
	}
	for _, frame := range drain(window) {
		if frame.Kind == "turn" || frame.Kind == "event" || frame.Kind == "closed" {
			t.Fatalf("a stale wake reached the window as %q: %+v", frame.Kind, frame)
		}
	}

	// AND THE NEW CONVERSATION'S OWN WAKE STILL CROSSES, which is the two
	// halves of the same guard: refusing the old lane's queue must not have
	// silenced the subscription the swap opened on the new agent.
	fresh := next.wake(t)
	fresh <- session.Event{Kind: session.EventTextDelta, Text: "from the new one"}
	close(fresh)
	told := decode[Turn](t, window.await(func(f Frame) bool { return f.Kind == "turn" }).Payload)
	if told.Stream == 0 {
		t.Fatal("the new conversation's wake never crossed to the window")
	}
	frame := window.await(func(f Frame) bool { return f.Kind == "event" && f.ID == told.Stream })
	if text := decode[EventWire](t, frame.Payload).Unwire().Text; text != "from the new one" {
		t.Fatalf("the new conversation's wake drew %q", text)
	}
	window.await(func(f Frame) bool { return f.Kind == "closed" && f.ID == told.Stream })
}

// A wake that runs while NOBODY is attached is the whole point of a hosted
// conversation — the terminal closed, the work went on — and the surface that
// arrives next is handed the turn by its welcome, mid-flight, exactly as it
// would be handed a turn some other window started.
func TestAWakeWithNobodyAttachedIsHandedToTheNextSurface(t *testing.T) {
	agent := newWakingAgent()
	sess := wakingSession(agent)

	// NOBODY IS ATTACHED. The wake runs anyway — the journal is the engine's
	// to write — and the stream is minted so the arrival below has a turn to
	// be told about rather than a silent conversation.
	events := agent.wake(t)
	events <- session.Event{Kind: session.EventTextDelta, Text: "the landing"}
	waitForEngine(t, func() bool { return sess.liveSeq(1) == 1 })

	late := dialSession(t, sess)
	welcome := decode[Welcome](t, late.hello(Hello{Version: Version}).Payload)
	if welcome.Live != 1 {
		t.Fatalf("a surface arriving on a woken conversation was told stream %d is live, want 1", welcome.Live)
	}
	events <- session.Event{Kind: session.EventTextDelta, Text: " and its answer"}
	close(events)

	seen := map[uint64]string{}
	for {
		frame := late.recvAny()
		if frame.Kind == "event" && frame.ID == welcome.Live {
			seen[frame.Seq] = decode[EventWire](t, frame.Payload).Unwire().Text
			continue
		}
		if frame.Kind == "closed" && frame.ID == welcome.Live {
			break
		}
	}
	if len(seen) != 2 || seen[1] != "the landing" || seen[2] != " and its answer" {
		t.Fatalf("the arrival drew %v, want each half exactly once", seen)
	}
}
