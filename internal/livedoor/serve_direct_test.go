package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// fakeDirectSession is the host half's record of one pending attempt: what was
// delivered to it, in order, and whether it was closed.
type fakeDirectSession struct {
	mu        sync.Mutex
	delivered [][]byte
	closed    chan struct{}
	once      sync.Once
}

func newFakeDirectSession() *fakeDirectSession {
	return &fakeDirectSession{closed: make(chan struct{})}
}

func (s *fakeDirectSession) Deliver(p []byte) {
	s.mu.Lock()
	s.delivered = append(s.delivered, append([]byte(nil), p...))
	s.mu.Unlock()
}
func (s *fakeDirectSession) Close() { s.once.Do(func() { close(s.closed) }) }

func (s *fakeDirectSession) deliveries() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.delivered...)
}

func (s *fakeDirectSession) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// fakeDirectHost records every offer it is handed. It answers with a session, or
// with nil (a refusal) when refuse is set.
type fakeDirectHost struct {
	mu       sync.Mutex
	refuse   bool
	devices  []string
	payloads [][]byte
	sessions []*fakeDirectSession
	signals  []Signaller
}

func (f *fakeDirectHost) Offered(_ context.Context, peer Peer, offer []byte, s Signaller) DirectSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = append(f.devices, peer.Device)
	f.payloads = append(f.payloads, append([]byte(nil), offer...))
	f.signals = append(f.signals, s)
	if f.refuse {
		return nil
	}
	session := newFakeDirectSession()
	f.sessions = append(f.sessions, session)
	return session
}

func (f *fakeDirectHost) offers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.devices)
}

func (f *fakeDirectHost) session(i int) *fakeDirectSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[i]
}

// fakeRoadTunnel is a registry entry with a road and nothing else, for the
// registry's own rules.
type fakeRoadTunnel struct {
	road    string
	reasons chan string
	permits atomic.Int64
}

func newFakeRoadTunnel(road string) *fakeRoadTunnel {
	return &fakeRoadTunnel{road: road, reasons: make(chan string, 4)}
}
func (f *fakeRoadTunnel) CloseWithReason(reason string) { f.reasons <- reason }
func (f *fakeRoadTunnel) PermitsChanged()               { f.permits.Add(1) }
func (f *fakeRoadTunnel) roadName() string              { return f.road }

func (f *fakeRoadTunnel) closedWith() (string, bool) {
	select {
	case reason := <-f.reasons:
		return reason, true
	default:
		return "", false
	}
}

// signalRig is one host tunnel over a pipe and the viewer's end of it.
type signalRig struct {
	v     *Mux
	ack   HelloAck
	inbox *signalInbox
	done  chan error
	pongs atomic.Int64
}

// startTunnel serves one tunnel for peer under cfg and returns the viewer's side
// once the host's HelloAck has arrived.
func startTunnel(t *testing.T, ctx context.Context, peer Peer, cfg ServeConfig) *signalRig {
	t.Helper()
	vConn, hConn := net.Pipe()
	t.Cleanup(func() { _ = vConn.Close(); _ = hConn.Close() })
	rig := &signalRig{inbox: newSignalInbox(), done: make(chan error, 1)}
	guard.Go("livedoor.test.direct.serve", func() { rig.done <- Serve(ctx, hConn, peer, cfg) })
	rig.v = NewMux(vConn, Viewer, MuxConfig{Clock: cfg.Clock, OnSignal: rig.inbox.onSignal, OnPong: func(uint32) { rig.pongs.Add(1) }})
	t.Cleanup(func() { _ = rig.v.Close() })
	ack, err := rig.v.WaitHelloAck(ctx)
	if err != nil {
		t.Fatalf("WaitHelloAck: %v", err)
	}
	rig.ack = ack
	return rig
}

