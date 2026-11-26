package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

type fakeResolverFunc func(ctx context.Context, chat string) (net.Conn, Route, error)

func (f fakeResolverFunc) Resolve(ctx context.Context, chat string) (net.Conn, Route, error) {
	return f(ctx, chat)
}

func reasonOf(err error) string {
	var r interface{ Reason() string }
	if errors.As(err, &r) {
		return r.Reason()
	}
	var ref *RefusedError
	if errors.As(err, &ref) {
		return ref.Reason
	}
	return ""
}

// TestAnOlderHolderThatEchoesNoStampGetsNoViewerBytes verifies contract V28
// (D10): when a holder replies with a welcome frame that lacks the echoed
// Remote stamp, the router resets the channel with holder-old, forwards zero
// viewer bytes after the rewritten hello to the holder (even when the viewer
// pipelined a Submit call right behind its hello), never forwards the old
// holder's welcome to the viewer, and forwards the same pipelined Submit when
// connected to a holder that echoes the stamp.
func TestAnOlderHolderThatEchoesNoStampGetsNoViewerBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Old holder: reads hello, ignores unknown fields, replies with a welcome
	// without Remote, and records every byte that arrives after the hello line.
	var (
		oldMu          sync.Mutex
		oldHello       remote.Hello
		oldPostBytes   bytes.Buffer
		oldHolderEnded = make(chan struct{})
	)
	oldResolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v28.oldHolder", func() {
			defer close(oldHolderEnded)
			defer holderSide.Close()

			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f) == nil {
				oldMu.Lock()
				_ = json.Unmarshal(f.Payload, &oldHello)
				oldMu.Unlock()
			}
			if scan.wpos > scan.rpos {
				oldMu.Lock()
				oldPostBytes.Write(scan.buf[scan.rpos:scan.wpos])
				oldMu.Unlock()
			}

			welcomePayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   "/work/proj",
				SessionFile: "/work/proj/transcript.jsonl",
				Model:       "test-model",
			})
			welcomeFrame, _ := json.Marshal(remote.Frame{
				Kind:    "welcome",
				Payload: welcomePayload,
			})
			_, _ = holderSide.Write(append(welcomeFrame, '\n'))

			var buf [1024]byte
			for {
				n, rErr := holderSide.Read(buf[:])
				if n > 0 {
					oldMu.Lock()
					oldPostBytes.Write(buf[:n])
					oldMu.Unlock()
				}
				if rErr != nil {
					return
				}
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       1001,
			Session:   "/work/proj/transcript.jsonl",
			Workspace: "/work/proj",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})

	peer := Peer{
		Device: "dev-viewer-1",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
		Class:  pair.ClassNew,
	}
	guard.Go("livedoor.test.v28.serveOld", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: oldResolver,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })

	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	vCh, err := vMux.Open(ctx, "chat-old")
	if err != nil {
		t.Fatalf("Open(chat-old) failed: %v", err)
	}

	helloPayload, _ := json.Marshal(remote.Hello{
		Version:  remote.Version,
		ClientID: "client-v28",
		Surface:  "laptop",
	})
	helloLine, _ := json.Marshal(remote.Frame{
		Kind:    "hello",
		Payload: helloPayload,
	})
	submitPayload, _ := json.Marshal(remote.SubmitArgs{Text: "pipelined secret command"})
	submitLine, _ := json.Marshal(remote.Frame{
		Kind:    "call",
		ID:      7,
		Method:  remote.MethodSubmit,
		Payload: submitPayload,
	})

	pipelined := append(append(helloLine, '\n'), append(submitLine, '\n')...)
	if _, err := vCh.Write(pipelined); err != nil {
		t.Fatalf("writing pipelined hello+submit failed: %v", err)
	}

	var readBuf [4096]byte
	n, readErr := vCh.Read(readBuf[:])
	if n != 0 {
		t.Fatalf("viewer received %d bytes (%q) from old holder, want 0 bytes", n, readBuf[:n])
	}
	if got := reasonOf(readErr); got != ReasonHolderOld {
		t.Fatalf("vCh.Read err = %v (reason=%q), want %q", readErr, got, ReasonHolderOld)
	}
	<-oldHolderEnded

	oldMu.Lock()
	gotPost := oldPostBytes.String()
	gotOldRemote := oldHello.Remote
	oldMu.Unlock()

	if gotOldRemote == nil || gotOldRemote.Device != peer.Device || !gotOldRemote.Drive {
		t.Fatalf("rewritten hello sent to old holder had Remote=%+v, want Device=%q Drive=true", gotOldRemote, peer.Device)
	}
	if gotPost != "" {
		t.Fatalf("old holder received %d bytes after hello (%q), want nothing", len(gotPost), gotPost)
	}

	// 2. Correct holder: echoes Welcome.Remote matching the stamp, and receives
	// the exact same pipelined Submit call line.
	submitReceived := make(chan remote.Frame, 1)
	goodResolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v28.goodHolder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f); err != nil {
				return
			}
			var h remote.Hello
			if err := json.Unmarshal(f.Payload, &h); err != nil {
				return
			}
			welcomePayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Model:       "test-model",
				Remote:      h.Remote,
			})
			welcomeFrame, _ := json.Marshal(remote.Frame{
				Kind:    "welcome",
				Payload: welcomePayload,
			})
			if _, err := holderSide.Write(append(welcomeFrame, '\n')); err != nil {
				return
			}
			callLine, err := scan.ReadLine(MaxLine)
			if err != nil {
				return
			}
			var callFrame remote.Frame
			if err := json.Unmarshal(bytes.TrimRight(callLine, "\r\n"), &callFrame); err == nil {
				submitReceived <- callFrame
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       1002,
			Session:   "/work/proj/transcript.jsonl",
			Workspace: "/work/proj",
		}, nil
	})

	vConn2, hConn2 := net.Pipe()
	t.Cleanup(func() {
		_ = vConn2.Close()
		_ = hConn2.Close()
	})
	guard.Go("livedoor.test.v28.serveGood", func() {
		_ = Serve(ctx, hConn2, peer, ServeConfig{
			Resolver: goodResolver,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux2 := NewMux(vConn2, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux2.Close() })

	if _, err := vMux2.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck (good holder) failed: %v", err)
	}
	vCh2, err := vMux2.Open(ctx, "chat-good")
	if err != nil {
		t.Fatalf("Open(chat-good) failed: %v", err)
	}
	if _, err := vCh2.Write(pipelined); err != nil {
		t.Fatalf("writing pipelined hello+submit to good holder failed: %v", err)
	}

	vScan2 := newLineScanner(vCh2, MaxLine)
	wLine, err := vScan2.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading welcome from good holder failed: %v", err)
	}
	var wFrame remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(wLine, "\r\n"), &wFrame); err != nil || wFrame.Kind != "welcome" {
		t.Fatalf("first frame from good holder = %+v (err=%v), want welcome", wFrame, err)
	}

	gotCall := <-submitReceived
	if gotCall.Kind != "call" || gotCall.ID != 7 || gotCall.Method != remote.MethodSubmit {
		t.Fatalf("good holder received frame %+v, want call ID=7 Method=%q", gotCall, remote.MethodSubmit)
	}
	var gotArgs remote.SubmitArgs
	if err := json.Unmarshal(gotCall.Payload, &gotArgs); err != nil || gotArgs.Text != "pipelined secret command" {
		t.Fatalf("good holder SubmitArgs = %+v (err=%v), want Text=%q", gotArgs, err, "pipelined secret command")
	}
}

