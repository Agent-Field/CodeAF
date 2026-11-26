package livedoor

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The tests in this file are derived from the validation contract (SPEC-P2 V31
// and V37). They run a REAL holder (remote.Session behind remote.ServeAttach) and
// a REAL roaming client (remote.Roam over the Link's opener), so that "arrives
// once" is a property of the whole path a person's words take, not of a stub.

// swapGate holds one message inside the agent until the test lets it go, so that
// a send is IN FLIGHT at the holder at the instant a road swap commits.
type swapGate struct {
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

// swapAgent is the Desktop's agent as the contract sees it: it counts how many
// times each message was APPLIED, and it hands the test the event stream of every
// turn so that the test decides when the turn says what.
type swapAgent struct {
	*minimalWrappedAgent

	mu      sync.Mutex
	applied map[string]int
	plain   int
	gates   map[string]*swapGate
	streams map[string]chan session.Event
	next    map[string]int
	said    map[string]string
}

func newSwapAgent() *swapAgent {
	return &swapAgent{
		minimalWrappedAgent: &minimalWrappedAgent{title: "Swap Chat"},
		applied:             map[string]int{},
		gates:               map[string]*swapGate{},
		streams:             map[string]chan session.Event{},
		next:                map[string]int{},
		said:                map[string]string{},
	}
}

// SubmitMessage is the door a send carrying a ClientID comes in by. Its count is
// the answer to "how many times did the Desktop's transcript take this message".
func (a *swapAgent) SubmitMessage(_ context.Context, m session.Submission) (<-chan session.Event, error) {
	a.mu.Lock()
	a.applied[m.ClientID]++
	gate := a.gates[m.ClientID]
	ch := make(chan session.Event, 4096)
	a.streams[m.ClientID] = ch
	a.mu.Unlock()
	if gate != nil {
		gate.enterOnce.Do(func() { close(gate.entered) })
		<-gate.release
	}
	return ch, nil
}

// Submit is the door a send with NO ClientID would come in by. The test sends none
// without one, so a count here means the id was lost on the way.
func (a *swapAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	a.mu.Lock()
	a.plain++
	a.mu.Unlock()
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

// hold makes the agent keep message id inside SubmitMessage until released.
func (a *swapAgent) hold(id string) *swapGate {
	g := &swapGate{entered: make(chan struct{}), release: make(chan struct{})}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gates[id] = g
	return g
}

func (g *swapGate) let() { g.releaseOnce.Do(func() { close(g.release) }) }

// pushNext has stream id say its next n words, numbered, under one lock so that
// two goroutines pushing the same stream can never interleave the numbering.
func (a *swapAgent) pushNext(id string, n int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ch := a.streams[id]
	if ch == nil {
		return fmt.Errorf("the agent holds no stream for %q", id)
	}
	for i := 0; i < n; i++ {
		text := fmt.Sprintf("%s:%04d;", id, a.next[id])
		a.next[id]++
		a.said[id] += text
		ch <- session.Event{Kind: session.EventTextDelta, Text: text}
	}
	return nil
}

// finish ends the turn of stream id.
func (a *swapAgent) finish(id string) {
	a.mu.Lock()
	ch := a.streams[id]
	a.mu.Unlock()
	if ch == nil {
		return
	}
	ch <- session.Event{Kind: session.EventTurnDone}
	close(ch)
}

func (a *swapAgent) saidOn(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.said[id]
}

func (a *swapAgent) appliedOf(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applied[id]
}

func (a *swapAgent) plainCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.plain
}

// collected is what a viewer saw of one stream: every word in the order it
// arrived, and how many times the turn was reported done.
type collected struct {
	mu    sync.Mutex
	text  string
	dones int
}

// collect reads events in its own goroutine for as long as the stream lives.
func collect(events <-chan session.Event) *collected {
	c := &collected{}
	guard.Go("livedoor.test.swap.collect", func() {
		for ev := range events {
			c.mu.Lock()
			switch ev.Kind {
			case session.EventTextDelta:
				c.text += ev.Text
			case session.EventTurnDone:
				c.dones++
			}
			c.mu.Unlock()
		}
	})
	return c
}

func (c *collected) seen() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, c.dones
}

// swapRig is the direct rig with a real holder behind every road and a roaming
// client in front of the Link.
type swapRig struct {
	*directRig
	ag *swapAgent
	cl *remote.Client
}

const swapSessionFile = "/work/p/swap.jsonl"

func newSwapRig(t *testing.T) *swapRig {
	t.Helper()
	ag := newSwapAgent()
	sess := remote.NewSession(&remote.Engine{Agent: ag, Workspace: "/work/p", SessionFile: swapSessionFile}, true)
	t.Cleanup(func() { _ = sess.Close() })
	holder := func(r *directRig, road string, tunnel int) Resolver {
		return fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
			r.recordOpen(road, tunnel, chat)
			routerSide, holderSide := net.Pipe()
			guard.Go("livedoor.test.swap.holder", func() {
				defer holderSide.Close()
				_ = remote.ServeAttach(holderSide, holderSide, remote.AttachOptions{
					Open: func(remote.Hello) (*remote.Session, error) { return sess, nil },
				})
			})
			return routerSide, Route{Kind: "daemon", PID: 7002, Session: swapSessionFile, Workspace: "/work/p"}, nil
		})
	}
	// A hundred hours of idle time: the test moves the fake clock by minutes and the
	// view must not detach under it.
	rig := newDirectRig(t, rigOpts{idleDetach: 100 * time.Hour, holder: holder})
	return &swapRig{directRig: rig, ag: ag}
}