func (r *signalRig) send(t *testing.T, s Signal) []byte {
	t.Helper()
	payload, err := EncodeSignal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.v.SendSignal(payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// roundTrip sends a ping and waits for its pong. The host answers in the order it
// handled frames, so everything it queued before that pong has already been read
// by the viewer: a way to prove that something did NOT arrive.
func (r *signalRig) roundTrip(t *testing.T) {
	t.Helper()
	want := r.pongs.Load() + 1
	if err := r.v.SendPing(uint32(want)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the host to answer a ping", func() bool { return r.pongs.Load() >= want })
}

func watchPeer(device string) Peer {
	return Peer{Device: device, Label: device, Permit: pair.Permit{Watch: true, Drive: true}}
}

func expectSignal(t *testing.T, inbox *signalInbox) Signal {
	t.Helper()
	sig, err := DecodeSignal(inbox.take(t))
	if err != nil {
		t.Fatalf("the host sent a signal that does not decode: %v", err)
	}
	return sig
}

// V38: the HelloAck advertises the direct road only when the host has a direct
// half, the tunnel is a relay tunnel, the viewer is allowed to watch, and the host
// has room for another direct tunnel.
func TestHelloAckAdvertisesDirectOnlyWhenTheHostAllows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &fakeDirectHost{}

	if ack := startTunnel(t, ctx, watchPeer("dev-a"), ServeConfig{Host: "h"}).ack; ack.Direct {
		t.Fatal("a host with no direct half advertised the direct road")
	}
	if ack := startTunnel(t, ctx, watchPeer("dev-b"), ServeConfig{Host: "h", Direct: host}).ack; !ack.Direct {
		t.Fatal("a host with a direct half did not advertise the direct road")
	}
	if ack := startTunnel(t, ctx, watchPeer("dev-c"), ServeConfig{Host: "h", Direct: host, Road: RoadDirect}).ack; ack.Direct || ack.Road != RoadDirect {
		t.Fatalf("a direct tunnel's ack = %+v, want road direct and no further offer", ack)
	}
	noWatch := Peer{Device: "dev-d", Label: "d", Permit: pair.Permit{Watch: false}}
	if ack := startTunnel(t, ctx, noWatch, ServeConfig{Host: "h", Direct: host}).ack; ack.Direct || ack.Watch {
		t.Fatalf("a peer that may not watch was offered the direct road: %+v", ack)
	}

	full := NewRegistry()
	var removers []func()
	for i := 0; i < MaxDirect; i++ {
		removers = append(removers, full.Add("dev-full-"+string(rune('a'+i)), newFakeRoadTunnel(RoadDirect)))
	}
	if ack := startTunnel(t, ctx, watchPeer("dev-e"), ServeConfig{Host: "h", Direct: host, Registry: full}).ack; ack.Direct {
		t.Fatalf("a host holding %d direct tunnels still advertised the direct road", MaxDirect)
	}
	removers[0]()
	if ack := startTunnel(t, ctx, watchPeer("dev-f"), ServeConfig{Host: "h", Direct: host, Registry: full}).ack; !ack.Direct {
		t.Fatal("a host with one direct slot free did not advertise the direct road")
	}
	for _, remove := range removers {
		remove()
	}
}

// V38: an older host never advertises and an older viewer ignores the field, so
// nobody offers to a host that cannot answer; a signal an older host does not
// understand is skipped and the link carries on.
func TestAnOlderHostThatAdvertisesNothingIsNeverOffered(t *testing.T) {
	older, err := json.Marshal(HelloAck{V: 1, Watch: true, Drive: true, Road: RoadRelay})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(older, []byte(`"direct"`)) {
		t.Fatalf("an ack with no direct half carries the field anyway: %s", older)
	}
	var ack HelloAck
	if err := json.Unmarshal([]byte(`{"v":1,"watch":true,"drive":true,"road":"relay","limits":{}}`), &ack); err != nil || ack.Direct {
		t.Fatalf("an older host's ack read as %+v, %v; want Direct=false", ack, err)
	}
	var newer HelloAck
	if err := json.Unmarshal([]byte(`{"v":1,"watch":true,"drive":false,"road":"relay","limits":{},"direct":true,"a_field_from_the_future":9}`), &newer); err != nil || !newer.Direct {
		t.Fatalf("a newer host's ack read as %+v, %v; want Direct=true and the unknown field ignored", newer, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig := startTunnel(t, ctx, watchPeer("dev-old"), ServeConfig{Host: "h"})
	// A host with no direct half, handed junk and a signal it has no use for,
	// answers neither and keeps the link up.
	if err := rig.v.SendSignal([]byte("not json at all")); err != nil {
		t.Fatal(err)
	}
	rig.send(t, Signal{T: SignalCandidate, ID: "nobody", C: "x"})
	if err := rig.v.SendPing(9); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rig.v.Done():
		t.Fatal("a signal the host had no use for ended the link")
	case <-time.After(50 * time.Millisecond):
	}
}

// V36 / V38: an offer is refused with a closed word and NOTHING is allocated when
// the host has no direct half (relay-only), when its half says no (busy), when
// the host holds MaxDirect tunnels, and when it arrives on a direct tunnel.
func TestAnOfferIsRefusedWhenTheRoadIsRelayOnlyAndNothingIsAllocated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("no direct half", func(t *testing.T) {
		rig := startTunnel(t, ctx, watchPeer("dev-1"), ServeConfig{Host: "h"})
		rig.send(t, Signal{T: SignalOffer, ID: "attempt-1", SDP: "v=0"})
		got := expectSignal(t, rig.inbox)
		if got.T != SignalRefuse || got.Reason != RefuseRelayOnly || got.ID != "attempt-1" {
			t.Fatalf("the answer was %+v, want refuse relay-only for attempt-1", got)
		}
	})
	t.Run("the direct half says no", func(t *testing.T) {
		host := &fakeDirectHost{refuse: true}
		rig := startTunnel(t, ctx, watchPeer("dev-2"), ServeConfig{Host: "h", Direct: host})
		rig.send(t, Signal{T: SignalOffer, ID: "attempt-2", SDP: "v=0"})
		if got := expectSignal(t, rig.inbox); got.T != SignalRefuse || got.Reason != RefuseBusy {
			t.Fatalf("the answer was %+v, want refuse busy", got)
		}
		if host.offers() != 1 {
			t.Fatalf("the host half was asked %d times, want once", host.offers())
		}
	})
	t.Run("the host already holds its direct tunnels", func(t *testing.T) {
		host := &fakeDirectHost{}
		full := NewRegistry()
		for i := 0; i < MaxDirect; i++ {
			full.Add("full-"+string(rune('a'+i)), newFakeRoadTunnel(RoadDirect))
		}
		// A host that advertised just before it filled up, as a race can have it,
		// still refuses the offer; with a full registry the ack itself says no
		// direct, which the viewer is free to ignore.
		rigFull := startTunnel(t, ctx, watchPeer("dev-3"), ServeConfig{Host: "h", Direct: host, Registry: full})
		rigFull.send(t, Signal{T: SignalOffer, ID: "attempt-3", SDP: "v=0"})
		if got := expectSignal(t, rigFull.inbox); got.T != SignalRefuse || got.Reason != RefuseBusy {
			t.Fatalf("the answer was %+v, want refuse busy", got)
		}
		if host.offers() != 0 {
			t.Fatalf("the host half was asked %d times while full, want never", host.offers())
		}
	})
	t.Run("an offer on a direct tunnel", func(t *testing.T) {
		host := &fakeDirectHost{}
		rig := startTunnel(t, ctx, watchPeer("dev-4"), ServeConfig{Host: "h", Direct: host, Road: RoadDirect})
		rig.send(t, Signal{T: SignalOffer, ID: "attempt-4", SDP: "v=0"})
		if got := expectSignal(t, rig.inbox); got.T != SignalRefuse || got.Reason != RefuseOff {
			t.Fatalf("the answer was %+v, want refuse off", got)
		}
		if host.offers() != 0 {
			t.Fatal("a direct tunnel's offer reached the host half")
		}
	})
}

// V34: an offer is refused, with nothing allocated, when the host has already
// taken watching away from the peer, even before the permit watcher has told the
// tunnel to close.
func TestAnOfferFromAPeerWhoseWatchWasTakenAwayIsRefusedBeforeAnythingIsAllocated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &fakeDirectHost{}
	var watching atomic.Bool
	watching.Store(true)
	permit := func(string) pair.Permit { return pair.Permit{Watch: watching.Load(), Drive: true} }
	rig := startTunnel(t, ctx, watchPeer("dev-edited"), ServeConfig{Host: "h", Direct: host, Permit: permit})
	if !rig.ack.Direct {
		t.Fatal("the host did not advertise the direct road to a peer that may watch")
	}
	watching.Store(false)
	rig.send(t, Signal{T: SignalOffer, ID: "too-late", SDP: "v=0"})
	if got := expectSignal(t, rig.inbox); got.T != SignalRefuse || got.Reason != RefuseOff {
		t.Fatalf("the answer was %+v, want refuse off", got)
	}
	if host.offers() != 0 {
		t.Fatal("the host half was asked to answer a peer that may no longer watch")
	}
}

// V30 (router half): an offer reaches the host half with the AUTHENTICATED peer
// and the payload untouched, later candidates of the same attempt reach its
// session in order, a candidate for another attempt is dropped, the host's own
// signals go back through the same link, `end` closes the session, and a new
// offer from the same viewer supersedes the pending one.
func TestAnOfferIsHandedToTheHostAndLaterSignalsReachItsSessionInOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &fakeDirectHost{}
	rig := startTunnel(t, ctx, watchPeer("dev-real"), ServeConfig{Host: "h", Direct: host})

	offer := rig.send(t, Signal{T: SignalOffer, ID: "first", SDP: "v=0 offer"})
	eventually(t, "the host half to be offered", func() bool { return host.offers() == 1 })
	host.mu.Lock()
	gotDevice, gotPayload, signaller := host.devices[0], host.payloads[0], host.signals[0]
	host.mu.Unlock()
	if gotDevice != "dev-real" || !bytes.Equal(gotPayload, offer) {
		t.Fatalf("offered device %q payload %q; want the authenticated peer and the payload as sent", gotDevice, gotPayload)
	}

	for _, c := range []string{"c1", "c2", "c3"} {
		rig.send(t, Signal{T: SignalCandidate, ID: "first", C: c})
	}
	rig.send(t, Signal{T: SignalCandidate, ID: "someone-else", C: "stray"})
	first := host.session(0)
	eventually(t, "three candidates to arrive", func() bool { return len(first.deliveries()) == 3 })
	for i, want := range []string{"c1", "c2", "c3"} {
		sig, err := DecodeSignal(first.deliveries()[i])
		if err != nil || sig.C != want {
			t.Fatalf("delivery %d = %+v, %v; want candidate %q in order", i, sig, err, want)
		}
	}

	answer, err := EncodeSignal(Signal{T: SignalAnswer, ID: "first", SDP: "v=0 answer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := signaller.Send(answer); err != nil {
		t.Fatal(err)
	}
	if got := expectSignal(t, rig.inbox); got.T != SignalAnswer || got.SDP != "v=0 answer" {
		t.Fatalf("the viewer got %+v from the host's signaller", got)
	}

	rig.send(t, Signal{T: SignalOffer, ID: "second", SDP: "v=0 again"})
	eventually(t, "the second offer to reach the host half", func() bool { return host.offers() == 2 })
	eventually(t, "the first session to be closed by its successor", first.isClosed)
	if len(first.deliveries()) != 3 {
		t.Fatalf("the superseded session saw %d deliveries, want its original three", len(first.deliveries()))
	}

	second := host.session(1)
	rig.send(t, Signal{T: SignalEnd, ID: "second"})
	eventually(t, "`end` to close the session", second.isClosed)
}

// V34: a pending direct attempt dies with its relay tunnel, so a viewer that goes
// away, is removed, or whose tunnel is closed by the host cannot leave a peer
// connection half built; and once the tunnel is closing a late offer is ignored.
func TestARemovedDeviceCannotOpenADirectRoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg := NewRegistry()
	host := &fakeDirectHost{}
	rig := startTunnel(t, ctx, watchPeer("dev-gone"), ServeConfig{Host: "h", Direct: host, Registry: reg})

	rig.send(t, Signal{T: SignalOffer, ID: "mid-setup", SDP: "v=0"})
	eventually(t, "the offer to reach the host half", func() bool { return host.offers() == 1 })
	session := host.session(0)

	reg.Revoke("dev-gone")
	eventually(t, "a revoked device's pending attempt to be closed", session.isClosed)
	select {
	case <-rig.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the revoked tunnel did not end")
	}

	// The viewer's end of a revoked tunnel is gone too, so a late offer has
	// nowhere to go and says so, and nothing more reaches the host half. (The
	// door refuses the same device's NEXT direct tunnel before Noise: that is
	// filesdoor's TestARemovedDeviceLosesItsDirectStreamAndCannotOpenAnother.)
	select {
	case <-rig.v.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the viewer's link outlived a revoked tunnel")
	}
	if err := rig.v.SendSignal([]byte(`{"v":1,"t":"offer","id":"late","sdp":"v=0"}`)); err == nil {
		t.Fatal("a signal was accepted on a link that has been closed")
	}
	if host.offers() != 1 {
		t.Fatalf("an offer on a revoked tunnel reached the host half (%d offers)", host.offers())
	}
}

