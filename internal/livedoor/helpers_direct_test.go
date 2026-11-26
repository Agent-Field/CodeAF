package livedoor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// This file is the rig the direct-road Link tests stand on. It is built from the
// contract and the package's PUBLIC surface only: a Link is given a fake
// DirectConfig.Dial that hands back one end of a net.Pipe with a real Serve on the
// other, so the road the Link believes it is on is a real tunnel whose frames the
// tests can count, stall, kill and read back, and the FakeClock is the only time
// the Link's own logic ever sees.

// within spins, with no sleeping, for up to ten seconds of real time until cond
// holds. WHETHER A THING HAPPENS IS THE CONTRACT; THAT IT HAS HAPPENED BY THE
// NEXT LINE IS NOT, because the Link hands work to goroutines and the race
// detector makes a scheduler tick long.
func within(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// quiesce gives every goroutine that is ready to run a long turn. It is used
// before a NEGATIVE assertion ("nothing happened"), where there is no condition
// to wait for, so that a thing that was about to happen has had its chance.
func quiesce() {
	for i := 0; i < 300; i++ {
		runtime.Gosched()
	}
}

// tapFrame is one frame as it crossed a carrier.
type tapFrame struct {
	op      uint8
	channel uint32
}

// frameTap decodes frames out of the byte stream written to one side of a
// carrier, so a test can say "exactly one CLOSE and no PING crossed here" in the
// wire's own terms instead of counting write calls.
type frameTap struct {
	mu     sync.Mutex
	buf    []byte
	frames []tapFrame
}

func (f *frameTap) feed(p []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buf = append(f.buf, p...)
	for len(f.buf) >= frameHeaderBytes {
		n := int(binary.BigEndian.Uint32(f.buf[5:9]))
		if len(f.buf) < frameHeaderBytes+n {
			return
		}
		f.frames = append(f.frames, tapFrame{op: f.buf[0], channel: binary.BigEndian.Uint32(f.buf[1:5])})
		f.buf = f.buf[frameHeaderBytes+n:]
	}
}

// count is how many frames of op have crossed.
func (f *frameTap) count(op uint8) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, fr := range f.frames {
		if fr.op == op {
			n++
		}
	}
	return n
}

// mark is a position in the stream, for since.
func (f *frameTap) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

// since is every frame after mark, in order.
func (f *frameTap) since(mark int) []tapFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	if mark >= len(f.frames) {
		return nil
	}
	return append([]tapFrame(nil), f.frames[mark:]...)
}

// opsSince is the number of frames of op after mark.
func (f *frameTap) opsSince(mark int, op uint8) int {
	n := 0
	for _, fr := range f.since(mark) {
		if fr.op == op {
			n++
		}
	}
	return n
}

// stallConn is the viewer's end of a tunnel as the Link sees it. It records every
// frame the Link tries to send, and it can be STALLED: from then on nothing the
// Link writes reaches the host and nothing the host writes reaches the Link,
// while nothing is closed. That is a path that has died without telling anyone,
// which is the failure the fallback exists for.
type stallConn struct {
	inner   net.Conn
	sent    frameTap
	stalled atomic.Bool
	closed  atomic.Bool
}

func (c *stallConn) Read(p []byte) (int, error) {
	for {
		n, err := c.inner.Read(p)
		if c.stalled.Load() {
			if err != nil {
				return 0, err
			}
			// Dropped on the floor, but still drained, so the host's writes
			// complete and the host never blocks on a black hole.
			continue
		}
		return n, err
	}
}

func (c *stallConn) Write(p []byte) (int, error) {
	c.sent.feed(p)
	if c.stalled.Load() {
		return len(p), nil
	}
	return c.inner.Write(p)
}

func (c *stallConn) Close() error {
	c.closed.Store(true)
	return c.inner.Close()
}

// tapWriteConn records the frames the host wrote, after each write has been
// taken in by the viewer's side, so a count of host pongs means pongs the Link
// has had delivered.
type tapWriteConn struct {
	net.Conn
	tap *frameTap
}

func (c *tapWriteConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.tap.feed(p[:n])
	}
	return n, err
}

// tunnelEnd is one tunnel the rig has served: the viewer's end with its taps and
// stall switch, the host's end and the host's own taps.
type tunnelEnd struct {
	road    string
	index   int
	viewer  *stallConn
	host    net.Conn
	hostTap frameTap
	served  chan struct{}
}

// stall black-holes this tunnel: no close, no error, just silence both ways.
func (e *tunnelEnd) stall() { e.viewer.stalled.Store(true) }

