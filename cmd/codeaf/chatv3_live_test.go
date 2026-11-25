package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/viewcache"
)

type fakeLiveClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeLiveClock(t0 time.Time) *fakeLiveClock {
	return &fakeLiveClock{now: t0}
}

func (c *fakeLiveClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeLiveClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeOlderCall struct {
	chat   string
	before int
	limit  int
}

type fakeLiveRoad struct {
	mu           sync.Mutex
	road         string
	drive        bool
	readOnly     string
	read         tui3.LiveRead
	changed      chan struct{}
	touched      int
	reconnected  int
	olderCalls   []fakeOlderCall
	olderEntries []session.DisplayEntry
	olderBase    int
	olderMore    bool
	olderErr     error
	dialFn       func(chat string) remote.Dialer
}

func newFakeLiveRoad() *fakeLiveRoad {
	return &fakeLiveRoad{
		road:    "relay",
		drive:   true,
		changed: make(chan struct{}),
	}
}

func (r *fakeLiveRoad) Dial(chat string) remote.Dialer {
	r.mu.Lock()
	fn := r.dialFn
	r.mu.Unlock()
	if fn != nil {
		return fn(chat)
	}
	return nil
}

func (r *fakeLiveRoad) Read() tui3.LiveRead {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read
}

func (r *fakeLiveRoad) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

func (r *fakeLiveRoad) Touch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.touched++
}

func (r *fakeLiveRoad) touches() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.touched
}

func (r *fakeLiveRoad) Reconnect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconnected++
}

func (r *fakeLiveRoad) Road() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.road
}

func (r *fakeLiveRoad) Permit() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drive, r.readOnly
}

func (r *fakeLiveRoad) Older(_ context.Context, chat string, before, limit int) ([]session.DisplayEntry, int, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.olderCalls = append(r.olderCalls, fakeOlderCall{chat: chat, before: before, limit: limit})
	return append([]session.DisplayEntry(nil), r.olderEntries...), r.olderBase, r.olderMore, r.olderErr
}

type fakeLivePeerAgent struct {
	quietAgent
	mu         sync.Mutex
	model      string
	title      string
	entries    []session.DisplayEntry
	submitFn   func(text string) (<-chan session.Event, error)
	closes     int
	detaches   int
	interrupts int
}

func (a *fakeLivePeerAgent) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.model != "" {
		return a.model
	}
	return "model/live"
}

func (a *fakeLivePeerAgent) Title() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

func (a *fakeLivePeerAgent) Transcript() []session.DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.DisplayEntry(nil), a.entries...)
}

func (a *fakeLivePeerAgent) setEntries(entries ...session.DisplayEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append([]session.DisplayEntry(nil), entries...)
}

func (a *fakeLivePeerAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	a.mu.Lock()
	fn := a.submitFn
	a.mu.Unlock()
	if fn != nil {
		return fn(text)
	}
	return a.quietAgent.Submit(ctx, text)
}

func (a *fakeLivePeerAgent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closes++
	return nil
}

func (a *fakeLivePeerAgent) Detach() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detaches++
	return nil
}

func (a *fakeLivePeerAgent) Interrupt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.interrupts++
}

func (a *fakeLivePeerAgent) InterruptFor(session.StopDoor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.interrupts++
}

func (a *fakeLivePeerAgent) counts() (closes, detaches, interrupts int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closes, a.detaches, a.interrupts
}

// switchRemoteControlOn is the setting the door refuses without, the way a person
// switches it on from outside the window: the variable, which wins over the
// persisted row.
func switchRemoteControlOn(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
}

