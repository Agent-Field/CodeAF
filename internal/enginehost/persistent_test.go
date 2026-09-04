package enginehost

// persistent_test.go is the promise the whole host exists for, driven end to
// end on a real socket with no model in it: WORK GOES ON AFTER THE TERMINAL
// CLOSES, and the person who comes back is put into the same conversation
// rather than a copy of it.
//
// Everything faked here is the conversation. The socket, the lock, the frames,
// the client and the idle policy are all the real ones, because every question
// below is about them.

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// workingAgent is a conversation with a turn in it that takes as long as the
// test says. It also carries the standing lanes a real session agent has, so a
// window that comes back can be asked whether it is subscribed to the same
// conversation it left.
type workingAgent struct {
	stubAgent

	mu sync.Mutex
	// turn is the stream the current Submit handed back, held so the test can
	// finish that turn AFTER the surface that started it has gone.
	turn chan session.Event
	// finished counts the turns that ran to completion, however few surfaces
	// were watching.
	finished int
	closes   int
	designs  []chan session.Event
	said     []string
	work     []session.WorkNode
	// cards and standing are the questions this conversation has up, newest
	// last. A card stands until its answer arrives.
	cards    map[uint64]session.Event
	standing []uint64
	answers  []uint64
}

// WorkingNow is the authoritative "is anything still happening" reading the
// idle policy consults ([remote.Session.IdleSince]).
func (a *workingAgent) WorkingNow() []session.WorkNode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.WorkNode(nil), a.work...)
}

func (a *workingAgent) setWork(nodes []session.WorkNode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.work = nodes
}

func (a *workingAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	lane := make(chan session.Event, 8)
	a.mu.Lock()
	a.turn = lane
	a.said = append(a.said, text)
	a.mu.Unlock()
	lane <- session.Event{Kind: session.EventTextDelta, Text: "working on it"}
	return lane, nil
}

// finish ends the turn in flight, which is the far machine getting on with the
// work while nobody is watching.
func (a *workingAgent) finish() {
	a.mu.Lock()
	lane := a.turn
	a.turn = nil
	a.finished++
	a.mu.Unlock()
	if lane == nil {
		return
	}
	lane <- session.Event{Kind: session.EventTurnDone, Text: "the refactor is done"}
	close(lane)
}

func (a *workingAgent) turns() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.finished
}

func (a *workingAgent) closed() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closes
}

func (a *workingAgent) Close() error {
	a.mu.Lock()
	a.closes++
	a.mu.Unlock()
	return nil
}

// WatchHarnessDesigns mirrors internal/session's own semantics, which is the
// point of the test below: EVERY NEW SUBSCRIPTION IS REPLAYED THE CARDS THAT ARE
// STILL STANDING (harness_build.go replays design cards and subharness
// proposals as a watcher attaches). That is what makes a card raised while
// nobody was attached survive a reconnect, and it is why these cards are not
// also in the waiting room — held.go would then hand the same question over
// twice.
func (a *workingAgent) WatchHarnessDesigns() (<-chan session.Event, func()) {
	lane := make(chan session.Event, 8)
	a.mu.Lock()
	for _, id := range a.standing {
		lane <- a.cards[id]
	}
	a.designs = append(a.designs, lane)
	a.mu.Unlock()
	var once sync.Once
	return lane, func() {
		once.Do(func() {
			a.mu.Lock()
			for at, one := range a.designs {
				if one == lane {
					a.designs = append(a.designs[:at], a.designs[at+1:]...)
					break
				}
			}
			a.mu.Unlock()
			close(lane)
		})
	}
}

// raiseDesign puts one card up: it stands until it is answered, and it goes to
// whoever is watching right now — which may be nobody.
func (a *workingAgent) raiseDesign(event session.Event) {
	a.mu.Lock()
	if a.cards == nil {
		a.cards = map[uint64]session.Event{}
	}
	if _, up := a.cards[event.ID]; !up {
		a.standing = append(a.standing, event.ID)
	}
	a.cards[event.ID] = event
	lanes := append([]chan session.Event(nil), a.designs...)
	a.mu.Unlock()
	for _, lane := range lanes {
		lane <- event
	}
}

