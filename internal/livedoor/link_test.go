package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// countingRWC wraps an io.ReadWriteCloser and counts every byte and write call
// sent by the viewer mux onto the underlying tunnel.
type countingRWC struct {
	rwc     io.ReadWriteCloser
	writes  atomic.Int64
	written atomic.Int64
	closed  atomic.Bool
	closeCh chan struct{}
	once    sync.Once
}

func newCountingRWC(rwc io.ReadWriteCloser) *countingRWC {
	return &countingRWC{
		rwc:     rwc,
		closeCh: make(chan struct{}),
	}
}

func (c *countingRWC) Read(p []byte) (int, error) {
	return c.rwc.Read(p)
}

func (c *countingRWC) Write(p []byte) (int, error) {
	n, err := c.rwc.Write(p)
	if n > 0 {
		c.writes.Add(1)
		c.written.Add(int64(n))
	}
	return n, err
}

func (c *countingRWC) Close() error {
	c.closed.Store(true)
	c.once.Do(func() {
		close(c.closeCh)
	})
	return c.rwc.Close()
}

func (c *countingRWC) ResetCounts() {
	c.writes.Store(0)
	c.written.Store(0)
}

// startStubHostResolver returns a Resolver backed by the hand-made stub holder
// pattern from serve_test.go so Serve can accept viewer channels over net.Pipe.
func startStubHostResolver(t *testing.T) Resolver {
	t.Helper()
	return fakeResolverFunc(func(_ context.Context, chat string) (net.Conn, Route, error) {
		routerSide, holderSide := net.Pipe()
		guard.Go("livedoor.test.link.stubHolder", func() {
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
				Model:       "test-model",
				Remote:      h.Remote,
			})
			wFrame, _ := json.Marshal(remote.Frame{
				Kind:    "welcome",
				Payload: wPayload,
			})
			_, _ = holderSide.Write(append(wFrame, '\n'))
			_, _ = io.Copy(io.Discard, holderSide)
		})
		return routerSide, Route{
			Kind:      "daemon",
			PID:       7001,
			Session:   "/work/p/" + chat + "/transcript.jsonl",
			Workspace: "/work/p",
		}, nil
	})
}

// nextPendingTimerWait returns the duration from fc.Now() until the earliest
// pending timer on fc.
func nextPendingTimerWait(t *testing.T, fc *FakeClock) time.Duration {
	t.Helper()
	fc.WaitForTimers(1)
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.pending) == 0 {
		t.Fatal("expected at least 1 pending timer on FakeClock, found 0")
	}
	best := fc.pending[0]
	for _, tm := range fc.pending[1:] {
		if tm.at.Before(best.at) {
			best = tm
		}
	}
	return best.at.Sub(fc.now)
}

