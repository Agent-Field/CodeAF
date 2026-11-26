package livedoor

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// These tests are written from the contract for the person turning remote
// control off on the computer being watched, not from the code that implements
// it:
//
//	C1  A viewer that has a chat open on a host whose remote control goes off has
//	    that view ended with ONE reason that says so. The link does not redial: no
//	    timer is armed and no tunnel is dialled again by itself.
//	C2  A host that goes away without saying anything is still a host that went
//	    away: the link redials as it always did.
//	C3  A host that changes the permit (watching taken away while remote control
//	    stays on) still ends the view with the permit's reason, as it always did.
//	C4  A build that does not know the new reason ends the view cleanly: the reason
//	    is an opaque string on the wire, the viewer says a plain loss of
//	    connection, and it redials as it did before the reason existed.
//	C5  Turning remote control back on is enough for the next view a person opens
//	    to work, without restarting the viewer.

// switchableHost serves one viewer tunnel per dial with a permit and a switch the
// test turns, the way syncsetup's door reads the setting on every question.
type switchableHost struct {
	watch atomic.Bool
	off   atomic.Bool
	reg   *Registry
	peer  Peer
	dials atomic.Int32
}

func newSwitchableHost(t *testing.T, ctx context.Context, fc *FakeClock) (*switchableHost, *Link) {
	t.Helper()
	h := &switchableHost{
		reg:  NewRegistry(),
		peer: Peer{Device: "dev-viewer", Label: "laptop", Permit: pair.Permit{Watch: true, Drive: true}},
	}
	h.watch.Store(true)
	resolver := startStubHostResolver(t)
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			h.dials.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			guard.Go("livedoor.test.rcoff.serve", func() {
				_ = Serve(ctx, hSide, h.peer, ServeConfig{
					Resolver: resolver,
					Registry: h.reg,
					Host:     "Desktop",
					Road:     "relay",
					Permit: func(string) pair.Permit {
						if !h.watch.Load() {
							return pair.Permit{}
						}
						return pair.Permit{Watch: true, Drive: true}
					},
					Off: func() bool { return h.off.Load() },
				})
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)
	return h, link
}

// readEnd reads a channel until it ends and answers the error it ended with.
func readEnd(t *testing.T, ch io.Reader) error {
	t.Helper()
	done := make(chan error, 1)
	guard.Go("livedoor.test.rcoff.read", func() {
		var buf [32]byte
		_, err := ch.Read(buf[:])
		done <- err
	})
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the view did not end")
		return nil
	}
}

// C1.
func TestRemoteControlOffEndsAnOpenViewWithItsOwnReasonAndTheLinkDoesNotRedial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	host, link := newSwitchableHost(t, ctx, fc)

	ch, err := link.OpenerContext("chat-off")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	ended := make(chan string, 1)
	link.OnChannelEnd(func(_, reason string) { ended <- reason })

	host.watch.Store(false)
	host.off.Store(true)
	host.reg.PermitsChanged("")

	readErr := readEnd(t, ch)
	if reason, ok := ReasonOf(readErr); !ok || reason != ReasonRemoteControlOff {
		t.Fatalf("the view ended with %v (reason %q), want the reason %q", readErr, reason, ReasonRemoteControlOff)
	}
	if got := <-ended; got != ReasonRemoteControlOff {
		t.Fatalf("OnChannelEnd said %q, want %q", got, ReasonRemoteControlOff)
	}

	eventually(t, "the link to rest with the host's reason and no timer armed", func() bool {
		st := link.State()
		return st.Kind == Revoked && st.Reason == ReasonRemoteControlOff && fc.BlockedTimers() == 0
	})
	if said := chatlist.LiveSaid(link.State().Reason, "Desktop"); said != "Desktop turned remote control off." {
		t.Fatalf("the one sentence reads %q", said)
	}
	fc.Advance(time.Hour)
	if got := host.dials.Load(); got != 1 {
		t.Fatalf("the link dialled again by itself: %d dials, want 1", got)
	}
	if st := link.State(); st.Kind == Reconnecting || st.Kind == WentOffline {
		t.Fatalf("the link claims %v although the host is up and said no", st.Kind)
	}
}

// C5: and turning it back on is taken by the next open.
func TestTheFirstOpeningAfterRemoteControlIsBackOnDialsAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	host, link := newSwitchableHost(t, ctx, fc)

	ch, err := link.OpenerContext("chat-off")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	host.watch.Store(false)
	host.off.Store(true)
	host.reg.PermitsChanged("")
	_ = readEnd(t, ch)
	eventually(t, "the link to rest", func() bool { return link.State().Kind == Revoked })

	host.watch.Store(true)
	host.off.Store(false)
	again, err := link.OpenerContext("chat-off")(ctx)
	if err != nil {
		t.Fatalf("opening again with remote control back on: %v", err)
	}
	defer again.Close()
	if got := host.dials.Load(); got != 2 {
		t.Fatalf("%d dials, want 2: the open after remote control came back did not dial", got)
	}
	eventually(t, "the link to be live again", func() bool { return link.State().Kind == Live })
}