// V34: a pending direct attempt is closed when its relay tunnel simply ends.
func TestAClosingTunnelClosesItsPendingDirectSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &fakeDirectHost{}
	rig := startTunnel(t, ctx, watchPeer("dev-leaves"), ServeConfig{Host: "h", Direct: host})
	rig.send(t, Signal{T: SignalOffer, ID: "pending", SDP: "v=0"})
	eventually(t, "the offer to reach the host half", func() bool { return host.offers() == 1 })
	session := host.session(0)
	_ = rig.v.Close()
	eventually(t, "the pending session to close with its tunnel", session.isClosed)
	select {
	case <-rig.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

// directRoadRig is one device with a relay tunnel and a direct tunnel to the same
// host and registry, a channel open on each, and the holder's stamps recorded.
type directRoadRig struct {
	reg       *Registry
	relay     *signalRig
	direct    *signalRig
	relayMux  *Mux
	directMux *Mux
	relayCh   *Channel
	directCh  *Channel
	relayAck  HelloAck
	directAck HelloAck
	hellos    chan remote.Hello
}

func startBothRoads(t *testing.T, ctx context.Context, device string, permit func(string) pair.Permit, budget *Budget, clk Clock) *directRoadRig {
	t.Helper()
	rig := &directRoadRig{reg: NewRegistry(), hellos: make(chan remote.Hello, 8)}
	resolver := newEchoStampResolver(func(h remote.Hello) { rig.hellos <- h })
	peer := watchPeer(device)
	relayCfg := ServeConfig{Resolver: resolver, Registry: rig.reg, Budget: budget, Permit: permit, Clock: clk, Host: "workstation", Road: RoadRelay}
	// THE DIRECT TUNNEL IS GIVEN THE SAME BUDGET ON PURPOSE: Serve must ignore it.
	directCfg := ServeConfig{Resolver: resolver, Registry: rig.reg, Budget: budget, Permit: permit, Clock: clk, Host: "workstation", Road: RoadDirect}
	relay := startTunnel(t, ctx, peer, relayCfg)
	direct := startTunnel(t, ctx, peer, directCfg)
	rig.relay, rig.direct = relay, direct
	rig.relayMux, rig.directMux = relay.v, direct.v
	rig.relayAck, rig.directAck = relay.ack, direct.ack
	rig.relayCh = openAndHandshake(t, ctx, relay.v, "chat-both")
	<-rig.hellos
	rig.directCh = openAndHandshake(t, ctx, direct.v, "chat-both")
	<-rig.hellos
	return rig
}

func endedWith(t *testing.T, ch *Channel) string {
	t.Helper()
	var buf [64]byte
	done := make(chan error, 1)
	guard.Go("livedoor.test.direct.read", func() {
		_, err := ch.Read(buf[:])
		done <- err
	})
	select {
	case err := <-done:
		return reasonOf(err)
	case <-time.After(5 * time.Second):
		t.Fatal("the channel did not end")
		return ""
	}
}

// V34: Registry.Revoke closes the relay tunnel and the direct tunnel of a device
// together, each with the reason `removed`.
func TestARevokeClosesRelayAndDirectTunnelsTogetherWithRemoved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig := startBothRoads(t, ctx, "dev-revoked", nil, nil, nil)
	if !rig.reg.HasRoad("dev-revoked", RoadRelay) || !rig.reg.HasRoad("dev-revoked", RoadDirect) {
		t.Fatal("the registry does not hold one tunnel on each road")
	}
	rig.reg.Revoke("dev-revoked")
	if got := endedWith(t, rig.relayCh); got != ReasonRemoved {
		t.Fatalf("the relay channel ended with %q, want %q", got, ReasonRemoved)
	}
	if got := endedWith(t, rig.directCh); got != ReasonRemoved {
		t.Fatalf("the direct channel ended with %q, want %q", got, ReasonRemoved)
	}
	if rig.reg.HasRoad("dev-revoked", RoadRelay) || rig.reg.HasRoad("dev-revoked", RoadDirect) {
		t.Fatal("a revoked device is still registered on a road")
	}
}