// ResolveSubharness is the intake card's answer, and answering takes the card
// down — so it is not replayed to the next window.
func (a *workingAgent) ResolveSubharness(id uint64, run bool, input json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answers = append(a.answers, id)
	delete(a.cards, id)
	for at, standing := range a.standing {
		if standing == id {
			a.standing = append(a.standing[:at], a.standing[at+1:]...)
			break
		}
	}
}

func (a *workingAgent) answered() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]uint64(nil), a.answers...)
}

func (a *workingAgent) watching() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.designs)
}

var _ remote.WrappedAgent = (*workingAgent)(nil)

// hostHolding starts a real host on a real socket around one agent, and answers
// when it is listening. The agent is built ONCE and handed to every hello, which
// is what a host does with a conversation two windows ask for.
func hostHolding(t *testing.T, agent remote.WrappedAgent) string {
	t.Helper()
	shortHome(t)
	workspace := "/home/somebody/api"
	stopped := make(chan error, 1)
	go func() {
		stopped <- Run(workspace, Options{
			Boot: func(remote.Hello) (*remote.Engine, error) {
				return &remote.Engine{
					Agent:       agent,
					Workspace:   workspace,
					SessionFile: workspace + "/j.jsonl",
				}, nil
			},
			Key: func(hello remote.Hello) string { return hello.Session },
		})
	}()
	t.Cleanup(func() {
		_, _ = Stop(workspace)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Log("the host did not stop within five seconds")
		}
	})
	conn, err := waitForHost(workspace, 5*time.Second)
	if err != nil {
		t.Fatalf("no host answered: %v", err)
	}
	_ = conn.Close()
	return workspace
}

