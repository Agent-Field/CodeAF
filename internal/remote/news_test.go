package remote

// news_test.go is the two halves of the live status row across a connection:
// the engine steering each piece of news to the conversation it names, and the
// surface putting what arrives back on the desk internal/tui3 reads.
//
// EVERY TEST HERE PUTS THE READERS BACK. internal/session keeps one desk for
// the whole build, so a test that took it and walked away would take the next
// test's news with it — and the engine half installs its own reader the moment
// a conversation is filed, so the order these run in must not matter.

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// newsAgent is the scripted agent with a name for its news, which is what makes
// a conversation reachable from the newsroom at all (news.go's [newsKeyed]).
type newsAgent struct {
	*fakeAgent
	key string
}

func (a *newsAgent) NewsKey() string { return a.key }

// answeredOffer records the answer a far `y` carried home.
type offerAgent struct {
	*fakeAgent
	mu       sync.Mutex
	answers  []bool
	standing bool
}

func (a *offerAgent) AnswerLaneOffer(yes bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answers = append(a.answers, yes)
	return a.standing
}

// newsSession is one persistent conversation whose news is filed under key.
func newsSession(t *testing.T, agent WrappedAgent) *Session {
	t.Helper()
	engine := engineOn(&fakeAgent{})
	engine.Agent = agent
	sess := NewSession(engine, true)
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// heardPhases collects everything this process's phase desk is told, and puts
// the previous reader back when the test ends.
func heardPhases(t *testing.T) func() []session.PhaseNews {
	t.Helper()
	var mu sync.Mutex
	var heard []session.PhaseNews
	previous := session.OnPhaseNews(func(news session.PhaseNews) {
		mu.Lock()
		heard = append(heard, news)
		mu.Unlock()
	})
	t.Cleanup(func() { session.OnPhaseNews(previous) })
	return func() []session.PhaseNews {
		mu.Lock()
		defer mu.Unlock()
		return append([]session.PhaseNews(nil), heard...)
	}
}

func heardLanes(t *testing.T) func() []session.LaneNews {
	t.Helper()
	var mu sync.Mutex
	var heard []session.LaneNews
	previous := session.OnLaneNews(func(news session.LaneNews) {
		mu.Lock()
		heard = append(heard, news)
		mu.Unlock()
	})
	t.Cleanup(func() { session.OnLaneNews(previous) })
	return func() []session.LaneNews {
		mu.Lock()
		defer mu.Unlock()
		return append([]session.LaneNews(nil), heard...)
	}
}

// ── the surface half ────────────────────────────────────────────────────────

// A PHASE MEASURED ON THE ENGINE'S MACHINE REACHES THE SURFACE'S OWN DESK.
//
// This is the whole defect in one test: everything the status row draws about a
// turn in flight is posted where the request is made, and a bare `aforge` makes
// its requests in another process.
func TestAPhaseFrameReachesTheSurfacesOwnPhaseReader(t *testing.T) {
	heard := heardPhases(t)
	client, engine := newEngine(t)
	defer func() { _ = client.Close() }()

	engine.send(Frame{Kind: "phase", Payload: mustClientJSON(PhaseWire{
		Phase:   "writing",
		Model:   "openai/gpt-5",
		Role:    string(lane.RoleTalk),
		Lane:    "friendli",
		Rate:    38.5,
		Detail:  "4s",
		SinceMS: 4000,
	})})

	waitForNews(t, "the phase to reach the desk", func() bool { return len(heard()) > 0 })
	news := heard()[0]
	if news.Phase != provider.PhaseWriting || news.Model != "openai/gpt-5" {
		t.Fatalf("the desk was told %q on %q, want writing on openai/gpt-5", news.Phase, news.Model)
	}
	if news.Lane != "friendli" || news.Rate != 38.5 {
		t.Fatalf("the desk was told %q at %v tok/s, want friendli at 38.5", news.Lane, news.Rate)
	}
	if news.Role != lane.RoleTalk {
		t.Fatalf("the phase arrived for %q, want the conversation's own role", news.Role)
	}
	// THE MOMENTS ARE THIS MACHINE'S. A surface ages a phase out against its own
	// clock, so a wall clock carried from another machine would drop every phase
	// on arrival or draw one that started before it did (wire.go's [PhaseWire]).
	if news.Relayed != true {
		t.Fatal("a phase off the wire was not stamped as relayed, which is what stops a loop")
	}
	if lasted := news.At.Sub(news.Since); lasted < 3*time.Second || lasted > 6*time.Second {
		t.Fatalf("the phase had been running %v when it landed, want about the four seconds it carried", lasted)
	}
	if news.At.After(time.Now()) || time.Since(news.At) > time.Minute {
		t.Fatalf("the phase landed at %v, want a reading of this machine's own clock", news.At)
	}
}

// A DEADLINE CROSSES AS TIME LEFT AND NEVER AS A MOMENT, for the same reason.
func TestAPhaseFrameRebuildsItsDeadlineAgainstThisMachinesClock(t *testing.T) {
	heard := heardPhases(t)
	client, engine := newEngine(t)
	defer func() { _ = client.Close() }()

	engine.send(Frame{Kind: "phase", Payload: mustClientJSON(PhaseWire{
		Phase:      "first word",
		Model:      "openai/gpt-5",
		Role:       string(lane.RoleTalk),
		SinceMS:    3100,
		DeadlineMS: 1300,
		Then:       "parasail",
	})})

	waitForNews(t, "the phase to reach the desk", func() bool { return len(heard()) > 0 })
	news := heard()[0]
	if news.Then != "parasail" {
		t.Fatalf("the consequence arrived as %q, want the lane the rescue would go to", news.Then)
	}
	if left := time.Until(news.Deadline); left < 500*time.Millisecond || left > 3*time.Second {
		t.Fatalf("the deadline is %v away, want about the 1.3s it carried", left)
	}
}

// THE `via <machine>` RIDER AND `/status`'s served ROW COME OFF THE LANE FRAME.
func TestALaneFrameReachesTheSurfacesOwnLaneReader(t *testing.T) {
	heard := heardLanes(t)
	client, engine := newEngine(t)
	defer func() { _ = client.Close() }()

	engine.send(Frame{Kind: "lane", Payload: mustClientJSON(LaneWire{
		Model:  "openai/gpt-5",
		Lane:   "coreweave",
		Winner: "parasail",
		Alt:    "parasail",
		TTFTMS: 3100,
		Rate:   61,
		Hedged: true,
		Reason: "slow",
		Role:   string(lane.RoleTalk),
	})})

	waitForNews(t, "the sighting to reach the desk", func() bool { return len(heard()) > 0 })
	news := heard()[0]
	if news.Lane != "coreweave" || news.Winner != "parasail" || !news.Hedged {
		t.Fatalf("the sighting arrived as %+v, want the rescued pair intact", news)
	}
	if news.TTFT != 3100*time.Millisecond || news.Rate != 61 {
		t.Fatalf("the sighting timed %v at %v tok/s, want 3.1s at 61", news.TTFT, news.Rate)
	}
	if !news.Relayed {
		t.Fatal("a sighting off the wire was not stamped as relayed")
	}
	if time.Since(news.At) > time.Minute {
		t.Fatalf("the sighting landed at %v, want a reading of this machine's own clock", news.At)
	}
}

// A FRAME KIND THIS BUILD DOES NOT KNOW IS STILL IGNORED, which is the law that
// let these two ride a wire version that predates them (wire.go's [Frame.Kind]).
func TestAFrameKindThisBuildDoesNotKnowIsStillIgnored(t *testing.T) {
	heard := heardPhases(t)
	client, engine := newEngine(t)
	defer func() { _ = client.Close() }()

	engine.send(Frame{Kind: "weather", Payload: mustClientJSON(PhaseWire{Phase: "raining"})})
	// The conversation is still there afterwards, which is the whole claim.
	if _, err := client.Ping(); err != nil {
		t.Fatalf("an unknown kind took the conversation down: %v", err)
	}
	if len(heard()) != 0 {
		t.Fatalf("an unknown kind reached the phase desk: %+v", heard())
	}
}

// ── the engine half ─────────────────────────────────────────────────────────

// newsWatching waits until this conversation has that many outboxes open.
//
// IT IS NOT PEDANTRY. The outbox opens at the very end of an arrival, after the
// welcome and the replay are on the wire (news.go says why), so a phase raised
// in the instant between the two is DROPPED — which is right, and invisible in
// a running build because a phase says itself again every second while it
// lasts. A test posts exactly once, so it waits for the road to exist first.
func newsWatching(t *testing.T, sess *Session, want int) {
	t.Helper()
	waitForNews(t, "the surface's outbox to open", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return len(sess.newsfeeds) >= want
	})
}

