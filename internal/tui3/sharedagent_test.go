package tui3

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── A DOOR WHOSE CONVERSATIONS SHARE ONE HANDLE ─────────────────────────────
//
// The engine doors — `--host`, `--at`, and the socket an ordinary `aforge chat`
// opens onto this machine's own engine — all answer [Options.Resume] with the
// AGENT THEY WERE GIVEN, because that agent holds no state: it is a handle on
// whichever conversation the engine currently has open, and the engine has just
// swapped which one that is (cmd/aforge's chatv3_host.go, internal/remote's
// Agent, and chatv3_host_shared_test.go proves both against the real server).
//
// This file is the SURFACE's half of that contract. The fake below is faithful
// to the wire in the one way that matters: opening a conversation mutates what
// the single handle names and what it is called, and the far end ends the
// conversation it replaced.

// sharedEngine is the far side of a connection with ONE open conversation.
type sharedEngine struct {
	// at is the transcript the engine currently has open, and names is what each
	// conversation on its disk is called.
	at    string
	names map[string]string
	// ended is the conversations the ENGINE closed as part of a swap, which is
	// where a conversation being left actually goes (internal/remote's
	// Session.swap interrupts and closes the previous agent).
	ended []string
	// stopped and shut are what the SURFACE asked of the handle, each recorded
	// with the conversation that was open at the moment of asking — which is the
	// whole point: a close made after a swap is a close of the WRONG session,
	// and only the pairing shows it.
	stopped, shut []string
	// lanes is how many task lanes are open on this one handle right now. A
	// second reader here is the keeper draining the conversation the surface is
	// itself drawing.
	lanes int
}

// open is the engine's own swap.
func (e *sharedEngine) open(file string) {
	if e.at != "" && e.at != file {
		e.ended = append(e.ended, e.at)
	}
	e.at = file
}

// sharedHandle is the one agent this connection has.
type sharedHandle struct {
	*fakeAgent
	e *sharedEngine
}

func (h *sharedHandle) Title() string { return h.e.names[h.e.at] }
func (h *sharedHandle) Interrupt() {
	h.e.stopped = append(h.e.stopped, h.e.at)
	h.fakeAgent.Interrupt()
}
func (h *sharedHandle) Close() error {
	h.e.shut = append(h.e.shut, h.e.at)
	return h.fakeAgent.Close()
}

// WatchTaskUpdates is the lane the keeper's watcher would take a second copy of.
func (h *sharedHandle) WatchTaskUpdates() (<-chan session.Event, func()) {
	h.e.lanes++
	lane := make(chan session.Event)
	var once sync.Once
	return lane, func() { once.Do(func() { h.e.lanes--; close(lane) }) }
}

// sharedSurface is a window on a connection, sitting in conversation A.
func sharedSurface(t *testing.T) (*app, *sharedEngine, *sharedHandle) {
	t.Helper()
	engine := &sharedEngine{at: "/srv/app/a.jsonl", names: map[string]string{
		"/srv/app/a.jsonl": "Sweeping journal",
		"/srv/app/b.jsonl": "Porting",
	}}
	handle := &sharedHandle{fakeAgent: &fakeAgent{model: "m"}, e: engine}
	a := newTestApp(handle)
	a.host = "devbox"
	a.shared = true
	a.stirs = make(chan string, stirDepth)
	// The door hands the SAME handle back and swaps what it names, which is the
	// hosted Resume, spelled here in four lines.
	a.resume = func(file string) (Agent, error) {
		engine.open(file)
		return handle, nil
	}
	drain(t, a, a.attachConversation(Conversation{Agent: handle,
		SessionFile: "/srv/app/a.jsonl", Workspace: "/srv/app"}, nil))
	return a, engine, handle
}

