package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAViewerThatStopsReadingIsResetAndOtherViewersAndMoveAreNotStalled
// verifies contract V17: when two viewers on two tunnels attach to a stub
// holder that streams a burst of events and viewer A never reads, A's channel
// is Reset with reason lagging, A's holder connection is closed, and viewer B
// receives all of its lines at full rate without the stub holder's writes to B
// ever stalling behind A.
func TestAViewerThatStopsReadingIsResetAndOtherViewersAndMoveAreNotStalled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lim := DefaultLimits()
	lim.ChannelWindow = 16 << 10
	lim.LinkWindow = 64 << 10
	lim.QueueMax = 32 << 10
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20

	const totalLines = 32
	payloadText := strings.Repeat("x", 2400)

	var (
		aHolderClosed  atomic.Bool
		aWriteAttempts atomic.Int32
		bWriteAttempts atomic.Int32
		bRead          atomic.Int32
		holderMu       sync.Mutex
		holderSideA    net.Conn
		holderSideB    net.Conn
		bothAttached   = make(chan struct{})
		attachedCount  int
	)

	acceptOnStubHolder := func(isA bool) net.Conn {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v17.handshake", func() {
			scan := newLineScanner(holderSide, lim.MaxLine)
			line, err := scan.ReadLine(lim.FirstLineMax)
			if err != nil {
				_ = holderSide.Close()
				return
			}
			var f remote.Frame
			if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f) != nil {
				_ = holderSide.Close()
				return
			}
			var h remote.Hello
			if json.Unmarshal(f.Payload, &h) != nil {
				_ = holderSide.Close()
				return
			}
			wPayload, _ := json.Marshal(remote.Welcome{
				Version:     remote.Version,
				Workspace:   h.Workspace,
				SessionFile: h.Session,
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{Kind: "welcome", Payload: wPayload})
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				_ = holderSide.Close()
				return
			}

			holderMu.Lock()
			if isA {
				holderSideA = holderSide
			} else {
				holderSideB = holderSide
			}
			attachedCount++
			if attachedCount == 2 {
				close(bothAttached)
			}
			holderMu.Unlock()
		})
		return routerSide
	}

	resolverA := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		return acceptOnStubHolder(true), Route{
			Kind:      "daemon",
			PID:       6001,
			Session:   "/work/v17/transcript.jsonl",
			Workspace: "/work/v17",
		}, nil
	})
	resolverB := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		return acceptOnStubHolder(false), Route{
			Kind:      "daemon",
			PID:       6001,
			Session:   "/work/v17/transcript.jsonl",
			Workspace: "/work/v17",
		}, nil
	})

	// Tunnel A: viewer A never reads from its channel after sending hello.
	vConnA, hConnA := net.Pipe()
	t.Cleanup(func() {
		_ = vConnA.Close()
		_ = hConnA.Close()
	})
	guard.Go("livedoor.test.v17.serveA", func() {
		_ = Serve(ctx, hConnA, Peer{
			Device: "dev-a",
			Label:  "slow-viewer",
			Permit: pair.Permit{Watch: true, Drive: true},
		}, ServeConfig{
			Resolver: resolverA,
			Limits:   lim,
			Host:     "workstation",
			Road:     "relay",
		})
	})
	vMuxA := NewMux(vConnA, Viewer, MuxConfig{Limits: lim})
	t.Cleanup(func() { _ = vMuxA.Close() })
	if _, err := vMuxA.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck(A) failed: %v", err)
	}
	vChA, err := vMuxA.Open(ctx, "chat-v17")
	if err != nil {
		t.Fatalf("Open(A) failed: %v", err)
	}

	// Tunnel B: viewer B reads all events at full rate.
	vConnB, hConnB := net.Pipe()
	t.Cleanup(func() {
		_ = vConnB.Close()
		_ = hConnB.Close()
	})
	guard.Go("livedoor.test.v17.serveB", func() {
		_ = Serve(ctx, hConnB, Peer{
			Device: "dev-b",
			Label:  "fast-viewer",
			Permit: pair.Permit{Watch: true, Drive: true},
		}, ServeConfig{
			Resolver: resolverB,
			Limits:   lim,
			Host:     "workstation",
			Road:     "relay",
		})
	})
	vMuxB := NewMux(vConnB, Viewer, MuxConfig{Limits: lim})
	t.Cleanup(func() { _ = vMuxB.Close() })
	if _, err := vMuxB.WaitHelloAck(ctx); err != nil {
		t.Fatalf("WaitHelloAck(B) failed: %v", err)
	}
	vChB, err := vMuxB.Open(ctx, "chat-v17")
	if err != nil {
		t.Fatalf("Open(B) failed: %v", err)
	}

	hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
	hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
	if _, err := vChA.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("Write hello(A) failed: %v", err)
	}
	if _, err := vChB.Write(append(hLine, '\n')); err != nil {
		t.Fatalf("Write hello(B) failed: %v", err)
	}

	scanB := newLineScanner(vChB, lim.MaxLine)
	if _, err := scanB.ReadLine(lim.MaxLine); err != nil {
		t.Fatalf("B reading welcome failed: %v", err)
	}
	<-bothAttached

	// One shared stub holder fans out the stream to both A and B.
	aClosedAck := make(chan struct{})
	guard.Go("livedoor.test.v17.sharedHolderFanout", func() {
		defer func() {
			_ = holderSideA.Close()
			_ = holderSideB.Close()
		}()
		aAlive := true
		for seq := 1; seq <= totalLines; seq++ {
			// Pace the producer against B's consumer so a loaded scheduler cannot
			// starve B's reader goroutine past the 32 KiB outbound queue while A
			// never reads and still overflows its 48 KiB window-plus-queue cap.
			paceDeadline := time.Now().Add(3 * time.Second)
			for int32(seq)-bRead.Load() > 4 {
				if ctx.Err() != nil || time.Now().After(paceDeadline) {
					return
				}
				runtime.Gosched()
			}
			evPayload, _ := json.Marshal(remote.WireEvent(session.Event{
				Kind: session.EventTextDelta,
				Text: payloadText,
			}))
			evFrame, _ := json.Marshal(remote.Frame{
				Kind:    "event",
				ID:      1,
				Seq:     uint64(seq),
				Payload: evPayload,
			})
			raw := append(evFrame, '\n')
			if aAlive {
				aWriteAttempts.Add(1)
				if _, err := holderSideA.Write(raw); err != nil {
					aAlive = false
					aHolderClosed.Store(true)
					close(aClosedAck)
				}
			}
			bWriteAttempts.Add(1)
			if _, err := holderSideB.Write(raw); err != nil {
				return
			}
			for i := 0; i < 8; i++ {
				runtime.Gosched()
			}
		}
		if aAlive {
			var drain [64]byte
			_, _ = holderSideA.Read(drain[:])
			aHolderClosed.Store(true)
			close(aClosedAck)
		}
	})

	for seq := 1; seq <= totalLines; seq++ {
		line, err := scanB.ReadLine(lim.MaxLine)
		if err != nil {
			t.Fatalf("B reading event %d failed: %v", seq, err)
		}
		var f remote.Frame
		if err := json.Unmarshal(bytes.TrimRight(line, "\r\n"), &f); err != nil || f.Seq != uint64(seq) {
			t.Fatalf("B event %d = %+v (err=%v), want Seq=%d", seq, f, err, seq)
		}
		bRead.Store(int32(seq))
	}

	<-vChA.Done()
	if got := vChA.Reason(); got != ReasonLagging {
		t.Fatalf("lagging viewer A channel Reason() = %q, want %q", got, ReasonLagging)
	}
	var buf [64]byte
	_, readErrA := vChA.Read(buf[:])
	if got := reasonOf(readErrA); got != ReasonLagging {
		t.Fatalf("vChA.Read after lagging reset returned %v (reason=%q), want %q", readErrA, got, ReasonLagging)
	}
	<-aClosedAck
	if !aHolderClosed.Load() {
		t.Fatal("holder conn for lagging viewer A was not closed")
	}
	if gotB := bWriteAttempts.Load(); gotB != totalLines {
		t.Fatalf("bWriteAttempts = %d, want %d", gotB, totalLines)
	}
	if gotA := aWriteAttempts.Load(); gotA >= totalLines {
		t.Fatalf("aWriteAttempts = %d, want < %d (A holder conn must close before stream ends)", gotA, totalLines)
	}
}