// kill is the host end of the tunnel going away abruptly.
func (e *tunnelEnd) kill() { _ = e.host.Close() }

// openRec is one channel the host side resolved: which road's tunnel it came in
// on, which tunnel of that road, and for which chat.
type openRec struct {
	road   string
	tunnel int
	chat   string
}

// openGate holds the host's resolution of one chat open until released, so a test
// can have an open in flight while something else happens.
type openGate struct {
	entered chan struct{}
	release chan struct{}
}

// directDialFn is what a fake direct dial does on its nth call.
type directDialFn func(ctx context.Context, n int, s Signaller) (io.ReadWriteCloser, error)

// rigOpts shapes a directRig.
type rigOpts struct {
	active      bool
	noDirect    bool
	noAdvertise bool
	idleDetach  time.Duration
	sharedReg   bool
	holder      func(r *directRig, road string, tunnel int) Resolver
}

// directRig is a viewer Link, a host that serves whatever tunnels it dials, and
// the instruments around them.
type directRig struct {
	t    *testing.T
	ctx  context.Context
	fc   *FakeClock
	link *Link
	peer Peer
	opts rigOpts

	host      *fakeDirectHost
	relayReg  *Registry
	directReg *Registry

	relayDials  atomic.Int32
	directDials atomic.Int32
	directDone  atomic.Int32

	mu          sync.Mutex
	tunnels     []*tunnelEnd
	relayTimes  []time.Time
	directTimes []time.Time
	opens       []openRec
	states      []State
	ended       []string
	signals     [][]byte
	directFn    directDialFn
	relayFn     func(ctx context.Context, n int) error
	gates       map[string]*openGate
	closers     []func()
}

// newDirectRig builds the rig. It dials nothing: the Link dials when a channel is
// first opened, as it always has.
func newDirectRig(t *testing.T, opts rigOpts) *directRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &directRig{
		t:        t,
		ctx:      ctx,
		fc:       NewFakeClock(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)),
		opts:     opts,
		host:     &fakeDirectHost{},
		relayReg: NewRegistry(),
		gates:    make(map[string]*openGate),
		peer:     Peer{Device: "dev-viewer", Label: "laptop", Permit: pair.Permit{Watch: true, Drive: true}},
	}
	r.directReg = NewRegistry()
	if opts.sharedReg {
		r.directReg = r.relayReg
	}
	// Registered first so that it runs LAST: the link is closed before the pipes
	// under it, and the host's context ends after both.
	t.Cleanup(func() {
		r.mu.Lock()
		closers := append([]func(){}, r.closers...)
		r.mu.Unlock()
		for _, c := range closers {
			c()
		}
		cancel()
	})

	lim := DefaultLimits()
	if opts.idleDetach > 0 {
		lim.IdleDetach = opts.idleDetach
	}
	cfg := LinkConfig{
		Clock:    r.fc,
		HostName: "Desktop",
		Limits:   lim,
		Dial:     r.dialRelay,
		OnState:  r.onState,
		OnSignal: r.onSignal,
	}
	if opts.active {
		cfg.Active = func() bool { return true }
	}
	if !opts.noDirect {
		cfg.Direct = &DirectConfig{Dial: r.dialDirect}
	}
	r.link = NewLink(cfg)
	r.link.OnChannelEnd(r.onEnd)
	t.Cleanup(r.link.Close)
	return r
}

func (r *directRig) onState(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *directRig) onSignal(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, append([]byte(nil), p...))
}

func (r *directRig) onEnd(chat, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended = append(r.ended, chat+":"+reason)
}

func (r *directRig) stateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states)
}

func (r *directRig) statesFrom(n int) []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n >= len(r.states) {
		return nil
	}
	return append([]State(nil), r.states[n:]...)
}

func (r *directRig) endedList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ended...)
}

func (r *directRig) signalsGot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.signals...)
}

// setDirect decides what the fake direct dial does from now on.
func (r *directRig) setDirect(fn directDialFn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.directFn = fn
}

// failDirect makes every direct dial fail at once, the way a path that cannot
// be found does.
func (r *directRig) failDirect() {
	r.setDirect(func(context.Context, int, Signaller) (io.ReadWriteCloser, error) {
		return nil, errors.New("no direct path")
	})
}