// EACH CONNECTION RECEIVES ONLY ITS OWN CONVERSATION'S NEWS.
//
// The readers are process-global and a host is not: one reader serves every
// conversation the host is running, so a piece of news that could not name its
// own would be drawn on every window at once.
func TestNewsGoesOnlyToTheConnectionOfTheConversationItNames(t *testing.T) {
	mine := newsSession(t, &newsAgent{fakeAgent: &fakeAgent{}, key: "conversation-one"})
	theirs := newsSession(t, &newsAgent{fakeAgent: &fakeAgent{}, key: "conversation-two"})

	here := dialSession(t, mine)
	here.hello(Hello{Version: Version, Surface: "macbook"})
	there := dialSession(t, theirs)
	there.hello(Hello{Version: Version, Surface: "studio"})
	newsWatching(t, mine, 1)
	newsWatching(t, theirs, 1)

	session.TellPhase(session.PhaseNews{
		Phase: provider.PhaseWriting, Model: "openai/gpt-5", Role: lane.RoleTalk,
		Lane: "friendli", Rate: 38, Session: "conversation-one", At: time.Now(),
	})

	frame := here.await(func(f Frame) bool { return f.Kind == "phase" })
	var wire PhaseWire
	if err := json.Unmarshal(frame.Payload, &wire); err != nil {
		t.Fatalf("the phase frame did not parse: %v", err)
	}
	if wire.Lane != "friendli" || wire.Rate != 38 {
		t.Fatalf("the phase crossed as %+v, want friendli at 38 tok/s", wire)
	}
	// AND THE OTHER CONVERSATION HEARD NOTHING. Its own turn is what its row is
	// about, and a clock from somebody else's window is worse than no clock.
	if frame, sent := nextFrameWithin(there, 200*time.Millisecond); sent {
		t.Fatalf("the other conversation was sent a %q frame: %s", frame.Kind, frame.Payload)
	}
}

