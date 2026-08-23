package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// spellFake is a scripted expander. It records what it was asked and answers
// from `block`, so every test here runs the whole gesture — hint, chord, block,
// enter — with no provider anywhere near it.
type spellFake struct {
	Agent
	calls int
	asked string
	block string
}

func (f *spellFake) SpellOut(_ context.Context, draft string) string {
	f.calls++
	f.asked = draft
	return f.block
}

// spellLab is the surface with an expander behind it, at a width where the
// legend keeps its hint slot.
func spellLab(t *testing.T) (*app, *spellFake) {
	t.Helper()
	base := &fakeAgent{model: "m"}
	f := &spellFake{Agent: base, block: session.SpellOutOpening +
		"\n- a form that says which field is wrong\n- a session that survives a refresh"}
	a := newTestApp(f)
	a.width = 120
	return a, f
}

func TestTheSpellHintOffersItselfOnAMakingDraftAndNeverMovesTheBox(t *testing.T) {
	a, _ := spellLab(t)
	height := a.inputHeight()
	typeDraft(t, a, "build me a login page")
	if !a.spellOffered() {
		t.Fatal("a making-shaped draft was not offered the chord")
	}
	if got := a.hintWord(); got != spellOutHint {
		t.Fatalf("the slot said %q, wanted %q", got, spellOutHint)
	}
	if !strings.Contains(plain(frame(a)), spellOutHint) {
		t.Fatalf("the hint was not on the frame:\n%s", plain(frame(a)))
	}
	// AND THE BOX DID NOT MOVE. The hint rides the legend, which is on the frame
	// in every state, so a draft that starts looking like something to build
	// changes one word at the end of a line and nothing else about the geometry.
	if a.inputHeight() != height {
		t.Fatalf("the box changed height with the hint: %d then %d", height, a.inputHeight())
	}
	for range "build me a login page" {
		drive(t, a, key("backspace"))
	}
	if strings.Contains(plain(frame(a)), spellOutHint) {
		t.Fatalf("the hint outlived the draft:\n%s", plain(frame(a)))
	}
	if a.inputHeight() != height {
		t.Fatalf("the box changed height as the hint went: %d then %d", height, a.inputHeight())
	}
}

// The shape law, read as a table. False negatives are free here — the list only
// has to catch enough of what people type to teach the chord exists — so what is
// pinned is the two halves that matter: a making verb, and room left to grow.
func TestTheShapeLawOffersOnlyMakingDraftsWithRoomToGrow(t *testing.T) {
	for _, tc := range []struct {
		draft string
		want  bool
	}{
		{"build me a login page", true},
		{"create a cli that reads csv", true},
		{"write a parser for this format", true},
		{"design the settings screen", true},
		{"add a retry to the uploader", true},
		{"implement the diff view", true},
		{"set up a github action for the tests", true},
		{"", false},
		{"what does this function do", false},
		{"/task build me a login page", false},
		// ALREADY SPELLED OUT, WHICH IS THE POINT AND NOT A LIMIT: a draft this
		// long has said what it wants, and an offer to add detail to it is a dim
		// line under every draft on the surface.
		{"build me a login page with email and password, a form that names the field that " +
			"is wrong, a session cookie that survives a refresh, and rate limiting", false},
		// A person who typed a list has done this gesture's work by hand.
		{"build me a login page\n- email and password\n- remember me", false},
	} {
		if got := looksMaking([]rune(tc.draft)); got != tc.want {
			t.Fatalf("looksMaking(%q) = %v", tc.draft, got)
		}
	}
}

// THE STANDING HINT WINS THE SLOT, and the rule lives in one place
// ([app.spellOffered]) rather than in the slot's ordering.
func TestTheStandingHintKeepsTheSlotWhenBothWouldOffer(t *testing.T) {
	a, _ := spellLab(t)
	band := &standBand{}
	band.wire(a)
	typeDraft(t, a, "always build the docs first")
	if !a.standMarkOffered() {
		t.Fatal("the standing hint did not offer on a standing-shaped draft")
	}
	if a.spellOffered() {
		t.Fatal("both hints offered at once")
	}
	if got := a.hintWord(); got != standMarkHint {
		t.Fatalf("the slot said %q, wanted the standing hint", got)
	}
}