// TestAnActiveViewerSaysTheHostIsGoneWithinTheDeadline verifies contract V7a:
// an active view (Touch every virtual second) whose host stops answering stays
// Live at +8.9 s, transitions to WentOffline past +9 s, closes the tunnel, and
// schedules a pending redial timer.
func TestAnActiveViewerSaysTheHostIsGoneWithinTheDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		mutePings  atomic.Bool
		firstConn  *countingRWC
		dialCalled atomic.Int32
	)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			n := dialCalled.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			counted := newCountingRWC(vSide)
			if n == 1 {
				firstConn = counted
			}

			// Host frame loop answers HELLO_ACK and OPEN, and answers PING only
			// while mutePings is false; once muted, it drains and discards frames
			// without ever replying so the host is silently gone.
			guard.Go("livedoor.test.v7a.host", func() {
				defer hSide.Close()
				ackPayload, _ := json.Marshal(HelloAck{
					V:      1,
					Watch:  true,
					Drive:  true,
					Road:   "relay",
					Limits: DefaultLimits(),
				})
				_, _ = writeFrame(hSide, frame{
					op:      OpHelloAck,
					channel: 0,
					payload: ackPayload,
				}, ChunkBytes)

				for {
					f, err := readFrame(hSide, ChunkBytes, nil)
					if err != nil {
						return
					}
					if mutePings.Load() {
						continue
					}
					switch f.op {
					case OpOpen:
						_, _ = writeFrame(hSide, frame{
							op:      OpOpened,
							channel: f.channel,
						}, ChunkBytes)
					case OpPing:
						_, _ = writeFrame(hSide, frame{
							op:      OpPong,
							channel: 0,
							payload: f.payload,
						}, ChunkBytes)
					}
				}
			})
			return counted, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-v7a")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-v7a) failed: %v", err)
	}
	defer ch.Close()

	if got := link.State().Kind; got != Live {
		t.Fatalf("initial State().Kind = %v, want %v", got, Live)
	}

	// Host stops answering right after the channel opens; the person acts every
	// virtual second.
	mutePings.Store(true)
	link.Touch()

	for sec := 1; sec <= 8; sec++ {
		fc.Advance(time.Second)
		eventually(t, "liveness timer to be armed", func() bool {
			return fc.BlockedTimers() >= 1
		})
		link.Touch()
		if got := link.State().Kind; got != Live {
			t.Fatalf("at +%ds State().Kind = %v, want %v", sec, got, Live)
		}
	}

	// At +8.9 s the link must still be Live and the tunnel still open.
	fc.Advance(900 * time.Millisecond)
	if got := link.State().Kind; got != Live {
		t.Fatalf("at +8.9s State().Kind = %v, want %v", got, Live)
	}
	if firstConn.closed.Load() {
		t.Fatal("at +8.9s the tunnel was already closed, want still open")
	}

	// Past +9 s (PingEvery 3s + DeadAfter 6s), State is WentOffline, the tunnel
	// has been closed, and a pending redial timer exists on the fake clock.
	fc.Advance(200 * time.Millisecond)
	eventually(t, "past +9s State().Kind to become WentOffline", func() bool {
		return link.State().Kind == WentOffline
	})
	eventually(t, "past +9s the tunnel to be closed", firstConn.closed.Load)
	eventually(t, "past +9s a pending redial timer to be scheduled", func() bool {
		return fc.BlockedTimers() >= 1
	})
}

// TestAnIdleViewerSendsNothingAndLearnsFromTheCloseOrThePresenceSignal verifies
// contract V7b and V15: an idle foreground view (no Touch, not Active) writes
// zero bytes across 10 virtual minutes minus a second, has zero pending timers
// and zero writes when idle with no channels, transitions to WentOffline
// immediately when the host closes the tunnel, and sends exactly one PING on a
// Touch after a quiet spell.
func TestAnIdleViewerSendsNothingAndLearnsFromTheCloseOrThePresenceSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		curCounted *countingRWC
		curHostEnd net.Conn
	)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			counted := newCountingRWC(vSide)
			curCounted = counted
			curHostEnd = hSide

			hMux := NewMux(hSide, Host, MuxConfig{})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{
				V:      1,
				Watch:  true,
				Drive:  true,
				Road:   "relay",
				Limits: DefaultLimits(),
			})
			guard.Go("livedoor.test.v7b.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
				}
			})
			return counted, nil
		},
	})
	t.Cleanup(link.Close)

	// 1. Open and then close a channel so the link is idle with no channels:
	// verify zero timers pending and zero writes while idle with no channels.
	ch0, err := link.OpenerContext("chat-temp")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-temp) failed: %v", err)
	}
	_ = ch0.Close()
	eventually(t, "idle timer teardown and channel close flush", func() bool {
		return fc.BlockedTimers() == 0 && curCounted.writes.Load() >= 2
	})
	curCounted.ResetCounts()
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers() while idle with no channels = %d, want 0", got)
	}
	fc.Advance(5 * time.Minute)
	if got := curCounted.written.Load(); got != 0 {
		t.Fatalf("bytes written while idle with no channels = %d, want 0", got)
	}

	// 2. Open a foreground channel and leave it idle (no Touch, not Active) for
	// 10 virtual minutes minus a second: assert ZERO bytes written.
	ch1, err := link.OpenerContext("chat-idle")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-idle) failed: %v", err)
	}
	defer ch1.Close()

	// Wait for the open frame to flush and the single idle detach timer to arm, then zero the counter.
	eventually(t, "open frame flush and idle detach timer arm", func() bool {
		return fc.BlockedTimers() == 1 && curCounted.writes.Load() >= 1
	})
	curCounted.ResetCounts()

	fc.Advance(IdleDetach - time.Second)
	if got := link.State().Kind; got != Live {
		t.Fatalf("at 9m59s State().Kind = %v, want %v", got, Live)
	}
	if got := curCounted.written.Load(); got != 0 {
		t.Fatalf("bytes written during 9m59s idle spell = %d, want 0", got)
	}

	// 3. A Touch() after a quiet spell sends exactly one PING.
	link.Touch()
	eventually(t, "probe PING write after Touch()", func() bool {
		return curCounted.writes.Load() >= 1
	})
	if writes := curCounted.writes.Load(); writes != 1 {
		t.Fatalf("writes after Touch() following quiet spell = %d, want 1", writes)
	}
	const pingFrameSize = frameHeaderBytes + 4
	if bytesWritten := curCounted.written.Load(); bytesWritten != pingFrameSize {
		t.Fatalf("bytes written by probe PING = %d, want %d", bytesWritten, pingFrameSize)
	}

	// Wait for the host's PONG to arrive and advance past ActiveWindow (30s) so
	// the link returns to an idle posture before testing an immediate host close.
	eventually(t, "host PONG to be recorded by the viewer link", func() bool {
		link.mu.Lock()
		defer link.mu.Unlock()
		return link.probeDeadline.IsZero() && link.lastHeard.Equal(fc.Now())
	})
	fc.Advance(ActiveWindow + time.Second)
	eventually(t, "active window expiry to re-arm single idle detach timer", func() bool {
		return fc.BlockedTimers() == 1
	})
	curCounted.ResetCounts()

	// 4. When the host closes the tunnel while the viewer is idle, State becomes
	// WentOffline immediately with no 3s wait.
	_ = curHostEnd.Close()
	eventually(t, "State().Kind to become WentOffline after host closed tunnel", func() bool {
		return link.State().Kind == WentOffline
	})
	if got := curCounted.written.Load(); got != 0 {
		t.Fatalf("bytes written when host closed idle tunnel = %d, want 0", got)
	}
}

