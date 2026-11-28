package clientrun

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// TestNotWatchingGuardLimitsWritesToOncePer30Seconds verifies that while a
// phone is not watching, the runner writes any changed slot at most once per
// 30 seconds (clientproto.WatchingFloor, contract L4).
func TestNotWatchingGuardLimitsWritesToOncePer30Seconds(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")

	var slotPuts []time.Time
	slotPath := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "PUT" && req.URL.Path == slotPath {
			slotPuts = append(slotPuts, g.clock.Now())
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)

	start := g.clock.Now()
	// Initial publication at t = 0s.
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(slotPuts) != 1 {
		t.Fatalf("initial slot puts = %d, want 1", len(slotPuts))
	}

	// Change the snapshot every 5s from t = 5s through t = 65s while not watching.
	for sec := 5; sec <= 65; sec += 5 {
		g.clock.Advance(5 * time.Second)
		g.e.asks = []appwire.Ask{{ID: fmt.Sprintf("ask-%d", sec), Agent: chat}}
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	// Expect puts only at 0s, 30s, and 60s.
	if len(slotPuts) != 3 {
		t.Fatalf("slot puts while not watching = %d (%v), want 3 (at 0s, 30s, 60s)", len(slotPuts), slotPuts)
	}
	for i, wantOffset := range []time.Duration{0, 30 * time.Second, 60 * time.Second} {
		if got := slotPuts[i].Sub(start); got != wantOffset {
			t.Fatalf("put %d at +%v, want +%v", i, got, wantOffset)
		}
	}
}

// TestWatchingWakePublishesAtOnceAndCoalescesStreamOnFakeClock verifies that:
//  1. While not watching, streaming deltas do not write slots before the 30 s floor.
//  2. On a watching on:true event, every changed slot is published at once, and
//     event-sourced watching holds across >30 s even when PUT replies omit
//     Codeaf-Client-Watching: 1.
//  3. While watching, any (phone, slot) enforces a minimum gap of 250 ms between
//     two PUTs (both for streaming deltas and direct slot changes), while the
//     first write after a >= 250 ms quiet gap goes at once.
//  4. on:false clears watching immediately, and when watching is set by the
//     Codeaf-Client-Watching: 1 header fallback it lasts WatchingHeaderFor (30 s)
//     from its last sighting (contract L1, L4, L7).
func TestWatchingWakePublishesAtOnceAndCoalescesStreamOnFakeClock(t *testing.T) {
	g := newRig(t)
	g.e.entryBytes = 10
	g.enroll(t, []string{"agents.entries", "asks.list"})

	type scheduledTimer struct {
		at      time.Time
		fn      func()
		stopped bool
	}
	var timerMu sync.Mutex
	var timers []*scheduledTimer

	afterFunc := func(d time.Duration, fn func()) func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		tm := &scheduledTimer{at: g.clock.Now().Add(d), fn: fn}
		timers = append(timers, tm)
		return func() {
			timerMu.Lock()
			tm.stopped = true
			timerMu.Unlock()
		}
	}
	advanceClock := func(d time.Duration) {
		g.clock.Advance(d)
		now := g.clock.Now()
		for {
			var due *scheduledTimer
			timerMu.Lock()
			for _, tm := range timers {
				if !tm.stopped && !tm.at.After(now) {
					due = tm
					tm.stopped = true
					break
				}
			}
			timerMu.Unlock()
			if due == nil {
				break
			}
			due.fn()
		}
	}

	var entriesPuts []time.Time
	var asksPuts []time.Time
	var injectWatchingHeader bool
	entriesPath := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("agents.entries", g.s.dev.ID())
	asksPath := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())

	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: g.dir,
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		AfterFunc: afterFunc,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				return nil, err
			}
			if req.Method == "PUT" && req.URL.Path == entriesPath {
				entriesPuts = append(entriesPuts, g.clock.Now())
			}
			if req.Method == "PUT" && req.URL.Path == asksPath {
				asksPuts = append(asksPuts, g.clock.Now())
			}
			if injectWatchingHeader && req.Method == "PUT" {
				resp.Header.Set(clientproto.WatchingHeader, "1")
			}
			return resp, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r

	// Initial round at t = 0s publishes initial slots while not watching.
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 1 || len(asksPuts) != 1 {
		t.Fatalf("initial puts: entries=%d asks=%d", len(entriesPuts), len(asksPuts))
	}

	// Phone is in the background for 10s while asks change and chat starts streaming.
	advanceClock(10 * time.Second)
	g.e.asks = []appwire.Ask{{ID: "bg-ask", Agent: chat}}
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 1, Kind: appwire.DeltaText, Text: "background-"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 1 || len(asksPuts) != 1 {
		t.Fatalf("background changes published before 30s floor: entries=%d asks=%d", len(entriesPuts), len(asksPuts))
	}

	// Another token arrives 50ms later (buffered in stream coalescer), then watching on:true arrives!
	advanceClock(50 * time.Millisecond)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 1, Kind: appwire.DeltaText, Text: "wake"})
	g.r.setWatching(g.client.dev.ID(), true)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Both changed slots (asks.list and agents.entries with the flushed "background-wake" text) are published at once!
	if len(entriesPuts) != 2 || len(asksPuts) != 2 {
		t.Fatalf("on:true did not publish all changed slots at once: entries=%d asks=%d", len(entriesPuts), len(asksPuts))
	}

	// Event-sourced on:true does NOT expire after 30s even though PUT replies
	// carry no Codeaf-Client-Watching: 1 header. Advance 35s (also a >= 250ms quiet gap).
	advanceClock(35 * time.Second)
	entriesPuts = nil
	asksPuts = nil
	t0 := g.clock.Now()

	// First token of Msg 2 after a >= 250ms quiet gap publishes at once at t0.
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 2, Kind: appwire.DeltaText, Text: "tok1-"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 1 || !entriesPuts[0].Equal(t0) {
		t.Fatalf("first token after quiet gap did not publish at once: %v", entriesPuts)
	}

	// Advance by 50ms (< 250ms). A second streaming token on agents.entries must wait,
	// while a first change to asks.list after its 35s quiet gap publishes at once at t0+50ms!
	advanceClock(50 * time.Millisecond)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 2, Kind: appwire.DeltaText, Text: "tok2-"})
	g.e.asks = []appwire.Ask{{ID: "ask-at-50ms", Agent: chat}}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 1 {
		t.Fatalf("second entries write within 50ms did not wait: %v", entriesPuts)
	}
	if len(asksPuts) != 1 || asksPuts[0].Sub(t0) != 50*time.Millisecond {
		t.Fatalf("first asks write after quiet gap did not go at once at +50ms: %v", asksPuts)
	}

	// Advance by another 50ms to t0+100ms (< 250ms since both slots' last PUT) and
	// change asks.list a second time directly; both slots must wait until their 250ms gap elapses!
	advanceClock(50 * time.Millisecond)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 2, Kind: appwire.DeltaText, Text: "tok3-"})
	g.e.asks = []appwire.Ask{{ID: "ask-at-100ms", Agent: chat}}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 1 || len(asksPuts) != 1 {
		t.Fatalf("second write within <250ms did not wait: entries=%d asks=%d", len(entriesPuts), len(asksPuts))
	}

	// More tokens at +150ms and +200ms are still coalesced.
	for _, step := range []string{"tok4-", "tok5"} {
		advanceClock(50 * time.Millisecond)
		g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 2, Kind: appwire.DeltaText, Text: step})
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(entriesPuts) != 1 || len(asksPuts) != 1 {
		t.Fatalf("writes inside 250ms window were not held back: entries=%d asks=%d", len(entriesPuts), len(asksPuts))
	}

	// Advancing to t0+250ms completes the 250ms gap for agents.entries (written at t0),
	// while asks.list (written at t0+50ms) still has 50ms remaining!
	advanceClock(50 * time.Millisecond)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 2 || entriesPuts[1].Sub(t0) != clientproto.StreamCoalesce {
		t.Fatalf("trailing coalesced entries publication at %v, want +%v", entriesPuts, clientproto.StreamCoalesce)
	}
	if len(asksPuts) != 1 {
		t.Fatalf("asks.list published only 200ms after its previous PUT: %v", asksPuts)
	}

	// Advancing another 50ms to t0+300ms completes the 250ms gap for asks.list (written at t0+50ms).
	advanceClock(50 * time.Millisecond)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asksPuts) != 2 || asksPuts[1].Sub(asksPuts[0]) != clientproto.StreamCoalesce {
		t.Fatalf("trailing coalesced asks publication at %v, want +%v after first put", asksPuts, clientproto.StreamCoalesce)
	}

	// After a >= 250ms quiet gap (advance 300ms to t0+600ms), the next token of the same message goes at once!
	advanceClock(300 * time.Millisecond)
	tQuiet := g.clock.Now()
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 2, Kind: appwire.DeltaText, Text: "-after-gap"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 3 || !entriesPuts[2].Equal(tQuiet) {
		t.Fatalf("first token after 250ms quiet gap did not publish at once: %v", entriesPuts)
	}

	// Verify on:false clears watching immediately so a change 1s later is held back by the 30s floor.
	g.r.setWatching(g.client.dev.ID(), false)
	advanceClock(time.Second)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 3, Kind: appwire.DeltaText, Text: "held-back"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 3 {
		t.Fatalf("on:false did not return runner to 30s floor: %d puts", len(entriesPuts))
	}

	// Advance to the 30s floor with Codeaf-Client-Watching: 1 set on the PUT reply (header fallback).
	injectWatchingHeader = true
	advanceClock(clientproto.WatchingFloor)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 4 {
		t.Fatalf("30s floor PUT did not fire: %d puts", len(entriesPuts))
	}

	// Because the PUT reply carried Codeaf-Client-Watching: 1, watching is now active for 30s even when later PUTs omit the header.
	injectWatchingHeader = false
	advanceClock(time.Second)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 4, Kind: appwire.DeltaText, Text: "via-header-fallback"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 5 {
		t.Fatalf("header fallback did not enable watching: %d puts", len(entriesPuts))
	}

	// After 30s without a watching header or on:true event, the header-sourced flag expires.
	advanceClock(clientproto.WatchingHeaderFor)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 5, Kind: appwire.DeltaText, Text: "at-floor"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 6 {
		t.Fatalf("put at 30s boundary missing: %d puts", len(entriesPuts))
	}
	advanceClock(time.Second)
	g.r.Delta(appwire.TurnDelta{Agent: chat, Turn: 1, Msg: 6, Kind: appwire.DeltaText, Text: "after-floor-expired"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entriesPuts) != 6 {
		t.Fatalf("header-sourced watching did not expire after 30s without header: %d puts", len(entriesPuts))
	}
}