// V34: a permit edit that takes away typing ends the channels of BOTH roads with
// `permit-changed`.
func TestAPermitEditEndsDirectChannelsWithPermitChanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var drive atomic.Bool
	drive.Store(true)
	permit := func(string) pair.Permit { return pair.Permit{Watch: true, Drive: drive.Load()} }
	rig := startBothRoads(t, ctx, "dev-edit", permit, nil, nil)
	drive.Store(false)
	rig.reg.PermitsChanged("dev-edit")
	if got := endedWith(t, rig.directCh); got != ReasonPermitChanged {
		t.Fatalf("the direct channel ended with %q, want %q", got, ReasonPermitChanged)
	}
	if got := endedWith(t, rig.relayCh); got != ReasonPermitChanged {
		t.Fatalf("the relay channel ended with %q, want %q", got, ReasonPermitChanged)
	}
}

// V35: a direct tunnel is served with no budget and the direct pacing, even when
// its caller hands it a budget: nothing it carries is counted, and it types.
func TestADirectTunnelIsServedWithNoBudgetAndNoPacing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC))
	budget := OpenBudget(shortTempDir(t), fc, 4096)
	t.Cleanup(func() { budget.Close() })

	resolver := newEchoStampResolver(nil)
	direct := startTunnel(t, ctx, watchPeer("dev-direct"), ServeConfig{
		Resolver: resolver, Budget: budget, Clock: fc, Host: "workstation", Road: RoadDirect,
	})
	if direct.ack.Limits.Rate != DirectRate || direct.ack.Limits.RateBurst != DirectRate {
		t.Fatalf("the direct tunnel's pacing is %d/%d, want %d", direct.ack.Limits.Rate, direct.ack.Limits.RateBurst, DirectRate)
	}
	if direct.ack.Limits.PingEvery != DirectPingEvery || direct.ack.Limits.DeadAfter != DirectDeadAfter {
		t.Fatalf("the direct tunnel's liveness is %v/%v, want %v/%v", direct.ack.Limits.PingEvery, direct.ack.Limits.DeadAfter, DirectPingEvery, DirectDeadAfter)
	}
	if !direct.ack.Drive {
		t.Fatal("a direct tunnel was not offered typing")
	}
	ch := openAndHandshake(t, ctx, direct.v, "chat-direct")
	before := budget.Bytes()
	big := makeFrameLineOfExactLength(t, "call", remote.MethodPing, 8192)
	if _, err := ch.Write(big); err != nil {
		t.Fatal(err)
	}
	direct.roundTrip(t)
	if budget.Exhausted() || budget.Bytes() != before {
		t.Fatalf("a direct tunnel was counted by the daily budget: %d bytes (was %d), exhausted=%v", budget.Bytes(), before, budget.Exhausted())
	}

	// The control: the same bytes on a relay tunnel DO spend it.
	relay := startTunnel(t, ctx, watchPeer("dev-relay"), ServeConfig{Resolver: resolver, Budget: budget, Clock: fc, Host: "workstation", Road: RoadRelay})
	relayCh := openAndHandshake(t, ctx, relay.v, "chat-relay")
	if _, err := relayCh.Write(big); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the relay's bytes to be counted", budget.Exhausted)
	if relay.ack.Limits.Rate != Rate {
		t.Fatalf("the relay tunnel's pacing is %d, want %d", relay.ack.Limits.Rate, Rate)
	}
}