// TestWhenTheHolderExitsTheViewerIsToldAndNoChatIsEverStarted verifies contract
// V29: when the holder closes mid-session the viewer channel ends with reason
// holder-gone, a fresh Open for the same chat is refused with not-running, and
// no start path is ever invoked.
func TestWhenTheHolderExitsTheViewerIsToldAndNoChatIsEverStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		running      atomic.Bool
		resolveCount atomic.Int32
		startCount   atomic.Int32
		exitHolder   = make(chan struct{})
	)
	running.Store(true)

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		resolveCount.Add(1)
		if !running.Load() {
			return nil, Route{}, ErrNotRunning
		}
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v29.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, MaxLine)
			line, err := scan.ReadLine(FirstLineMax)
			if err != nil {
				return
			}
			var f remote.Frame
			if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f); err != nil {
				return
			}
			var h remote.Hello
			if err := json.Unmarshal(f.Payload, &h); err != nil {
				return
			}
			if !h.Join || h.New {
				startCount.Add(1)
			}
			welcomePayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Model:       "test-model",
				Remote:      h.Remote,
			})
			welcomeFrame, _ := json.Marshal(remote.Frame{
				Kind:    "welcome",
				Payload: welcomePayload,
			})
			_, _ = holderSide.Write(append(welcomeFrame, '\n'))
			<-exitHolder
			running.Store(false)
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       2001,
			Session:   "/work/v29/transcript.jsonl",
			Workspace: "/work/v29",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	peer := Peer{
		Device: "dev-v29",
		Label:  "tablet",
		Permit: pair.Permit{Watch: true, Drive: true},
	}
	guard.Go("livedoor.test.v29.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}

	vCh, err := vMux.Open(ctx, "chat-v29")
	if err != nil {
		t.Fatalf("Open(chat-v29) failed: %v", err)
	}
	helloPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	helloLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: helloPayload})
	if _, err := vCh.Write(append(helloLine, '\n')); err != nil {
		t.Fatalf("writing hello failed: %v", err)
	}

	vScan := newLineScanner(vCh, MaxLine)
	if _, err := vScan.ReadLine(MaxLine); err != nil {
		t.Fatalf("reading welcome failed: %v", err)
	}

	// Close the holder mid-session and assert the viewer channel ends with holder-gone.
	close(exitHolder)
	var buf [256]byte
	_, readErr := vCh.Read(buf[:])
	if got := reasonOf(readErr); got != ReasonHolderGone {
		t.Fatalf("after holder exit, vCh.Read err = %v (reason=%q), want %q", readErr, got, ReasonHolderGone)
	}

	// A fresh Open for the same chat is refused with not-running and never starts a chat.
	_, openErr := vMux.Open(ctx, "chat-v29")
	if got := reasonOf(openErr); got != ReasonNotRunning {
		t.Fatalf("second Open after holder exit returned %v (reason=%q), want %q", openErr, got, ReasonNotRunning)
	}
	if got := startCount.Load(); got != 0 {
		t.Fatalf("startCount = %d, want 0 (live door must never start a chat)", got)
	}
	if got := resolveCount.Load(); got != 2 {
		t.Fatalf("resolveCount = %d, want 2", got)
	}
}

