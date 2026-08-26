package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── WHAT A FRAME AND A POINTER COST THE FAR MACHINE ─────────────────────────
//
// These are PERF.md's laws about `--host`, and they are counts of round trips
// rather than stopwatches for the reason that file's doctrine gives: a count is
// a fact about the code and the same fact on a loaded laptop as on idle CI.
//
// WHAT THEY WERE WRITTEN AFTER. The owner reported that over a connection
// "even hover seems to slow everything down", and that clicks and keys felt
// dead. Both were one defect: the status row asked the agent what the model was
// dialled to while it was DRAWING (view.go's [app.statusRow]), a hover below
// the conversation rebuilds the chrome to find its row (view.go's
// [app.chromeAt]), and over a connection that read is a round trip on the ssh
// pipe. Measured on the loopback client at the commit before this file:
//
//	100 frames                        100 far calls
//	200 pointer motions over the foot  200 far calls
//	one frame with /model open          13 far calls
//
// At the twenty-millisecond round trip a real link has, a pointer swept across
// the foot of the window put thirty-six milliseconds of network in front of the
// update loop PER CELL — so a two-hundred-cell sweep left seven seconds of
// keystrokes and clicks queued behind it. Driven through tmux over a pipe with
// that delay, a typed character took 7.4s to appear after a burst of two
// hundred motions and 21.8s after six hundred; with the fix, 0.014s, which is
// what the same script measures on a local session.
//
// reasoninglevel.go holds the fix and the whole of the reasoning behind it.

// farAgent is [remote.WrappedAgent] for these pins: every method the engine
// serves, answering instantly. INSTANTLY IS THE POINT — what is being counted is
// how many times the SURFACE asked, and an engine that took a moment would turn
// a count into a race.
type farAgent struct {
	model  string
	levels map[string]string
}

func newFarAgent() *farAgent {
	return &farAgent{
		model:  "anthropic/claude-sonnet-4.5",
		levels: map[string]string{"anthropic/claude-sonnet-4.5": "high"},
	}
}

func (f *farAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (f *farAgent) SubmitStanding(ctx context.Context, text string) (<-chan session.Event, error) {
	return f.Submit(ctx, text)
}

func (f *farAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	return f.Submit(ctx, text)
}

func (f *farAgent) FollowUp(text string) (<-chan session.Event, error) {
	return f.Submit(context.Background(), text)
}

func (f *farAgent) Interrupt()                        {}
func (f *farAgent) Compact(ctx context.Context) error { return nil }
func (f *farAgent) Close() error                      { return nil }
func (f *farAgent) Model() string                     { return f.model }
func (f *farAgent) SetModel(model string)             { f.model = model }
func (f *farAgent) SetContextWindow(tokens int)       {}
func (f *farAgent) ReasoningFor(model string) string  { return f.levels[model] }

// ReasoningLevels is the BULK door the whole table is seeded from
// (reasoninglevel.go): the engine states every level it holds, so the surface
// has nothing left to ask about and a picker's rows cost nothing whatever the
// catalog's length.
func (f *farAgent) ReasoningLevels() map[string]string {
	held := make(map[string]string, len(f.levels))
	for model, level := range f.levels {
		held[model] = level
	}
	return held
}

func (f *farAgent) SetReasoningFor(model, level string)                       { f.levels[model] = level }
func (f *farAgent) ResolveConsent(id uint64, allow bool)                      {}
func (f *farAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (f *farAgent) ResolveStanding(id uint64, answer session.StandingAnswer)  {}
func (f *farAgent) ResolveHarness(id uint64, run bool, model string)          {}
func (f *farAgent) ResolveConnect(id string, approve bool)                    {}
func (f *farAgent) ResolveConnectKey(id string, key string)                   {}
func (f *farAgent) NoteConnected(service, account string)                     {}
func (f *farAgent) Title() string                                             { return "a hosted conversation" }
func (f *farAgent) Usage() session.Usage                                      { return session.Usage{} }
func (f *farAgent) ContextTokens() int                                        { return 1200 }
func (f *farAgent) Transcript() []session.DisplayEntry                        { return nil }
func (f *farAgent) EarlierHistory() session.EarlierHistory                    { return session.EarlierHistory{} }
func (f *farAgent) RewindPoints() []session.RewindPoint                       { return nil }
func (f *farAgent) RewindAt(index int) ([]session.DisplayEntry, error)        { return nil, nil }

// hostedSurface is a surface over a REAL client talking to a REAL engine, with
// only the ssh child replaced (internal/remote's loopback.go). Nothing about
// these counts would be true of a hand-written double: what is being asserted is
// that the surface does not put a frame on the wire, and only the wire can say.
func hostedSurface(t *testing.T) (*app, *remote.Client) {
	t.Helper()
	loop, err := remote.Loopback(
		remote.Hello{Version: remote.Version, Workspace: "/srv/app"},
		remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: newFarAgent(), Workspace: "/srv/app",
				SessionFile: "/srv/app/j.jsonl"}, nil
		}},
	)
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	a := newApp(context.Background(), Options{
		Agent: loop.Client.Agent(), Host: "devbox", Workspace: "/srv/app",
	})
	// The same five pins [newTestApp] applies, and for its reasons: a count that
	// depended on the developer's TERM would be a count of their terminal.
	a.width, a.height = 120, 40
	a.pal = newPalette(tokens.ANSI256, false)
	a.tmux, a.remote, a.railAway = false, false, false
	a.pathLinks = true
	a.welcome = welcome{spent: true}
	a.entries = hostedProse()
	a.touch()
	// The first frame is drawn and thrown away, because a surface that has never
	// been drawn has never queued anything and would pass every count below by
	// having done nothing at all.
	_, _, _ = a.frame()
	return a, loop.Client
}