// V35: with the daily budget spent a relay view is watch-only while the same
// device's direct view still types.
func TestPastTheDailyBudgetADirectViewerStillTypes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC))
	budget := OpenBudget(shortTempDir(t), fc, 1)
	t.Cleanup(func() { budget.Close() })
	budget.Charge(1 << 20)
	if !budget.Exhausted() {
		t.Fatal("the budget is not spent")
	}

	rig := startBothRoads(t, ctx, "dev-spent", nil, budget, fc)
	if rig.relayAck.Drive {
		t.Fatal("a relay view could type with the budget spent")
	}
	if !rig.directAck.Drive {
		t.Fatal("a direct view could not type with the relay's budget spent")
	}
	// The same chat: the holder was told Drive=false for the relay stamp and
	// Drive=true for the direct stamp. startBothRoads consumed those hellos, so
	// open a second channel on each road and read them.
	_ = openAndHandshake(t, ctx, rig.directMux, "chat-second")
	if h := <-rig.hellos; h.Remote == nil || !h.Remote.Drive || h.Remote.Road != RoadDirect {
		t.Fatalf("the holder's stamp on the direct road = %+v, want Drive=true road=direct", h.Remote)
	}
	_ = openAndHandshake(t, ctx, rig.relayMux, "chat-second")
	if h := <-rig.hellos; h.Remote == nil || h.Remote.Drive || h.Remote.Road != RoadRelay {
		t.Fatalf("the holder's stamp on the relay = %+v, want Drive=false road=relay", h.Remote)
	}
}