// TestAHostBackstopClosesALinkSilentForTheIdleDetachPlusAMinute verifies link
// bookkeeping: the host-side backstop timer closes a tunnel that has received no
// frame at all for IdleDetach + 1 minute on the injected Clock.
func TestAHostBackstopClosesALinkSilentForTheIdleDetachPlusAMinute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})

	serveDone := make(chan error, 1)
	peer := Peer{
		Device: "dev-idle-backstop",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}
	guard.Go("livedoor.test.backstop.serve", func() {
		serveDone <- Serve(ctx, hConn, peer, ServeConfig{
			Clock: fc,
			Host:  "workstation",
			Road:  "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{Clock: fc})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	fc.WaitForTimers(1)

	// Advance to 1 second shy of IdleDetach + 1 minute: link must still be alive.
	fc.Advance(IdleDetach + 59*time.Second)
	select {
	case <-vMux.Done():
		t.Fatal("link closed at IdleDetach + 59s, want still open before IdleDetach + 1m")
	default:
	}

	// Advance 2 more seconds past IdleDetach + 1 minute: host backstop closes the link.
	fc.Advance(2 * time.Second)
	<-vMux.Done()
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve returned %v, want nil on clean backstop close", err)
	}
}

// TestAThirdTunnelOfOneDeviceClosesTheOldest verifies link bookkeeping: when a
// single viewer device opens a third tunnel beyond MaxTunnelsPerDevice (2), the
// oldest tunnel is closed with host-closing while the second and third stay open.
func TestAThirdTunnelOfOneDeviceClosesTheOldest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewRegistry()
	peer := Peer{
		Device: "dev-multi-tun",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.third.holder", func() {
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
			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			_, _ = holderSide.Write(append(wFrame, '\n'))
			_, _ = io.Copy(io.Discard, holderSide)
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       3001,
			Session:   "/work/p/" + chat + "/transcript.jsonl",
			Workspace: "/work/p",
		}, nil
	})

	startTunnel := func(chat string) (*Mux, *Channel) {
		vConn, hConn := net.Pipe()
		t.Cleanup(func() {
			_ = vConn.Close()
			_ = hConn.Close()
		})
		guard.Go("livedoor.test.third.serve", func() {
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
			t.Fatalf("WaitHelloAck(%s) failed: %v", chat, err)
		}
		vCh, err := vMux.Open(ctx, chat)
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", chat, err)
		}
		hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
		hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
		if _, err := vCh.Write(append(hLine, '\n')); err != nil {
			t.Fatalf("Write hello (%s) failed: %v", chat, err)
		}
		scan := newLineScanner(vCh, MaxLine)
		if _, err := scan.ReadLine(MaxLine); err != nil {
			t.Fatalf("Read welcome (%s) failed: %v", chat, err)
		}
		return vMux, vCh
	}

	mux1, ch1 := startTunnel("chat-1")
	mux2, ch2 := startTunnel("chat-2")
	if got := reg.Count(peer.Device); got != MaxTunnelsPerDevice {
		t.Fatalf("reg.Count after 2 tunnels = %d, want %d", got, MaxTunnelsPerDevice)
	}

	// Opening a 3rd tunnel for the same device must evict the 1st (oldest) with host-closing.
	mux3, ch3 := startTunnel("chat-3")
	if got := reg.Count(peer.Device); got != MaxTunnelsPerDevice {
		t.Fatalf("reg.Count after 3rd tunnel = %d, want %d", got, MaxTunnelsPerDevice)
	}

	var buf [64]byte
	_, err1 := ch1.Read(buf[:])
	if got := reasonOf(err1); got != ReasonHostClosing {
		t.Fatalf("oldest channel ch1 ended with %v (reason=%q), want %q", err1, got, ReasonHostClosing)
	}
	<-mux1.Done()

	select {
	case <-mux2.Done():
		t.Fatal("second tunnel mux2 closed when third tunnel opened")
	case <-ch2.Done():
		t.Fatal("second tunnel channel ch2 closed when third tunnel opened")
	case <-mux3.Done():
		t.Fatal("third tunnel mux3 closed")
	case <-ch3.Done():
		t.Fatal("third tunnel channel ch3 closed")
	default:
	}
}