// EVERY ROLE CROSSES AND THE SURFACE DECIDES which to draw, exactly as it does
// in one process (internal/tui3's phase.go). An engine that filtered here would
// be a second opinion about a question that already has one — and a task node's
// room would go dark on a hosted conversation.
func TestANodesOwnPhaseCrossesWithItsRoleOnIt(t *testing.T) {
	sess := newsSession(t, &newsAgent{fakeAgent: &fakeAgent{}, key: "conversation-one"})
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version, Surface: "macbook"})
	newsWatching(t, sess, 1)

	session.TellPhase(session.PhaseNews{
		Phase: session.PhaseRunning, Model: "openai/gpt-5", Role: lane.RoleLeafAttached,
		Detail: "go test", Session: "conversation-one", At: time.Now(),
	})

	frame := l.await(func(f Frame) bool { return f.Kind == "phase" })
	var wire PhaseWire
	if err := json.Unmarshal(frame.Payload, &wire); err != nil {
		t.Fatalf("the phase frame did not parse: %v", err)
	}
	if wire.Role != string(lane.RoleLeafAttached) || wire.Detail != "go test" {
		t.Fatalf("the node's phase crossed as %+v, want the node's own role and noun", wire)
	}
}

// A SURFACE THAT HAS STOPPED READING DOES NOT STALL THE TURN IT IS MEASURING.
//
// The fan-out runs on a turn's own stream goroutine, between two deltas, so an
// outbox that waited for room would be a status line able to hold up the answer
// it is describing. It drops instead, which is safe in a way a dropped event
// never would be: a phase says itself again every second while it lasts.
func TestAFullOutboxDropsRatherThanWaitingOnTheTurn(t *testing.T) {
	feed := &newsFeed{frames: make(chan Frame, newsBacklog), done: make(chan struct{})}
	t.Cleanup(feed.leave)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < newsBacklog*8; i++ {
			feed.offer(Frame{Kind: "phase"})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the poster was still waiting on an outbox nobody is draining")
	}
	if len(feed.frames) != newsBacklog {
		t.Fatalf("the outbox holds %d frames, want the %d it is bounded at", len(feed.frames), newsBacklog)
	}
}