// TestAViewerIdleForTenMinutesDetachesWithOneCloseAndKeepsItsView verifies
// contract V24: an idle viewer with open channels is still Live at 9m59s,
// detaches at 10m01s with State Idle, closes the tunnel with zero bytes written
// during the idle spell and at detach, reports reason "idle" to OnChannelEnd
// once for every open channel, and reconnects on a later Touch().
func TestAViewerIdleForTenMinutesDetachesWithOneCloseAndKeepsItsView(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	resolver := startStubHostResolver(t)
	peer := Peer{
		Device: "dev-v24",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}

	var (
		dialCount  atomic.Int32
		firstCount *countingRWC
	)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			n := dialCount.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			counted := newCountingRWC(vSide)
			if n == 1 {
				firstCount = counted
			}
			guard.Go("livedoor.test.v24.serve", func() {
				_ = Serve(ctx, hSide, peer, ServeConfig{
					Resolver: resolver,
					Host:     "Desktop",
					Road:     "relay",
				})
			})
			return counted, nil
		},
	})
	t.Cleanup(link.Close)

	var (
		endMu      sync.Mutex
		endedChats = make(map[string][]string)
	)
	link.OnChannelEnd(func(chat, reason string) {
		endMu.Lock()
		endedChats[chat] = append(endedChats[chat], reason)
		endMu.Unlock()
	})

	ch1, err := link.OpenerContext("chat-a")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-a) failed: %v", err)
	}
	ch2, err := link.OpenerContext("chat-b")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-b) failed: %v", err)
	}
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount for two chats on one Link = %d, want 1", got)
	}

	eventually(t, "open frames to flush and idle detach timer to arm", func() bool {
		return fc.BlockedTimers() == 1 && firstCount.writes.Load() >= 2
	})
	firstCount.ResetCounts()

	// Boundary 9m59s: still Live, tunnel open, zero bytes written.
	fc.Advance(9*time.Minute + 59*time.Second)
	if got := link.State().Kind; got != Live {
		t.Fatalf("at 9m59s State().Kind = %v, want %v", got, Live)
	}
	if firstCount.closed.Load() {
		t.Fatal("at 9m59s tunnel was already closed")
	}
	if got := firstCount.written.Load(); got != 0 {
		t.Fatalf("at 9m59s bytes written = %d, want 0", got)
	}

	// Boundary 10m01s: State Idle, tunnel closed, zero bytes written at detach,
	// and every channel ended with reason "idle".
	fc.Advance(2 * time.Second)
	eventually(t, "at 10m01s State().Kind to become Idle", func() bool {
		return link.State().Kind == Idle
	})
	if got := link.State().Reason; got != ReasonIdle {
		t.Fatalf("at 10m01s State().Reason = %q, want %q", got, ReasonIdle)
	}
	eventually(t, "the tunnel to be closed on idle detach at 10m01s", firstCount.closed.Load)
	if got := firstCount.written.Load(); got != 0 {
		t.Fatalf("bytes written during idle spell and detach = %d, want 0 (one close is the whole cost)", got)
	}
	eventually(t, "no timers to be pending after idle detach", func() bool { return fc.BlockedTimers() == 0 })

	eventually(t, "both channels to report the idle end", func() bool {
		endMu.Lock()
		defer endMu.Unlock()
		return len(endedChats["chat-a"]) > 0 && len(endedChats["chat-b"]) > 0
	})
	endMu.Lock()
	gotA := append([]string(nil), endedChats["chat-a"]...)
	gotB := append([]string(nil), endedChats["chat-b"]...)
	endMu.Unlock()
	if len(gotA) != 1 || gotA[0] != ReasonIdle {
		t.Fatalf("OnChannelEnd for chat-a = %v, want [%q]", gotA, ReasonIdle)
	}
	if len(gotB) != 1 || gotB[0] != ReasonIdle {
		t.Fatalf("OnChannelEnd for chat-b = %v, want [%q]", gotB, ReasonIdle)
	}

	var buf [32]byte
	_, err1 := ch1.Read(buf[:])
	if r, ok := ReasonOf(err1); !ok || r != ReasonIdle {
		t.Fatalf("ReasonOf(ch1.Read) = (%q, %v) err=%v, want (%q, true)", r, ok, err1, ReasonIdle)
	}
	_, err2 := ch2.Read(buf[:])
	if r, ok := ReasonOf(err2); !ok || r != ReasonIdle {
		t.Fatalf("ReasonOf(ch2.Read) = (%q, %v) err=%v, want (%q, true)", r, ok, err2, ReasonIdle)
	}

	// A later Touch() reconnects the link.
	link.Touch()
	if got := link.State().Kind; got != Live {
		t.Fatalf("State().Kind after Touch() = %v, want %v", got, Live)
	}
	if got := dialCount.Load(); got != 2 {
		t.Fatalf("dialCount after Touch() reconnect = %d, want 2", got)
	}
}