// TestRevocationNeverHoldsALockAcrossASpliceWrite verifies link bookkeeping:
// calling Registry.Revoke while a channel's writer is blocked on flow-control
// credit returns promptly without deadlocking on any splice write.
func TestRevocationNeverHoldsALockAcrossASpliceWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := NewRegistry()
	peer := Peer{
		Device: "dev-credit-blocked",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}

	lim := DefaultLimits()
	lim.ChannelWindow = 32 << 10
	lim.LinkWindow = 64 << 10
	lim.QueueMax = 512 << 10
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20

	holderQueued := make(chan struct{})
	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.revokelock.holder", func() {
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
			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			_, _ = holderSide.Write(append(wFrame, '\n'))

			// Stream enough events (> ChannelWindow, < QueueMax) so the channel
			// writer blocks on Channel.Write waiting for credit that never comes.
			evLine := append(bytes.Repeat([]byte{'e'}, 4096), '\n')
			for i := 0; i < 16; i++ {
				if _, err := holderSide.Write(evLine); err != nil {
					return
				}
			}
			close(holderQueued)
			_, _ = io.Copy(io.Discard, holderSide)
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       4001,
			Session:   "/work/p/transcript.jsonl",
			Workspace: "/work/p",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	guard.Go("livedoor.test.revokelock.serve", func() {
		_ = Serve(ctx, hConn, peer, ServeConfig{
			Resolver: resolver,
			Registry: reg,
			Limits:   lim,
			Host:     "workstation",
			Road:     "relay",
		})
	})

	vMux := NewMux(vConn, Viewer, MuxConfig{Limits: lim})
	t.Cleanup(func() { _ = vMux.Close() })
	if _, err := vMux.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	vCh, err := vMux.Open(ctx, "chat-blocked")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := vCh.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("Write hello failed: %v", err)
	}

	// Never call vCh.Read so the channel's send credit fills completely and
	// chanWriter blocks inside ch.Write.
	<-holderQueued

	revokeDone := make(chan struct{})
	guard.Go("livedoor.test.revokelock.revoke", func() {
		reg.Revoke(peer.Device)
		close(revokeDone)
	})

	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		t.Fatal("Registry.Revoke blocked while channel writer was parked on credit")
	}

	<-vCh.Done()
	if got := vCh.Reason(); got != ReasonRemoved {
		t.Fatalf("vCh.Reason() after Revoke = %q, want %q", got, ReasonRemoved)
	}
}