func TestTheChordMakesOneCallAndDrawsTheBlock(t *testing.T) {
	a, f := spellLab(t)
	typeDraft(t, a, "build me a login page")
	drive(t, a, key(spellOutKey))
	if f.calls != 1 {
		t.Fatalf("the chord made %d calls", f.calls)
	}
	if f.asked != "build me a login page" {
		t.Fatalf("the expander was asked %q", f.asked)
	}
	if !a.spellShowing() {
		t.Fatal("the block did not come up")
	}
	drawn := plain(frame(a))
	for _, want := range []string{session.SpellOutOpening, "a session that survives a refresh", spellOutVerbs} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the block did not draw %q:\n%s", want, drawn)
		}
	}
	// The draft is untouched until the person says so.
	if a.input.String() != "build me a login page" {
		t.Fatalf("the chord changed the draft: %q", a.input.String())
	}
}

func TestNoCallIsMadeWithoutTheChord(t *testing.T) {
	a, f := spellLab(t)
	typeDraft(t, a, "build me a login page")
	drive(t, a, key("enter"))
	typeDraft(t, a, "build me another one")
	for _, k := range []string{"esc", "up", "down", "ctrl+u", "tab"} {
		drive(t, a, key(k))
	}
	if f.calls != 0 {
		t.Fatalf("something other than the chord made %d calls", f.calls)
	}
	// AND THE CHORD ITSELF IS ABSENT WHERE THE HINT IS. A draft that already
	// spells itself out is not offered the key, so pressing it does nothing at
	// all rather than spending money quietly.
	drive(t, a, key("ctrl+u"))
	typeDraft(t, a, "what does this function do")
	drive(t, a, key(spellOutKey))
	if f.calls != 0 {
		t.Fatalf("the chord fired where it was not offered: %d calls", f.calls)
	}
}

func TestEnterAddsTheBlockVerbatimAndTheDraftSendsAsOneOrdinaryMessage(t *testing.T) {
	a, f := spellLab(t)
	base, _ := a.agent.(*spellFake).Agent.(*fakeAgent)
	typeDraft(t, a, "build me a login page")
	drive(t, a, key(spellOutKey))
	drive(t, a, key("enter"))

	want := "build me a login page\n\n" + f.block
	if got := a.input.String(); got != want {
		t.Fatalf("the draft is\n%q\nwanted\n%q", got, want)
	}
	if a.spellShowing() {
		t.Fatal("the block outlived being added")
	}
	if strings.Contains(plain(frame(a)), spellOutVerbs) {
		t.Fatalf("the block's verbs are still on the frame:\n%s", plain(frame(a)))
	}
	if len(base.sent) != 0 {
		t.Fatalf("adding the block sent something: %q", base.sent)
	}
	// AND IT IS ORDINARY TEXT AFTERWARDS: the next enter sends one message, and
	// what goes is exactly what is in the box.
	drive(t, a, key("enter"))
	if len(base.sent) != 1 {
		t.Fatalf("the draft sent %d messages", len(base.sent))
	}
	if base.sent[0] != want {
		t.Fatalf("the message sent was\n%q\nwanted\n%q", base.sent[0], want)
	}
}

func TestEscLeavesTheDraftByteIdenticalAndNoTrace(t *testing.T) {
	a, _ := spellLab(t)
	typeDraft(t, a, "build me a login page")
	before := a.input.String()
	rowsBefore := len(plainRows(a))
	drive(t, a, key(spellOutKey))
	drive(t, a, key("esc"))
	if got := a.input.String(); got != before {
		t.Fatalf("esc changed the draft: %q then %q", before, got)
	}
	if a.spellShowing() || a.spell.block != "" {
		t.Fatal("esc left the block behind")
	}
	drawn := plain(frame(a))
	if strings.Contains(drawn, session.SpellOutOpening) || strings.Contains(drawn, spellOutVerbs) {
		t.Fatalf("esc left a trace on the frame:\n%s", drawn)
	}
	if got := len(plainRows(a)); got != rowsBefore {
		t.Fatalf("esc left the conversation %d rows, was %d", got, rowsBefore)
	}
	// And the hint is back, because the draft is still making-shaped.
	if a.hintWord() != spellOutHint {
		t.Fatalf("the slot said %q after esc", a.hintWord())
	}
}