// TestARunningTurnKeepsTheLinkActive verifies contract V24: when cfg.Active
// returns true, the link does not detach after 30 virtual minutes and continues
// sending liveness pings every 3 seconds (PingEvery).
func TestARunningTurnKeepsTheLinkActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		hostPings atomic.Int64
		counted   *countingRWC
	)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Active:   func() bool { return true },
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			counted = newCountingRWC(vSide)
			hMux := NewMux(hSide, Host, MuxConfig{
				OnFrameRead: func(n int) {
					if n == frameHeaderBytes+4 {
						hostPings.Add(1)
					}
				},
			})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{
				V:      1,
				Watch:  true,
				Drive:  true,
				Road:   "relay",
				Limits: DefaultLimits(),
			})
			guard.Go("livedoor.test.v24active.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
				}
			})
			return counted, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-turn")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-turn) failed: %v", err)
	}
	defer ch.Close()

	eventually(t, "open frame to flush and active liveness timer to arm", func() bool {
		return fc.BlockedTimers() == 1 && counted.writes.Load() >= 1
	})
	counted.ResetCounts()
	hostPings.Store(0)

	steps := int((30 * time.Minute) / PingEvery)
	for i := 1; i <= steps; i++ {
		fc.Advance(PingEvery)
		wantHeard := fc.Now()
		// Wait for the ping-pong round trip over net.Pipe and the next liveness
		// timer registration to complete at this virtual timestamp before
		// advancing the fake clock again.
		eventually(t, fmt.Sprintf("ping #%d round trip", i), func() bool {
			if counted.writes.Load() < int64(i) || hostPings.Load() < int64(i) || fc.BlockedTimers() < 1 {
				return false
			}
			link.mu.Lock()
			heard := link.lastHeard
			link.mu.Unlock()
			return heard.Equal(wantHeard)
		})
		if got := link.State().Kind; got != Live {
			t.Fatalf("at step %d (%v) State().Kind = %v, want %v", i, time.Duration(i)*PingEvery, got, Live)
		}
	}

	if got := counted.writes.Load(); got != int64(steps) {
		t.Fatalf("after 30 virtual minutes writes = %d, want %d pings (one every %v)", got, steps, PingEvery)
	}
}