// V14: `codeaf chat --at` and `--host`, `codeaf serve` behave exactly as before, and remote control is off unless explicitly switched on.
func TestRemoteControlIsOffUnlessSwitchedOn(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	_ = os.Unsetenv("CODEAF_REMOTE_CONTROL")
	if remoteControlWanted() {
		t.Fatal("remoteControlWanted() = true when CODEAF_REMOTE_CONTROL is unset")
	}
	for _, off := range []string{"", "0", "off", "OFF", "false", "no", "unexpected"} {
		t.Setenv("CODEAF_REMOTE_CONTROL", off)
		if remoteControlWanted() {
			t.Fatalf("remoteControlWanted() = true for %q, want false", off)
		}
	}
	for _, on := range []string{"1", "on", "ON", "true", "TRUE", "yes", " 1 "} {
		t.Setenv("CODEAF_REMOTE_CONTROL", on)
		if !remoteControlWanted() {
			t.Fatalf("remoteControlWanted() = false for %q, want true", on)
		}
	}
}

// V14, as the 2026-11-24 hand test corrected it: wireLive leaves Options.OpenLive nil only when no road is installed or on a hosted window. Remote control being off is NOT a reason to leave it nil, because the setting is turned on in a window that is already open (see TestAWindowStartedWithRemoteControlOffOpensLiveOnceItIsSwitchedOn).
func TestWireLiveLeavesOpenLiveNilWithoutARoadOrOnAHostedWindowAndWiresItWhateverTheSettingSays(t *testing.T) {
	prevRoads := liveRoads
	t.Cleanup(func() { liveRoads = prevRoads })
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())

	road := newFakeLiveRoad()
	lookup := func(device, _ string) (liveRoad, string, bool) {
		return road, "Desktop", true
	}

	// Absent 1: no road adapter is installed, with remote control on.
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	liveRoads = nil
	var noRoadOpts tui3.Options
	wireLive(&noRoadOpts, t.TempDir())
	if noRoadOpts.OpenLive != nil {
		t.Fatal("wireLive wired OpenLive with liveRoads == nil")
	}

	// Absent 2: the window is itself a hosted session, with remote control on.
	liveRoads = lookup
	hostedOpts := tui3.Options{Host: "devbox"}
	wireLive(&hostedOpts, t.TempDir())
	if hostedOpts.OpenLive != nil || hostedOpts.RemoteControl != nil {
		t.Fatal("wireLive wired a live opener on a hosted window")
	}

	// Present, on: remote control is on, a road lookup is installed, and Host is empty.
	var liveOpts tui3.Options
	wireLive(&liveOpts, t.TempDir())
	if liveOpts.OpenLive == nil || liveOpts.RemoteControl == nil || !liveOpts.RemoteControl() {
		t.Fatal("wireLive did not offer live views with remote control on and a road")
	}

	// Present, off: the same wiring, and the live read says no.
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	var offOpts tui3.Options
	wireLive(&offOpts, t.TempDir())
	if offOpts.OpenLive == nil || offOpts.RemoteControl == nil {
		t.Fatal("wireLive left the opener unwired because remote control was off at start, so switching it on would need a restart")
	}
	if offOpts.RemoteControl() {
		t.Fatal("the live read says remote control is on while it is off")
	}
}