// connect attaches the viewer the way the door does: a roaming client whose every
// dial is the Link's opener, with sends resent after a redial.
func (s *swapRig) connect() {
	s.t.Helper()
	cl, err := remote.Roam("desktop", remote.Hello{Workspace: "/work/p", Session: swapSessionFile, Join: true}, remote.Roaming{
		Dial:             func() (io.ReadWriteCloser, error) { return s.openChatErr("chat-swap") },
		Window:           30 * time.Second,
		ResendUnanswered: true,
	})
	if err != nil {
		s.t.Fatalf("attaching the viewer failed: %v", err)
	}
	s.t.Cleanup(func() { _ = cl.Close() })
	s.cl = cl
	if err := cl.Take(); err != nil {
		s.t.Fatalf("taking the keyboard failed: %v", err)
	}
}

// send types one message with a fixed ClientID and, when the call is refused
// because the link is between roads, types the SAME message again, which is
// exactly what a person's retry is and what the host must not apply twice.
func (s *swapRig) send(id string) (<-chan session.Event, error) {
	var last error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		events, err := s.cl.Agent().SubmitMessage(s.ctx, session.Submission{Text: "message " + id, ClientID: id})
		if err == nil {
			return events, nil
		}
		last = err
		quiesce()
	}
	return nil, fmt.Errorf("the send %s never succeeded: %w", id, last)
}

// upgrade moves the clock in small steps until a direct attempt begins and the
// road reads direct. It gives up after twenty seconds of fake time, which is
// longer than the first rung of the retry ladder.
func (s *swapRig) upgrade() {
	s.t.Helper()
	before := s.directDials.Load()
	for i := 0; s.directDials.Load() == before; i++ {
		if i > 200 {
			s.t.Fatalf("no direct attempt began in twenty seconds of fake time (timers: %v)", timersWithin(s.fc, 1000*time.Hour))
		}
		s.advanceFine(100 * time.Millisecond)
	}
	s.waitRoad(RoadDirect)
}

// reattached waits until the client's redial has opened a channel on road beyond
// before opens and the client answers a call there.
func (s *swapRig) reattached(road string, before int) {
	s.t.Helper()
	within(s.t, "the client to redial onto "+road, func() bool { return len(s.opensOn(road)) > before })
	within(s.t, "the client to be answered on "+road, func() bool {
		if err := s.cl.Err(); err != nil {
			s.t.Fatalf("the client ended during a swap: %v", err)
		}
		_, err := s.cl.Ping()
		return err == nil
	})
}

