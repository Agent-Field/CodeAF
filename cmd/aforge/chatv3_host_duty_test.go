package main

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE DUTIES BEHIND THE HOST DOOR ─────────────────────────────────────────
//
// hostOptions keeps four readings of the far machine warm on goroutines of its
// own. These pins are the three laws chatv3_host_duty.go states: a duty with no
// connection never starts, a duty that dies lets go of its latch, and the
// surface hears about it once.

// faultLog parks the standard logger in a buffer for one test, so a recovered
// panic can be read back — and so that a test which must NOT produce one can
// prove it. It is what guard.Note writes to.
func faultLog(t *testing.T) func() string {
	t.Helper()
	buf := &lockedBuffer{}
	was := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(was) })
	return buf.String
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A DUTY WITH NO CONNECTION NEVER STARTS. The wire doors are closures made from
// the client, and a closure made from nil is a perfectly good function until it
// is called; the seam refuses before that, so the trip is never made, no fault
// is logged, and nothing is said.
func TestADutyArmedWithNoConnectionNeverStarts(t *testing.T) {
	logged := faultLog(t)
	var told []string
	var duty hostDuty
	hostFar{client: nil, tell: func(s string) { told = append(told, s) }}.arm(&duty, "reading the standing items")

	var client *remote.Client
	trip := func() { _, _ = client.World() }
	if duty.run("chatv3/host-world-prime", "", trip) {
		t.Fatal("a duty with no connection behind it started a trip")
	}
	time.Sleep(20 * time.Millisecond)
	if strings.Contains(logged(), "fault scope") {
		t.Fatalf("a duty that should never have started left a fault:\n%s", logged())
	}
	if len(told) != 0 {
		t.Fatalf("a duty that never ran said %q", told)
	}
	if !duty.due("", time.Hour) {
		t.Fatal("a duty that never ran should still read as due, so a client that arrives later is asked")
	}
}

// A DUTY THAT DIES LETS GO OF ITS LATCH AND SAYS SO ONCE. Before the seam, the
// latch was released inside the trip after the answer came back, so a panic left
// it taken for the life of the process: every later reading saw a fetch still
// out and the page froze on what it held, silently. Now the release is deferred
// under the trip, the fault is recorded where every fault is, one sentence goes
// to the notice line, and the next beat asks again.
func TestADutyThatFallsOverLetsGoOfItsLatchAndIsMentionedOnce(t *testing.T) {
	logged := faultLog(t)
	var mu sync.Mutex
	var told []string
	var duty hostDuty
	hostFar{client: &remote.Client{}, tell: func(s string) {
		mu.Lock()
		defer mu.Unlock()
		told = append(told, s)
	}}.arm(&duty, "reading what has been spent")

	trips := 0
	trip := func() {
		mu.Lock()
		trips++
		mu.Unlock()
		panic("the wire door fell over")
	}
	if !duty.run("chatv3/host-ledger", "", trip) {
		t.Fatal("a live duty refused to start")
	}
	settled := func() bool {
		duty.mu.Lock()
		defer duty.mu.Unlock()
		return !duty.out[""] && !duty.ended[""].IsZero()
	}
	if !waitFor(settled) {
		t.Fatal("the latch was never released after the trip panicked — this is the frozen page")
	}
	if !strings.Contains(logged(), `fault scope="chatv3/host-ledger"`) {
		t.Fatalf("the fault was not recorded:\n%s", logged())
	}
	mu.Lock()
	said := append([]string(nil), told...)
	mu.Unlock()
	if len(said) != 1 || !strings.Contains(said[0], "reading what has been spent") || !strings.Contains(said[0], "tried again") {
		t.Fatalf("the surface was told %q, want one sentence naming the duty and that it will be retried", said)
	}

	// THE NEXT BEAT ASKS AGAIN, and a second death is not a second sentence.
	duty.age("")
	if !duty.run("chatv3/host-ledger", "", trip) {
		t.Fatal("the duty would not run again after its latch was released")
	}
	if !waitFor(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return trips == 2
	}) {
		t.Fatal("the second trip never ran")
	}
	if !waitFor(settled) {
		t.Fatal("the second latch was never released")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(told) != 1 {
		t.Fatalf("a duty that fell over twice spoke twice: %q", told)
	}
}