func (r *directRig) dialRelay(ctx context.Context) (io.ReadWriteCloser, error) {
	n := int(r.relayDials.Add(1))
	r.mu.Lock()
	r.relayTimes = append(r.relayTimes, r.fc.Now())
	fn := r.relayFn
	r.mu.Unlock()
	if fn != nil {
		if err := fn(ctx, n); err != nil {
			return nil, err
		}
	}
	return r.newTunnel(RoadRelay), nil
}

func (r *directRig) dialDirect(ctx context.Context, s Signaller) (io.ReadWriteCloser, error) {
	n := int(r.directDials.Add(1))
	r.mu.Lock()
	r.directTimes = append(r.directTimes, r.fc.Now())
	fn := r.directFn
	r.mu.Unlock()
	defer r.directDone.Add(1)
	if fn == nil {
		return r.newTunnel(RoadDirect), nil
	}
	return fn(ctx, n, s)
}

// newTunnel serves one fresh tunnel of road with a real Serve on the host end and
// returns the viewer's end.
func (r *directRig) newTunnel(road string) *stallConn {
	vConn, hConn := net.Pipe()
	e := &tunnelEnd{road: road, host: hConn, served: make(chan struct{}), viewer: &stallConn{inner: vConn}}
	r.mu.Lock()
	e.index = 1
	for _, o := range r.tunnels {
		if o.road == road {
			e.index++
		}
	}
	r.tunnels = append(r.tunnels, e)
	r.closers = append(r.closers, func() { _ = vConn.Close(); _ = hConn.Close() })
	r.mu.Unlock()

	cfg := ServeConfig{Resolver: r.resolverFor(road, e.index), Host: "Desktop", Road: road}
	switch road {
	case RoadRelay:
		cfg.Registry = r.relayReg
		if !r.opts.noAdvertise {
			cfg.Direct = r.host
		}
	case RoadDirect:
		cfg.Registry = r.directReg
	}
	guard.Go("livedoor.test.direct.serve", func() {
		defer close(e.served)
		_ = Serve(r.ctx, &tapWriteConn{Conn: hConn, tap: &e.hostTap}, r.peer, cfg)
	})
	return e.viewer
}

func (r *directRig) resolverFor(road string, tunnel int) Resolver {
	if r.opts.holder != nil {
		return r.opts.holder(r, road, tunnel)
	}
	return r.stubResolver(road, tunnel)
}

func (r *directRig) recordOpen(road string, tunnel int, chat string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opens = append(r.opens, openRec{road: road, tunnel: tunnel, chat: chat})
}

// opensOn is the chats the host resolved on road's tunnels, in order.
func (r *directRig) opensOn(road string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, o := range r.opens {
		if o.road == road {
			out = append(out, o.chat)
		}
	}
	return out
}

func (r *directRig) lastOpen() openRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.opens) == 0 {
		return openRec{}
	}
	return r.opens[len(r.opens)-1]
}

// holdNextOpen makes the host's next resolution of chat on road wait until the
// returned gate is released.
func (r *directRig) holdNextOpen(road, chat string) *openGate {
	g := &openGate{entered: make(chan struct{}), release: make(chan struct{})}
	r.mu.Lock()
	r.gates[road+"/"+chat] = g
	r.mu.Unlock()
	return g
}

func (r *directRig) takeGate(road, chat string) *openGate {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := road + "/" + chat
	g := r.gates[key]
	delete(r.gates, key)
	return g
}

// stubResolver is a holder that welcomes whoever says hello and says, in the
// welcome's Model, WHICH ROAD THE CHANNEL CAME IN ON, so the viewer can read the
// road a channel is really carried on from the channel itself.
func (r *directRig) stubResolver(road string, tunnel int) Resolver {
	return fakeResolverFunc(func(ctx context.Context, chat string) (net.Conn, Route, error) {
		r.recordOpen(road, tunnel, chat)
		if g := r.takeGate(road, chat); g != nil {
			close(g.entered)
			select {
			case <-g.release:
			case <-ctx.Done():
				return nil, Route{}, ctx.Err()
			}
		}
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
			payload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Model:       road,
				Remote:      h.Remote,
			})
			frame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: payload})
			if _, err := holderSide.Write(append(frame, '\n')); err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, holderSide)
		})
		return routerSide, Route{Kind: "daemon", PID: 7001, Session: "/work/p/" + chat + "/transcript.jsonl", Workspace: "/work/p"}, nil
	})
}

// tunnelsOn is every tunnel the rig has served on road, oldest first.
func (r *directRig) tunnelsOn(road string) []*tunnelEnd {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*tunnelEnd
	for _, e := range r.tunnels {
		if e.road == road {
			out = append(out, e)
		}
	}
	return out
}

