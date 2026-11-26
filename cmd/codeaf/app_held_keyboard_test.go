package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// These tests are the desk's way of taking the keyboard back on a chat it holds
// (contract K1-K6 in NOTES-POLISH). They use the real chat opener over a real
// holder door and a real second computer dialled to it, and read what the app is
// TOLD (agent.changed carrying location.keyboard) and what the app's one call
// (chats.live_keyboard) does, never how the engine keeps the fact.

// driverViewer is another computer that may type: the hello the live door's
// router writes for a computer whose permit allows driving, so it can take the
// keyboard by asking. The rig's own viewer only watches.
func (r *deskHeldRig) driverViewer(socket string) (*remote.Client, error) {
	r.t.Helper()
	conn, err := livedoor.UnixTransport().Dial(context.Background(), socket)
	if err != nil {
		r.t.Fatalf("the holder door did not accept a connection: %v", err)
	}
	client, err := remote.Dial(conn, "studio", remote.Hello{
		Workspace: r.root, Session: r.file, Join: true, Back: true, Surface: "laptop",
		Remote: &remote.RemoteViewer{Device: "devicelaptop0002", Label: "laptop", Watch: true, Drive: true},
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	r.t.Cleanup(func() { _ = client.Close() })
	return client, nil
}

// keyboardOf is the keyboard fact an agent.changed carries, nil when it carries
// none.
func keyboardOf(data any) *appwire.LiveDriver {
	changed, ok := data.(appwire.AgentChanged)
	if !ok || changed.Agent.Location == nil {
		return nil
	}
	return changed.Agent.Location.Keyboard
}

// heldKeyboardRig is a desk with one held chat open, a backend over it, and the
// door's socket.
type heldKeyboardRig struct {
	*deskHeldRig
	b      *appBackend
	rec    *appRecorder
	id     string
	socket string
}

func newHeldKeyboardRig(t *testing.T) *heldKeyboardRig {
	t.Helper()
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
	return &heldKeyboardRig{deskHeldRig: r, b: b, rec: rec, id: agent.ID, socket: socket}
}

// view is the agent as the app would draw it now.
func (h *heldKeyboardRig) view() appwire.Agent {
	h.t.Helper()
	a, err := h.b.find(h.id)
	if err != nil {
		h.t.Fatal(err)
	}
	var view appwire.Agent
	h.b.locked(func() { view = a.view })
	return view
}

// waitKeyboard waits for an agent.changed for this chat whose keyboard fact
// satisfies ok, after the events already seen.
func (h *heldKeyboardRig) waitKeyboard(what string, ok func(*appwire.LiveDriver) bool) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var last *appwire.LiveDriver
		found := false
		h.rec.mu.Lock()
		for i, event := range h.rec.events {
			if event != appwire.EventAgentChanged {
				continue
			}
			if changed, isAgent := h.rec.data[i].(appwire.AgentChanged); isAgent && changed.Agent.ID == h.id {
				last, found = keyboardOf(changed), true
			}
		}
		h.rec.mu.Unlock()
		if found && ok(last) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("the app was never told %s; the view says %+v", what, h.view().Location)
}

// K3: nothing is said while the desk holds the keyboard, whether or not another
// computer is attached; it is said when another computer takes it (K1), the
// fact names the computer and is not "yours", and it goes (K4) the moment that
// computer leaves, with no click, because the engine hands the keyboard back.
func TestADeskHeldChatSaysWhoHasTheKeyboardAndWhenItIsBack(t *testing.T) {
	h := newHeldKeyboardRig(t)
	if loc := h.view().Location; loc != nil && loc.Keyboard != nil {
		t.Fatalf("a chat nobody else is in carries a keyboard fact: %+v", loc.Keyboard)
	}
	laptop, err := h.driverViewer(h.socket)
	if err != nil {
		t.Fatalf("another computer could not join: %v", err)
	}
	// Joining does not move the keyboard: a remote viewer takes it only by asking.
	time.Sleep(200 * time.Millisecond)
	if loc := h.view().Location; loc != nil && loc.Keyboard != nil {
		t.Fatalf("another computer joined without taking the keyboard and the desk says %+v", loc.Keyboard)
	}
	if err := laptop.Take(); err != nil {
		t.Fatal(err)
	}
	h.waitKeyboard("that the laptop has the keyboard", func(d *appwire.LiveDriver) bool {
		return d != nil && d.Machine == "laptop" && !d.Yours && !d.Here
	})
	if loc := h.view().Location; loc == nil || !loc.Here || loc.Keyboard == nil || loc.Keyboard.Machine != "laptop" {
		t.Fatalf("the agent's own view does not carry the fact on a chat held here: %+v", loc)
	}
	// The computer leaves; the desk is the typist again without anybody pressing.
	_ = laptop.Close()
	h.waitKeyboard("that the keyboard came back", func(d *appwire.LiveDriver) bool { return d == nil })
	if loc := h.view().Location; loc != nil && loc.Keyboard != nil {
		t.Fatalf("the fact outlived the other computer: %+v", loc.Keyboard)
	}
}

// K2, K6: pressing Type here is one call; the desk is the typist at once, the
// fact goes, the other computer is told and its own send is refused in the
// engine's sentence; and while the other computer holds the keyboard the desk's
// own send is refused the same way, which is the sentence the composer shows.
func TestTheDeskTakesTheKeyboardBackOnAChatItHolds(t *testing.T) {
	h := newHeldKeyboardRig(t)
	laptop, err := h.driverViewer(h.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := laptop.Take(); err != nil {
		t.Fatal(err)
	}
	h.waitKeyboard("that the laptop has the keyboard", func(d *appwire.LiveDriver) bool { return d != nil && d.Machine == "laptop" })

	a, _ := h.b.find(h.id)
	live := h.b.liveOf(a)
	if live == nil {
		t.Fatal("the held chat is not open")
	}
	// K6: the desk's own words are refused with the engine's sentence naming who has it.
	if _, err := live.conn.Conv.Submit(context.Background(), "typed on the desk"); err == nil || !strings.Contains(err.Error(), "laptop") {
		t.Fatalf("the desk's send while the laptop types was %v, want a refusal naming the laptop", err)
	}

	got, err := h.b.LiveKeyboard(appwire.ChatLiveKeyboardParams{Agent: h.id, Take: true})
	if err != nil {
		t.Fatalf("Type here was refused: %v", err)
	}
	if !got.Driver.Yours {
		t.Fatalf("the answer to Type here says %+v, want the desk as the typist", got.Driver)
	}
	h.waitKeyboard("that the keyboard is the desk's", func(d *appwire.LiveDriver) bool { return d == nil })
	if loc := h.view().Location; loc != nil && loc.Keyboard != nil {
		t.Fatalf("the desk took the keyboard and still says %+v", loc.Keyboard)
	}
	// The laptop is told, and its own send is now refused in the engine's words.
	deadline := time.Now().Add(10 * time.Second)
	for laptop.Driver().Yours && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if laptop.Driver().Yours {
		t.Fatal("the laptop still thinks it has the keyboard after the desk took it")
	}
	if _, err := laptop.Agent().Submit(context.Background(), "typed on the laptop"); err == nil {
		t.Fatal("the laptop's send was accepted while the desk has the keyboard")
	}
	// Nothing was lost: the desk's answer to its own words goes on as always.
	if told := h.say(h.liveConn(), "back on the desk"); len(told) == 0 {
		t.Fatal("the desk's own turn after taking the keyboard back said nothing")
	}
}

func (h *heldKeyboardRig) liveConn() *appConn {
	h.t.Helper()
	a, _ := h.b.find(h.id)
	live := h.b.liveOf(a)
	if live == nil {
		h.t.Fatal("the held chat is not open")
	}
	return live.conn
}

// K3, K5: Type here is answered only for a chat this desktop holds for others
// and only while remote control is on. On a desktop with it off the call
// answers what it always did, and no agent ever carries a keyboard fact.
func TestWithRemoteControlOffThereIsNoKeyboardFactAndTheCallAnswersAsBefore(t *testing.T) {
	r := newDeskHeldRig(t, false)
	rec := newAppRecorder()
	b := newFakeBackend(rec, func(appOpenAsk) (*appConn, error) { return r.open(), nil })
	t.Cleanup(func() { _ = b.Close() })
	agent, err := mustCreate(t, b, appwire.CreateAsk{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.LiveKeyboard(appwire.ChatLiveKeyboardParams{Agent: agent.ID, Take: true})
	var wireErr *appwire.Error
	if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeConflict || !strings.Contains(wireErr.Message, "this chat is not open live") {
		t.Fatalf("Type here on a chat that is not offered answered %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i, event := range rec.events {
		if event == appwire.EventAgentChanged && keyboardOf(rec.data[i]) != nil {
			t.Fatalf("an agent carried a keyboard fact with remote control off: %+v", rec.data[i])
		}
	}
}

// The fact is the app's own and the chat list's word about where the chat is
// held must not wipe it: a refreshed location (the chat is here) or none keeps
// what the engine last said, and a location that says the chat is NOT here
// carries no keyboard, because the desk is not running it.
func TestAChatListUpdateKeepsTheKeyboardFactOfAChatHeldHere(t *testing.T) {
	h := newHeldKeyboardRig(t)
	laptop, err := h.driverViewer(h.socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := laptop.Take(); err != nil {
		t.Fatal(err)
	}
	h.waitKeyboard("that the laptop has the keyboard", func(d *appwire.LiveDriver) bool { return d != nil && d.Machine == "laptop" })
	a, _ := h.b.find(h.id)

	h.b.located(a, &appwire.Location{Here: true, Cell: "c1", Seen: &appwire.Seen{Device: "d", Fence: 3}})
	loc := h.view().Location
	if loc == nil || loc.Cell != "c1" || loc.Keyboard == nil || loc.Keyboard.Machine != "laptop" {
		t.Fatalf("a refreshed location lost the keyboard fact: %+v", loc)
	}
	h.b.located(a, nil)
	if loc := h.view().Location; loc == nil || !loc.Here || loc.Keyboard == nil {
		t.Fatalf("a missing location lost the keyboard fact: %+v", loc)
	}
	h.b.located(a, &appwire.Location{Here: false, Cell: "c1", Line: "tower has this chat"})
	if loc := h.view().Location; loc == nil || loc.Keyboard != nil {
		t.Fatalf("a chat the list says is held elsewhere still carries a keyboard fact: %+v", loc)
	}
}

// The wire shape: absent unless the desk holds a chat another window types in,
// spelled with the same three words a live view's driver uses.
func TestTheKeyboardFactIsOmittedUnlessSaid(t *testing.T) {
	raw, err := json.Marshal(appwire.Location{Here: true})
	if err != nil || strings.Contains(string(raw), "keyboard") {
		t.Fatalf("a location with no fact says %s, %v", raw, err)
	}
	raw, err = json.Marshal(appwire.Location{Here: true, Keyboard: &appwire.LiveDriver{Machine: "laptop"}})
	if err != nil || !strings.Contains(string(raw), `"keyboard":{"machine":"laptop"}`) {
		t.Fatalf("the fact says %s, %v", raw, err)
	}
}