// C1, C3: a window wired while remote control was off follows the setting from then on. The row /settings writes turns it on with no new wiring, and the environment variable, when set to a value, still wins over the row in both directions.
func TestAWindowStartedWithRemoteControlOffOpensLiveOnceItIsSwitchedOn(t *testing.T) {
	prevRoads := liveRoads
	t.Cleanup(func() { liveRoads = prevRoads })
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	_ = os.Unsetenv("CODEAF_REMOTE_CONTROL")
	liveRoads = func(string, string) (liveRoad, string, bool) { return newFakeLiveRoad(), "Desktop", true }

	var opts tui3.Options
	wireLive(&opts, t.TempDir())
	if opts.OpenLive == nil || opts.RemoteControl == nil {
		t.Fatal("the window was not wired for live views while remote control was off")
	}
	if opts.RemoteControl() {
		t.Fatal("remote control reads on in a fresh profile")
	}

	// What /settings does: the row is written, nothing else.
	if err := applyRow(profile, config.KeyRemoteControl, "on"); err != nil {
		t.Fatal(err)
	}
	if !opts.RemoteControl() {
		t.Fatal("the window still reads remote control as off after the row was switched on")
	}
	if err := applyRow(profile, config.KeyRemoteControl, "off"); err != nil {
		t.Fatal(err)
	}
	if opts.RemoteControl() {
		t.Fatal("the window still reads remote control as on after the row was switched off")
	}

	// C3: a set variable wins over the row, live, in both directions. The row is
	// written first in each case, because a set variable makes the row read only.
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	if !opts.RemoteControl() {
		t.Fatal("CODEAF_REMOTE_CONTROL=1 did not win over an off row")
	}
	_ = os.Unsetenv("CODEAF_REMOTE_CONTROL")
	if err := applyRow(profile, config.KeyRemoteControl, "on"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_REMOTE_CONTROL", "0")
	if opts.RemoteControl() {
		t.Fatal("CODEAF_REMOTE_CONTROL=0 did not win over an on row")
	}
}

// C2: while remote control is off the door refuses with the surface's own "nothing to open" answer, even for an offline chat whose last-seen copy it holds and which needs no road, and it opens that same chat once it is on.
func TestTheLiveDoorRefusesWhileRemoteControlIsOffAndOpensWhenItIsOn(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	_ = os.Unsetenv("CODEAF_REMOTE_CONTROL")
	home := t.TempDir()
	cache := viewcache.Open(home)
	if err := cache.SaveNow("dev-desktop", "chat-1", viewcache.Snapshot{
		Title:   "Port the picker",
		Entries: []session.DisplayEntry{{Role: "user", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	road := newFakeLiveRoad()
	door := &liveDoor{
		home:  home,
		cache: cache,
		roads: func(string, string) (liveRoad, string, bool) { return road, "Desktop", true },
	}
	row := chatlist.Row{Cell: "chat-1", Title: "Port the picker", Device: "Desktop", DeviceID: "dev-desktop", Status: chatlist.Off}

	if _, err := door.Open(context.Background(), row); !errors.Is(err, tui3.ErrNoLiveView) {
		t.Fatalf("Open with remote control off = %v, want tui3.ErrNoLiveView", err)
	}
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	conv, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("Open with remote control on: %v", err)
	}
	if conv.Agent == nil {
		t.Fatal("Open with remote control on returned no agent")
	}
}

// Housekeeping decided on purpose: the last-seen copies are trimmed at start only when remote control is on then. With it off, wiring the opener must touch nothing on disk.
func TestWireLiveTrimsTheLastSeenCopiesOnlyWhenRemoteControlIsOnAtStart(t *testing.T) {
	prevRoads := liveRoads
	t.Cleanup(func() { liveRoads = prevRoads })
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	liveRoads = func(string, string) (liveRoad, string, bool) { return newFakeLiveRoad(), "Desktop", true }

	// A temporary file a crashed write left behind, an hour old, is what Prune removes.
	stray := func(home string) string {
		dir := filepath.Join(home, "v3", "viewed", "dev-desktop")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "stray.tmp")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	offHome := t.TempDir()
	offPath := stray(offHome)
	var offOpts tui3.Options
	wireLive(&offOpts, offHome)
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(offPath); err != nil {
		t.Fatalf("wiring the opener with remote control off trimmed the copies: %v", err)
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	onHome := t.TempDir()
	onPath := stray(onHome)
	var onOpts tui3.Options
	wireLive(&onOpts, onHome)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(onPath); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wiring the opener with remote control on never trimmed the copies")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// V24: a viewer idle or backgrounded for 10 minutes detaches; a keypress or a running turn keeps the live link active through the surface seam alone, with no timer in cmd/codeaf.
func TestAKeypressOrARunningTurnKeepsTheLiveLinkActive(t *testing.T) {
	road := newFakeLiveRoad()
	peer := &fakeLivePeerAgent{}
	turnCh := make(chan session.Event, 4)
	peer.submitFn = func(string) (<-chan session.Event, error) {
		return turnCh, nil
	}

	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device, _ string) (liveRoad, string, bool) {
			return road, "Desktop", device == "dev-desktop"
		},
		dial: func(liveRoad, chatlist.Row) (tui3.Agent, remote.Welcome, error) {
			return peer, remote.Welcome{Workspace: "/work/app", SessionFile: "/work/app/chat.jsonl"}, nil
		},
	}

	row := chatlist.Row{DeviceID: "dev-desktop", Device: "Desktop", Cell: "cell-1", Status: chatlist.Running}
	conv, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open live conversation: %v", err)
	}
	if conv.Live == nil || conv.Live.Touch == nil {
		t.Fatal("live conversation has no Touch seam")
	}
	if got := road.touches(); got != 0 {
		t.Fatalf("road was touched %d times on open without a call, want 0", got)
	}

	// A keypress touches the seam once per call.
	conv.Live.Touch()
	if got := road.touches(); got != 1 {
		t.Fatalf("touches after one seam Touch = %d, want 1", got)
	}

	// Starting a running turn on the agent does not touch the road on its own;
	// activity while a turn runs reaches the road only through the surface seam.
	stream, err := conv.Agent.Submit(context.Background(), "keep working")
	if err != nil {
		t.Fatalf("submit turn: %v", err)
	}
	turnCh <- session.Event{Kind: session.EventTextDelta, Text: "partial"}
	if ev := <-stream; ev.Text != "partial" {
		t.Fatalf("stream event = %+v, want partial delta", ev)
	}
	if got := road.touches(); got != 1 {
		t.Fatalf("running turn touched the road without the surface seam: got %d, want 1", got)
	}
	conv.Live.Touch()
	if got := road.touches(); got != 2 {
		t.Fatalf("touches after surface turn Touch = %d, want 2", got)
	}
	close(turnCh)
	for range stream {
	}
	if got := road.touches(); got != 2 {
		t.Fatalf("touches after turn finished = %d, want 2", got)
	}
}

// V27: an offline host with relay storage off opens the viewer's own last-seen copy read-only with a dated banner and sends nothing.
func TestAnOfflineComputerOpensTheLastSeenCopyAndSendsNothing(t *testing.T) {
	home := t.TempDir()
	clk := newFakeLiveClock(time.Date(2026, 11, 23, 14, 2, 0, 0, time.UTC))
	cache := viewcache.OpenWith(home, clk)
	road := newFakeLiveRoad()
	road.dialFn = func(string) remote.Dialer {
		return func() (io.ReadWriteCloser, error) {
			return nil, errors.New("Desktop is not running this chat right now.")
		}
	}

	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  home,
		cache: cache,
		clock: clk,
		roads: func(device, _ string) (liveRoad, string, bool) {
			return road, "Desktop", device == "dev-desktop"
		},
	}

	row := chatlist.Row{DeviceID: "dev-desktop", Device: "Desktop", Cell: "cell-offline", Status: chatlist.Running}.Quiet()
	if _, err := door.Open(context.Background(), row); !errors.Is(err, tui3.ErrNoLiveView) {
		t.Fatalf("offline computer without snapshot returned %v, want ErrNoLiveView", err)
	}

	wantEntries := []session.DisplayEntry{
		{Role: "user", Text: "check the build"},
		{Role: "assistant", Text: "all packages compile cleanly", Answer: true},
	}
	seenAt := clk.Now()
	if err := cache.SaveNow("dev-desktop", "cell-offline", viewcache.Snapshot{
		Host:      "dev-desktop",
		HostName:  "Desktop",
		Chat:      "cell-offline",
		Title:     "Build check",
		Model:     "model/offline",
		Workspace: "/work/build",
		SeenAt:    seenAt,
		Entries:   wantEntries,
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	conv, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open offline conversation with snapshot: %v", err)
	}
	if conv.Live == nil || conv.Live.Read == nil {
		t.Fatal("offline conversation is missing Live.Read")
	}
	if got := conv.Live.Read(); got.Kind != tui3.LiveCopy || !got.SeenAt.Equal(seenAt) {
		t.Fatalf("Live.Read() = %+v, want Kind=LiveCopy SeenAt=%v", got, seenAt)
	}
	if got := conv.Agent.Transcript(); len(got) != len(wantEntries) || got[0].Text != wantEntries[0].Text || got[1].Text != wantEntries[1].Text {
		t.Fatalf("Transcript() = %+v, want %+v", got, wantEntries)
	}
	if got := conv.Agent.Model(); got != "model/offline" {
		t.Fatalf("Model() = %q, want model/offline", got)
	}
	wantRefusal := chatlist.LiveNotSentOffline("Desktop")
	if _, err := conv.Agent.Submit(context.Background(), "try sending"); err == nil || err.Error() != wantRefusal {
		t.Fatalf("Submit() error = %v, want %q", err, wantRefusal)
	}
	if _, err := conv.Agent.SubmitImage(context.Background(), "try image", nil); err == nil || err.Error() != wantRefusal {
		t.Fatalf("SubmitImage() error = %v, want %q", err, wantRefusal)
	}
	if conv.Link == nil || conv.Link.SendRefusal == nil || conv.Link.SendRefusal() != wantRefusal {
		t.Fatalf("Link.SendRefusal() = %v, want %q", conv.Link, wantRefusal)
	}
}

// V27: the viewer's last-seen copy is saved on the quiet beat after a turn ends and on Detach, at most once per viewcache.WriteEvery and never per streaming delta.
func TestTheLiveCopyIsSavedAfterATurnAndOnDetachNeverPerDelta(t *testing.T) {
	home := t.TempDir()
	t0 := time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)
	clk := newFakeLiveClock(t0)
	cache := viewcache.OpenWith(home, clk)
	road := newFakeLiveRoad()
	peer := &fakeLivePeerAgent{title: "Cache pacing", model: "model/paced"}

	var currentTurn chan session.Event
	peer.submitFn = func(string) (<-chan session.Event, error) {
		currentTurn = make(chan session.Event, 16)
		return currentTurn, nil
	}

	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  home,
		cache: cache,
		clock: clk,
		roads: func(device, _ string) (liveRoad, string, bool) {
			return road, "Desktop", device == "dev-desktop"
		},
		dial: func(liveRoad, chatlist.Row) (tui3.Agent, remote.Welcome, error) {
			return peer, remote.Welcome{Workspace: "/work/paced", SessionFile: "/work/paced/chat.jsonl"}, nil
		},
	}

	row := chatlist.Row{DeviceID: "dev-desktop", Device: "Desktop", Cell: "cell-paced", Status: chatlist.Running}
	conv, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open live conversation: %v", err)
	}

	// Turn 1: stream many deltas; no snapshot may be written before the turn ends.
	stream1, err := conv.Agent.Submit(context.Background(), "turn one")
	if err != nil {
		t.Fatalf("submit turn 1: %v", err)
	}
	peer.setEntries(
		session.DisplayEntry{Role: "user", Text: "turn one"},
		session.DisplayEntry{Role: "assistant", Text: "reply one", Answer: true},
	)
	for i := 0; i < 10; i++ {
		currentTurn <- session.Event{Kind: session.EventTextDelta, Text: "delta"}
		<-stream1
		if _, ok := cache.Load("dev-desktop", "cell-paced"); ok {
			t.Fatalf("cache wrote on delta %d while turn 1 was still running", i)
		}
	}
	currentTurn <- session.Event{Kind: session.EventTurnDone}
	close(currentTurn)
	for range stream1 {
	}

	snap1, ok := cache.Load("dev-desktop", "cell-paced")
	if !ok {
		t.Fatal("cache did not save after turn 1 ended")
	}
	if len(snap1.Entries) != 2 || !snap1.SeenAt.Equal(t0) {
		t.Fatalf("snapshot after turn 1 = %+v, want 2 entries at %v", snap1, t0)
	}

	// Turn 2 ends before viewcache.WriteEvery has elapsed: throttled, no disk write.
	clk.Advance(viewcache.WriteEvery / 2)
	stream2, err := conv.Agent.Submit(context.Background(), "turn two")
	if err != nil {
		t.Fatalf("submit turn 2: %v", err)
	}
	peer.setEntries(
		session.DisplayEntry{Role: "user", Text: "turn one"},
		session.DisplayEntry{Role: "assistant", Text: "reply one", Answer: true},
		session.DisplayEntry{Role: "user", Text: "turn two"},
		session.DisplayEntry{Role: "assistant", Text: "reply two", Answer: true},
	)
	for i := 0; i < 8; i++ {
		currentTurn <- session.Event{Kind: session.EventTextDelta, Text: "delta"}
		<-stream2
	}
	currentTurn <- session.Event{Kind: session.EventTurnDone}
	close(currentTurn)
	for range stream2 {
	}

	snap2, ok := cache.Load("dev-desktop", "cell-paced")
	if !ok || len(snap2.Entries) != 2 || !snap2.SeenAt.Equal(t0) {
		t.Fatalf("cache wrote inside WriteEvery window: got %+v, want turn 1 snapshot at %v", snap2, t0)
	}

	// Detach writes immediately via SaveNow even inside the WriteEvery window.
	clk.Advance(100 * time.Millisecond)
	detachAt := clk.Now()
	detacher, ok := conv.Agent.(interface{ Detach() error })
	if !ok {
		t.Fatal("liveAgent does not implement Detach()")
	}
	if err := detacher.Detach(); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	snap3, ok := cache.Load("dev-desktop", "cell-paced")
	if !ok || len(snap3.Entries) != 4 || !snap3.SeenAt.Equal(detachAt) {
		t.Fatalf("snapshot after Detach = %+v, want 4 entries at %v", snap3, detachAt)
	}
}