// hostedProse is a conversation with paths in it, so the far-disk pass has
// something to be asked about and cannot be the thing that happens to be quiet.
func hostedProse() []entry {
	var out []entry
	for i := 0; i < 20; i++ {
		out = append(out, entry{kind: entryUser, text: "what does internal/tui3/app.go do?"})
		out = append(out, entry{kind: entryAssistant, text: strings.Join([]string{
			"I read internal/tui3/app.go and internal/tui3/hover.go.",
			"The change lands in internal/remote/client.go and in PERF.md.",
			"The ratio 1/2 and the tag v2.0 are not files and never become links.",
		}, "\n")})
	}
	return out
}

// A FRAME OVER A CONNECTION ASKS THE FAR MACHINE NOTHING. The renderer paints on
// its own clock, so a question asked while drawing is a question asked thirty
// times a second — and over a connection each one is a round trip with a
// ten-second deadline on it.
func TestAFrameOverAConnectionAsksTheFarMachineNothing(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	for range 100 {
		a.dirty = true
		_, _, _ = a.frame()
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("a hundred frames put %d calls on the wire; a frame must put none", made)
	}
}

// AND A POINTER CROSSING THE WINDOW ASKS IT NOTHING EITHER. A pointer sends one
// motion per CELL, and the rows below the conversation resolve theirs by
// rebuilding the chrome ([app.chromeAt]) — so anything the chrome asks for is
// asked once per cell, in the update loop, in front of every key and click
// behind it.
func TestAPointerMovingOverAConnectionAsksTheFarMachineNothing(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	for i := range 200 {
		// Both halves of the window: the conversation's own rows, and the foot
		// of the frame where the chrome is.
		a.Update(tea.MouseMotionMsg{X: 5 + i%80, Y: 3 + i%36})
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("two hundred pointer motions put %d calls on the wire; motion must put none", made)
	}
}

// AND THE MODEL PICKER DRAWS ITS WHOLE LIST FOR NOTHING. Every row of it spells
// the level that row's model is dialled to, so a list that asked while drawing
// asked once per row per frame — the same defect multiplied by the length of
// the list.
func TestTheModelPickerOverAConnectionDrawsItsRowsForNothing(t *testing.T) {
	a, client := hostedSurface(t)
	a.openPicker()
	a.dirty = true
	_, _, _ = a.frame()
	before := client.CallsMade()
	for range 20 {
		a.dirty = true
		_, _, _ = a.frame()
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("twenty frames of the open picker put %d calls on the wire", made)
	}
}