// TestTheFirstRetryAfterADropWaitsAJitteredSecondAndThenDoubles verifies
// contract (reconnect): after an active tunnel drops, the first retry wait is
// within [1 s, 6 s) and subsequent retries wait within [0.75, 1.25) x doubled
// (2 s, 4 s, 8 s, 15 s cap) on the fake clock.
func TestTheFirstRetryAfterADropWaitsAJitteredSecondAndThenDoubles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		dialCount atomic.Int32
		allowDial atomic.Bool
		hostConn  net.Conn
	)
	allowDial.Store(true)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Active:   func() bool { return true },
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dialCount.Add(1)
			if !allowDial.Load() {
				return nil, errors.New("connection refused")
			}
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			hostConn = hSide
			hMux := NewMux(hSide, Host, MuxConfig{})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{
				V:      1,
				Watch:  true,
				Drive:  true,
				Road:   "relay",
				Limits: DefaultLimits(),
			})
			guard.Go("livedoor.test.backoff.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
				}
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-roam")(ctx)
	if err != nil {
		t.Fatalf("initial OpenerContext failed: %v", err)
	}
	defer ch.Close()

	if got := dialCount.Load(); got != 1 {
		t.Fatalf("initial dialCount = %d, want 1", got)
	}

	// Drop the active tunnel and keep dials failing through the backoff ramp.
	allowDial.Store(false)
	_ = hostConn.Close()

	// Wait for the closed pipe to be observed and State to transition to Reconnecting.
	eventually(t, "State().Kind to become Reconnecting after active tunnel drop", func() bool {
		return link.State().Kind == Reconnecting
	})
	wait1 := nextPendingTimerWait(t, fc)
	if wait1 < time.Second || wait1 >= 6*time.Second {
		t.Fatalf("first retry wait = %v, want in [1s, 6s)", wait1)
	}

	// Advance to 1ms before wait1: dial must not have fired yet.
	fc.Advance(wait1 - time.Millisecond)
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount 1ms before first retry = %d, want 1", got)
	}
	// Advance past wait1: first retry fires (dialCount -> 2) and schedules wait2.
	fc.Advance(2 * time.Millisecond)
	eventually(t, "dialCount to reach 2 at first retry", func() bool {
		return dialCount.Load() == 2
	})

	expectedDoubled := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		15 * time.Second,
		15 * time.Second,
	}
	for i, base := range expectedDoubled {
		low := time.Duration(float64(base) * 0.75)
		high := time.Duration(float64(base) * 1.25)
		wait := nextPendingTimerWait(t, fc)
		if wait < low || wait >= high {
			t.Fatalf("retry #%d (base %v) wait = %v, want in [%v, %v)", i+2, base, wait, low, high)
		}

		if i == len(expectedDoubled)-1 {
			allowDial.Store(true)
		}
		before := dialCount.Load()
		fc.Advance(wait - time.Millisecond)
		if got := dialCount.Load(); got != before {
			t.Fatalf("retry #%d fired early at %v", i+2, wait-time.Millisecond)
		}
		fc.Advance(2 * time.Millisecond)
		eventually(t, fmt.Sprintf("retry #%d to fire at %v", i+2, wait), func() bool {
			return dialCount.Load() == before+1
		})
	}

	eventually(t, "State().Kind to become Live after successful retry", func() bool {
		return link.State().Kind == Live
	})
}