// tunnel is the nth (1-based) tunnel served on road.
func (r *directRig) tunnel(road string, n int) *tunnelEnd {
	r.t.Helper()
	all := r.tunnelsOn(road)
	if n < 1 || n > len(all) {
		r.t.Fatalf("the rig has served %d %s tunnels, wanted number %d", len(all), road, n)
	}
	return all[n-1]
}

// latest is the newest tunnel served on road.
func (r *directRig) latest(road string) *tunnelEnd {
	r.t.Helper()
	all := r.tunnelsOn(road)
	if len(all) == 0 {
		r.t.Fatalf("the rig has served no %s tunnel", road)
	}
	return all[len(all)-1]
}

// openChatErr opens chat through the Link's opener with a real-time watchdog, so
// an open that hangs fails the test instead of the package's whole timeout.
func (r *directRig) openChatErr(chat string) (io.ReadWriteCloser, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	r.mu.Lock()
	r.closers = append(r.closers, cancel)
	r.mu.Unlock()
	watchdog := time.AfterFunc(10*time.Second, cancel)
	defer watchdog.Stop()
	return r.link.OpenerContext(chat)(ctx)
}

func (r *directRig) openChat(chat string) io.ReadWriteCloser {
	r.t.Helper()
	ch, err := r.openChatErr(chat)
	if err != nil {
		r.t.Fatalf("opening %s failed: %v", chat, err)
	}
	return ch
}

// trySpeak says hello on ch and reads the holder's welcome, returning the road the
// holder says the channel arrived on.
func (r *directRig) trySpeak(ch io.ReadWriteCloser) (string, error) {
	payload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	line, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: payload})
	if _, err := ch.Write(append(line, '\n')); err != nil {
		return "", err
	}
	type result struct {
		road string
		err  error
	}
	done := make(chan result, 1)
	guard.Go("livedoor.test.direct.welcome", func() {
		scan := newLineScanner(ch, MaxLine)
		raw, err := scan.ReadLine(MaxLine)
		if err != nil {
			done <- result{err: err}
			return
		}
		var f remote.Frame
		if err := json.Unmarshal(bytes.TrimRight(raw, "\r\n"), &f); err != nil || f.Kind != "welcome" {
			done <- result{err: fmt.Errorf("the first line was %q, not a welcome", raw)}
			return
		}
		var w remote.Welcome
		_ = json.Unmarshal(f.Payload, &w)
		done <- result{road: w.Model}
	})
	select {
	case res := <-done:
		return res.road, res.err
	case <-time.After(10 * time.Second):
		r.t.Fatal("no welcome arrived on the channel")
		return "", nil
	}
}

// speak is trySpeak that fails the test on an error.
func (r *directRig) speak(ch io.ReadWriteCloser) string {
	r.t.Helper()
	road, err := r.trySpeak(ch)
	if err != nil {
		r.t.Fatalf("saying hello on the channel failed: %v", err)
	}
	return road
}

// endOf reads ch until it ends and returns the error that ended it.
func (r *directRig) endOf(ch io.ReadWriteCloser) error {
	r.t.Helper()
	done := make(chan error, 1)
	guard.Go("livedoor.test.direct.end", func() {
		var buf [64]byte
		for {
			if _, err := ch.Read(buf[:]); err != nil {
				done <- err
				return
			}
		}
	})
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		r.t.Fatal("the channel did not end")
		return nil
	}
}

// isPlanned is whether err is the viewer-local planned redial, the answer a
// remote.Roam reads to skip its first pause.
func isPlanned(err error) bool {
	var ce *ChannelError
	return errors.As(err, &ce) && ce.PlannedRedial()
}

func (r *directRig) waitRoad(road string) {
	r.t.Helper()
	within(r.t, "the road to read "+road, func() bool { return r.link.Road() == road && r.link.State().Road == road })
}

func (r *directRig) waitState(kind StateKind, road string) {
	r.t.Helper()
	within(r.t, fmt.Sprintf("the state to read %v on %s", kind, road), func() bool {
		s := r.link.State()
		return s.Kind == kind && s.Road == road
	})
}

// ── the fake clock, read ────────────────────────────────────────────────────