// AND THE FRAME CLOCK'S OWN BEAT MAKES NO CALL ON THE LOOP. It reads the
// session's running cost every [usageEvery] slots, which is three times a second
// — and made from inside [app.Update] that was three round trips a second in
// front of the events of the turn it was reporting on. It is a tea.Cmd now, so
// the call happens on a goroutine and lands as a message.
func TestTheFrameClockOverAConnectionMakesNoCallOnTheLoop(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	// Far more slots than usageEvery, so the reading is certainly due.
	for range 100 {
		a.Update(frameMsg{})
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("a hundred beats put %d calls on the update loop; the beat must put none", made)
	}
}

// ── and the level is still right ────────────────────────────────────────────
//
// Everything above is about what the surface does NOT ask. This is the other
// half: the answer it draws is still the true one, which is the only thing that
// makes a cache in front of a knob worth having.

// THE FIRST FRAME NAMES THE LEVEL. It is seeded where the model and the title
// are (reasoninglevel.go's three moments), so the status row does not spend its
// opening frame saying this session is undialled when it is not.
func TestTheFirstFrameOverAConnectionAlreadyKnowsTheLevel(t *testing.T) {
	a, _ := hostedSurface(t)
	if got := a.reasoningFor(a.model); got != "high" {
		t.Fatalf("the level on the first frame is %q, want %q", got, "high")
	}
}

// AND ctrl+t IS NEVER A FRAME LATE. The surface is the only thing that sets
// these, so what it just set is what is true — a cache that made a person press
// the key twice to see it move would be worse than the round trip it replaced.
func TestDiallingAModelShowsTheNewLevelAtOnce(t *testing.T) {
	agent := &fakeAgent{model: "deepseek/deepseek-v4", levels: map[string]string{}}
	a := newTestApp(agent)
	a.pick.startFor([]Model{{ID: "deepseek/deepseek-v4", Reasoning: true}}, a.model, chatModel)
	a.cycleReasoning()
	if got := a.reasoningFor("deepseek/deepseek-v4"); got != "low" {
		t.Fatalf("the row reads %q the moment after ctrl+t, want %q", got, "low")
	}
}

// AND A ROW THE SURFACE HAS NEVER ASKED ABOUT LEARNS ITS LEVEL OFF THE LOOP.
// The picker draws it as no level on the frame it first appears — which is what
// the emptiness law draws for a fact nobody has been told — and the frame clock
// asks, exactly as the far disk's file facts are asked (remotefiles.go).
func TestALevelNobodyHasAskedAboutIsLearnedOffTheDrawPath(t *testing.T) {
	agent := &fakeAgent{model: "deepseek/deepseek-v4",
		levels: map[string]string{"anthropic/claude-sonnet-4.5": "medium"}}
	a := newTestApp(agent)
	if got := a.reasoningFor("anthropic/claude-sonnet-4.5"); got != "" {
		t.Fatalf("a level nobody had been told about drew as %q on the first look", got)
	}
	if !a.levelsWaiting() {
		t.Fatal("the id was drawn and nothing was queued to ask about it")
	}
	cmd := a.levelKick()
	if cmd == nil {
		t.Fatal("the frame clock had nothing to ask")
	}
	msg, ok := cmd().(levelsMsg)
	if !ok {
		t.Fatalf("the ask answered %T", msg)
	}
	a.levelsBack(msg)
	if got := a.reasoningFor("anthropic/claude-sonnet-4.5"); got != "medium" {
		t.Fatalf("after the answer landed the row reads %q, want %q", got, "medium")
	}
	if a.levelsWaiting() {
		t.Fatal("the id was answered and is still queued")
	}
}