// TestALaggingChannelReopensAtOnceWithNoWaitAndTheTunnelStays verifies contract
// (reconnect exception): when a single channel ends with reason "lagging" (or
// "permit-changed"), the tunnel stays open with zero redial wait and the very
// next Opener call opens a new channel on the same tunnel immediately.
func TestALaggingChannelReopensAtOnceWithNoWaitAndTheTunnelStays(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var (
		dialCount  atomic.Int32
		acceptStep atomic.Int32
		hostCh1Ch  = make(chan *Channel, 1)
	)

	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dialCount.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			hMux := NewMux(hSide, Host, MuxConfig{})
			t.Cleanup(func() { _ = hMux.Close() })
			_ = hMux.SendHelloAck(HelloAck{
				V:      1,
				Watch:  true,
				Drive:  true,
				Road:   "relay",
				Limits: DefaultLimits(),
			})
			guard.Go("livedoor.test.lagging.accept", func() {
				for {
					ch, _, err := hMux.Accept(ctx)
					if err != nil {
						return
					}
					_ = ch.Accept()
					if acceptStep.Add(1) == 1 {
						hostCh1Ch <- ch
					}
				}
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	endedReason := make(chan string, 1)
	link.OnChannelEnd(func(chat, reason string) {
		endedReason <- reason
	})

	ch1, err := link.OpenerContext("chat-lag")(ctx)
	if err != nil {
		t.Fatalf("first OpenerContext(chat-lag) failed: %v", err)
	}
	hostCh1 := <-hostCh1Ch

	// Host resets channel 1 with ReasonLagging.
	if err := hostCh1.Reset(ReasonLagging); err != nil {
		t.Fatalf("hostCh1.Reset(ReasonLagging) failed: %v", err)
	}

	var buf [32]byte
	_, readErr := ch1.Read(buf[:])
	if r, ok := ReasonOf(readErr); !ok || r != ReasonLagging {
		t.Fatalf("ReasonOf(ch1.Read) = (%q, %v), want (%q, true)", r, ok, ReasonLagging)
	}
	if got := <-endedReason; got != ReasonLagging {
		t.Fatalf("OnChannelEnd reason = %q, want %q", got, ReasonLagging)
	}

	// The tunnel must still be Live and the next Opener call must succeed at
	// once without advancing the clock or opening a second tunnel.
	if got := link.State().Kind; got != Live {
		t.Fatalf("State().Kind after lagging channel reset = %v, want %v", got, Live)
	}
	ch2, err := link.OpenerContext("chat-lag")(ctx)
	if err != nil {
		t.Fatalf("second OpenerContext(chat-lag) failed: %v", err)
	}
	defer ch2.Close()

	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount after reopening lagging channel = %d, want 1 (same tunnel)", got)
	}
}

// TestRemovedStopsTheRetriesForGood verifies contract (reconnect exception):
// when the host revokes the viewer with reason "removed", State becomes Revoked
// with Reason "removed", all retries stop for good, and subsequent Opener calls
// fail with ReasonRemoved without dialling again.
func TestRemovedStopsTheRetriesForGood(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	reg := NewRegistry()
	resolver := startStubHostResolver(t)
	peer := Peer{
		Device: "dev-removed",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: true},
	}

	var dialCount atomic.Int32
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dialCount.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			guard.Go("livedoor.test.removed.serve", func() {
				_ = Serve(ctx, hSide, peer, ServeConfig{
					Resolver: resolver,
					Registry: reg,
					Host:     "Desktop",
					Road:     "relay",
				})
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	ch, err := link.OpenerContext("chat-revoked")(ctx)
	if err != nil {
		t.Fatalf("OpenerContext(chat-revoked) failed: %v", err)
	}

	reg.Revoke(peer.Device)

	var buf [32]byte
	_, readErr := ch.Read(buf[:])
	if r, ok := ReasonOf(readErr); !ok || r != ReasonRemoved {
		t.Fatalf("ReasonOf(ch.Read) after Revoke = (%q, %v), want (%q, true)", r, ok, ReasonRemoved)
	}

	eventually(t, "State() to become Revoked with no blocked timers after Revoke", func() bool {
		return link.State().Kind == Revoked && fc.BlockedTimers() == 0
	})
	st := link.State()
	if st.Kind != Revoked || st.Reason != ReasonRemoved {
		t.Fatalf("State() after Revoke = %+v, want Kind=%v Reason=%q", st, Revoked, ReasonRemoved)
	}
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers() after Revoke = %d, want 0 (retries must stop for good)", got)
	}

	fc.Advance(time.Hour)
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount after advancing 1h post-Revoke = %d, want 1", got)
	}

	_, openErr := link.OpenerContext("chat-revoked")(ctx)
	if r, ok := ReasonOf(openErr); !ok || r != ReasonRemoved {
		t.Fatalf("Opener after Revoke returned %v (ReasonOf=%q, %v), want %q", openErr, r, ok, ReasonRemoved)
	}
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount after Opener call post-Revoke = %d, want 1", got)
	}
}

