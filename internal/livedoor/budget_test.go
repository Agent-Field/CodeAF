package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// TestPastTheDailyBudgetEverySessionBecomesWatchOnlyWithAReason verifies
// contract V19: once traffic across live tunnels reaches the daily byte budget,
// every open channel on every link is reset once with reason budget, and when
// the viewer redials on the tunnel it receives a watch-only stamp (Drive:false)
// and continues watching without being cut again.
func TestPastTheDailyBudgetEverySessionBecomesWatchOnlyWithAReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	homeDir := shortTempDir(t)
	fc := NewFakeClock(time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC))
	reg := NewRegistry()

	// Set a small limit (4 KiB) so initial handshakes succeed under the budget
	// and a single call pushes the day's total over the limit.
	budget := OpenBudget(homeDir, fc, 4096)
	t.Cleanup(func() { budget.Close() })

	hellos := make(chan remote.Hello, 8)
	resolver := newEchoStampResolver(func(h remote.Hello) {
		hellos <- h
	})

	startLink := func(device string) (*Mux, *Channel) {
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
		guard.Go("livedoor.test.v19.serve", func() {
			_ = Serve(ctx, hConn, peer, ServeConfig{
				Resolver: resolver,
				Registry: reg,
				Budget:   budget,
				Clock:    fc,
				Host:     "workstation",
				Road:     "relay",
			})
		})
		vMux := NewMux(vConn, Viewer, MuxConfig{Clock: fc})
		t.Cleanup(func() { _ = vMux.Close() })
		if _, err := vMux.WaitHelloAck(ctx); err != nil {
			t.Fatalf("WaitHelloAck(%s) failed: %v", device, err)
		}
		ch := openAndHandshake(t, ctx, vMux, "chat-budget")
		h := <-hellos
		if h.Remote == nil || !h.Remote.Drive {
			t.Fatalf("initial hello for %s had Remote=%+v, want Drive=true before budget exhaustion", device, h.Remote)
		}
		return vMux, ch
	}

	vMux1, ch1 := startLink("dev-budget-1")
	_, ch2 := startLink("dev-budget-2")
	if budget.Exhausted() {
		t.Fatalf("budget already exhausted after handshakes (bytes=%d)", budget.Bytes())
	}

	// Send a call frame large enough to push the shared budget past 4096 bytes.
	bigCall := makeFrameLineOfExactLength(t, "call", remote.MethodPing, 4096)
	_, _ = ch1.Write(bigCall)

	var buf [64]byte
	for i, ch := range []*Channel{ch1, ch2} {
		_, err := ch.Read(buf[:])
		if got := reasonOf(err); got != ReasonBudget {
			t.Fatalf("channel %d when budget exhausted ended with %v (reason=%q), want %q", i+1, err, got, ReasonBudget)
		}
	}
	if !budget.Exhausted() {
		t.Fatal("budget.Exhausted() is false after exceeding limit")
	}

	// Redialling on the same link opens a watch-only channel (Drive:false) that
	// stays open even as further bytes are charged.
	chRedial := openAndHandshake(t, ctx, vMux1, "chat-budget")
	redialHello := <-hellos
	if redialHello.Remote == nil || redialHello.Remote.Drive || !redialHello.Remote.Watch || !redialHello.Watch {
		t.Fatalf("redial hello after budget exhaustion had Remote=%+v Watch=%v, want Drive=false Watch=true",
			redialHello.Remote, redialHello.Watch)
	}

	// A disallowed Submit on the watch-only redial channel is refused with the
	// watch-only sentence rather than resetting the channel again.
	submitLine, _ := json.Marshal(remote.Frame{
		Kind:   "call",
		ID:     99,
		Method: remote.MethodSubmit,
	})
	if _, err := chRedial.Write(append(submitLine, '\n')); err != nil {
		t.Fatalf("Write on watch-only redial channel failed: %v", err)
	}
	scan := newLineScanner(chRedial, MaxLine)
	resLine, err := scan.ReadLine(MaxLine)
	if err != nil {
		t.Fatalf("reading refusal on watch-only redial channel failed: %v", err)
	}
	var res remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(resLine, "\r\n"), &res); err != nil {
		t.Fatalf("unmarshaling refusal failed: %v", err)
	}
	wantErr := remote.GuestRefusal("workstation", remote.MethodSubmit, false)
	if res.ID != 99 || res.Error != wantErr {
		t.Fatalf("watch-only redial Submit result = %+v, want ID=99 Error=%q", res, wantErr)
	}
}