// V1: closing or detaching a live conversation closes only the local Roam client and never asks the other computer to end its chat.
func TestClosingALiveConversationNeverEndsTheChatOnTheOtherComputer(t *testing.T) {
	peer := &fakeLivePeerAgent{}
	var releaseCalls int
	var mu sync.Mutex
	hostSession := remote.NewSession(&remote.Engine{
		Agent:       peer,
		Workspace:   "/srv/desktop/app",
		SessionFile: "/srv/desktop/app/chat.jsonl",
		ReleaseOnLeave: func(remote.WrappedAgent) bool {
			mu.Lock()
			defer mu.Unlock()
			releaseCalls++
			return true
		},
	}, true)

	road := newFakeLiveRoad()
	road.dialFn = func(string) remote.Dialer {
		return func() (io.ReadWriteCloser, error) {
			surface, engine := remote.Pipe()
			guard.Go("test/live-host-serve", func() {
				_ = remote.ServeAttach(engine, engine, remote.AttachOptions{
					Open: func(remote.Hello) (*remote.Session, error) {
						return hostSession, nil
					},
				})
				_ = engine.Close()
			})
			return surface, nil
		}
	}

	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device, _ string) (liveRoad, string, bool) {
			return road, "Desktop", device == "dev-desktop"
		},
	}
	row := chatlist.Row{DeviceID: "dev-desktop", Device: "Desktop", Cell: "cell-keep", Status: chatlist.Running}

	// Close the first live view.
	conv1, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open first live conversation: %v", err)
	}
	if err := conv1.Agent.Close(); err != nil {
		t.Fatalf("Close live conversation: %v", err)
	}

	// Open a second live view of the same chat and Detach it.
	conv2, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open second live conversation: %v", err)
	}
	detacher, ok := conv2.Agent.(interface{ Detach() error })
	if !ok {
		t.Fatal("liveAgent does not implement Detach()")
	}
	if err := detacher.Detach(); err != nil {
		t.Fatalf("Detach live conversation: %v", err)
	}

	closes, detaches, interrupts := peer.counts()
	mu.Lock()
	releases := releaseCalls
	mu.Unlock()
	if closes != 0 || detaches != 0 || interrupts != 0 || releases != 0 {
		t.Fatalf("other computer saw closes=%d detaches=%d interrupts=%d releases=%d, want all 0", closes, detaches, interrupts, releases)
	}
}