// A person who asks again while the host is still off is refused in the same
// words (the host's answer to the dial carries them), and the view they open
// the next time, after the host is back on, works: the refusal is not remembered
// as this computer's permit being empty.
func TestAnOpenWhileRemoteControlIsStillOffIsRefusedAsOffAndLiftedWhenItIsBackOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	host, link := newSwitchableHost(t, ctx, fc)

	ch, err := link.OpenerContext("chat-off")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	host.watch.Store(false)
	host.off.Store(true)
	host.reg.PermitsChanged("")
	_ = readEnd(t, ch)
	eventually(t, "the link to rest", func() bool { return link.State().Kind == Revoked })

	_, err = link.OpenerContext("chat-off")(ctx)
	if reason, ok := ReasonOf(err); !ok || reason != ReasonRemoteControlOff {
		t.Fatalf("an open on a host that is off was refused with %v (reason %q), want %q", err, reason, ReasonRemoteControlOff)
	}
	if st := link.State(); st.Kind != Revoked || st.Reason != ReasonRemoteControlOff || fc.BlockedTimers() != 0 {
		t.Fatalf("after the refused open the link is %+v with %d timers, want it resting with the host's reason", st, fc.BlockedTimers())
	}

	host.watch.Store(true)
	host.off.Store(false)
	again, err := link.OpenerContext("chat-off")(ctx)
	if err != nil {
		t.Fatalf("opening with remote control back on, after a refused open: %v", err)
	}
	defer again.Close()
}

// C3: a real permit change keeps its own reason and its own sentence.
func TestAPermitThatTakesWatchingAwayKeepsItsReasonWhileRemoteControlIsOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	host, link := newSwitchableHost(t, ctx, fc)

	ch, err := link.OpenerContext("chat-permit")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	host.watch.Store(false) // remote control stays on: host.off is false
	host.reg.PermitsChanged("")

	readErr := readEnd(t, ch)
	if reason, ok := ReasonOf(readErr); !ok || reason != ReasonPermitChanged {
		t.Fatalf("the view ended with %v (reason %q), want the permit's %q", readErr, reason, ReasonPermitChanged)
	}
	var ce *ChannelError
	if errors.As(readErr, &ce) && ce.EndsView() {
		t.Fatal("a changed permit ends the view for good; it must keep redialling to learn the new permit")
	}
	if got := chatlist.LiveSaid(ReasonPermitChanged, "Desktop"); got != "Desktop changed what this computer may do in its chats." {
		t.Fatalf("the permit sentence reads %q", got)
	}
	// It redials, learns it may not watch at all, and says that, as before. The
	// clock is advanced inside the wait because the link arms its redial timer
	// just after the channel's end is read.
	eventually(t, "the redial to be told watching is off", func() bool {
		fc.Advance(time.Second)
		st := link.State()
		return st.Kind == Revoked && st.Reason == ReasonNotAllowed
	})
}

// C2: a host that simply goes away is a host that went away.
func TestAHostThatGoesAwayWithoutSayingAnythingIsStillRedialled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		dials atomic.Int32
		sides = make(chan net.Conn, 4)
	)
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dials.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() { _ = vSide.Close(); _ = hSide.Close() })
			hMux := NewMux(hSide, Host, MuxConfig{})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{V: 1, Watch: true, Drive: true, Road: "relay", Limits: DefaultLimits()})
			guard.Go("livedoor.test.vanish.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
				}
			})
			sides <- hSide
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-vanish")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	_ = (<-sides).Close() // the host's process is killed: nothing is said

	readErr := readEnd(t, ch)
	var ce *ChannelError
	if errors.As(readErr, &ce) && ce.EndsView() {
		t.Fatalf("a connection that dropped without a word ended the view for good: %v", readErr)
	}
	eventually(t, "the link to redial", func() bool {
		st := link.State()
		return (st.Kind == Reconnecting || st.Kind == WentOffline) && fc.BlockedTimers() > 0
	})
	fc.Advance(time.Minute)
	eventually(t, "the second dial", func() bool { return dials.Load() >= 2 })
}

// C4: a viewer built before the reason existed meets a string it has no case
// for. This drives the current link with exactly that: a reason it does not know.
func TestAReasonTheViewerHasNoSentenceForEndsTheViewCleanlyAndIsRedialled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		dials  atomic.Int32
		hostCh = make(chan *Channel, 1)
	)
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dials.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() { _ = vSide.Close(); _ = hSide.Close() })
			hMux := NewMux(hSide, Host, MuxConfig{})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{V: 1, Watch: true, Drive: true, Road: "relay", Limits: DefaultLimits()})
			guard.Go("livedoor.test.unknown.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
					select {
					case hostCh <- ch:
					default:
					}
				}
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-new")(ctx)
	if err != nil {
		t.Fatalf("opening the view: %v", err)
	}
	const fromANewerBuild = "a-reason-from-a-newer-build"
	if err := (<-hostCh).Reset(fromANewerBuild); err != nil {
		t.Fatalf("the host's reset: %v", err)
	}

	readErr := readEnd(t, ch)
	reason, ok := ReasonOf(readErr)
	if !ok || reason != fromANewerBuild {
		t.Fatalf("the view ended with %v (reason %q, %v), want the opaque reason carried through", readErr, reason, ok)
	}
	var ce *ChannelError
	if errors.As(readErr, &ce) && ce.EndsView() {
		t.Fatal("an unknown reason ended the view for good")
	}
	said := chatlist.LiveSaid(reason, "Desktop")
	if said != "Lost the connection to Desktop." || strings.ContainsAny(said, "-_") {
		t.Fatalf("an unknown reason is said as %q, want the plain loss of connection", said)
	}
	if st := link.State(); st.Kind == Revoked {
		t.Fatalf("an unknown reason revoked the link: %+v", st)
	}
}

// The reason is a plain string on a frame every build already reads: no frame
// op, no field and no length changed with it.
func TestTheNewReasonIsAnOrdinaryResetPayload(t *testing.T) {
	if len(ReasonRemoteControlOff) == 0 || len(ReasonRemoteControlOff) > ChunkBytes {
		t.Fatalf("the reason %q does not fit a frame", ReasonRemoteControlOff)
	}
	if ReasonRemoteControlOff != "remote-control-off" {
		t.Fatalf("the reason's bytes are %q: persisted and on-the-wire identifiers keep theirs", ReasonRemoteControlOff)
	}
}