// THE DEFECT, END TO END ON THE SURFACE: A → B → A, with the switcher's own map
// and the far machine's own record asserted at every step.
//
// What it looked like before the fix: the keeper took the handle the door
// answered with — the conversation now IN FRONT — and filed it under the key of
// the one being left. So the switcher listed the conversation just opened twice,
// one of those rows under the name of the conversation that was gone, pressing
// that row opened the wrong body, and a second reader sat draining the lanes of
// the conversation on screen.
func TestOverASharedHandleOpeningAnotherConversationSwapsRatherThanKeepingBoth(t *testing.T) {
	a, engine, handle := sharedSurface(t)
	lanes := engine.lanes

	if got := a.title; got != "Sweeping journal" {
		t.Fatalf("the window opened on %q", got)
	}

	// A → B, through the door home, the switcher and the search place all press.
	cmd, refusal := a.openBeside("/srv/app", "/srv/app/b.jsonl")
	if refusal != "" {
		t.Fatalf("opening the other conversation was refused: %s", refusal)
	}
	drain(t, a, cmd)

	if a.file != "/srv/app/b.jsonl" || a.title != "Porting" {
		t.Fatalf("the surface is on %q called %q", a.file, a.title)
	}
	// NOTHING IS HELD, because there is nothing to hold: one handle, and the
	// engine has ended the conversation it replaced.
	if len(a.behind) != 0 {
		t.Fatalf("the keeper holds %d conversations on a connection that has one", len(a.behind))
	}
	if a.openCount() != 1 {
		t.Fatalf("this window says it holds %d conversations", a.openCount())
	}
	if got := strings.Join(engine.ended, ","); got != "/srv/app/a.jsonl" {
		t.Fatalf("the engine ended %q", got)
	}
	// AND THE SURFACE CLOSED NOTHING, which is the other half: the handle it
	// would have closed is the one naming B.
	if len(engine.shut) != 0 || len(engine.stopped) != 0 {
		t.Fatalf("the surface interrupted %v and closed %v — on this door both land on the conversation just opened",
			engine.stopped, engine.shut)
	}
	if engine.lanes != lanes {
		t.Fatalf("%d task lanes are open on one handle, and one conversation was drawn (%d before)", engine.lanes, lanes)
	}
	if handle.closes != 0 {
		t.Fatalf("the one handle on this connection was closed %d times", handle.closes)
	}

	// B → A, back the way it came. The name has to follow the handle.
	cmd, refusal = a.openBeside("/srv/app", "/srv/app/a.jsonl")
	if refusal != "" {
		t.Fatalf("going back was refused: %s", refusal)
	}
	drain(t, a, cmd)

	if a.file != "/srv/app/a.jsonl" || a.title != "Sweeping journal" {
		t.Fatalf("after going back the surface is on %q called %q", a.file, a.title)
	}
	if len(a.behind) != 0 {
		t.Fatalf("the keeper holds %d conversations after going back", len(a.behind))
	}
	if got := strings.Join(engine.ended, ","); got != "/srv/app/a.jsonl,/srv/app/b.jsonl" {
		t.Fatalf("the engine ended %q", got)
	}
	if len(engine.shut) != 0 {
		t.Fatalf("the surface closed %v", engine.shut)
	}
	if engine.lanes != lanes {
		t.Fatalf("%d task lanes are open after two swaps (%d before)", engine.lanes, lanes)
	}
}

// A door that could not keep the conversation beside says so, rather than
// leaving somebody to notice their work is off the switcher.
func TestASharedHandleSaysItHoldsOneConversationAtATime(t *testing.T) {
	a, _, _ := sharedSurface(t)
	cmd, refusal := a.openBeside("/srv/app", "/srv/app/b.jsonl")
	if refusal != "" {
		t.Fatalf("opening the other conversation was refused: %s", refusal)
	}
	drain(t, a, cmd)
	line := plain(lastNote(t, a))
	if !strings.Contains(line, oneConversationWord) {
		t.Fatalf("the door said %q and never that it holds one conversation at a time", line)
	}
	if !strings.Contains(line, "Sweeping journal") {
		t.Fatalf("the door said %q without naming the conversation it closed", line)
	}
}