// V31: a message typed at the instant the road changes, in either direction,
// arrives once: the Desktop's agent applies it once, and the events of a running
// turn arrive once and in order. Twenty swaps alternate relay to direct and back;
// at each one a held send is in flight at the holder, a second send is typed at
// a different distance from the commit every time, and the running turn is
// talking while the swap commits.
func TestASendAcrossARoadSwapIsAppliedOnceAndEventsArriveOnceInOrder(t *testing.T) {
	const swaps = 20
	s := newSwapRig(t)
	s.connect()
	ag := s.ag

	// waitDone fails with everything a reader needs to see what went missing: what
	// the viewer showed, what the agent said and how often it was told.
	waitDone := func(id string, c *collected) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, dones := c.seen(); dones >= 1 {
				return
			}
			quiesce()
		}
		got, dones := c.seen()
		t.Fatalf("the turn of %s never finished on the viewer: it showed %q (done %d times), the agent said %q (applied %d times, %d unnamed), the client's error is %v",
			id, got, dones, ag.saidOn(id), ag.appliedOf(id), ag.plainCount(), s.cl.Err())
	}

	// The running turn lives across every swap.
	turnEvents, err := s.send("turn")
	if err != nil {
		t.Fatal(err)
	}
	turn := collect(turnEvents)
	if err := ag.pushNext("turn", 2); err != nil {
		t.Fatal(err)
	}

	type sent struct {
		events <-chan session.Event
		err    error
	}
	streams := map[string]*collected{}
	var ids []string

	// typed starts a send on its own goroutine and says where its answer will be.
	typed := func(id string) chan sent {
		result := make(chan sent, 1)
		guard.Go("livedoor.test.swap.send", func() {
			events, err := s.send(id)
			result <- sent{events: events, err: err}
		})
		return result
	}
	// answered waits for a typed send's answer and starts reading its stream.
	answered := func(i int, id string, result chan sent) {
		t.Helper()
		var res sent
		select {
		case res = <-result:
		case <-time.After(20 * time.Second):
			t.Fatalf("swap %d: the send %s never returned", i, id)
		}
		if res.err != nil {
			t.Fatalf("swap %d: the send %s failed: %v", i, id, res.err)
		}
		streams[id] = collect(res.events)
	}

	for i := 0; i < swaps; i++ {
		to := RoadDirect
		if i%2 == 1 {
			to = RoadRelay
		}
		before := len(s.opensOn(to))
		held := fmt.Sprintf("held-%02d", i)
		raced := fmt.Sprintf("race-%02d", i)
		ids = append(ids, held, raced)

		// The first message is typed and waits at the holder, in flight.
		gate := ag.hold(held)
		heldResult := typed(held)
		select {
		case <-gate.entered:
		case <-time.After(10 * time.Second):
			gate.let()
			t.Fatalf("swap %d: the message %s never reached the holder", i, held)
		}

		// The second is typed a little before the road changes, a little further
		// from it each time, so that across the swaps it lands before the commit,
		// across it and after it.
		racedResult := typed(raced)
		for k := 0; k < i*25; k++ {
			runtime.Gosched()
		}

		// The running turn keeps talking from another goroutine while the road
		// changes under it.
		talked := make(chan error, 1)
		guard.Go("livedoor.test.swap.talk", func() {
			for k := 0; k < 4; k++ {
				if err := ag.pushNext("turn", 3); err != nil {
					talked <- err
					return
				}
				quiesce()
			}
			talked <- nil
		})

		// The road changes.
		if to == RoadDirect {
			s.upgrade()
		} else {
			s.swapToRelay()
		}
		gate.let()
		if err := <-talked; err != nil {
			t.Fatalf("swap %d: %v", i, err)
		}
		answered(i, held, heldResult)
		answered(i, raced, racedResult)
		s.reattached(to, before)
		if road := s.link.Road(); road != to {
			t.Fatalf("swap %d: the road reads %q, want %q", i, road, to)
		}

		// After the swap everything still flows: each send's own turn speaks and
		// ends, and the running turn goes on. A turn that FINISHES is allowed to
		// be seen finishing before the road changes again: the ring of a finished
		// stream is gone and the transcript is the authority on its tail (the
		// remote package's own law), so only a turn still running has its events
		// carried across a swap, and the running turn above is that.
		for _, id := range []string{held, raced} {
			if err := ag.pushNext(id, 2); err != nil {
				t.Fatalf("swap %d: %v", i, err)
			}
			ag.finish(id)
		}
		if err := ag.pushNext("turn", 2); err != nil {
			t.Fatalf("swap %d: %v", i, err)
		}
		waitDone(held, streams[held])
		waitDone(raced, streams[raced])

		// A direct link that stays up a minute is a link whose later death starts
		// no ladder climb, so every direction of the next swap is a first attempt.
		if to == RoadDirect {
			s.advanceHealthy(s.latest(RoadDirect), DirectStableAfter+time.Second)
		}
	}

	// The running turn ends.
	if err := ag.pushNext("turn", 1); err != nil {
		t.Fatal(err)
	}
	ag.finish("turn")
	waitDone("turn", turn)
	quiesce()

	// Every word arrived once, in the order it was said, and every turn ended once.
	if got, dones := turn.seen(); got != ag.saidOn("turn") || dones != 1 {
		t.Fatalf("the running turn showed %q (done %d times), the agent said %q", got, dones, ag.saidOn("turn"))
	}
	for _, id := range ids {
		if n := ag.appliedOf(id); n != 1 {
			t.Fatalf("the message %s was applied %d times, want once", id, n)
		}
		if got, dones := streams[id].seen(); got != ag.saidOn(id) || dones != 1 {
			t.Fatalf("the turn of %s showed %q (done %d times), the agent said %q", id, got, dones, ag.saidOn(id))
		}
	}
	if n := ag.appliedOf("turn"); n != 1 {
		t.Fatalf("the running turn's own message was applied %d times, want once", n)
	}
	if n := ag.plainCount(); n != 0 {
		t.Fatalf("%d messages reached the agent with no id, want none", n)
	}
	// The swaps really happened: both roads carried the client, and it never ended.
	if d, r := len(s.opensOn(RoadDirect)), len(s.opensOn(RoadRelay)); d < swaps/2 || r < swaps/2+1 {
		t.Fatalf("the holder was reached %d times over direct and %d over the relay in %d swaps", d, r, swaps)
	}
	if err := s.cl.Err(); err != nil {
		t.Fatalf("the client ended across the swaps: %v", err)
	}
}