// V4: scrolling up on a live chat pages older entries from the host through the road and updates TranscriptBase.
func TestALiveAgentPagesOlderEntriesThroughTheRoad(t *testing.T) {
	road := newFakeLiveRoad()
	peer := &fakeLivePeerAgent{}
	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device, _ string) (liveRoad, string, bool) {
			return road, "Desktop", device == "dev-desktop"
		},
		dial: func(liveRoad, chatlist.Row) (tui3.Agent, remote.Welcome, error) {
			return peer, remote.Welcome{Workspace: "/work/app", SessionFile: "/work/app/chat.jsonl"}, nil
		},
	}

	row := chatlist.Row{DeviceID: "dev-desktop", Device: "Desktop", Cell: "cell-older", Status: chatlist.Running}
	conv, err := door.Open(context.Background(), row)
	if err != nil {
		t.Fatalf("open live conversation: %v", err)
	}
	older, ok := conv.Agent.(tui3.OlderReader)
	if !ok {
		t.Fatal("live conversation agent does not satisfy tui3.OlderReader")
	}
	if got := older.TranscriptBase(); got != 0 {
		t.Fatalf("initial TranscriptBase() = %d, want 0", got)
	}

	road.olderEntries = []session.DisplayEntry{
		{Role: "user", Text: "page one user"},
		{Role: "assistant", Text: "page one reply", Answer: true},
	}
	road.olderBase = 120
	road.olderMore = true

	entries, more, err := older.Older(context.Background(), 160, 40)
	if err != nil {
		t.Fatalf("first Older: %v", err)
	}
	if !more || len(entries) != 2 || entries[0].Text != "page one user" {
		t.Fatalf("first Older = (%+v, more=%v), want 2 entries and more=true", entries, more)
	}
	if got := older.TranscriptBase(); got != 120 {
		t.Fatalf("TranscriptBase() after first page = %d, want 120", got)
	}
	if len(road.olderCalls) != 1 || road.olderCalls[0] != (fakeOlderCall{chat: "cell-older", before: 160, limit: 40}) {
		t.Fatalf("olderCalls = %+v, want [{chat:cell-older before:160 limit:40}]", road.olderCalls)
	}

	road.olderEntries = []session.DisplayEntry{
		{Role: "user", Text: "earliest user"},
	}
	road.olderBase = 0
	road.olderMore = false

	entries, more, err = older.Older(context.Background(), 120, 120)
	if err != nil {
		t.Fatalf("second Older: %v", err)
	}
	if more || len(entries) != 1 || entries[0].Text != "earliest user" {
		t.Fatalf("second Older = (%+v, more=%v), want 1 entry and more=false", entries, more)
	}
	if got := older.TranscriptBase(); got != 0 {
		t.Fatalf("TranscriptBase() after reaching the start = %d, want 0", got)
	}
}

