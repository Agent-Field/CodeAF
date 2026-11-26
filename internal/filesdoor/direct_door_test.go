package filesdoor

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// liveCall is one live tunnel the door handed to the live handler: which device
// it was, which road the door says carried it, and the handles a test uses to
// end it or to see that it ended.
type liveCall struct {
	device  string
	road    string
	release chan struct{}
	ended   chan struct{}
}

// directRig is a door with a live handler that echoes four bytes, and the
// acceptor the Direct hook was handed. A direct stream is one end of a pipe
// given to that acceptor, and the other end given to DialDirect, which is what
// the peer-to-peer host and the viewer's link do with a data channel.
type directRig struct {
	t      *testing.T
	w      *world
	served *served

	acceptor StreamAcceptor
	owner    context.Context
	calls    chan *liveCall
}

// newDirectRig starts the door. mutate may change the config before it starts
// (a shorter roster time, no live handler).
func newDirectRig(t *testing.T, mutate func(*ServeConfig)) *directRig {
	t.Helper()
	w := newWorld(t)
	rig := &directRig{t: t, w: w, calls: make(chan *liveCall, 64)}
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.LiveRoad = func(ctx context.Context, tun io.ReadWriteCloser, device, road string) error {
		defer tun.Close()
		call := &liveCall{device: device, road: road, release: make(chan struct{}), ended: make(chan struct{})}
		defer close(call.ended)
		rig.calls <- call
		echoed := make(chan struct{})
		go func() {
			defer close(echoed)
			buf := make([]byte, 4)
			for {
				if _, err := io.ReadFull(tun, buf); err != nil {
					return
				}
				if _, err := tun.Write(buf); err != nil {
					return
				}
			}
		}()
		select {
		case <-call.release:
		case <-ctx.Done():
		case <-echoed:
		}
		return nil
	}
	hooked := make(chan struct{})
	cfg.Direct = func(ctx context.Context, accept StreamAcceptor) {
		rig.owner, rig.acceptor = ctx, accept
		close(hooked)
	}
	if mutate != nil {
		mutate(&cfg)
	}
	rig.served = w.serve(cfg)
	if cfg.Direct != nil {
		select {
		case <-hooked:
		case <-time.After(5 * time.Second):
			t.Fatal("the door did not hand the host its acceptor")
		}
	}
	return rig
}

// pipe gives the acceptor one end and returns the other, which is the viewer's.
func (r *directRig) pipe() net.Conn {
	r.t.Helper()
	near, far := net.Pipe()
	r.t.Cleanup(func() { _ = near.Close(); _ = far.Close() })
	_ = near.SetDeadline(time.Now().Add(10 * time.Second))
	go r.acceptor(r.owner, far)
	return near
}

func (r *directRig) dialDirect() (io.ReadWriteCloser, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return DialDirect(ctx, r.pipe(), r.w.id, r.w.b, r.w.a.ID())
}

func (r *directRig) nextCall() *liveCall {
	r.t.Helper()
	select {
	case call := <-r.calls:
		return call
	case <-time.After(5 * time.Second):
		r.t.Fatal("no live tunnel reached the live handler")
		return nil
	}
}