func makeFrameLineOfExactLength(t *testing.T, kind, method string, targetLen int) []byte {
	t.Helper()
	baseFrame := remote.Frame{
		Kind:    kind,
		ID:      1,
		Method:  method,
		Payload: json.RawMessage(`""`),
	}
	baseBytes, err := json.Marshal(baseFrame)
	if err != nil {
		t.Fatalf("Marshal baseFrame failed: %v", err)
	}
	padLen := targetLen - len(baseBytes)
	if padLen < 0 {
		t.Fatalf("targetLen %d smaller than base frame %d", targetLen, len(baseBytes))
	}
	paddedPayload, err := json.Marshal(strings.Repeat("p", padLen))
	if err != nil {
		t.Fatalf("Marshal paddedPayload failed: %v", err)
	}
	fullFrame := remote.Frame{
		Kind:    kind,
		ID:      1,
		Method:  method,
		Payload: paddedPayload,
	}
	line, err := json.Marshal(fullFrame)
	if err != nil {
		t.Fatalf("Marshal fullFrame failed: %v", err)
	}
	if len(line) != targetLen {
		t.Fatalf("constructed line length = %d, want %d", len(line), targetLen)
	}
	return append(line, '\n')
}

// TestALineOverTheCapResetsTheChannelInBothDirections verifies contract V18:
// a line of exactly MaxLine bytes passes in both directions, while a viewer
// line of MaxLine+1 bytes and a holder line of MaxLine+1 bytes each reset the
// channel with frame-too-large.
func TestALineOverTheCapResetsTheChannelInBothDirections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lim := DefaultLimits()
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20

	// Holder echoes allowed calls or emits a line of a requested length.
	nextHolderLineLen := make(chan int, 4)
	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v18.holder", func() {
			defer holderSide.Close()
			scan := newLineScanner(holderSide, lim.MaxLine+64)
			line, err := scan.ReadLine(lim.FirstLineMax)
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
			if _, err := holderSide.Write(append(wFrame, '\n')); err != nil {
				return
			}

			for {
				cLine, err := scan.ReadLine(lim.MaxLine + 64)
				if err != nil {
					return
				}
				var call remote.Frame
				if json.Unmarshal(bytes.TrimRight(cLine, "\r\n"), &call) != nil {
					return
				}
				replyLen := len(cLine) - 1
				select {
				case custom := <-nextHolderLineLen:
					replyLen = custom
				default:
				}
				outLine := makeFrameLineOfExactLength(t, "result", "", replyLen)
				if _, err := holderSide.Write(outLine); err != nil {
					return
				}
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       6002,
			Session:   "/work/v18/transcript.jsonl",
			Workspace: "/work/v18",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	guard.Go("livedoor.test.v18.serve", func() {
		_ = Serve(ctx, hConn, Peer{
			Device: "dev-v18",
			Label:  "laptop",
			Permit: pair.Permit{Watch: true, Drive: true},
		}, ServeConfig{
			Resolver: resolver,
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

	openWithHello := func(chat string) (*Channel, *lineScanner) {
		ch, err := vMux.Open(ctx, chat)
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", chat, err)
		}
		hPayload, _ := json.Marshal(remote.Hello{Version: remote.Version})
		hLine, _ := json.Marshal(remote.Frame{Kind: "hello", Payload: hPayload})
		if _, err := ch.Write(append(hLine, '\n')); err != nil {
			t.Fatalf("Write hello(%s) failed: %v", chat, err)
		}
		sc := newLineScanner(ch, lim.MaxLine+64)
		if _, err := sc.ReadLine(lim.MaxLine); err != nil {
			t.Fatalf("Read welcome(%s) failed: %v", chat, err)
		}
		return ch, sc
	}

	// 1. A line of exactly MaxLine bytes passes in both directions (viewer -> holder
	// and holder -> viewer).
	ch1, sc1 := openWithHello("chat-exact-cap")
	exactCall := makeFrameLineOfExactLength(t, "call", remote.MethodPing, MaxLine)
	if _, err := ch1.Write(exactCall); err != nil {
		t.Fatalf("writing MaxLine viewer call failed: %v", err)
	}
	exactReply, err := sc1.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading MaxLine holder reply failed: %v", err)
	}
	if gotLen := len(bytes.TrimRight(exactReply, "\r\n")); gotLen != MaxLine {
		t.Fatalf("exact MaxLine reply length = %d, want %d", gotLen, MaxLine)
	}

	// 2. A viewer line of MaxLine+1 bytes resets the channel with frame-too-large.
	overViewerCall := makeFrameLineOfExactLength(t, "call", remote.MethodPing, MaxLine+1)
	_, _ = ch1.Write(overViewerCall)
	_, err = sc1.ReadLine(MaxLine + 64)
	if got := reasonOf(err); got != ReasonFrameTooLarge {
		t.Fatalf("viewer MaxLine+1 line ended channel with %v (reason=%q), want %q", err, got, ReasonFrameTooLarge)
	}

	// 3. A holder line of MaxLine+1 bytes resets the channel with frame-too-large.
	ch2, sc2 := openWithHello("chat-holder-over-cap")
	nextHolderLineLen <- MaxLine + 1
	smallCall := makeFrameLineOfExactLength(t, "call", remote.MethodPing, 128)
	if _, err := ch2.Write(smallCall); err != nil {
		t.Fatalf("writing trigger call failed: %v", err)
	}
	_, err = sc2.ReadLine(MaxLine + 64)
	if got := reasonOf(err); got != ReasonFrameTooLarge {
		t.Fatalf("holder MaxLine+1 line ended channel with %v (reason=%q), want %q", err, got, ReasonFrameTooLarge)
	}
}

// TestAFirstLineOverTheLimitIsRefused verifies contract V18: a viewer's first
// line on a channel of FirstLineMax+1 bytes resets the channel with
// frame-too-large and closes the holder connection before anything is written
// to the holder.
func TestAFirstLineOverTheLimitIsRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lim := DefaultLimits()
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20

	var holderReadBytes atomic.Int64
	holderClosed := make(chan struct{})

	resolver := fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.v18.firstline.holder", func() {
			defer close(holderClosed)
			defer holderSide.Close()
			var buf [1024]byte
			for {
				n, err := holderSide.Read(buf[:])
				if n > 0 {
					holderReadBytes.Add(int64(n))
				}
				if err != nil {
					return
				}
			}
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       6003,
			Session:   "/work/v18-first/transcript.jsonl",
			Workspace: "/work/v18-first",
		}, nil
	})

	vConn, hConn := net.Pipe()
	t.Cleanup(func() {
		_ = vConn.Close()
		_ = hConn.Close()
	})
	guard.Go("livedoor.test.v18.firstline.serve", func() {
		_ = Serve(ctx, hConn, Peer{
			Device: "dev-firstline",
			Label:  "laptop",
			Permit: pair.Permit{Watch: true, Drive: true},
		}, ServeConfig{
			Resolver: resolver,
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

	vCh, err := vMux.Open(ctx, "chat-oversize-hello")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	oversizeHello := makeFrameLineOfExactLength(t, "hello", "", FirstLineMax+1)
	_, _ = vCh.Write(oversizeHello)

	var buf [64]byte
	_, readErr := vCh.Read(buf[:])
	if got := reasonOf(readErr); got != ReasonFrameTooLarge {
		t.Fatalf("oversize first line readErr = %v (reason=%q), want %q", readErr, got, ReasonFrameTooLarge)
	}
	<-holderClosed
	if got := holderReadBytes.Load(); got != 0 {
		t.Fatalf("holder received %d bytes on oversize first line, want 0", got)
	}
}