// TestNoLiveDoorMeansTheHostIsOfflineAndIsNotPolled verifies contract
// (reconnect exception): when Dial returns an error wrapping ErrHostOffline,
// State transitions immediately to WentOffline and the redial timer goes
// straight to the 15 s cap without polling at 1 s, 2 s, 4 s, or 8 s.
func TestNoLiveDoorMeansTheHostIsOfflineAndIsNotPolled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	var dialCount atomic.Int32
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "Desktop",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			dialCount.Add(1)
			return nil, fmt.Errorf("relay 404: %w", ErrHostOffline)
		},
	})
	t.Cleanup(link.Close)

	_, err := link.OpenerContext("chat-offline")(ctx)
	if err == nil {
		t.Fatal("OpenerContext against offline host succeeded, want error")
	}
	if !errors.Is(err, ErrHostOffline) {
		t.Fatalf("OpenerContext error %v does not wrap ErrHostOffline", err)
	}
	if got := link.State().Kind; got != WentOffline {
		t.Fatalf("State().Kind after ErrHostOffline = %v, want %v", got, WentOffline)
	}

	// The scheduled wait must go straight to the 15s cap x [0.75, 1.25) =
	// [11.25s, 18.75s), never polling at 1s..10s.
	wait := nextPendingTimerWait(t, fc)
	low := time.Duration(float64(15*time.Second) * 0.75)
	high := time.Duration(float64(15*time.Second) * 1.25)
	if wait < low || wait >= high {
		t.Fatalf("redial wait after ErrHostOffline = %v, want in [%v, %v)", wait, low, high)
	}

	fc.Advance(10 * time.Second)
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dialCount after 10s = %d, want 1 (must not poll missing host)", got)
	}
}