// A replacement must not race ahead of the notice belonging to the trip it
// replaces. The blocked notice makes the publication boundary observable without
// depending on how quickly the logger or either goroutine happens to run.
func TestADutyPublishesItsFaultBeforeReleasingItsLatch(t *testing.T) {
	faultLog(t)
	publishing := make(chan struct{})
	allowPublication := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(allowPublication) }) }
	defer unblock()
	var duty hostDuty
	hostFar{client: &remote.Client{}, tell: func(string) {
		close(publishing)
		<-allowPublication
	}}.arm(&duty, "reading what has been spent")
	if !duty.run("chatv3/host-ledger", "", func() { panic("the wire door fell over") }) {
		t.Fatal("a live duty refused to start")
	}
	select {
	case <-publishing:
	case <-time.After(5 * time.Second):
		t.Fatal("the duty never reached its fault publication")
	}
	duty.mu.Lock()
	out, ended := duty.out[""], duty.ended[""]
	duty.mu.Unlock()
	if !out || !ended.IsZero() {
		t.Fatal("the trip read as landed before its fault was published")
	}
	if duty.run("chatv3/host-ledger", "", func() {}) {
		t.Fatal("a replacement started before the prior fault was published")
	}
	unblock()
	if !waitFor(func() bool {
		duty.mu.Lock()
		defer duty.mu.Unlock()
		return !duty.out[""] && !duty.ended[""].IsZero()
	}) {
		t.Fatal("the latch was not released after the fault was published")
	}
}

// ONE TRIP AT A TIME, whatever the caller does: the surface asks on every frame,
// and the latch is what keeps that from being a goroutine and a wire frame per
// frame. A second run while the first is out is refused and the first's answer
// is the one that lands.
func TestADutyHoldsOneTripOutAtATime(t *testing.T) {
	var duty hostDuty
	hold := make(chan struct{})
	started := make(chan struct{}, 4)
	trip := func() { started <- struct{}{}; <-hold }
	if !duty.run("chatv3/host-world", "", trip) {
		t.Fatal("the first trip did not start")
	}
	<-started
	for i := 0; i < 3; i++ {
		if duty.run("chatv3/host-world", "", trip) {
			t.Fatal("a second trip started while the first was still out")
		}
	}
	// Another key is another latch — the standing reading keeps one per
	// workspace, so a slow workspace does not hold up a fast one.
	if !duty.run("chatv3/host-standing", "/srv/other", trip) {
		t.Fatal("a different key was refused by the first key's latch")
	}
	close(hold)
	if !waitFor(func() bool {
		duty.mu.Lock()
		defer duty.mu.Unlock()
		return !duty.out[""] && !duty.out["/srv/other"]
	}) {
		t.Fatal("the trips never landed")
	}
	if duty.due("", time.Hour) {
		t.Fatal("a trip that just landed reads as due again")
	}
	duty.age("")
	if !duty.due("", time.Hour) {
		t.Fatal("an aged key does not read as due")
	}
}

// THE NOTICE LINE STAYS ONE LINE. The connection's own news comes first, the
// duties' after it, joined the way the client joins two of its own, and the
// reading drains: a sentence is news, not a condition, and a second reading
// answers nothing.
func TestDutyNewsJoinsTheConnectionsNoticeAndDrains(t *testing.T) {
	news := &hostNews{}
	connection := "the engine did not keep the turn"
	notice := news.join(func() string { s := connection; connection = ""; return s })
	news.say("reading what is remembered over this connection fell over once and will be tried again")
	got := notice()
	want := "the engine did not keep the turn — reading what is remembered over this connection fell over once and will be tried again"
	if got != want {
		t.Fatalf("notice = %q\nwant     %q", got, want)
	}
	if again := notice(); again != "" {
		t.Fatalf("a drained notice answered %q again", again)
	}
	if news.join(nil)() != "" {
		t.Fatal("no connection and no news should be the empty string, never a placeholder")
	}
}