// A STALE EXPANSION MUST NOT BE ADDED TO A CHANGED SENTENCE, so the block is
// gone the moment the draft it was made about is.
func TestEditingTheDraftDismissesTheBlock(t *testing.T) {
	for _, edit := range []string{"x", "backspace", "ctrl+u", "ctrl+w"} {
		a, _ := spellLab(t)
		typeDraft(t, a, "build me a login page")
		drive(t, a, key(spellOutKey))
		if !a.spellShowing() {
			t.Fatalf("%s: the block did not come up", edit)
		}
		drive(t, a, key(edit))
		if a.spellShowing() {
			t.Fatalf("%s: the block survived the edit", edit)
		}
		if strings.Contains(plain(frame(a)), session.SpellOutOpening) {
			t.Fatalf("%s: the block is still drawn:\n%s", edit, plain(frame(a)))
		}
	}
}

// An answer that comes back about a sentence the person has since changed is
// dropped in the same silence a failure is.
func TestAnAnswerAboutAChangedDraftIsDropped(t *testing.T) {
	a, f := spellLab(t)
	typeDraft(t, a, "build me a login page")
	a.spell = spellState{asking: true, at: []rune("build me a login page")}
	drive(t, a, key("s"))
	drive(t, a, spelledMsg{draft: "build me a login page", block: f.block})
	if a.spell.block != "" {
		t.Fatalf("a stale answer was kept: %q", a.spell.block)
	}
}

// A FAILED OR SLOW CALL JUST PUTS THE HINT BACK. The engine answers "" for a
// refusal, a timeout and a model that ignored the format alike (spellout.go), so
// the surface's whole behaviour is tested by an expander that answers nothing.
func TestAFailedCallPutsTheHintBackWithNoErrorProse(t *testing.T) {
	a, f := spellLab(t)
	f.block = ""
	typeDraft(t, a, "build me a login page")
	rowsBefore := len(plainRows(a))
	drive(t, a, key(spellOutKey))
	if a.spell.asking {
		t.Fatal("the call is still marked out")
	}
	if a.spellShowing() {
		t.Fatal("an empty answer drew a block")
	}
	if got := a.hintWord(); got != spellOutHint {
		t.Fatalf("the slot said %q, wanted the hint back", got)
	}
	if got := len(plainRows(a)); got != rowsBefore {
		t.Fatalf("a failed call wrote %d rows", got-rowsBefore)
	}
	for _, banned := range []string{"failed", "error", "could not"} {
		if strings.Contains(strings.ToLower(plain(frame(a))), banned) {
			t.Fatalf("a failed call said %q:\n%s", banned, plain(frame(a)))
		}
	}
}

// While the call is out the slot turns the build's own spinner over the same
// words, and the paint clock is kept awake to turn it.
func TestTheSlotTurnsTheSpinnerWhileTheCallIsOut(t *testing.T) {
	a, _ := spellLab(t)
	typeDraft(t, a, "build me a login page")
	a.spell = spellState{asking: true, at: append([]rune(nil), a.input.value...)}
	got := a.hintWord()
	if !strings.HasSuffix(got, "spell it out") {
		t.Fatalf("the working slot said %q", got)
	}
	if got == spellOutHint || strings.Contains(got, spellOutKey) {
		t.Fatalf("the working slot still names the chord: %q", got)
	}
	if _, cmd := a.Update(frameMsg{}); cmd == nil {
		t.Fatal("the paint clock stopped while the call was out")
	}
}

// A build whose agent has no expander offers nothing at all — absent, not
// broken, and no keystroke that fails.
func TestABuildWithNoExpanderNeverOffersTheChord(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width = 120
	typeDraft(t, a, "build me a login page")
	if a.spellOffered() {
		t.Fatal("a build with no door offered the chord")
	}
	if strings.Contains(plain(frame(a)), "spell it out") {
		t.Fatalf("a build with no door drew the hint:\n%s", plain(frame(a)))
	}
	drive(t, a, key(spellOutKey))
	if a.spellShowing() {
		t.Fatal("the chord did something in a build with no door")
	}
}

// The block is never up while the pointer is somewhere else on the surface — it
// belongs to the draft, and a page that replaced the draft took it with them.
func TestTheBlockIsNotOfferedInCopyMode(t *testing.T) {
	a, _ := spellLab(t)
	typeDraft(t, a, "build me a login page")
	a.copy.on = true
	if a.spellOffered() {
		t.Fatal("the chord was offered while the viewport was frozen")
	}
	a.copy.on = false
	a.state = stateWorking
	if a.spellOffered() {
		t.Fatal("the chord was offered while a turn was running")
	}
}
