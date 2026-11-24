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
	"github.com/Agent-Field/codeaf/internal/guard"
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

// V14: `codeaf chat --at` and `--host`, `codeaf serve` behave exactly as before, and remote control is off unless explicitly switched on.
func TestRemoteControlIsOffUnlessSwitchedOn(t *testing.T) {
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

// V14: wireLive leaves Options.OpenLive nil when remote control is off, when no road is installed, or on a hosted window, and sets it only when all three hold.
func TestWireLiveLeavesOpenLiveNilWhenOffOrWithoutARoadOrOnAHostedWindow(t *testing.T) {
	prevRoads := liveRoads
	t.Cleanup(func() { liveRoads = prevRoads })

	road := newFakeLiveRoad()
	lookup := func(device string) (liveRoad, string, bool) {
		return road, "Desktop", true
	}

	// Absent 1: remote control is off.
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	liveRoads = lookup
	var offOpts tui3.Options
	wireLive(&offOpts, t.TempDir())
	if offOpts.OpenLive != nil {
		t.Fatal("wireLive wired OpenLive while remote control was off")
	}

	// Absent 2: no road adapter is installed.
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	liveRoads = nil
	var noRoadOpts tui3.Options
	wireLive(&noRoadOpts, t.TempDir())
	if noRoadOpts.OpenLive != nil {
		t.Fatal("wireLive wired OpenLive with liveRoads == nil")
	}

	// Absent 3: the window is itself a hosted session.
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	liveRoads = lookup
	hostedOpts := tui3.Options{Host: "devbox"}
	wireLive(&hostedOpts, t.TempDir())
	if hostedOpts.OpenLive != nil {
		t.Fatal("wireLive wired OpenLive on a hosted window")
	}

	// Present path: remote control is on, a road lookup is installed, and Host is empty.
	var liveOpts tui3.Options
	wireLive(&liveOpts, t.TempDir())
	if liveOpts.OpenLive == nil {
		t.Fatal("wireLive left OpenLive nil when remote control was on with a road")
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

	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device string) (liveRoad, string, bool) {
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

	door := &liveDoor{
		home:  home,
		cache: cache,
		clock: clk,
		roads: func(device string) (liveRoad, string, bool) {
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

	door := &liveDoor{
		home:  home,
		cache: cache,
		clock: clk,
		roads: func(device string) (liveRoad, string, bool) {
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

	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device string) (liveRoad, string, bool) {
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
	door := &liveDoor{
		home:  t.TempDir(),
		cache: viewcache.Open(t.TempDir()),
		roads: func(device string) (liveRoad, string, bool) {
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

	forgetViewedChat(home, "dev-desktop", "cell-moved")
	if _, ok := cache.Load("dev-desktop", "cell-moved"); ok {
		t.Fatal("forgetViewedChat left cell-moved in the viewer cache")
	}
	if _, ok := cache.Load("dev-desktop", "cell-remaining"); !ok {
		t.Fatal("forgetViewedChat removed another chat's copy")
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
