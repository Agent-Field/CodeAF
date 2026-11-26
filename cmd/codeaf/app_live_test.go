package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The rig of these tests is a host that is a real conversation behind a real
// remote session, reached through the same client a live view uses, with only
// the relay and its tunnel stood in for: the link's dialer is never run, and
// the roaming client is attached to the host in-process. What is asserted is
// therefore what the desktop sees, never how the engine got it.

const liveRigHostName = "studio"
const liveRigHostID = "devicestudio0001"
const liveRigCell = "01JD6Z8K3M4N5P6Q7R8S9T0V1W"

type liveRig struct {
	t        *testing.T
	b        *appBackend
	rec      *appRecorder
	host     *remote.Session
	engine   *session.Agent
	provider *httptest.Server
	asked    atomic.Int32
	roams    atomic.Int32
	hellos   chan remote.Hello
	links    chan string
	loops    []*remote.Loop
	file     string
	mu       sync.Mutex
}

func newLiveRig(t *testing.T, remoteControl bool) *liveRig {
	t.Helper()
	shortStateRoot(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("CODEAF_SYNC_URL", "off")
	if remoteControl {
		t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	} else {
		t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	}
	r := &liveRig{t: t, rec: newAppRecorder(), hellos: make(chan remote.Hello, 8), links: make(chan string, 8)}
	r.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.asked.Add(1)
		_, _ = io.Copy(io.Discard, req.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"live answer"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(r.provider.Close)
	root := t.TempDir()
	file := filepath.Join(root, "transcript.jsonl")
	r.file = file
	engine, err := session.New(session.Config{Workspace: root, SessionFile: file, Model: "test/model", APIKey: "fixture",
		BaseURL: r.provider.URL, OneModel: true, DeterministicNames: true, PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	r.engine = engine
	r.host = remote.NewSession(&remote.Engine{Agent: engine, SessionFile: file, Workspace: root}, true)
	t.Cleanup(func() { _ = r.host.Close() })
	// The host's chat already holds one exchange, so there is a past to read.
	r.hostSays("first question")

	oldDial, oldLink, oldRoam := appLiveDialable, appLiveNewLink, appLiveRoam
	t.Cleanup(func() { appLiveDialable, appLiveNewLink, appLiveRoam = oldDial, oldLink, oldRoam })
	appLiveDialable = func(*appBackend) bool { return true }
	appLiveNewLink = func(_ *appBackend, hostID, hostName string, _ func() bool, _ func(livedoor.State)) (*livedoor.Link, error) {
		r.links <- hostID
		return livedoor.NewLink(livedoor.LinkConfig{
			Dial: func(context.Context) (io.ReadWriteCloser, error) {
				return nil, errors.New("this rig's link is never dialled")
			},
			Host: hostID, HostName: hostName,
		}), nil
	}
	appLiveRoam = func(host string, hello remote.Hello, roam remote.Roaming) (*remote.Client, error) {
		r.roams.Add(1)
		r.hellos <- hello
		if roam.Dial == nil || roam.MaxLine != livedoor.MaxLine {
			t.Errorf("a live view must roam over the link's opener with the live line cap: %+v", roam)
		}
		// The host's live door names the conversation from the chat id the
		// channel was opened for; the rig does what the door's resolver does.
		hello.Session = r.file
		loop, err := remote.LoopbackAttach(hello, remote.AttachOptions{Open: func(remote.Hello) (*remote.Session, error) { return r.host, nil }})
		if err != nil {
			t.Logf("the rig's loopback attach failed: %v", err)
			return nil, err
		}
		r.mu.Lock()
		r.loops = append(r.loops, loop)
		r.mu.Unlock()
		return loop.Client, nil
	}
	r.b = newFakeBackend(r.rec, refuseAppOpen)
	t.Cleanup(func() { _ = r.b.Close() })
	r.listRunning()
	return r
}

// hostSays runs one turn on the host's own conversation, as the host's person
// typing there would.
func (r *liveRig) hostSays(text string) {
	r.t.Helper()
	events, err := r.engine.Submit(context.Background(), text)
	if err != nil {
		r.t.Fatal(err)
	}
	for range events {
	}
}

// listRunning files the chat list's row for a chat the studio is running.
func (r *liveRig) listRunning() {
	online := true
	r.b.chats.cacheRows([]appwire.ChatRow{{
		Cell: liveRigCell, Title: "Studio chat", Status: string(chatlist.Running), Online: &online,
		Holder: &appwire.LocationHolder{ID: liveRigHostID, Name: liveRigHostName},
		Seen:   appwire.Seen{Device: liveRigHostID, Fence: 7}, CanTake: true,
	}}, 1, false)
}

func (r *liveRig) open() appwire.Agent {
	r.t.Helper()
	agent, err := r.b.OpenLive(appwire.ChatOpenLiveParams{Cell: liveRigCell})
	if err != nil {
		r.t.Fatalf("OpenLive: %v", err)
	}
	return agent
}

// stateEvents are the agent.changed events that carry a live fact, in order.
func (r *liveRig) liveEvents() []appwire.LocationLive {
	r.rec.mu.Lock()
	defer r.rec.mu.Unlock()
	var out []appwire.LocationLive
	for i, event := range r.rec.events {
		if event != appwire.EventAgentChanged {
			continue
		}
		if changed, ok := r.rec.data[i].(appwire.AgentChanged); ok && changed.Agent.Location != nil && changed.Agent.Location.Live != nil {
			out = append(out, *changed.Agent.Location.Live)
		}
	}
	return out
}

// V1/V2 (a chat another computer is running opens on the desktop as an ordinary
// agent): chats.open_live answers an agent of the chat's own id whose location
// says it is held elsewhere and viewed live; its history is the host's, with
// the ids an app agent's entries always have; it is not listed as a chat of
// this computer; and the live fact moves with the link's word, in the closed
// spellings the desktop reads.
func TestAppOpenLiveServesARemoteChatAsALocalLookingAgent(t *testing.T) {
	r := newLiveRig(t, true)
	if !r.b.ChatsLiveOffered() {
		t.Fatal("with remote control on and a door to dial, chats.live must be offered")
	}
	agent := r.open()
	if agent.ID != liveRigCell {
		t.Fatalf("the live agent's id = %q, want the chat's own %q", agent.ID, liveRigCell)
	}
	loc := agent.Location
	if loc == nil || loc.Here || loc.Cell != liveRigCell || loc.Holder == nil || loc.Holder.Name != liveRigHostName || loc.Live == nil {
		t.Fatalf("location = %+v, want held elsewhere by %s and viewed live", loc, liveRigHostName)
	}
	if got := <-r.links; got != liveRigHostID {
		t.Fatalf("the link was made to %q, want the holder %q", got, liveRigHostID)
	}
	hello := <-r.hellos
	if !hello.Join || hello.Surface == "" {
		t.Fatalf("a live view joins the host's conversation and names this computer: %+v", hello)
	}

	listed, err := r.b.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range listed {
		if other.ID == liveRigCell {
			t.Fatalf("a chat with no folder here is not one of this computer's agents: %+v", other)
		}
	}

	page, err := r.b.EntriesWithFallback(agent.ID, "", 50)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	var said []string
	for _, entry := range page.Entries {
		said = append(said, entry.ID+"/"+entry.Kind+":"+entry.Text)
		if strings.Contains(entry.Text, appLivePlaceholder) {
			t.Fatalf("a stand-in for an older message was shown: %+v", entry)
		}
	}
	if len(page.Entries) < 2 || page.Entries[0].ID != "u0" || page.Entries[0].Text != "first question" || !strings.Contains(strings.Join(said, "|"), "live answer") {
		t.Fatalf("the host's history did not arrive as an agent's entries: %v", said)
	}

	// The link's word becomes agent.changed with the whole agent.
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for _, step := range []struct {
		state livedoor.State
		check func(appwire.LocationLive) bool
		name  string
	}{
		{livedoor.State{Kind: livedoor.Live, Road: "relay"}, func(l appwire.LocationLive) bool { return l.State == "live" && l.Road == "relay" }, "live"},
		{livedoor.State{Kind: livedoor.Reconnecting}, func(l appwire.LocationLive) bool { return l.State == "reconnecting" }, "reconnecting"},
		{livedoor.State{Kind: livedoor.WentOffline, Since: now}, func(l appwire.LocationLive) bool {
			return l.State == "offline" && l.SeenAt == "2026-11-23T12:00:00Z"
		}, "offline"},
		{livedoor.State{Kind: livedoor.WatchOnly}, func(l appwire.LocationLive) bool {
			return l.State == "watching" && l.Permit != nil && l.Permit.Watch && !l.Permit.Drive && l.Permit.Reason == chatlist.LiveWatchOnly(liveRigHostName)
		}, "watching"},
		{livedoor.State{Kind: livedoor.StateBudget, Reason: livedoor.ReasonBudget}, func(l appwire.LocationLive) bool {
			return l.State == "watching" && l.Permit != nil && l.Permit.Reason == chatlist.LiveBudget
		}, "watching with the allowance used"},
		{livedoor.State{Kind: livedoor.Idle, Reason: livedoor.ReasonIdle}, func(l appwire.LocationLive) bool { return l.State == "idle" }, "idle"},
	} {
		r.b.liveStateMoved(liveRigHostID, step.state)
		events := r.liveEvents()
		if len(events) == 0 || !step.check(events[len(events)-1]) {
			t.Fatalf("after a %s link the last live fact was %+v", step.name, events)
		}
	}

	// A removed computer is offline here, and says why in the host's words.
	r.b.liveStateMoved(liveRigHostID, livedoor.State{Kind: livedoor.Revoked, Reason: livedoor.ReasonRemoved, Since: now})
	r.rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return event == appwire.EventEntryAdded && ok && added.Entry.Text == chatlist.LiveRemoved
	})
}

// V41 (the road of a live view reaches the desktop): a road flip the link
// reports is one agent.changed that carries the new road with the view still
// live, and the same word said again is no event at all. The desktop draws its
// strip from this field, so a flip it never hears of would leave the strip
// saying the wrong road until something else moved.
func TestARoadFlipReachesTheDesktopAsOneAgentChangedAndARepeatIsSilent(t *testing.T) {
	r := newLiveRig(t, true)
	r.open()
	r.b.liveStateMoved(liveRigHostID, livedoor.State{Kind: livedoor.Live, Road: "relay"})
	settled := len(r.liveEvents())

	r.b.liveStateMoved(liveRigHostID, livedoor.State{Kind: livedoor.Live, Road: "direct"})
	events := r.liveEvents()
	if len(events) != settled+1 || events[len(events)-1].State != "live" || events[len(events)-1].Road != "direct" {
		t.Fatalf("a flip to the direct road must be exactly one live fact that still says live: %+v", events[min(settled, len(events)):])
	}
	r.b.liveStateMoved(liveRigHostID, livedoor.State{Kind: livedoor.Live, Road: "direct"})
	if n := len(r.liveEvents()); n != settled+1 {
		t.Fatalf("the same road said twice emitted %d more events, want none", n-settled-1)
	}

	r.b.liveStateMoved(liveRigHostID, livedoor.State{Kind: livedoor.Live, Road: "relay"})
	events = r.liveEvents()
	if len(events) != settled+2 || events[len(events)-1].Road != "relay" {
		t.Fatalf("a flip back to the relay must be one more live fact that says relay: %+v", events[min(settled, len(events)):])
	}
}

// V2 (typing in a live view): a send to a live agent is agents.send, which
// reaches the host's running conversation and needs no stored copy anywhere:
// with no sync service the waiting-note door refuses, and the live send is not
// that door.
func TestALiveSendIsAnAgentsSendAndNeverAChatsSend(t *testing.T) {
	r := newLiveRig(t, true)
	agent := r.open()
	// The rig has no sync service at all, so nothing can be stored anywhere.
	if _, err := r.b.SendToChat(appwire.ChatSendParams{Cell: liveRigCell, Text: "a waiting note"}); err == nil {
		t.Fatal("the waiting-note door must keep refusing while storage is off: the live send is not that door")
	}
	before := r.asked.Load()
	if _, err := r.b.Send(agent.ID, "second question", "client-1"); err != nil {
		t.Fatalf("agents.send on a live agent: %v", err)
	}
	r.rec.waitFor(t, 10*time.Second, appTurnDone)
	if r.asked.Load() == before {
		t.Fatal("the send never reached the host's conversation")
	}
	record := session.ReadTranscript(r.file)
	var users []string
	for _, entry := range appRecordItems(record) {
		if entry.Role == "user" {
			users = append(users, entry.Text)
		}
	}
	if !slices.Equal(users, []string{"first question", "second question"}) {
		t.Fatalf("the host's record holds %v: the live send must land as one message of its own conversation", users)
	}
	page, err := r.b.EntriesWithFallback(agent.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, entry := range page.Entries {
		ids[entry.ID] = true
	}
	if !ids["u0"] || !ids["u1"] {
		t.Fatalf("the two messages must be u0 and u1, counted from the first: %v", ids)
	}
	// The entry the send announced is the one a later read finds under that id.
	r.rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return event == appwire.EventEntryAdded && ok && added.Entry.Kind == "user" && added.Entry.Text == "second question" && added.Entry.ID == "u1"
	})
	if notes := r.b.chats.notes.deliveredTo(liveRigCell, false); len(notes) != 0 {
		t.Fatalf("a live send left a waiting note behind: %v", notes)
	}
}

// V13 (an engine without the feature is never sent a new call): with remote
// control off the desktop's hello names neither chats.live nor devices.permit,
// the two calls answer unknown_method if one is sent anyway, no row says it can
// be opened live, and no device row carries a permit.
func TestWithoutTheFeatureTheDeskNeverSeesTheNewCalls(t *testing.T) {
	r := newLiveRig(t, false)
	if r.b.ChatsLiveOffered() || r.b.DevicePermitsOffered() {
		t.Fatal("with remote control off neither door may be offered")
	}
	if _, err := r.b.OpenLive(appwire.ChatOpenLiveParams{Cell: liveRigCell}); err == nil {
		t.Fatal("chats.open_live must refuse while remote control is off")
	}
	if r.roams.Load() != 0 {
		t.Fatal("a refused open must dial nothing")
	}

	wire := newActionWire(t, r.b)
	for _, feature := range wire.hello {
		if feature == appwire.FeatureChatsLive || feature == appwire.FeatureDevicesPermit {
			t.Fatalf("hello names %v with remote control off: %v", feature, wire.hello)
		}
	}
	for method, params := range map[string]any{
		"chats.open_live":     appwire.ChatOpenLiveParams{Cell: liveRigCell},
		"devices.permit":      map[string]any{"id": "x", "watch": true},
		"chats.live_keyboard": appwire.ChatLiveKeyboardParams{Agent: liveRigCell, Take: true},
	} {
		if _, err := wire.call(method, params); err == nil || err.Code != appwire.CodeUnknownMethod {
			t.Fatalf("%s while the feature is off answered %v", method, err)
		}
	}

	// And its behaviour is the same as before for a chat that is not live: no
	// row says it can be opened live, and an agent held elsewhere with no live
	// view is still refused.
	online := true
	row := appwire.ChatRow{Cell: liveRigCell, Status: string(chatlist.Running), Online: &online}
	if appCanLive(row, chatlist.Row{DeviceID: liveRigHostID}, true) {
		t.Fatal("a row said it could be opened live with remote control off")
	}
	refusal := appLocationRefusal(&appwire.Location{Cell: liveRigCell, Line: "held by studio", Holder: &appwire.LocationHolder{ID: liveRigHostID, Name: liveRigHostName}})
	var held *appwire.Error
	if !errors.As(refusal, &held) || held.Code != appwire.CodeConflict {
		t.Fatalf("an agent held elsewhere without a live view must keep its refusal: %v", refusal)
	}
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	if !appCanLive(row, chatlist.Row{DeviceID: liveRigHostID}, true) {
		t.Fatal("with remote control on a running chat on an online computer can be opened live")
	}
}

// V12 (leaving the app): closing the backend closes every link, and the link of
// a host nothing views any more is closed at once, so a view nobody is using
// costs the relay nothing.
func TestAnIdleLiveViewHoldsNoLinkAndTheNextTouchReconnects(t *testing.T) {
	r := newLiveRig(t, true)
	agent := r.open()
	<-r.links
	a, err := r.b.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.b.liveChannelEnded(liveRigHostID, liveRigCell, livedoor.ReasonIdle)
	deadline := time.Now().Add(5 * time.Second)
	for r.b.liveOf(a) != nil {
		if time.Now().After(deadline) {
			t.Fatal("an idle detach never let go of the connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.b.liveMu.Lock()
	hosts := len(r.b.liveHosts)
	r.b.liveMu.Unlock()
	if hosts != 0 {
		t.Fatalf("%d links are still held with no view using them", hosts)
	}
	if last := r.liveEvents(); len(last) == 0 || last[len(last)-1].State != "idle" {
		t.Fatalf("a view whose connection was let go reads idle, got %+v", last)
	}
	// The next agents.* call reconnects, through the usual open.
	if _, err := r.b.Send(agent.ID, "back again", "client-2"); err != nil {
		t.Fatalf("the send that follows an idle detach must reconnect: %v", err)
	}
	r.rec.waitFor(t, 10*time.Second, appTurnDone)
	if r.roams.Load() != 2 {
		t.Fatalf("the connection was opened %d times, want 2 (the open, then the reconnect)", r.roams.Load())
	}
}

// V16 (a live view is the other computer's to set): its model, posture and
// budget are fenced, and a live agent leaves its client tools alone.
func TestALiveViewsDialsAreTheHostsToSet(t *testing.T) {
	r := newLiveRig(t, true)
	agent := r.open()
	if _, err := r.b.SetModel(agent.ID, "other/model"); err == nil || !strings.Contains(err.Error(), "another computer") {
		t.Fatalf("the model of a live view is set on its own computer, got %v", err)
	}
}