// TestTheOpenerErrorSpeaksTheHostsWords verifies contract (words): when the
// host refuses a channel with "not-running" (or HELLO_ACK denies watch / sets
// watch-only), Opener returns a *ReasonError whose Error() equals
// chatlist.LiveSaid("not-running", HostName) and ReasonOf extracts the reason.
func TestTheOpenerErrorSpeaksTheHostsWords(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	// 1. Host with Drive:false sets State to WatchOnly with State.Permit, and a
	// chat that is not running refuses with ReasonNotRunning.
	peer := Peer{
		Device: "dev-words",
		Label:  "laptop",
		Permit: pair.Permit{Watch: true, Drive: false},
	}
	link := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "StudioMac",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			guard.Go("livedoor.test.words.serve", func() {
				_ = Serve(ctx, hSide, peer, ServeConfig{
					Resolver: nil, // nil resolver refuses every channel with ReasonNotRunning
					Host:     "StudioMac",
					Road:     "relay",
				})
			})
			return vSide, nil
		},
	})
	t.Cleanup(link.Close)

	_, err := link.Opener("missing-chat")()
	if err == nil {
		t.Fatal("Opener(missing-chat) succeeded, want refusal")
	}

	var re *ReasonError
	if !errors.As(err, &re) {
		t.Fatalf("Opener error type = %T (%v), want *ReasonError", err, err)
	}
	wantSaid := chatlist.LiveSaid(ReasonNotRunning, "StudioMac")
	if err.Error() != wantSaid {
		t.Fatalf("err.Error() = %q, want %q", err.Error(), wantSaid)
	}
	if reason, ok := ReasonOf(err); !ok || reason != ReasonNotRunning {
		t.Fatalf("ReasonOf(err) = (%q, %v), want (%q, true)", reason, ok, ReasonNotRunning)
	}

	st := link.State()
	if st.Kind != WatchOnly || !st.Permit.Watch || st.Permit.Drive {
		t.Fatalf("State() with drive:false HELLO_ACK = %+v, want Kind=WatchOnly Permit={Watch:true Drive:false}", st)
	}

	// 2. Host with Watch:false sets State to Revoked with reason "not-allowed"
	// and makes every Opener fail with "not-allowed".
	deniedPeer := Peer{
		Device: "dev-denied",
		Label:  "laptop",
		Permit: pair.Permit{Watch: false, Drive: false},
	}
	var deniedDials atomic.Int32
	deniedLink := NewLink(LinkConfig{
		Clock:    fc,
		HostName: "StudioMac",
		Dial: func(_ context.Context) (io.ReadWriteCloser, error) {
			deniedDials.Add(1)
			vSide, hSide := net.Pipe()
			t.Cleanup(func() {
				_ = vSide.Close()
				_ = hSide.Close()
			})
			guard.Go("livedoor.test.words.serveDenied", func() {
				_ = Serve(ctx, hSide, deniedPeer, ServeConfig{
					Host: "StudioMac",
					Road: "relay",
				})
			})
			return vSide, nil
		},
	})
	t.Cleanup(deniedLink.Close)

	_, err1 := deniedLink.Opener("any-chat")()
	if reason, ok := ReasonOf(err1); !ok || reason != ReasonNotAllowed {
		t.Fatalf("ReasonOf(err1) on watch:false = (%q, %v), want (%q, true)", reason, ok, ReasonNotAllowed)
	}
	if err1.Error() != chatlist.LiveSaid(ReasonNotAllowed, "StudioMac") {
		t.Fatalf("err1.Error() = %q, want %q", err1.Error(), chatlist.LiveSaid(ReasonNotAllowed, "StudioMac"))
	}
	if got := deniedLink.State(); got.Kind != Revoked || got.Reason != ReasonNotAllowed {
		t.Fatalf("deniedLink.State() = %+v, want Kind=Revoked Reason=%q", got, ReasonNotAllowed)
	}

	// A second Opener call must also fail with not-allowed without dialling again.
	_, err2 := deniedLink.Opener("other-chat")()
	if reason, ok := ReasonOf(err2); !ok || reason != ReasonNotAllowed {
		t.Fatalf("ReasonOf(err2) on second opener = (%q, %v), want (%q, true)", reason, ok, ReasonNotAllowed)
	}
	if got := deniedDials.Load(); got != 1 {
		t.Fatalf("deniedDials = %d, want 1", got)
	}
}

// TestEveryReasonConstantHasWordsInChatlist verifies contract (words
// consistency): every Reason* constant of the mux has a distinct, non-fallback
// chatlist.LiveSaid sentence, and chatlist.LiveIdle names the exact minutes of
// IdleDetach.
func TestEveryReasonConstantHasWordsInChatlist(t *testing.T) {
	const host = "Desktop"
	fallback := chatlist.LiveSaid("unknown-reason-sentinel", host)

	reasons := []string{
		ReasonNotRunning,
		ReasonNotAllowed,
		ReasonHolderGone,
		ReasonHolderOld,
		ReasonRemoved,
		ReasonPermitChanged,
		ReasonLagging,
		ReasonFrameTooLarge,
		ReasonBudget,
		ReasonIdle,
		ReasonHostClosing,
		ReasonTooMany,
		ReasonRemoteControlOff,
	}

	seen := make(map[string]string, len(reasons))
	for _, r := range reasons {
		said := chatlist.LiveSaid(r, host)
		if strings.TrimSpace(said) == "" {
			t.Fatalf("reason %q produced an empty sentence", r)
		}
		if said == fallback {
			t.Fatalf("reason %q fell back to the generic sentence %q", r, fallback)
		}
		if prev, dup := seen[said]; dup {
			t.Fatalf("reasons %q and %q share the same sentence %q", r, prev, said)
		}
		seen[said] = r
	}

	wantMinutes := fmt.Sprint(int(IdleDetach / time.Minute))
	if !strings.Contains(chatlist.LiveIdle, wantMinutes) {
		t.Fatalf("chatlist.LiveIdle = %q, does not contain IdleDetach minutes %q", chatlist.LiveIdle, wantMinutes)
	}
}