// AND A QUESTION THE DRAW PATH QUEUED ARMS THE FRAME CLOCK, because the ask is
// sent on that clock and nowhere else. The picker opens on a keystroke, with
// nothing on the screen moving — so without this its rows would be drawn without
// their levels until something unrelated woke the surface up ([app.Update]).
func TestAQueuedLevelArmsTheFrameClock(t *testing.T) {
	agent := &fakeAgent{model: "deepseek/deepseek-v4",
		levels: map[string]string{"anthropic/claude-sonnet-4.5": "medium"}}
	a := newTestApp(agent)
	a.painting = false
	a.openPicker()
	// The list is drawn, which is where a row it has not been told about is
	// queued — the render pass writes the want list, exactly as the far disk's
	// does (remotefiles.go).
	_, _, _ = a.frame()
	if !a.levelsWaiting() {
		t.Fatal("the picker drew its rows and queued nothing to ask about them")
	}
	if _, cmd := a.Update(key("x")); cmd == nil {
		t.Fatal("a surface owed an answer returned no command to keep its clock turning")
	}
	if !a.painting {
		t.Fatal("the clock is not turning, and the ask is sent on the clock")
	}
}

// A KEY OVER A CONNECTION ASKS THE FAR MACHINE NOTHING. Typing is the one thing
// a person does continuously, and a keystroke that waited on a network would
// make the composer feel broken on a link that is working perfectly.
func TestAKeyOverAConnectionAsksTheFarMachineNothing(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	for _, r := range "fix the roof and then the gutter" {
		a.Update(key(string(r)))
	}
	for _, chord := range []string{"up", "down", "left", "right", "esc"} {
		a.Update(key(chord))
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("thirty-six keystrokes put %d calls on the wire; a key must put none", made)
	}
}

// AND A SUBMIT IS EXACTLY ONE. What goes up is the intent — the sentence — and
// nothing else: a send that also asked what the model was, or what the last turn
// cost, would be three round trips on the one keystroke a person is waiting on.
//
// THE UPDATE THAT ECHOES THE LINE IS ZERO. The line is on the page before
// anything is written to the wire, because the call happens on the command that
// update returns (echo.go).
func TestASubmitOverAConnectionIsExactlyOneCall(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	said := len(a.entries)

	cmd := a.submit("fix the roof")
	if len(a.entries) != said+1 {
		t.Fatalf("the message was not on the page before the wire was touched")
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("the update that echoed the line put %d calls on the wire", made)
	}
	a.adopt(runSubmit(t, cmd))
	if made := client.CallsMade() - before; made != 1 {
		t.Fatalf("a submit put %d calls on the wire, want exactly 1", made)
	}
}

// AND A TURN ENDING IS ZERO. The settle reads the spending, the weight and the
// effort table the instant the turn's ending lands, and every one of them is a
// memory read of the replica the engine has already refreshed ahead of that
// event (internal/remote's server.go).
func TestATurnEndingOverAConnectionAsksTheFarMachineNothing(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	a.settle()
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("a turn ending put %d calls on the wire; a settle must put none", made)
	}
}

// AND THE WHOLE EFFORT TABLE IS THERE FROM THE FIRST FRAME, so a picker opened
// on a catalog of any length has nothing to discover and queues nothing.
func TestTheFirstFrameOverAConnectionHoldsEveryLevelTheEngineHolds(t *testing.T) {
	a, client := hostedSurface(t)
	before := client.CallsMade()
	if got := a.reasoningFor("anthropic/claude-sonnet-4.5"); got != "high" {
		t.Fatalf("the level the engine holds reads %q on the surface", got)
	}
	// AND IT IS FOUND UNDER ANY SPELLING OF THE ID, because both sides fold it
	// the same way ([session.ReasoningKey]).
	if got := a.reasoningFor("Anthropic/Claude-Sonnet-4.5"); got != "high" {
		t.Fatalf("the level under a differently-spelled id reads %q", got)
	}
	if a.levelsWaiting() {
		t.Fatal("a table the engine filled still has something queued to ask about")
	}
	if made := client.CallsMade() - before; made != 0 {
		t.Fatalf("reading two levels put %d calls on the wire", made)
	}
}