// TestTheCountSurvivesARestartAndRollsAtUTCMidnight verifies contract V19:
// the persistence timer schedules nothing until the first charge, charging to
// the limit and advancing 30s (or closing) writes <home>/v3/remote/budget.json
// with mode 0600 so reopening from disk restores the exact count, and advancing
// the clock past 00:00 UTC rolls the day and resets the count to zero.
func TestTheCountSurvivesARestartAndRollsAtUTCMidnight(t *testing.T) {
	homeDir := shortTempDir(t)
	start := time.Date(2026, 11, 23, 23, 50, 0, 0, time.UTC)
	fc := NewFakeClock(start)

	const limit = 10 << 20
	b1 := OpenBudget(homeDir, fc, limit)
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers before first charge = %d, want 0", got)
	}

	// First charge lazily arms the 30-second persistence timer.
	if over := b1.Charge(limit / 2); over {
		t.Fatalf("Charge(%d) reported over=true below limit %d", limit/2, limit)
	}
	if got := fc.BlockedTimers(); got != 1 {
		t.Fatalf("BlockedTimers after first charge = %d, want 1", got)
	}

	// Charge to the limit and fire the 30s timer so it persists to disk even before Close.
	if over := b1.Charge(limit / 2); !over {
		t.Fatalf("Charge to limit (%d) reported over=false, want true", limit)
	}
	if !b1.Exhausted() || b1.Bytes() != limit {
		t.Fatalf("b1 Exhausted=%v Bytes=%d, want true and %d", b1.Exhausted(), b1.Bytes(), limit)
	}
	fc.Advance(30 * time.Second)
	b1.Close()

	budgetPath := filepath.Join(homeDir, "v3", "remote", "budget.json")
	info, err := os.Stat(budgetPath)
	if err != nil {
		t.Fatalf("Stat(%q) failed: %v", budgetPath, err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("budget.json mode = %04o, want 0600", perm)
		}
	}

	// Reopen from disk on the same UTC day: count and exhausted state must match.
	b2 := OpenBudget(homeDir, fc, limit)
	t.Cleanup(func() { b2.Close() })
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers on reopened budget before charge = %d, want 0", got)
	}
	if got := b2.Bytes(); got != limit {
		t.Fatalf("reopened budget Bytes() = %d, want %d", got, limit)
	}
	if !b2.Exhausted() {
		t.Fatal("reopened budget Exhausted() = false, want true")
	}

	// Advance past 00:00 UTC into 2026-11-24: count must roll over to 0.
	fc.Advance(15 * time.Minute)
	if got := b2.Bytes(); got != 0 {
		t.Fatalf("Bytes() after UTC midnight rollover = %d, want 0", got)
	}
	if b2.Exhausted() {
		t.Fatal("Exhausted() after UTC midnight rollover = true, want false")
	}
}

// TestAnIdleBudgetSchedulesNothing verifies contract V19: an opened budget that
// receives no charges schedules zero timers on the clock and writes no file
// while idle.
func TestAnIdleBudgetSchedulesNothing(t *testing.T) {
	homeDir := shortTempDir(t)
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))

	b := OpenBudget(homeDir, fc, 0)
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers on newly opened idle budget = %d, want 0", got)
	}
	if b.Exhausted() || b.Bytes() != 0 {
		t.Fatalf("idle budget Exhausted=%v Bytes=%d, want false and 0", b.Exhausted(), b.Bytes())
	}

	fc.Advance(2 * time.Hour)
	if got := fc.BlockedTimers(); got != 0 {
		t.Fatalf("BlockedTimers after 2 virtual hours of idle = %d, want 0", got)
	}
	budgetPath := filepath.Join(homeDir, "v3", "remote", "budget.json")
	if _, err := os.Stat(budgetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("idle budget created %q before any charge or Close (err=%v)", budgetPath, err)
	}
}

// C2: the day's byte count is remote control's own file in a home. It must
// never make that home count as having local work, so a computer that watched
// or was watched, and was then left alone, can still join another set of
// computers, and a home that was only started still looks unused.
func TestTheByteCountNeverMakesAHomeLookUsed(t *testing.T) {
	homeDir := shortTempDir(t)
	if _, err := identity.EnsureSolo(homeDir); err != nil {
		t.Fatal(err)
	}
	b := OpenBudget(homeDir, NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)), 1<<20)
	b.Charge(1024)
	b.Close()
	if _, err := os.Stat(filepath.Join(homeDir, "v3", "remote", "budget.json")); err != nil {
		t.Fatalf("the byte count was not written, so this proves nothing: %v", err)
	}
	if ok, err := identity.Pristine(homeDir); err != nil || !ok {
		t.Fatalf("the byte count made a home that was only started look used: pristine=%v err=%v", ok, err)
	}
	if ok, err := identity.Joinable(homeDir); err != nil || !ok {
		t.Fatalf("the byte count blocked a join: joinable=%v err=%v", ok, err)
	}
}
