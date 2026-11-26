package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func newEchoStampResolver(onHello func(remote.Hello)) Resolver {
	return fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.grants.holder", func() {
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
			if onHello != nil {
				onHello(h)
			}
			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, holderSide)
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       7001,
			Session:   "/work/grants/" + chat + "/transcript.jsonl",
			Workspace: "/work/grants",
		}, nil
	})
}

func openAndHandshake(t *testing.T, ctx context.Context, vMux *Mux, chat string) *Channel {
	t.Helper()
	ch, err := vMux.Open(ctx, chat)
	if err != nil {
		t.Fatalf("Open(%s) failed: %v", chat, err)
	}
	hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := ch.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("Write hello(%s) failed: %v", chat, err)
	}
	scan := newLineScanner(ch, MaxLine)
	if _, err := scan.ReadLine(MaxLine); err != nil {
		t.Fatalf("Read welcome(%s) failed: %v", chat, err)
	}
	return ch
}

// TestRemovingADeviceClosesItsOpenLiveViewsAtOnce verifies contract V10:
// Registry.Revoke(device) closes every open channel of that device with reason
// removed, closes its tunnel link, and leaves another device's tunnel and
// channels untouched.
func TestRemovingADeviceClosesItsOpenLiveViewsAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewRegistry()
	resolver := newEchoStampResolver(nil)

	startDevice := func(device string) (*Mux, *Channel, *Channel) {
		vConn, hConn := net.Pipe()
		t.Cleanup(func() {
			_ = vConn.Close()
			_ = hConn.Close()
		})
		peer := Peer{
			Device: device,
			Label:  device,
			Permit: pair.Permit{Watch: true, Drive: true},
		}
		guard.Go("livedoor.test.v10.revoke.serve", func() {
			_ = Serve(ctx, hConn, peer, ServeConfig{
				Resolver: resolver,
				Registry: reg,
				Host:     "workstation",
				Road:     "relay",
			})
		})
		vMux := NewMux(vConn, Viewer, MuxConfig{})
		t.Cleanup(func() { _ = vMux.Close() })
		if _, err := vMux.WaitHelloAck(ctx); err != nil {
			t.Fatalf("WaitHelloAck(%s) failed: %v", device, err)
		}
		ch1 := openAndHandshake(t, ctx, vMux, "chat-1")
		ch2 := openAndHandshake(t, ctx, vMux, "chat-2")
		return vMux, ch1, ch2
	}

	muxA, chA1, chA2 := startDevice("dev-revoke-me")
	muxB, chB1, chB2 := startDevice("dev-keep-me")

	reg.Revoke("dev-revoke-me")

	var buf [64]byte
	for i, ch := range []*Channel{chA1, chA2} {
		_, err := ch.Read(buf[:])
		if got := reasonOf(err); got != ReasonRemoved {
			t.Fatalf("revoked device channel %d ended with %v (reason=%q), want %q", i+1, err, got, ReasonRemoved)
		}
	}
	<-muxA.Done()

	select {
	case <-muxB.Done():
		t.Fatal("other device's tunnel muxB was closed by Revoke(dev-revoke-me)")
	case <-chB1.Done():
		t.Fatal("other device's channel chB1 was closed by Revoke(dev-revoke-me)")
	case <-chB2.Done():
		t.Fatal("other device's channel chB2 was closed by Revoke(dev-revoke-me)")
	default:
	}
}

// TestTurningWatchOffClosesItsViews verifies contract V10: when a device's
// permit changes to Watch:false and Registry.PermitsChanged is called, every
// open channel ends with permit-changed and the tunnel link closes.
func TestTurningWatchOffClosesItsViews(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewRegistry()
	var (
		permMu sync.Mutex
		permit = pair.Permit{Watch: true, Drive: true}
	)
	getPermit := func(string) pair.Permit {
		permMu.Lock()
		defer permMu.Unlock()
		return permit
	}

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-watch-off",
		Label:  "laptop",
		Permit: permit,
	}
	guard.Go("livedoor.test.v10.watchoff.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: newEchoStampResolver(nil),
			Registry: reg,
			Permit:   getPermit,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	ch1 := openAndHandshake(t, ctx, vMux, "chat-w1")
	ch2 := openAndHandshake(t, ctx, vMux, "chat-w2")

	permMu.Lock()
	permit = pair.Permit{Watch: false, Drive: false}
	permMu.Unlock()
	reg.PermitsChanged(peer.Device)

	var buf [64]byte
	for i, ch := range []*Channel{ch1, ch2} {
		_, err := ch.Read(buf[:])
		if got := reasonOf(err); got != ReasonPermitChanged {
			t.Fatalf("channel %d after Watch:false ended with %v (reason=%q), want %q", i+1, err, got, ReasonPermitChanged)
		}
	}
	<-vMux.Done()
}

