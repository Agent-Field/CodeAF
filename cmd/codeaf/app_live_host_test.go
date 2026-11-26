package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The rig of these tests is the real chat opener's own tail
// ([appOpenAssembled]) over a real conversation and a stand-in provider, so
// what is asserted is how the desktop holds a chat and who else can reach it,
// never how the engine was assembled. The holder door is a real one on a real
// socket under a short state root, and the "other computer" is a real client
// dialled to it the way the live door's router dials a holder.

type deskHeldRig struct {
	t        *testing.T
	root     string
	file     string
	engine   *session.Agent
	provider *httptest.Server
	asked    atomic.Int32
	booted   atomic.Int32
}

func newDeskHeldRig(t *testing.T, remoteControl bool) *deskHeldRig {
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
	// A door of this test's own, taken down with it.
	previous := deskHost
	deskHost = newAppDeskHost()
	t.Cleanup(func() {
		deskHost.mu.Lock()
		l := deskHost.listener
		deskHost.listener = nil
		deskHost.mu.Unlock()
		if l != nil {
			_ = l.Close()
		}
		deskHost = previous
	})

	r := &deskHeldRig{t: t}
	r.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.asked.Add(1)
		_, _ = io.Copy(io.Discard, req.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"desk answer"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(r.provider.Close)
	r.root = t.TempDir()
	r.file = filepath.Join(r.root, "transcript.jsonl")
	engine, err := session.New(session.Config{Workspace: r.root, SessionFile: r.file, Model: "test/model", APIKey: "fixture",
		BaseURL: r.provider.URL, OneModel: true, DeterministicNames: true, PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	r.engine = engine
	return r
}

// open is the desktop opening this chat as its own.
func (r *deskHeldRig) open() *appConn {
	r.t.Helper()
	conn, err := appOpenAssembled(remote.Hello{Workspace: r.root, Session: r.file}, func(remote.Hello) (*remote.Engine, error) {
		r.booted.Add(1)
		return &remote.Engine{Agent: r.engine, SessionFile: r.file, Workspace: r.root}, nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return conn
}

// registry reads the holder door's registry file for this process, the file the
// live door's resolver dials, and returns its socket's full path.
func (r *deskHeldRig) registry() (socket string, ok bool) {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(home.Dir(), "v3", "live", fmt.Sprint(os.Getpid())+".json"))
	if err != nil {
		return "", false
	}
	var entry struct {
		PID    int    `json:"pid"`
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil || entry.PID != os.Getpid() || entry.Socket == "" {
		r.t.Fatalf("the registry file is not the holder's own: %s", raw)
	}
	return filepath.Join(home.Dir(), "v3", "live", entry.Socket), true
}

// viewer is another computer's connection to the chat through the holder door:
// the hello the live door's router writes for a watcher, over the door's socket.
func (r *deskHeldRig) viewer(socket string) (*remote.Client, error) {
	r.t.Helper()
	conn, err := livedoor.UnixTransport().Dial(context.Background(), socket)
	if err != nil {
		r.t.Fatalf("the holder door did not accept a connection: %v", err)
	}
	client, err := remote.Dial(conn, "studio", remote.Hello{
		Workspace: r.root, Session: r.file, Join: true, Watch: true, Back: true,
		Remote: &remote.RemoteViewer{Device: "devicelaptop0001", Label: "laptop", Watch: true},
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	r.t.Cleanup(func() { _ = client.Close() })
	return client, nil
}

// say is the desktop's person sending one message in the chat, and what the
// desktop's own view was told of the turn, in order.
func (r *deskHeldRig) say(conn *appConn, text string) []string {
	r.t.Helper()
	events, err := conn.Conv.Submit(context.Background(), text)
	if err != nil {
		r.t.Fatal(err)
	}
	var told []string
	for ev := range events {
		told = append(told, fmt.Sprintf("%v|%s|%s", ev.Kind, ev.Tool, ev.Text))
	}
	return told
}

func (r *deskHeldRig) transcriptOf(client *remote.Client) []string {
	r.t.Helper()
	page, err := client.Agent().TranscriptPage(context.Background(), 0, 50)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, entry := range page.Entries {
		out = append(out, entry.Role+"|"+entry.Text)
	}
	return out
}

// V-desk-1 / V-desk-2 / V-desk-3 (a chat the desktop is running is reachable
// through the desktop's holder door, and only while the desktop holds it): with
// remote control on, a chat the desktop opens is listed in the holder door's
// registry and another computer's watcher joins THE SAME conversation the
// desktop's own window is in, seeing a turn the desktop sends. Closing the chat
// takes it off the door at once, ends the watcher's connection, and closes the
// engine's conversation; a second watcher is told the chat is not open here. A
// watcher's own departure ends nothing: the desktop's window is still in the room.
func TestADeskHeldChatIsRoutableByTheDoorAndReleasedWhenClosed(t *testing.T) {
	r := newDeskHeldRig(t, true)
	if _, found := r.registry(); found {
		t.Fatal("the door must not exist before a chat is held")
	}
	conn := r.open()
	t.Cleanup(conn.Release)
	socket, found := r.registry()
	if !found {
		t.Fatal("a chat the desktop opened with remote control on has no holder door")
	}
	if got := deskHost.viewers(r.file); got != 0 {
		t.Fatalf("nobody else is watching yet, the door says %d", got)
	}
	if !conn.Conv.(*remote.Agent).WorkOutlivesExit() {
		t.Fatal("the chat behind the door is a persistent conversation, so a viewer's torn pipe keeps it")
	}

	viewer, err := r.viewer(socket)
	if err != nil {
		t.Fatalf("another computer could not join the chat through the door: %v", err)
	}
	if got := deskHost.viewers(r.file); got != 1 {
		t.Fatalf("one other computer is watching, the door says %d", got)
	}
	r.say(conn, "from the desktop")
	if r.asked.Load() == 0 {
		t.Fatal("the desktop's own turn never reached the provider")
	}
	var seen []string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if seen = r.transcriptOf(viewer); strings.Contains(strings.Join(seen, "\n"), "desk answer") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if want := "user|from the desktop"; !strings.Contains(strings.Join(seen, "\n"), want) || !strings.Contains(strings.Join(seen, "\n"), "assistant|desk answer") {
		t.Fatalf("the watcher joined another conversation: %q does not hold %q and the answer", seen, want)
	}

	// A watcher leaving is not the chat ending.
	_ = viewer.Close()
	deadline = time.Now().Add(10 * time.Second)
	for deskHost.viewers(r.file) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if deskHost.viewers(r.file) != 0 || r.engine.Closed() {
		t.Fatal("a watcher's departure closed the desktop's chat or was never noticed")
	}
	second, err := r.viewer(socket)
	if err != nil {
		t.Fatalf("a watcher that came back could not join: %v", err)
	}

	conn.Release()
	if !r.engine.Closed() {
		t.Fatal("closing the chat left its conversation open")
	}
	if _, ok := deskHost.lookup(r.file); ok {
		t.Fatal("a closed chat is still handed out by the door")
	}
	deadline = time.Now().Add(10 * time.Second)
	for second.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if second.Err() == nil {
		t.Fatal("the watcher of a closed chat was never told")
	}
	if _, err := r.viewer(socket); err == nil || !strings.Contains(err.Error(), "not open here") {
		t.Fatalf("a chat that is not open here must say so, got %v", err)
	}
	deskHost.closeListener()
	if _, found := r.registry(); found {
		t.Fatal("the registry file outlived the door")
	}
}

// V-desk-4 (a desktop with remote control off has no door and holds chats as it
// always did): no listener, no registry file, no socket, and the chat's
// conversation is a lifetime of its one pipe, the shape the desktop has always
// opened.
func TestADeskChatWithRemoteControlOffHasNoDoor(t *testing.T) {
	r := newDeskHeldRig(t, false)
	conn := r.open()
	t.Cleanup(conn.Release)
	if _, found := r.registry(); found {
		t.Fatal("remote control is off, and the desktop opened a holder door")
	}
	if entries, err := os.ReadDir(filepath.Join(home.Dir(), "v3", "live")); err == nil && len(entries) > 0 {
		t.Fatalf("remote control is off, and the desktop left files in the live folder: %v", entries)
	}
	if conn.Conv.(*remote.Agent).WorkOutlivesExit() {
		t.Fatal("remote control is off, and the chat is a persistent conversation")
	}
	deskHost.mu.Lock()
	held := len(deskHost.sessions)
	deskHost.mu.Unlock()
	if held != 0 {
		t.Fatalf("remote control is off, and the desktop filed %d chats under a door", held)
	}
}

// V-desk-5 (a desk chat nobody else is watching behaves as it always did): the
// same exchange through the door and without it tells the desktop's own view the
// same turn in the same order, leaves the same record, tells the app the same
// opening facts, and ends the conversation the same way when it is let go. The
// persistent shape is invisible to the one view that was always there.
func TestADeskChatNobodyElseWatchesBehavesAsWithoutTheDoor(t *testing.T) {
	type observed struct {
		told        []string
		transcript  []string
		note        string
		interrupted string
		inRoot      bool
		users       int
		closed      bool
	}
	run := func(on bool) observed {
		r := newDeskHeldRig(t, on)
		conn := r.open()
		out := observed{note: conn.Note, interrupted: strings.Join(conn.Interrupted, ","), inRoot: conn.Workspace == r.root, users: conn.UserMessages}
		out.told = r.say(conn, "same words")
		out.told = append(out.told, r.say(conn, "and again")...)
		page, err := conn.Conv.(*remote.Agent).TranscriptPage(context.Background(), 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			out.transcript = append(out.transcript, entry.Role+"|"+entry.Text)
		}
		conn.Release()
		out.closed = r.engine.Closed()
		// Letting go twice is the same as once, as it has always been.
		conn.Release()
		return out
	}
	off, on := run(false), run(true)
	// THE TITLE ARRIVES FROM ITS OWN GOROUTINE, so where among the turn's first
	// events it lands is the engine's timing on either shape and not something
	// the door could change. It is compared as a fact (the same words, once);
	// everything else is compared in order.
	titled := func(told []string) (title string, rest []string) {
		mark := fmt.Sprintf("%v||", session.EventTitleChanged)
		for _, line := range told {
			if strings.HasPrefix(line, mark) {
				title += line
				continue
			}
			rest = append(rest, line)
		}
		return title, rest
	}
	offTitle, offRest := titled(off.told)
	onTitle, onRest := titled(on.told)
	off.told, on.told = offRest, onRest
	if offTitle != onTitle || offTitle == "" {
		t.Fatalf("the chat was named differently: without the door %q, with it %q", offTitle, onTitle)
	}
	if strings.Join(off.told, "\n") != strings.Join(on.told, "\n") || len(off.told) == 0 {
		t.Fatalf("the desktop's own view was told a different turn:\n without the door: %q\n with the door:    %q", off.told, on.told)
	}
	if strings.Join(off.transcript, "\n") != strings.Join(on.transcript, "\n") || len(off.transcript) < 4 {
		t.Fatalf("the record differs:\n without the door: %q\n with the door:    %q", off.transcript, on.transcript)
	}
	if off.note != on.note || off.interrupted != on.interrupted || off.inRoot != on.inRoot || off.users != on.users {
		t.Fatalf("the app was told different opening facts: %+v vs %+v", off, on)
	}
	if !off.closed || !on.closed {
		t.Fatalf("a chat let go must be closed: without the door %v, with it %v", off.closed, on.closed)
	}
}

// V-desk-6 (a chat somebody is watching through this desktop is not let go as
// idle, and is again once they leave): the idle sweep keeps a held chat while
// another computer is attached to it, and the clock starts over when they go.
func TestTheIdleSweepKeepsAChatAnotherComputerIsWatching(t *testing.T) {
	r := newDeskHeldRig(t, true)
	rec := newAppRecorder()
	b := newFakeBackend(rec, func(appOpenAsk) (*appConn, error) { return r.open(), nil })
	t.Cleanup(func() { _ = b.Close() })
	agent, err := mustCreate(t, b, appwire.CreateAsk{})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := b.find(agent.ID)
	b.locked(func() { a.transcript = r.file })
	socket, found := r.registry()
	if !found {
		t.Fatal("the held chat has no holder door")
	}
	viewer, err := r.viewer(socket)
	if err != nil {
		t.Fatal(err)
	}
	old := func() {
		b.locked(func() {
			if live := a.live; live != nil {
				live.lastUse = b.now().Add(-appAgentIdle - time.Second)
				live.running = false
			}
		})
	}
	old()
	b.sweepAgent(a, b.now())
	if b.liveOf(a) == nil {
		t.Fatal("the sweep let go a chat another computer is watching")
	}
	_ = viewer.Close()
	deadline := time.Now().Add(10 * time.Second)
	for deskHost.viewers(r.file) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	old()
	b.sweepAgent(a, b.now())
	if b.liveOf(a) != nil {
		t.Fatal("the sweep kept an idle chat nobody is watching")
	}
}