// V35 / D12: the per-device cap applies within a road, so a reconnecting relay
// tunnel never evicts a healthy direct one and the reverse; HasRoad answers from
// the registry.
func TestTheRegistryCapsTunnelsPerRoadAndNeverEvictsADirectOneForARelayOne(t *testing.T) {
	reg := NewRegistry()
	relayOld, relayNew := newFakeRoadTunnel(RoadRelay), newFakeRoadTunnel(RoadRelay)
	directOld, directNew := newFakeRoadTunnel(RoadDirect), newFakeRoadTunnel(RoadDirect)
	const device = "dev-caps"

	reg.Add(device, relayOld)
	reg.Add(device, directOld)
	reg.Add(device, newFakeRoadTunnel(RoadRelay))
	if _, evicted := directOld.closedWith(); evicted {
		t.Fatal("a second relay tunnel evicted the direct one")
	}
	if reg.Count(device) != 3 || !reg.HasRoad(device, RoadDirect) {
		t.Fatalf("registry holds %d tunnels, direct=%v; want 3 and a direct one", reg.Count(device), reg.HasRoad(device, RoadDirect))
	}

	// A third relay tunnel (the cap is 2 per road) evicts the OLDEST RELAY tunnel.
	reg.Add(device, relayNew)
	if reason, evicted := relayOld.closedWith(); !evicted || reason != ReasonHostClosing {
		t.Fatalf("the oldest relay tunnel was not evicted with %q (got %q, %v)", ReasonHostClosing, reason, evicted)
	}
	if _, evicted := directOld.closedWith(); evicted {
		t.Fatal("a third relay tunnel evicted the direct one")
	}

	// And a third direct tunnel evicts only the oldest direct one.
	reg.Add(device, newFakeRoadTunnel(RoadDirect))
	reg.Add(device, directNew)
	if reason, evicted := directOld.closedWith(); !evicted || reason != ReasonHostClosing {
		t.Fatalf("the oldest direct tunnel was not evicted (got %q, %v)", reason, evicted)
	}
	if _, evicted := relayNew.closedWith(); evicted {
		t.Fatal("a direct tunnel evicted a relay one")
	}
	if reg.HasRoad("someone-else", RoadDirect) || reg.HasRoad(device, "carrier-pigeon") {
		t.Fatal("HasRoad answered true for a device or road with no tunnel")
	}
}