func dialHost(t *testing.T, workspace string) *remote.Client {
	t.Helper()
	conn, err := Dial(workspace)
	if err != nil {
		t.Fatalf("dial the host: %v", err)
	}
	client, err := remote.Dial(conn, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return client
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}

// THE PROMISE: a window that goes away mid-turn does not take the work with it,
// and the conversation is still there when somebody comes back to it.
//
// The link is CUT rather than closed, which is the event this is really about —
// a lid shut, a network gone, a terminal killed — and the difference matters:
// [remote.MethodClose] is a person saying the conversation is over, and that
// one does end it (the test below).
func TestWorkGoesOnAfterTheWindowIsCutAndTheNextWindowIsInTheSameConversation(t *testing.T) {
	far := &workingAgent{}
	workspace := hostHolding(t, far)

	first := dialHost(t, workspace)
	if !first.Welcome().Persistent {
		t.Fatal("a host said its conversations were not persistent")
	}
	if _, err := first.Agent().Submit(context.Background(), "refactor the parser"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// The terminal goes away without saying anything.
	_ = first.Close()

	// AND THE TURN FINISHES ANYWAY. Nothing here waits for a surface: the agent
	// is asked to finish while the room is empty, and the conversation must
	// still be the one holding it.
	far.finish()
	waitUntil(t, "the turn ran to completion with nobody watching", func() bool { return far.turns() == 1 })
	if far.closed() != 0 {
		t.Fatal("a cut window closed the conversation — the work was thrown away")
	}

	// AND THE NEXT WINDOW IS IN THAT CONVERSATION, not in a copy of it: the same
	// session file, and the same agent underneath — which is why the turn count
	// above is still readable through it.
	second := dialHost(t, workspace)
	t.Cleanup(func() { _ = second.Close() })
	if got, want := second.Welcome().SessionFile, workspace+"/j.jsonl"; got != want {
		t.Fatalf("the second window opened %q, want %q", got, want)
	}
	if second.Welcome().Attached != 0 {
		t.Fatalf("the second window was told %d others were attached", second.Welcome().Attached)
	}

	// AND ITS STANDING LANE IS SUBSCRIBED TO THE SAME CONVERSATION. A card
	// raised after the reconnect reaches the window that came back, which is the
	// half a reattached surface used to lose entirely.
	lane, stop := second.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitUntil(t, "the returning window subscribed", func() bool { return far.watching() == 1 })
	far.raiseDesign(session.Event{Kind: session.EventHarnessDesign, ID: 2, Text: "a page about parsers"})
	select {
	case event, ok := <-lane:
		if !ok || event.ID != 2 {
			t.Fatalf("the returning window's lane carried %v (open=%v)", event, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the returning window's lane")
	}
}

// AND THE CUT WINDOW'S SUBSCRIPTION IS LET GO OF, so a host holding a
// conversation for hours is not also holding a pump per window that ever
// attached to it.
func TestACutWindowLeavesItsLaneBehind(t *testing.T) {
	far := &workingAgent{}
	workspace := hostHolding(t, far)

	client := dialHost(t, workspace)
	_, stop := client.Agent().WatchHarnessDesigns()
	defer stop()
	waitUntil(t, "the window subscribed", func() bool { return far.watching() == 1 })

	_ = client.Close()
	waitUntil(t, "the host let go of the subscription", func() bool { return far.watching() == 0 })
}

// SAYING GOODBYE IS DIFFERENT FROM WALKING AWAY, and the difference is the
// whole of the lifetime a person is promised: [remote.MethodClose] ends the
// conversation — the journal is flushed and the agent closed — while the cut
// above ended nothing.
func TestClosingTheConversationEndsItWhereACutDoesNot(t *testing.T) {
	far := &workingAgent{}
	workspace := hostHolding(t, far)

	client := dialHost(t, workspace)
	if err := client.Agent().Close(); err != nil {
		t.Fatalf("close the conversation: %v", err)
	}
	waitUntil(t, "the conversation was closed", func() bool { return far.closed() == 1 })
	_ = client.Close()
}

// A CONVERSATION WITH A TURN IN IT IS NEVER IDLE, which is what keeps the sweep
// from retiring the very work the host exists to carry. The reading is
// [remote.Session.IdleSince] and the policy is [Host.sweepOnce]; this drives
// both through the socket rather than trusting either on its own.
func TestASweepKeepsAConversationThatIsStillWorking(t *testing.T) {
	far := &workingAgent{}
	shortHome(t)
	workspace := "/home/somebody/api"
	host := stubHostAround(t, workspace, far)

	surface, engine := net.Pipe()
	go host.attach(engine)
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := client.Agent().Submit(context.Background(), "the long one"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	_ = client.Close()
	_ = surface.Close()

	waitUntil(t, "the host noticed the window had gone", func() bool {
		for _, sess := range host.held() {
			if sess.Attached() == 0 {
				return true
			}
		}
		return false
	})
	// The turn is still running, so the conversation reads as busy however long
	// the room stays empty — an idle clock that had started here would retire
	// the work at the far end of it.
	for _, sess := range host.held() {
		if !sess.IdleSince().IsZero() {
			t.Fatal("a conversation with a turn in it started an idle clock")
		}
	}
	if host.sweepOnce() {
		t.Fatal("the sweep retired a host that is still carrying work")
	}
	if far.closed() != 0 {
		t.Fatal("the sweep closed a conversation with a turn in it")
	}

	// And when the work lands and the room is still empty, the clock starts —
	// the conversation is kept for its idle span rather than for ever.
	far.finish()
	waitUntil(t, "the conversation went idle once its turn landed", func() bool {
		for _, sess := range host.held() {
			if !sess.IdleSince().IsZero() {
				return true
			}
		}
		return false
	})
}

// stubHostAround is [stubHost] holding one agent the test can drive, without a
// socket: the questions above are about the sweep and the sessions map.
func stubHostAround(t *testing.T, workspace string, agent remote.WrappedAgent) *Host {
	t.Helper()
	host := stubHost(t, workspace)
	host.opts.Boot = func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: workspace + "/j.jsonl"}, nil
	}
	return host
}

// held is every conversation this host is holding, copied out from under the
// lock so a test can read them without racing the sweep.
func (h *Host) held() []*remote.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*remote.Session, 0, len(h.sessions))
	for _, sess := range h.sessions {
		out = append(out, sess)
	}
	return out
}

// THE SWEEP'S OWN CLOCK, WITH WORK UNDER IT. The turn is over — its ring is
// gone, which is what made the conversation read as idle — and a task is still
// running. Waiting out the real thirty minutes is not a test, so the policy's
// span is shortened and the pass is asked directly.
func TestTheIdleSweepDoesNotRetireAConversationWhoseTaskIsStillRunning(t *testing.T) {
	was := sessionIdle
	sessionIdle = 20 * time.Millisecond
	t.Cleanup(func() { sessionIdle = was })

	far := &workingAgent{}
	far.work = []session.WorkNode{{ID: "task:4", Title: "port the parser", State: session.WorkRunning}}
	shortHome(t)
	workspace := "/home/somebody/api"
	host := stubHostAround(t, workspace, far)

	surface, engine := net.Pipe()
	go host.attach(engine)
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := client.Agent().Submit(context.Background(), "port the parser"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// The TURN ends here — the task it started does not.
	far.finish()
	_ = client.Close()
	_ = surface.Close()
	waitUntil(t, "the window had gone and the turn had landed", func() bool {
		for _, sess := range host.held() {
			if sess.Attached() == 0 {
				return true
			}
		}
		return false
	})

	// Well past the shortened span, and still kept.
	time.Sleep(60 * time.Millisecond)
	host.sweepOnce()
	if far.closed() != 0 {
		t.Fatal("the sweep closed a conversation whose task was still running")
	}
	if len(host.held()) == 0 {
		t.Fatal("the sweep let go of a conversation whose task was still running")
	}

	// The task lands, the span passes again, and now it is retired — the clock
	// is a real clock and not a way of never retiring anything.
	far.setWork(nil)
	time.Sleep(60 * time.Millisecond)
	host.sweepOnce()
	waitUntil(t, "the conversation was retired once its work had landed", func() bool {
		return far.closed() == 1 && len(host.held()) == 0
	})
}

// A CARD RAISED WITH EVERY WINDOW GONE IS THERE WHEN SOMEBODY COMES BACK, once,
// and answering it takes it down for good.
//
// This is the half a lane alone does not buy. The card is not on any turn's
// stream and not in the waiting room either; what carries it across the gap is
// that a fresh subscription is replayed whatever is still standing, and that the
// engine opens a FRESH subscription for every window that asks
// (internal/remote's standinglane.go).
func TestACardRaisedWithNobodyAttachedIsHandedToTheNextWindowOnce(t *testing.T) {
	far := &workingAgent{}
	workspace := hostHolding(t, far)

	first := dialHost(t, workspace)
	_, stop := first.Agent().WatchHarnessDesigns()
	waitUntil(t, "the first window subscribed", func() bool { return far.watching() == 1 })
	stop()
	_ = first.Close()
	waitUntil(t, "the room emptied", func() bool { return far.watching() == 0 })

	// Nobody is attached. The conversation asks anyway.
	far.raiseDesign(session.Event{Kind: session.EventSubharnessProposal, ID: 12, Text: "release-notes"})

	second := dialHost(t, workspace)
	t.Cleanup(func() { _ = second.Close() })
	lane, stopSecond := second.Agent().WatchHarnessDesigns()
	t.Cleanup(stopSecond)

	select {
	case event, ok := <-lane:
		if !ok || event.ID != 12 || event.Kind != session.EventSubharnessProposal {
			t.Fatalf("the returning window got %v (open=%v), want the card raised while it was away", event, ok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the card raised with nobody attached never reached the next window")
	}
	// Exactly once: a card that arrived on the replay must not arrive again.
	select {
	case event := <-lane:
		t.Fatalf("the card was handed over twice: %v", event)
	case <-time.After(200 * time.Millisecond):
	}

	// The key works from here, and the answer takes the card down.
	second.Agent().ResolveSubharness(12, true, json.RawMessage(`{"since":"v2"}`))
	waitUntil(t, "the answer reached the conversation", func() bool {
		said := far.answered()
		return len(said) == 1 && said[0] == 12
	})

	// AND AN ANSWERED CARD DOES NOT COME BACK. A third window — or the same one
	// after a redial — opens a fresh subscription and is told nothing.
	third := dialHost(t, workspace)
	t.Cleanup(func() { _ = third.Close() })
	quiet, stopThird := third.Agent().WatchHarnessDesigns()
	t.Cleanup(stopThird)
	select {
	case event, ok := <-quiet:
		if ok {
			t.Fatalf("an answered card was replayed to the next window: %v", event)
		}
	case <-time.After(300 * time.Millisecond):
	}
}