type noWatchDirClient struct {
	directory.Client
}

func (n noWatchDirClient) Watch(context.Context) (dirwatch.Stream, error) {
	return nil, directory.ErrUnreachable
}

func (n noWatchDirClient) WatchServing(context.Context) (dirwatch.Stream, error) {
	return nil, directory.ErrUnreachable
}

// TestWatchSocketDownFor35SecondsStillPublishesFromTimerAtFloorRate verifies
// that when the directory watch socket is down for 35 s on the fake clock,
// Serve continues running from its refresh timer and publishes changed slots at
// the 30 s floor rate (contract L7).
func TestWatchSocketDownFor35SecondsStillPublishesFromTimerAtFloorRate(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"}, "asks.answer")

	ticks := make(chan time.Time)
	// armed is told every time the runner arms its refresh timer, which it does only
	// once a round has finished, so a receive here means the runner is idle and the
	// fake clock may move without a round still reading it.
	armed := make(chan struct{}, 64)
	var putsMu sync.Mutex
	var asksPuts []time.Time
	asksPath := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())
	outcomesPath := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("outcomes", g.s.dev.ID())

	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: noWatchDirClient{Client: g.dir},
		Engine:    g.e,
		Log:       noopLog,
		Now:       g.clock.Now,
		// ONLY THE REFRESH TIMER MAY RECEIVE THE TEST'S TICK. The runner also arms coalescing
		// and backoff timers through After; handing every one of them the same channel lets a
		// coalescing timer steal a tick meant for the refresh loop, which made this test depend
		// on goroutine scheduling. Any other delay gets a nil channel, a timer that never fires.
		After: func(d time.Duration) <-chan time.Time {
			if d == refreshEvery {
				select {
				case armed <- struct{}{}:
				default:
				}
				return ticks
			}
			return nil
		},
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "PUT" && req.URL.Path == asksPath {
				putsMu.Lock()
				asksPuts = append(asksPuts, g.clock.Now())
				putsMu.Unlock()
			}
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r

	start := g.clock.Now()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.r.Serve(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksPath })
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+outcomesPath })

	// Advance the fake clock in 5s steps through 35s while the watch socket is down.
	idle := func(what string) {
		t.Helper()
		select {
		case <-armed:
		case <-time.After(30 * time.Second):
			t.Fatalf("Serve did not go idle %s", what)
		}
	}
	for sec := 5; sec <= 35; sec += 5 {
		// THE CLOCK MOVES ONLY WHILE THE RUNNER IS IDLE. A round still finishing when the
		// clock jumped would read the new time and publish a round early, and the test's
		// request log, cleared below, would then miss the publish it waits for.
		idle(fmt.Sprintf("before +%ds", sec))
		g.clock.Advance(refreshEvery)
		g.e.asks = []appwire.Ask{{ID: fmt.Sprintf("down-%d", sec), Agent: chat}}
		g.clearRequests()
		select {
		case ticks <- g.clock.Now():
		case <-time.After(30 * time.Second):
			t.Fatalf("Serve did not wait on refresh timer at +%ds", sec)
		}
		g.waitForRequest(t, func(s string) bool { return s == "GET /v1/dir/list" })
		g.waitForRequest(t, func(s string) bool { return s == "GET /v1/clients" })
		if sec == 30 {
			g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksPath })
		}
	}

	idle("after +35s")
	putsMu.Lock()
	defer putsMu.Unlock()
	if len(asksPuts) != 2 {
		t.Fatalf("asks puts while watch socket down for 35s = %d (%v), want 2 (at 0s and 30s)", len(asksPuts), asksPuts)
	}
	if got := asksPuts[0].Sub(start); got != 0 {
		t.Fatalf("first put at +%v, want +0s", got)
	}
	if got := asksPuts[1].Sub(start); got != clientproto.WatchingFloor {
		t.Fatalf("second put at +%v, want +%v", got, clientproto.WatchingFloor)
	}
}