// V35: the daily budget running out resets the channels of relay tunnels and
// leaves direct tunnels alone.
func TestTheDailyBudgetResetSkipsDirectTunnels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig := startBothRoads(t, ctx, "dev-reset", nil, nil, nil)
	rig.reg.onBudgetExhausted()
	if got := endedWith(t, rig.relayCh); got != ReasonBudget {
		t.Fatalf("the relay channel ended with %q, want %q", got, ReasonBudget)
	}
	// The pong proves the host has handled everything it queued before it, so a
	// reset of the direct channel would already have reached this side.
	rig.direct.roundTrip(t)
	if channelEnded(rig.directCh) {
		t.Fatal("the budget reset a direct channel")
	}
	select {
	case <-rig.direct.v.Done():
		t.Fatal("the budget reset closed a direct tunnel")
	default:
	}
	if !rig.reg.HasRoad("dev-reset", RoadDirect) {
		t.Fatal("the direct tunnel left the registry")
	}
}

// D7 / V38: a relay tunnel that is a silent standby outlives the host's silence
// backstop while a direct tunnel of the same device is registered, and is closed
// by the next firing once that direct tunnel is gone.
func TestAStandbyRelayTunnelOutlivesTheSilenceBackstopWhileItsDirectOneLivesAndNotAfter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	reg := NewRegistry()
	removeDirect := reg.Add("dev-standby", newFakeRoadTunnel(RoadDirect))
	rig := startTunnel(t, ctx, watchPeer("dev-standby"), ServeConfig{Registry: reg, Clock: fc, Host: "workstation", Road: RoadRelay})
	fc.WaitForTimers(1)

	fc.Advance(IdleDetach + hostBackstopExtra + time.Second)
	// The backstop fired, saw a direct tunnel of the same device, and armed again.
	fc.WaitForTimers(1)
	select {
	case <-rig.v.Done():
		t.Fatal("the standby relay tunnel was closed while its direct tunnel was open")
	default:
	}

	// An unrelated device's direct tunnel does not excuse it.
	other := reg.Add("another-device", newFakeRoadTunnel(RoadDirect))
	defer other()
	removeDirect()
	fc.Advance(hostBackstopExtra + time.Second)
	select {
	case <-rig.v.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the standby relay tunnel outlived the direct tunnel that excused it")
	}
	if err := <-rig.done; err != nil {
		t.Fatalf("Serve returned %v, want nil on a clean backstop close", err)
	}
}