// THE DOOR ITSELF, over a real client on a real engine (internal/remote's
// loopback), primes all three readings and none of them faults: the engine here
// has no ledger, no memory and no world to answer with, which is an honest
// error on each wire door and NOT a panic. This is the shape that used to be
// built with a nil client and logged three recovered nil dereferences per run.
func TestTheHostDoorPrimesItsReadingsWithoutAFault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logged := faultLog(t)
	client := hostedClient(t)
	options := hostOptions(onePipeFleet("devbox", client), client.Welcome(), false)
	if options.World == nil || options.Ledger == nil {
		t.Fatal("the door handed over no world or ledger seam")
	}
	// The primes are goroutines; give them their round trips.
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(logged(), "fault scope") {
		t.Fatalf("priming the door's readings faulted:\n%s", logged())
	}
	// And the notice line has nothing to say, which is the emptiness law.
	if said := options.Link.Notice(); said != "" {
		t.Fatalf("a door with nothing wrong said %q", said)
	}
}

// hostedClient is a real client dialled against a real engine in this process,
// with only the ssh child replaced. The engine answers the handshake and refuses
// every place it has no store for, which is exactly what a freshly paired
// machine does.
func hostedClient(t *testing.T) *remote.Client {
	t.Helper()
	loop, err := remote.Loopback(
		remote.Hello{Version: remote.Version, Workspace: "/srv/app"},
		remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: &quietAgent{}, Workspace: "/srv/app",
				SessionFile: "/srv/app/j.jsonl"}, nil
		}},
	)
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop.Client
}

// quietAgent is [remote.WrappedAgent] with nothing to say: every method the
// engine serves answers at once and empty, because these tests are about the
// door and never about a turn.
type quietAgent struct{}

func (q *quietAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}
func (q *quietAgent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	return q.Submit(ctx, text)
}
func (q *quietAgent) SubmitImage(ctx context.Context, text string, _ []session.Image) (<-chan session.Event, error) {
	return q.Submit(ctx, text)
}
func (q *quietAgent) FollowUp(text string) (<-chan session.Event, error) {
	return q.Submit(context.Background(), text)
}
func (q *quietAgent) Steer(text string) (<-chan session.Event, error)           { return q.FollowUp(text) }
func (q *quietAgent) Interrupt()                                                {}
func (q *quietAgent) InterruptFor(session.StopDoor)                             {}
func (q *quietAgent) Compact(context.Context) error                             { return nil }
func (q *quietAgent) Close() error                                              { return nil }
func (q *quietAgent) Model() string                                             { return "a/b" }
func (q *quietAgent) SetModel(string)                                           {}
func (q *quietAgent) SetContextWindow(int)                                      {}
func (q *quietAgent) ReasoningFor(string) string                                { return "" }
func (q *quietAgent) ReasoningLevels() map[string]string                        { return map[string]string{} }
func (q *quietAgent) SetReasoningFor(string, string)                            {}
func (q *quietAgent) ResolveConsent(uint64, bool)                               {}
func (q *quietAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (q *quietAgent) ResolveStanding(uint64, session.StandingAnswer)            {}
func (q *quietAgent) ResolveHarness(uint64, bool, string)                       {}
func (q *quietAgent) ResolveConnect(string, bool)                               {}
func (q *quietAgent) ResolveConnectKey(string, string)                          {}
func (q *quietAgent) NoteConnected(string, string)                              {}
func (q *quietAgent) Title() string                                             { return "" }
func (q *quietAgent) Usage() session.Usage                                      { return session.Usage{} }
func (q *quietAgent) ContextTokens() int                                        { return 0 }
func (q *quietAgent) Transcript() []session.DisplayEntry                        { return nil }
func (q *quietAgent) EarlierHistory() session.EarlierHistory                    { return session.EarlierHistory{} }
func (q *quietAgent) RewindPoints() []session.RewindPoint                       { return nil }
func (q *quietAgent) RewindAt(int) ([]session.DisplayEntry, error)              { return nil, nil }