// AND AN OUTBOX THAT HAS BEEN LEFT TAKES NOTHING MORE, which is what stops a
// connection that has gone from holding frames nobody will ever send.
func TestAnOutboxThatHasBeenLeftTakesNothingMore(t *testing.T) {
	feed := &newsFeed{frames: make(chan Frame, 1), done: make(chan struct{})}
	feed.leave()
	feed.leave() // idempotent, because every road out of a connection reaches it
	feed.offer(Frame{Kind: "phase"})
	feed.offer(Frame{Kind: "phase"})
	if len(feed.frames) > 1 {
		t.Fatalf("a left outbox took %d frames", len(feed.frames))
	}
}

// ── the answer road the phase opened ────────────────────────────────────────

// THE `y` THAT ANSWERS A STALLED PIN HAS SOMEWHERE TO GO.
//
// Until the phase crossed, a hosted engine had nobody reading phases at all and
// borrowed the other lane without asking. Now the question is asked and drawn,
// so the answer needs the same road back or the surface would be showing
// `switch to auto? (y)` over a key that does nothing.
func TestAFarStalledPinIsAnsweredOverTheWire(t *testing.T) {
	agent := &offerAgent{fakeAgent: &fakeAgent{}, standing: true}
	sess := newsSession(t, agent)
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version, Surface: "macbook"})

	frame := l.call(1, MethodAnswerLaneOffer, true)
	if frame.Error != "" {
		t.Fatalf("the answer was refused: %v", frame.Error)
	}
	var answered bool
	if err := json.Unmarshal(frame.Payload, &answered); err != nil {
		t.Fatalf("the answer did not parse: %v", err)
	}
	if !answered {
		t.Fatal("a standing offer answered false")
	}
	agent.mu.Lock()
	got := append([]bool(nil), agent.answers...)
	agent.mu.Unlock()
	if len(got) != 1 || !got[0] {
		t.Fatalf("the engine was told %v, want one yes", got)
	}
}

// AN ENGINE WITH NO OFFER TO ANSWER SAYS FALSE AND NOT AN ERROR. False is a
// real answer on this door — the lane came good, the request ended, the
// question aged out — so a capability that is absent reads as the answer a
// person's key already gets.
func TestAnEngineWithNoOfferDoorAnswersFalse(t *testing.T) {
	sess := newsSession(t, &fakeAgent{})
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version, Surface: "macbook"})

	frame := l.call(1, MethodAnswerLaneOffer, true)
	if frame.Error != "" {
		t.Fatalf("an engine with no offer door refused instead of answering: %v", frame.Error)
	}
	var answered bool
	if err := json.Unmarshal(frame.Payload, &answered); err != nil {
		t.Fatalf("the answer did not parse: %v", err)
	}
	if answered {
		t.Fatal("an engine with no offer door said it had answered one")
	}
}

// nextFrameWithin is the next frame on a link, or nothing at all within the
// wait. It is the shape a test that asserts SILENCE needs, which [link.recv]
// deliberately does not offer: that one fails the test when nothing comes.
func nextFrameWithin(l *link, wait time.Duration) (Frame, bool) {
	if len(l.spare) > 0 {
		frame := l.spare[0]
		l.spare = l.spare[1:]
		return frame, true
	}
	select {
	case frame, open := <-l.frames:
		return frame, open
	case <-time.After(wait):
		return Frame{}, false
	}
}

func waitForNews(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