// V27: moving a chat here forgets its cached copy, and removing a computer forgets that computer's entire cache folder.
func TestMovingAChatHereForgetsItsLastSeenCopy(t *testing.T) {
	home := t.TempDir()
	cache := viewcache.Open(home)
	for _, cell := range []string{"cell-moved", "cell-remaining"} {
		if err := cache.SaveNow("dev-desktop", cell, viewcache.Snapshot{
			Host:     "dev-desktop",
			HostName: "Desktop",
			Chat:     cell,
			Entries:  []session.DisplayEntry{{Role: "user", Text: cell}},
		}); err != nil {
			t.Fatalf("save %s: %v", cell, err)
		}
	}

	forgetViewedChatEverywhere(home, "cell-moved")
	if _, ok := cache.Load("dev-desktop", "cell-moved"); ok {
		t.Fatal("forgetViewedChatEverywhere left cell-moved in the viewer cache")
	}
	if _, ok := cache.Load("dev-desktop", "cell-remaining"); !ok {
		t.Fatal("forgetViewedChatEverywhere removed another chat's copy")
	}

	// The window that moved the chat does not know which computer the copy was
	// last seen on, so a second computer's copy of the same chat goes too.
	if err := cache.SaveNow("dev-laptop", "cell-moved", viewcache.Snapshot{Host: "dev-laptop", Chat: "cell-moved", Entries: []session.DisplayEntry{{Role: "user", Text: "x"}}}); err != nil {
		t.Fatalf("save second computer's copy: %v", err)
	}
	forgetViewedChatEverywhere(home, "cell-moved")
	if _, ok := cache.Load("dev-laptop", "cell-moved"); ok {
		t.Fatal("forgetViewedChatEverywhere left another computer's copy of the moved chat")
	}
	// A takeover built without a home (every test's literal) forgets nothing and does not fail.
	takeover{}.movedHere("cell-remaining")
	if _, ok := cache.Load("dev-desktop", "cell-remaining"); !ok {
		t.Fatal("a takeover with no home removed a copy")
	}

	forgetViewed(home, "dev-desktop")
	if _, ok := cache.Load("dev-desktop", "cell-remaining"); ok {
		t.Fatal("forgetViewed left cell-remaining in the viewer cache")
	}
	hostDir := filepath.Join(home, "v3", "viewed", "dev-desktop")
	if _, err := os.Stat(hostDir); !os.IsNotExist(err) {
		t.Fatalf("forgetViewed left host folder %s on disk (err=%v)", hostDir, err)
	}
}