// V40: a watch-only peer on a direct tunnel is refused calls that type, with the
// same words as on the relay, and an allowed read still reaches the holder.
func TestAWatchOnlyPeerOnADirectTunnelIsRefusedCallsThatTypeAndToldWhy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const hostName = "mini-server"
	holderCalls := make(chan remote.Frame, 4)
	resolver := newEchoStampResolverWithCalls(holderCalls)
	rig := startTunnel(t, ctx, Peer{Device: "dev-watch", Label: "ipad", Permit: pair.Permit{Watch: true, Drive: false}},
		ServeConfig{Resolver: resolver, Host: hostName, Road: RoadDirect})
	ch := openAndHandshake(t, ctx, rig.v, "chat-watch")
	scan := newLineScanner(ch, MaxLine)

	submit, _ := json.Marshal(remote.Frame{Kind: "call", ID: 41, Method: remote.MethodSubmit})
	if _, err := ch.Write(append(submit, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := scan.ReadLine(MaxLine)
	if err != nil {
		t.Fatal(err)
	}
	var res remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &res); err != nil {
		t.Fatal(err)
	}
	if want := remote.GuestRefusal(hostName, remote.MethodSubmit, false); res.Kind != "result" || res.ID != 41 || res.Error != want {
		t.Fatalf("the Submit answer was %+v, want a result for 41 saying %q", res, want)
	}
	ping, _ := json.Marshal(remote.Frame{Kind: "call", ID: 42, Method: remote.MethodPing})
	if _, err := ch.Write(append(ping, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-holderCalls:
		if call.Method != remote.MethodPing {
			t.Fatalf("the holder saw %q first; only Ping may reach it", call.Method)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an allowed read did not reach the holder")
	}
}

// newEchoStampResolverWithCalls is newEchoStampResolver with the holder also
// recording every call frame the router lets through.
func newEchoStampResolverWithCalls(calls chan<- remote.Frame) Resolver {
	return fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.direct.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f) != nil {
				return
			}
			var h remote.Hello
			if json.Unmarshal(f.Payload, &h) != nil {
				return
			}
			wPayload, _ := json.Marshal(remote.Welcome{Version: remote.Version, Workspace: h.Workspace, SessionFile: h.Session, Remote: h.Remote})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				return
			}
			for {
				callLine, err := scan.ReadLine(MaxLine)
				if err != nil {
					return
				}
				var call remote.Frame
				if json.Unmarshal(bytes.TrimRight(callLine, "\r\n"), &call) == nil {
					calls <- call
				}
			}
		})
		return routerSide, Route{Kind: "daemon", PID: 7002, Session: "/work/direct/" + chat + "/transcript.jsonl", Workspace: "/work/direct"}, nil
	})
}