func echoOnce(t *testing.T, tun io.ReadWriter) {
	t.Helper()
	if _, err := tun.Write([]byte("ping")); err != nil {
		t.Fatalf("a write on a live tunnel failed: %v", err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(tun, got); err != nil || string(got) != "ping" {
		t.Fatalf("the live tunnel echoed %q, %v; want ping", got, err)
	}
}

// readsEOFBeforeAnyNoise reports whether the far end closed the stream having
// written nothing at all back, which is what a door does with an intent it will
// not serve.
func readsEOFBeforeAnyNoise(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var buf [16]byte
	n, err := conn.Read(buf[:])
	return n == 0 && errors.Is(err, io.EOF)
}

// D8 / V38 / security: the direct road has its own intent and prologue. A direct
// stream completes only with both, the live and files prologues never complete
// against it, and the relay registration (whose prologue map has no direct
// entry) closes a direct intent before any byte is written back.
func TestADirectPrologueNeverCompletesAgainstTheLiveOrFilesPrologue(t *testing.T) {
	rig := newDirectRig(t, nil)
	mine, err := DoorKey(rig.w.id, rig.w.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := DoorKey(rig.w.id, rig.w.a.ID())
	if err != nil {
		t.Fatal(err)
	}

	for _, wrong := range []struct{ name, prologue string }{
		{"the live prologue", LiveTunnelPrologue},
		{"the files prologue", FilesTunnelPrologue},
		{"the task door's prologue", "codeaf task door v1 connect"},
	} {
		t.Run("a direct intent with "+wrong.name+" never completes", func(t *testing.T) {
			if _, err := pair.ConnectWithKeyIntent(rig.pipe(), theirs.PublicKey().Bytes(), mine, pair.IntentDirect, wrong.prologue); err == nil {
				t.Fatalf("a direct stream completed with %s", wrong.name)
			}
		})
	}
	for _, other := range []struct {
		name   string
		intent byte
		pro    string
	}{
		{"live", pair.IntentLive, DirectTunnelPrologue},
		{"files", pair.IntentFiles, DirectTunnelPrologue},
		{"files with its own prologue", pair.IntentFiles, FilesTunnelPrologue},
		{"live with its own prologue", pair.IntentLive, LiveTunnelPrologue},
	} {
		t.Run("the "+other.name+" intent is closed before Noise on a direct stream", func(t *testing.T) {
			conn := rig.pipe()
			if _, err := conn.Write([]byte{other.intent}); err != nil {
				t.Fatal(err)
			}
			if !readsEOFBeforeAnyNoise(conn) {
				t.Fatalf("a direct stream answered the %s intent", other.name)
			}
		})
	}
	t.Run("the relay registration closes a direct intent before Noise", func(t *testing.T) {
		name, err := DoorName(rig.w.id, rig.w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		stream, err := relay.DialDoor(context.Background(), rig.w.service, name, relay.DoorLive)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		if _, err := stream.Write([]byte{pair.IntentDirect}); err != nil {
			t.Fatal(err)
		}
		var buf [16]byte
		if n, err := stream.Read(buf[:]); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("the relay door answered a direct intent: %d bytes, %v", n, err)
		}
	})

	t.Run("the direct intent with the direct prologue completes and the handler is told the road", func(t *testing.T) {
		tun, err := rig.dialDirect()
		if err != nil {
			t.Fatalf("a direct dial failed: %v", err)
		}
		defer tun.Close()
		call := rig.nextCall()
		if call.road != "direct" || call.device != rig.w.b.ID() {
			t.Fatalf("the handler was told road %q for device %q, want direct for the viewer", call.road, call.device)
		}
		echoOnce(t, tun)
	})
	t.Run("a relay live stream is told the road is relay", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tun, err := DialLive(ctx, rig.w.service, rig.w.id, rig.w.b, rig.w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer tun.Close()
		if call := rig.nextCall(); call.road != "relay" {
			t.Fatalf("a relay live tunnel was told road %q, want relay", call.road)
		}
	})
}

// D8: a direct stream is refused before Noise when the door has no live handler,
// exactly as a relay live intent is.
func TestADoorWithNoLiveHandlerRefusesADirectIntentBeforeNoise(t *testing.T) {
	rig := newDirectRig(t, func(cfg *ServeConfig) { cfg.LiveRoad = nil })
	// With nothing to serve the door ends the stream at once, so the intent byte
	// may not even be accepted; either way no byte comes back.
	conn := rig.pipe()
	if _, err := conn.Write([]byte{pair.IntentDirect}); err == nil && !readsEOFBeforeAnyNoise(conn) {
		t.Fatal("a door with no live handler answered a direct intent")
	}
}

// D8 / V38: a direct tunnel is owned by the lifetime of Serve, so the relay
// registration dropping and being remade does not end it, while a relay live
// tunnel on the same registration does end. The direct tunnel still echoes
// after the registration has come back.
func TestADirectStreamSurvivesTheRelayRegistrationEnding(t *testing.T) {
	rig := newDirectRig(t, nil)
	direct, err := rig.dialDirect()
	if err != nil {
		t.Fatalf("a direct dial failed: %v", err)
	}
	defer direct.Close()
	directCall := rig.nextCall()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	viaRelay, err := DialLive(ctx, rig.w.service, rig.w.id, rig.w.b, rig.w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer viaRelay.Close()
	relayCall := rig.nextCall()
	if relayCall.road != "relay" || directCall.road != "direct" {
		t.Fatalf("roads were %q and %q", directCall.road, relayCall.road)
	}

	before := rig.served.regs.Load()
	rig.w.relay.dropRegistrations()
	select {
	case <-relayCall.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("a relay live tunnel outlived the registration it came in on")
	}
	deadline := time.After(20 * time.Second)
	for rig.served.regs.Load() <= before {
		select {
		case <-rig.served.ready:
		case <-deadline:
			t.Fatal("the door did not register again")
		}
	}
	select {
	case <-directCall.ended:
		t.Fatal("a direct tunnel ended with the relay registration")
	default:
	}
	echoOnce(t, direct)
}

// D8: ending Serve ends every direct stream and Serve waits for them, so
// nothing runs against the door once it has returned.
func TestEndingServeEndsEveryDirectStreamAndWaitsForThem(t *testing.T) {
	rig := newDirectRig(t, nil)
	tun, err := rig.dialDirect()
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	call := rig.nextCall()

	rig.served.stop()
	select {
	case <-call.ended:
	default:
		t.Fatal("Serve returned while a direct stream's handler was still running")
	}
	// And a stream arriving once Serve is over is closed, not served.
	conn := rig.pipe()
	if _, err := conn.Write([]byte{pair.IntentDirect}); err == nil && !readsEOFBeforeAnyNoise(conn) {
		t.Fatal("a stream was served after Serve ended")
	}
}

// V21 / V35: the host admits at most MaxDirect direct streams, closes the next
// one after its intent byte with nothing written back, counts them apart from
// relay live streams (so a relay one is still admitted), and admits a new
// direct stream once one ends.
func TestTheFifteenthDirectStreamIsRefusedBeforeNoise(t *testing.T) {
	rig := newDirectRig(t, nil)
	var held []io.ReadWriteCloser
	var calls []*liveCall
	t.Cleanup(func() {
		for _, c := range calls {
			select {
			case <-c.release:
			default:
				close(c.release)
			}
		}
		for _, h := range held {
			_ = h.Close()
		}
	})
	for i := 0; i < MaxDirect; i++ {
		tun, err := rig.dialDirect()
		if err != nil {
			t.Fatalf("direct dial %d failed: %v", i+1, err)
		}
		held = append(held, tun)
		calls = append(calls, rig.nextCall())
	}
	over := rig.pipe()
	if _, err := over.Write([]byte{pair.IntentDirect}); err != nil {
		t.Fatal(err)
	}
	if !readsEOFBeforeAnyNoise(over) {
		t.Fatalf("direct stream number %d was answered", MaxDirect+1)
	}

	// The relay's live count is separate: a relay live stream still gets in.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	viaRelay, err := DialLive(ctx, rig.w.service, rig.w.id, rig.w.b, rig.w.a.ID())
	if err != nil {
		t.Fatalf("a relay live dial was refused while the direct slots were full: %v", err)
	}
	defer viaRelay.Close()
	if call := rig.nextCall(); call.road != "relay" {
		t.Fatalf("the relay tunnel was told road %q", call.road)
	}

	// One direct stream ends; its slot is given back.
	close(calls[0].release)
	select {
	case <-calls[0].ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the first direct handler did not end")
	}
	_ = held[0].Close()
	var replaced io.ReadWriteCloser
	for attempt := 0; attempt < 50 && replaced == nil; attempt++ {
		if tun, err := rig.dialDirect(); err == nil {
			replaced = tun
		}
	}
	if replaced == nil {
		t.Fatal("a direct stream was refused after one ended")
	}
	held = append(held, replaced)
	calls = append(calls, rig.nextCall())
}

// V34 / security: a removed device cannot open a direct road (its hello is
// refused with the removal sentence), and a device removed while its direct
// tunnel is open loses the tunnel, as its relay tunnel would.
func TestARemovedDeviceLosesItsDirectStreamAndCannotOpenAnother(t *testing.T) {
	saved := liveRecheck
	liveRecheck = 20 * time.Millisecond
	t.Cleanup(func() { liveRecheck = saved })
	rig := newDirectRig(t, func(cfg *ServeConfig) { cfg.RosterFor = 10 * time.Millisecond })

	tun, err := rig.dialDirect()
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	call := rig.nextCall()
	echoOnce(t, tun)

	rig.w.remove(rig.w.b.ID())
	select {
	case <-call.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("a removed device's direct tunnel was still open")
	}
	_, err = rig.dialDirect()
	var refused *RefusedError
	if !errors.As(err, &refused) || refused.Reason != RemovedLine {
		t.Fatalf("a removed device's direct dial gave %v, want a refusal that says %q", err, RemovedLine)
	}
}

// D8: the stream a direct dial is given is closed when the handshake fails or
// the dial ends, so a failed attempt leaves no peer connection behind.
func TestADirectDialClosesItsStreamWhenTheHandshakeFails(t *testing.T) {
	w := newWorld(t)
	near, far := net.Pipe()
	defer far.Close()
	var closed atomic.Bool
	var once sync.Once
	watched := &closeWatcher{Conn: near, onClose: func() { once.Do(func() { closed.Store(true) }) }}
	// The far end answers with nonsense, so the Noise handshake cannot finish.
	go func() {
		var b [1]byte
		_, _ = far.Read(b[:])
		_ = far.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := DialDirect(ctx, watched, w.id, w.b, w.a.ID()); err == nil {
		t.Fatal("a direct dial against a stream that closes completed")
	}
	if !closed.Load() {
		t.Fatal("the stream was not closed after the handshake failed")
	}
}

type closeWatcher struct {
	net.Conn
	onClose func()
}

func (c *closeWatcher) Close() error {
	c.onClose()
	return c.Conn.Close()
}