// /resume over a connection must not close what it just opened. It is the same
// handle, and [app.openSession] opens BEFORE it closes.
func TestResumingOverASharedHandleDoesNotCloseTheConversationItJustOpened(t *testing.T) {
	a, engine, handle := sharedSurface(t)
	cmd, refusal := a.openSession(Session{File: "/srv/app/b.jsonl"})
	if refusal != "" {
		t.Fatalf("/resume was refused: %s", refusal)
	}
	drain(t, a, cmd)

	if a.file != "/srv/app/b.jsonl" || a.title != "Porting" {
		t.Fatalf("/resume left the surface on %q called %q", a.file, a.title)
	}
	if handle.closes != 0 || handle.stops != 0 {
		t.Fatalf("/resume interrupted the handle %d times and closed it %d — both would land on the conversation it opened",
			handle.stops, handle.closes)
	}
	if len(engine.shut) != 0 {
		t.Fatalf("the surface closed %v", engine.shut)
	}
	if len(a.behind) != 0 {
		t.Fatalf("/resume left %d conversations in the keeper", len(a.behind))
	}
}

// AND THE LOCAL DOOR IS UNCHANGED, which is the other half of the guard: an
// in-process conversation is a real second agent, /resume closes the one it
// leaves, and a door that opens beside keeps it running.
func TestALocalDoorStillClosesWhatResumeLeavesAndKeepsWhatItOpensBeside(t *testing.T) {
	first := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(first)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan string, stirDepth)
	second := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.resume = func(string) (Agent, error) { return second, nil }

	// Beside: the conversation left behind is kept, running.
	cmd, refusal := a.openBeside("/tmp/lab/two", "/tmp/lab/two/transcript.jsonl")
	if refusal != "" {
		t.Fatalf("opening beside was refused: %s", refusal)
	}
	drain(t, a, cmd)
	if len(a.behind) != 1 || first.closed {
		t.Fatalf("the local keeper holds %d conversations and the one left was closed=%v", len(a.behind), first.closed)
	}

	// /resume: the conversation on screen is closed for real.
	third := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.resume = func(string) (Agent, error) { return third, nil }
	cmd, refusal = a.openSession(Session{File: "/tmp/lab/three/transcript.jsonl"})
	if refusal != "" {
		t.Fatalf("/resume was refused: %s", refusal)
	}
	drain(t, a, cmd)
	if !second.closed {
		t.Fatal("/resume on a local door left the conversation it was holding open")
	}
}

// The option is what carries the fact from the door to the surface.
func TestTheSharedHandleContractArrivesThroughTheOption(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if a.shared {
		t.Fatal("a door that said nothing was read as sharing one handle")
	}
	b := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, SharedAgent: true,
		Workspace: "/tmp/lab", UsageLedger: labLedger()})
	if !b.shared {
		t.Fatal("a door that said its conversations share one handle was not believed")
	}
}

func TestSharedChatRoundTripRestoresEachDraftAndCaret(t *testing.T) {
	a, _, _ := sharedSurface(t)
	a.draftFile = filepath.Join(t.TempDir(), "window-draft.txt")
	draftPath := a.draftFile
	a.width, a.height = 160, 40
	a.tabsRow(160)
	a.input.setText("draft for A")
	a.input.cursor = 3
	cmd, refusal := a.openBeside("/srv/app", "/srv/app/b.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	strip := plain(a.tabsRow(160))
	if !strings.Contains(strip, "Sweeping journal") || !strings.Contains(strip, "Porting") {
		t.Fatalf("shared selection forgot the outgoing tab: %q", strip)
	}
	if a.draftFile != draftPath {
		t.Fatalf("shared selection dropped its local draft store: %q", a.draftFile)
	}
	if a.input.String() != "" {
		t.Fatalf("A's unsent words leaked into B: %q", a.input.String())
	}
	a.input.setText("draft for B")
	a.input.cursor = 5
	cmd, refusal = a.openBeside("/srv/app", "/srv/app/a.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	if a.input.String() != "draft for A" || a.input.cursor != 3 {
		t.Fatalf("A lost its composer: %q at %d", a.input.String(), a.input.cursor)
	}
	cmd, refusal = a.openBeside("/srv/app", "/srv/app/b.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	if a.input.String() != "draft for B" || a.input.cursor != 5 {
		t.Fatalf("B lost its composer: %q at %d", a.input.String(), a.input.cursor)
	}
}