// TestServeHoldsServingWatchSocketAndReportsOnlineToPhone verifies that a
// computer is reported serving=true in GET /v1/clients/{device}/computers only
// while Runner.Serve holds its serving=1 watch socket alive, and shows
// serving=false when no runner is alive or after the runner stops (contract L8).
func TestServeHoldsServingWatchSocketAndReportsOnlineToPhone(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"})

	getServing := func() bool {
		t.Helper()
		var out struct {
			Now       int64 `json:"now"`
			Computers []struct {
				Device  string `json:"device"`
				Serving bool   `json:"serving"`
				At      int64  `json:"at"`
			} `json:"computers"`
		}
		g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/computers", nil, &out, 200)
		for _, c := range out.Computers {
			if c.Device == g.s.dev.ID() {
				return c.Serving
			}
		}
		t.Fatalf("computer %s not listed in %+v", g.s.dev.ID(), out.Computers)
		return false
	}

	// Before Serve is running, the computer is awake in the directory but serving is false.
	if getServing() {
		t.Fatal("computer reported serving=true without a running runner")
	}

	g.clearRequests()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.r.Serve(ctx)
	}()

	asksSlot := "/v1/clients/" + g.client.dev.ID() + "/slots/" + SlotName("asks.list", g.s.dev.ID())
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksSlot })
	for !getServing() {
		select {
		case <-g.reqCh:
		case <-time.After(30 * time.Second):
			cancel()
			<-done
			t.Fatal("computer never reported serving=true while Runner.Serve was running")
		}
	}

	// Reading a slot from the phone triggers the relay's watching event on Serve's
	// watch socket and wakes the runner when a subsequent change arrives after the
	// 250ms coalesce gap from the initial PUT.
	_ = g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)
	g.clock.Advance(clientproto.StreamCoalesce)
	g.clearRequests()
	g.e.asks = []appwire.Ask{{ID: "live-wake", Agent: chat}}
	g.r.Changed()
	g.waitForRequest(t, func(s string) bool { return s == "PUT "+asksSlot })
	p := g.readBoxSlot(t, "asks.list", g.s.dev.ID(), 0)
	if !strings.Contains(string(p.Payload), "live-wake") {
		cancel()
		<-done
		t.Fatalf("Serve did not publish changed slot immediately while phone was watching: %s", p.Payload)
	}

	// Stopping Serve closes the serving=1 watch socket; poll the computers route
	// while advancing the fake clock on a bounded loop until the relay observes
	// the closed socket and reports serving=false (contract L8).
	cancel()
	<-done
	deadline := time.Now().Add(3 * time.Second)
	for {
		g.clock.Advance(30 * time.Second)
		if !getServing() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("computer still reported serving=true after Runner.Serve stopped")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestOutcomesSlotPublishesLast32Outcomes verifies that outcomes.<self>
// persists and publishes at most the newest 32 execution outcomes in an
// authenticated slot box (design 3.6, contract L6).
func TestOutcomesSlotPublishesLast32Outcomes(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"asks.list"})
	for n := int64(1); n <= 40; n++ {
		if err := g.r.RecordOutcome(g.client.dev.ID(), clientproto.Outcome{
			Number: n,
			Status: clientproto.OutcomeDone,
			Agent:  chat,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := g.readBoxSlot(t, "outcomes", g.s.dev.ID(), 0)
	got, err := clientproto.DecodeOutcomes(p.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != clientproto.MaxOutcomes {
		t.Fatalf("len(outcomes) = %d, want %d", len(got), clientproto.MaxOutcomes)
	}
	if got[0].Number != 9 || got[len(got)-1].Number != 40 {
		t.Fatalf("outcomes range = %d..%d, want 9..40", got[0].Number, got[len(got)-1].Number)
	}
}