// settleTap waits until no frame has crossed tap for a run of yields, which is the
// moment an after-the-swap count is fair: the swap's own closing frame has left.
func settleTap(tap *frameTap) {
	for stable := 0; stable < 4; {
		n := tap.mark()
		quiesce()
		if tap.mark() == n {
			stable++
		} else {
			stable = 0
		}
	}
}

// V37: after the swap the relay standby sends no frame. The channel the swap
// closed leaves one closing frame, and from then on, through traffic on the
// direct road, key presses and minutes of fake time, the relay carrier sees no
// frame from the viewer, and the host sends it nothing that carries a word.
func TestTheRelayCarriesNoFrameAfterTheSwap(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	first := rig.openChat("chat-a")
	rig.speak(first)
	rig.swapToDirect()
	relay := rig.tunnel(RoadRelay, 1)
	if !isPlanned(rig.endOf(first)) {
		t.Fatal("the swap did not end the relay channel with the planned redial")
	}

	ch := rig.openChat("chat-a")
	if road := rig.speak(ch); road != RoadDirect {
		t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
	}
	other := rig.openChat("chat-b")
	if road := rig.speak(other); road != RoadDirect {
		t.Fatalf("chat-b was carried on %q after the swap, want direct", road)
	}
	direct := rig.latest(RoadDirect)
	settleTap(&relay.viewer.sent)
	settleTap(&relay.hostTap)
	viewerMark := relay.viewer.sent.mark()
	hostMark := relay.hostTap.mark()
	directMark := direct.viewer.sent.mark()

	// A working view: words both ways on the direct road, keys, scrolling, and
	// minutes of a link that is alive.
	for i := 0; i < 40; i++ {
		if _, err := ch.Write([]byte("a line typed on the direct road\n")); err != nil {
			t.Fatalf("a write on the direct channel failed: %v", err)
		}
		if _, err := other.Write([]byte("and another\n")); err != nil {
			t.Fatalf("a write on the second direct channel failed: %v", err)
		}
		rig.link.Touch()
	}
	rig.advanceHealthy(direct, 3*time.Minute)
	rig.link.Touch()
	rig.advanceHealthy(direct, 3*time.Minute)

	if frames := relay.viewer.sent.since(viewerMark); len(frames) != 0 {
		t.Fatalf("the viewer sent %v on the relay after the swap, want no frame", frames)
	}
	for _, fr := range relay.hostTap.since(hostMark) {
		switch fr.op {
		case OpData, OpOpen, OpOpened, OpPing, OpPong, OpSignal:
			t.Fatalf("the host sent the relay carrier %v after the swap; want nothing that carries a word", relay.hostTap.since(hostMark))
		}
	}
	if relay.viewer.closed.Load() {
		t.Fatal("the relay standby was closed while direct carried")
	}
	// The traffic did cross the direct road, so the silence above is not an idle
	// test.
	if n := direct.viewer.sent.opsSince(directMark, OpData); n < 40 {
		t.Fatalf("only %d data frames crossed the direct road, want the traffic the test wrote", n)
	}
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
		t.Fatalf("State() = %+v, want Live on direct", st)
	}
}