// TestTurningDriveOffResetsItsChannelsAndTheyComeBackWatchOnly verifies
// contract V10: when a device's permit changes to Drive:false and
// Registry.PermitsChanged is called, every open channel is reset with
// permit-changed while keeping the tunnel link open, and a subsequent Open on
// the same tunnel is stamped with Drive:false.
func TestTurningDriveOffResetsItsChannelsAndTheyComeBackWatchOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewRegistry()
	var (
		permMu sync.Mutex
		permit = pair.Permit{Watch: true, Drive: true}
	)
	getPermit := func(string) pair.Permit {
		permMu.Lock()
		defer permMu.Unlock()
		return permit
	}

	hellos := make(chan remote.Hello, 4)
	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-drive-off",
		Label:  "laptop",
		Permit: permit,
	}
	guard.Go("livedoor.test.v10.driveoff.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: newEchoStampResolver(func(h remote.Hello) { hellos <- h }),
			Registry: reg,
			Permit:   getPermit,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	ch1 := openAndHandshake(t, ctx, vMux, "chat-d1")
	firstHello := <-hellos
	if firstHello.Remote == nil || !firstHello.Remote.Drive || firstHello.Watch {
		t.Fatalf("initial hello Remote=%+v Watch=%v, want Drive=true Watch=false", firstHello.Remote, firstHello.Watch)
	}

	// Turn Drive off and notify the registry.
	permMu.Lock()
	permit = pair.Permit{Watch: true, Drive: false}
	permMu.Unlock()
	reg.PermitsChanged(peer.Device)

	var buf [64]byte
	_, err := ch1.Read(buf[:])
	if got := reasonOf(err); got != ReasonPermitChanged {
		t.Fatalf("ch1 after Drive:false ended with %v (reason=%q), want %q", err, got, ReasonPermitChanged)
	}

	// Re-open on the same tunnel; the holder must now see Drive:false and Watch:true.
	_ = openAndHandshake(t, ctx, vMux, "chat-d1")
	secondHello := <-hellos
	if secondHello.Remote == nil || secondHello.Remote.Drive || !secondHello.Remote.Watch || !secondHello.Watch {
		t.Fatalf("re-opened hello Remote=%+v Watch=%v, want Remote.Drive=false Remote.Watch=true Watch=true",
			secondHello.Remote, secondHello.Watch)
	}
}

// TestAnUnpairedDeviceCannotListOrOpenAChat verifies contract V10: when Serve
// starts for a device whose permit has Watch:false, the viewer receives a
// HELLO_ACK with watch:false, the link closes at once, and the resolver is
// never asked.
func TestAnUnpairedDeviceCannotListOrOpenAChat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var resolveCalls atomic.Int32
	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		resolveCalls.Add(1)
		return nil, Route{}, ErrNotRunning
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-unallowed",
		Label:  "stranger",
		Permit: pair.Permit{Watch: false, Drive: false},
	}
	serveDone := make(chan error, 1)
	guard.Go("livedoor.test.v10.unpaired.serve", func() {
		serveDone <- Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Permit:   func(string) pair.Permit { return pair.Permit{Watch: false, Drive: false} },
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })

	ack, err := vMux.WaitHelloAck(ctx)
	if err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	if ack.Watch || ack.Drive {
		t.Fatalf("HelloAck for Watch:false peer = %+v, want Watch=false Drive=false", ack)
	}

	<-vMux.Done()
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve returned %v, want nil", err)
	}
	if _, openErr := vMux.Open(ctx, "any-chat"); openErr == nil {
		t.Fatal("Open on closed unpaired link succeeded, want error")
	}
	if got := resolveCalls.Load(); got != 0 {
		t.Fatalf("resolver was called %d times for Watch:false peer, want 0", got)
	}
}