// timersWithin is how far ahead each pending fake-clock timer is, nearest first,
// keeping only those due in more than zero and at most max.
func timersWithin(fc *FakeClock, max time.Duration) []time.Duration {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	var out []time.Duration
	for _, tm := range fc.pending {
		if d := tm.at.Sub(fc.now); d > 0 && d <= max {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// hasTimerIn is whether some pending timer is due in exactly d.
func hasTimerIn(fc *FakeClock, d time.Duration) bool {
	for _, got := range timersWithin(fc, 1000*time.Hour) {
		if got == d {
			return true
		}
	}
	return false
}

// waitAttemptArmed waits for a timer to be pending exactly d ahead. That is the
// attempt timer being armed: a test must not move the clock before it exists, or
// the timer would be armed from the later reading.
func (r *directRig) waitAttemptArmed(d time.Duration) {
	r.t.Helper()
	within(r.t, fmt.Sprintf("a timer to be armed %v ahead (have %v)", d, timersWithin(r.fc, 1000*time.Hour)), func() bool {
		return hasTimerIn(r.fc, d)
	})
}

// runToNextAttempt moves the fake clock timer by timer, to each next deadline
// within eleven minutes, until a direct attempt begins, and says how far it
// moved. Moving to a deadline exactly (never past it) keeps an attempt that
// starts there from having its own connect timer fired by the same Advance.
func (r *directRig) runToNextAttempt() time.Duration {
	r.t.Helper()
	before := r.directDials.Load()
	deadline := time.Now().Add(10 * time.Second)
	var moved time.Duration
	for r.directDials.Load() == before {
		if time.Now().After(deadline) {
			r.t.Fatalf("no direct attempt began (the clock moved %v; timers: %v)", moved, timersWithin(r.fc, 1000*time.Hour))
		}
		cands := timersWithin(r.fc, 11*time.Minute)
		if len(cands) == 0 {
			runtime.Gosched()
			continue
		}
		r.fc.Advance(cands[0])
		moved += cands[0]
		for i := 0; i < 2000 && r.directDials.Load() == before; i++ {
			runtime.Gosched()
		}
	}
	return moved
}

// swapToDirect runs the clock to the next attempt and waits for the road to read
// direct.
func (r *directRig) swapToDirect() {
	r.t.Helper()
	r.runToNextAttempt()
	r.waitRoad(RoadDirect)
}

// swapToRelay has the host end the newest direct tunnel abruptly and waits for the
// road to read relay.
func (r *directRig) swapToRelay() {
	r.t.Helper()
	r.latest(RoadDirect).kill()
	r.waitRoad(RoadRelay)
}

// settleDirect waits until the host has answered every ping the nested link sent
// on e, which is how a test advances a healthy direct link a little at a time
// without letting the virtual clock outrun the real round trips.
func (r *directRig) settleDirect(e *tunnelEnd) {
	r.t.Helper()
	quiesce()
	within(r.t, "the host's pongs to catch up with the direct link's pings", func() bool {
		return e.hostTap.count(OpPong) >= e.viewer.sent.count(OpPing)
	})
	// The pong has been WRITTEN once the host's tap has seen it, but the viewer's
	// reader goroutine may not yet have decoded it and moved the link's last-heard
	// instant. With a clock that stands still during a round trip that lag is the
	// whole difference between "heard a moment ago" and "one pong missed", so the
	// reader is given its turn before the clock moves again.
	for i := 0; i < 8; i++ {
		quiesce()
	}
}

// advanceHealthy moves the clock d forward in quarter-second steps over a direct
// link that is alive, letting each ping be answered before the next step.
func (r *directRig) advanceHealthy(e *tunnelEnd, d time.Duration) {
	r.t.Helper()
	const step = 250 * time.Millisecond
	for d > 0 {
		s := step
		if d < s {
			s = d
		}
		d -= s
		r.fc.Advance(s)
		r.settleDirect(e)
	}
}

// alignOnPong advances the clock in tenth-of-a-second steps until one more pong
// has reached the direct link, and stops there, so the link's last-heard instant
// is the clock's reading at the moment of the call.
func (r *directRig) alignOnPong(e *tunnelEnd) {
	r.t.Helper()
	before := e.hostTap.count(OpPong)
	for i := 0; e.hostTap.count(OpPong) == before; i++ {
		if i > 60 {
			r.t.Fatal("the direct link was never answered")
		}
		r.fc.Advance(100 * time.Millisecond)
		quiesce()
	}
	quiesce()
}

// ladderStep waits for the next attempt to be armed exactly want ahead, shows that
// it does not fire a millisecond early, and lets it fire.
func (r *directRig) ladderStep(want time.Duration) {
	r.t.Helper()
	before := r.directDials.Load()
	r.waitAttemptArmed(want)
	r.fc.Advance(want - time.Millisecond)
	quiesce()
	if got := r.directDials.Load(); got != before {
		r.t.Fatalf("an attempt began %v early (a timer due in %v)", time.Millisecond, want)
	}
	r.fc.Advance(time.Millisecond)
	within(r.t, fmt.Sprintf("the attempt due after %v to begin", want), func() bool { return r.directDials.Load() > before })
}

// advanceFine moves the clock d forward in tenth-of-a-second steps, letting every
// goroutine that is ready run between the steps. Periodic timers the Link re-arms
// from a goroutine are re-armed from the clock's reading when they fire, so one
// big Advance would skip their ticks; small steps keep their cadence honest. It
// does not wait on any pong, so it is for paths that are dead or about to be.
func (r *directRig) advanceFine(d time.Duration) {
	r.t.Helper()
	for d > 0 {
		s := 100 * time.Millisecond
		if d < s {
			s = d
		}
		d -= s
		r.fc.Advance(s)
		quiesce()
	}
}

// askAgainAfterDropErr opens chat on a Link whose relay tunnel has just gone, the
// way a roaming client does: an open that is refused because the dying tunnel
// had not been let go of yet is simply asked again, and one that is refused with a
// SPOKEN reason is the answer. The Link may dial at once or wait for its redial
// timer, and both are phase 1's to decide; this moves the clock to the redial
// timer only while an open is still waiting for it.
func (r *directRig) askAgainAfterDropErr(chat string) (io.ReadWriteCloser, error) {
	r.t.Helper()
	var last error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ch, err := r.reopenOnce(chat)
		if err == nil {
			return ch, nil
		}
		if _, spoken := ReasonOf(err); spoken {
			return nil, err
		}
		last = err
		// An open that is refused while the Link waits out its own redial timer is
		// phase 1's way of saying "not yet"; the roaming client asks again after its
		// pause, and here the pause is the fake clock moving to that timer, once the
		// Link has armed it.
		quiesce()
		r.moveToRedialTimer()
	}
	return nil, last
}

// moveToRedialTimer moves the fake clock to the nearest timer within twenty
// seconds, but ONLY while the Link is not Live: a Live Link has already dialled,
// and the timers it holds then belong to the direct ladder, which a test must be
// the one to move.
func (r *directRig) moveToRedialTimer() {
	if r.link.State().Kind == Live {
		return
	}
	if cands := timersWithin(r.fc, 20*time.Second); len(cands) > 0 {
		r.fc.Advance(cands[0])
	}
}

// reopenOnce is one open of chat, with the fake clock moved to the redial timer
// if the open is waiting on it.
func (r *directRig) reopenOnce(chat string) (io.ReadWriteCloser, error) {
	r.t.Helper()
	type result struct {
		ch  io.ReadWriteCloser
		err error
	}
	done := make(chan result, 1)
	guard.Go("livedoor.test.direct.reopen", func() {
		ch, err := r.openChatErr(chat)
		done <- result{ch: ch, err: err}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case res := <-done:
			return res.ch, res.err
		default:
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("reopening %s never finished", chat)
		}
		quiesce()
		select {
		case res := <-done:
			return res.ch, res.err
		default:
		}
		r.moveToRedialTimer()
	}
}

func (r *directRig) askAgainAfterDrop(chat string) io.ReadWriteCloser {
	r.t.Helper()
	ch, err := r.askAgainAfterDropErr(chat)
	if err != nil {
		r.t.Fatalf("reopening %s failed: %v", chat, err)
	}
	return ch
}

// hostSays makes the host of reg's tunnels end every channel of the device with
// reason while leaving each tunnel open, which is the host speaking a reason on a
// channel and nothing else.
func hostSays(t *testing.T, reg *Registry, device, reason string) {
	t.Helper()
	var targets []channelResetter
	reg.mu.Lock()
	for _, entry := range reg.byDevice[device] {
		if cr, ok := entry.closer.(channelResetter); ok {
			targets = append(targets, cr)
		}
	}
	reg.mu.Unlock()
	if len(targets) == 0 {
		t.Fatalf("the registry holds no tunnel of %s to speak on", device)
	}
	for _, cr := range targets {
		cr.resetAllChannels(reason)
	}
}

// bounded runs fn on its own goroutine and fails the test if it has not returned
// within ten seconds of real time. It is for calls that must never block.
func bounded(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	guard.Go("livedoor.test.direct.bounded", func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}