// V13 of the phase 2 contract, a phase 1 completion: a message typed just before
// the link dropped is sent again once, and not lost. The client does that only
// when its policy says ResendUnanswered, and nothing but this door sets the
// policy of a live view, so the test reads what the door asks the client to do.
func TestTheLiveRoamingTurnsOnResendUnansweredTheLineBoundAndTheGapHandler(t *testing.T) {
	road := newFakeLiveRoad()
	dialer := remote.Dialer(func() (io.ReadWriteCloser, error) { return nil, errors.New("no host in this test") })
	road.dialFn = func(string) remote.Dialer { return dialer }
	var asked remote.Roaming
	stop := errors.New("stop here: the policy is all this test reads")
	switchRemoteControlOn(t)
	door := &liveDoor{
		home:  t.TempDir(),
		roads: func(device, name string) (liveRoad, string, bool) { return road, "desktop", true },
		roam: func(host string, hello remote.Hello, roaming remote.Roaming) (*remote.Client, error) {
			asked = roaming
			if hello.Window {
				t.Errorf("the viewer sent a windowed hello, which the host's router would have to rewrite")
			}
			return nil, stop
		},
	}
	_, err := door.Open(t.Context(), chatlist.Row{Cell: "cell-1", DeviceID: "dev_desktop", Device: "desktop", Status: chatlist.Running})
	if !errors.Is(err, stop) {
		t.Fatalf("Open = %v, want the error the test's dial returned", err)
	}
	if asked.Dial == nil {
		t.Errorf("the live view asks to redial by no dialler at all")
	}
	if !asked.ResendUnanswered {
		t.Errorf("a message typed just before the link drops is not resent after the redial: ResendUnanswered is off")
	}
	if asked.MaxLine != livedoor.MaxLine {
		t.Errorf("the viewer reads lines of at most %d bytes, want the live door's own %d", asked.MaxLine, livedoor.MaxLine)
	}
	if !asked.SpreadReturns {
		t.Errorf("a live view is dialled through a relay, so its returns must be spread (V22): SpreadReturns is off")
	}
	if asked.Window != 30*time.Minute {
		t.Errorf("the live view keeps trying for %v after a drop, want 30 minutes for a laptop that comes back from sleep", asked.Window)
	}
	if asked.OnGap == nil {
		t.Fatalf("a replay that lost events goes unnoticed: OnGap is nil")
	}
	asked.OnGap(remote.Gap{Stream: 3, Have: 10, First: 40})
}
