package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// The thinking chip's acceptance tests: what the tray says, what the chord does
// to it, and what the ladder it opens does with a click.
//
// Each asserts the FACT the behaviour exists for. The chip must name what will
// actually happen and not what somebody chose; the chord must reach the ladder
// with a sentence half typed and leave that sentence alone; and the door beside
// the chord must be pressable without moving the caret in the box under it.

// ── the scripted dial ───────────────────────────────────────────────────────

// effortAgent is a [fakeAgent] that can say how hard it thinks. It is a separate
// double rather than three more methods on the plain fake for the reason
// [effortDialer] exists at all: a session with no dial has no chip, and the
// suite needs both of those sessions.
type effortAgent struct {
	*fakeAgent
	// conversation is the rung this session was set to, the scope the chip and
	// the ladder both write.
	conversation effort.Rung
	// turn is a rung dialled onto the model itself, which outranks the
	// conversation's (internal/effort's Resolve). Empty on every test but the one
	// about a dial that cannot move.
	turn effort.Rung
	// installed is the install's own rung — the `effort` settings row.
	installed effort.Rung
	// sets is every word the surface handed to SetConversationEffort, in order,
	// refusals included: what the chord WROTE is a different question from what
	// the resolver then answered.
	sets []string
}

func (e *effortAgent) ConversationEffort() string { return e.conversation.String() }

func (e *effortAgent) ResolvedEffort() string {
	return effort.Resolve(effort.Scope{
		Turn:         e.turn,
		Conversation: e.conversation,
		Default:      e.installed,
	}).String()
}

func (e *effortAgent) SetConversationEffort(rung string) bool {
	e.sets = append(e.sets, rung)
	parsed, ok := effort.Parse(rung)
	if !ok {
		return false
	}
	e.conversation = parsed
	return true
}

// dialled is an app whose session runs at the shipped rung and can be moved off
// it, which is every ordinary install.
func dialled(t *testing.T) (*effortAgent, *app) {
	t.Helper()
	agent := &effortAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4"}, installed: effort.Ship}
	return agent, newTestApp(agent)
}

// trayRow is the screen row the tray is drawn on, and overlayRowY is where one
// row of the open list landed. Both are found by asking [app.chromeAt] what is
// on each row, which is the same question the pointer asks — an arithmetic of
// their own would be a second copy of the layout for the test to be wrong in.
func trayRow(a *app) int { return markedRowY(a, chromeDraft, 0) }

func overlayRowY(a *app, index int) int { return markedRowY(a, chromeOverlay, index) }

func markedRowY(a *app, kind chromeKind, index int) int {
	_, height := a.size()
	for y := range height {
		if mark, ok := a.chromeAt(y); ok && mark.kind == kind && mark.index == index {
			return y
		}
	}
	return -1
}

// ── 1. the chip ─────────────────────────────────────────────────────────────

// THE CHIP NAMES THE RUNG THE NEXT TURN WILL ACTUALLY ASK FOR, not the rung
// somebody chose. On a session where nobody has chosen anything the two are
// different — the stored rung is absence — and it is the resolved one a person
// needs.
func TestTheTrayChipNamesTheResolvedThinkingRung(t *testing.T) {
	agent, a := dialled(t)

	if got := agent.ConversationEffort(); got != "" {
		t.Fatalf("the session started with a chosen rung: %q", got)
	}
	strip := plain(a.chipStrip(a.width))
	if !strings.Contains(strip, glyphEffort+" high") {
		t.Fatalf("the tray does not name the shipped rung: %q", strip)
	}

	// And it follows the resolver rather than remembering anything: a rung set on
	// the conversation moves the word on the next frame.
	agent.conversation = effort.Max
	if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, glyphEffort+" max") {
		t.Fatalf("the tray kept the old rung: %q", strip)
	}
}

// THE EMPTINESS LAW: a session that asks for no thinking at all has nothing to
// report, and a chip saying "off" would be a permanent reminder of an absence.
func TestAnInstallWithThinkingOffDrawsNoChipAtAll(t *testing.T) {
	agent, a := dialled(t)
	agent.installed = effort.None

	if strip := a.chipStrip(a.width); strip != "" {
		t.Fatalf("the tray drew a row for a rung nobody asked for: %q", plain(strip))
	}
	// The chord still works from there, which is what puts the chip back.
	drive(t, a, key(effortKey))
	if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, glyphEffort+" low") {
		t.Fatalf("the chord did not bring the chip back: %q", strip)
	}
}

// A SESSION THAT CANNOT SAY HOW HARD IT THINKS HAS NO CHIP — the design law
// that a capability with nothing behind it is absent rather than broken. The
// plain scripted agent is one, so this is also what keeps the rest of the suite
// reading the tray it has always had.
func TestASessionWithNoDialDrawsNoChip(t *testing.T) {
	_, a := wired(nil)
	if strip := a.chipStrip(a.width); strip != "" {
		t.Fatalf("a session with no dial drew a tray: %q", plain(strip))
	}
	if _, ok := a.effortDial(); ok {
		t.Fatal("the plain scripted agent claimed a thinking dial")
	}
}

// THE DIAL KEEPS ITS COLUMNS WHATEVER ELSE IS ON THE ROW. It is right-aligned
// and the cargo grows from the left, so a screenshot dropped on the tray does
// not move the control a person's hand has learned.
func TestTheChipHoldsItsColumnsWhenCargoArrives(t *testing.T) {
	_, a := dialled(t)

	a.chipStrip(a.width)
	bare := a.effortSpan
	if !bare.pressable() {
		t.Fatal("the dial recorded no columns")
	}
	a.harnChip = "release-notes-weekly"
	strip := plain(a.chipStrip(a.width))
	if a.effortSpan != bare {
		t.Fatalf("the dial moved from %+v to %+v when the tray took cargo", bare, a.effortSpan)
	}
	if !strings.Contains(strip, "release-notes-weekly") || !strings.Contains(strip, "high") {
		t.Fatalf("the tray lost one of its two halves: %q", strip)
	}
}

// ── 2. the chord ────────────────────────────────────────────────────────────

// THE CHORD WALKS THE FIVE RUNGS AND WRAPS, and every step goes through the
// session's own setter — the chip is drawn from the resolver, so a step the
// surface only remembered would be a step nothing else in the process saw.
func TestCtrlVCyclesTheConversationRungAndWraps(t *testing.T) {
	agent, a := dialled(t)

	// The shipped rung is high, so the ladder is walked from there.
	want := []string{"xhigh", "max", "low", "medium", "high", "xhigh"}
	for at, rung := range want {
		drive(t, a, key(effortKey))
		if got := agent.ConversationEffort(); got != rung {
			t.Fatalf("press %d left the conversation at %q, want %q", at+1, got, rung)
		}
		if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, glyphEffort+" "+rung) {
			t.Fatalf("press %d drew %q, want %q", at+1, strip, rung)
		}
	}
	if len(agent.sets) != len(want) {
		t.Fatalf("the chord wrote %d rungs for %d presses: %v", len(agent.sets), len(want), agent.sets)
	}
}

// THE CHORD RIDES THE CHORD NAMESPACE, so it reaches the ladder with a sentence
// half typed — and leaves the sentence and the caret exactly where they were.
// A letter still types: ctrl+v carries no text, and the router reads it in the
// plain switch under everything that could have wanted it.
func TestTheChordWorksMidDraftAndDisturbsNeitherTextNorCaret(t *testing.T) {
	agent, a := dialled(t)
	typeInto(t, a, "what changed in the relay")
	drive(t, a, key("left"), key("left"), key("left"))

	want, caret := a.input.String(), a.input.cursor
	drive(t, a, key(effortKey))

	if got := a.input.String(); got != want {
		t.Fatalf("the chord changed the draft to %q, want %q", got, want)
	}
	if a.input.cursor != caret {
		t.Fatalf("the chord moved the caret to %d, want %d", a.input.cursor, caret)
	}
	if got := agent.ConversationEffort(); got != "xhigh" {
		t.Fatalf("the chord did not reach the ladder mid-draft: %q", got)
	}
	// And the letter after it is still a letter.
	drive(t, a, key("y"))
	if got := a.input.String(); got != want[:caret]+"y"+want[caret:] {
		t.Fatalf("the key after the chord typed %q", got)
	}
}

// THE MOMENT IT CHANGES IS THE ONE MOMENT THIS CHIP IS ACCENT. Before the chord
// and after the flash it is furniture, in the dim tier the rest of the tray
// wears — THE ACCENT BUDGET is one lit element per screen and a rung that sat
// lit forever would have spent it on a fact that changes once a week.
func TestTheChipIsEmphasizedOnlyWhileItsChangeIsFresh(t *testing.T) {
	_, a := dialled(t)

	if a.effortFlashing() {
		t.Fatal("the chip opened already lit")
	}
	rest := a.chipStrip(a.width)
	drive(t, a, key(effortKey))
	if !a.effortFlashing() {
		t.Fatal("the chord did not light the chip")
	}
	if lit := a.chipStrip(a.width); lit == rest {
		t.Fatal("the lit chip is painted exactly like the resting one")
	}
	// The clock is the whole of the state: past the window it settles back with
	// no second flag to disagree with.
	a.clock = func() time.Time { return time.Now().Add(effortFlashFor + time.Second) }
	if a.effortFlashing() {
		t.Fatal("the chip stayed lit past its window")
	}
}

// A DIAL THAT CANNOT MOVE SAYS SO. A level set on the model itself is the turn
// scope and beats the conversation's, so the chord writes a rung the resolver
// then ignores — which is a knob doing nothing, and the surface owes the person
// the reason and the door.
func TestTheChordSaysSoWhenTheModelsOwnLevelIsWinning(t *testing.T) {
	agent, a := dialled(t)
	agent.turn = effort.Low

	drive(t, a, key(effortKey))
	if got := agent.ConversationEffort(); got == "" {
		t.Fatal("the chord did not write the conversation's rung")
	}
	got := plain(frame(a))
	for _, want := range []string{"thinking stays low", "deepseek-v4", "ctrl+t"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the note is missing %q:\n%s", want, got)
		}
	}
}

// ── 3. the menu ─────────────────────────────────────────────────────────────

// CLICKING THE CHIP OPENS THE LADDER AND MOVES NO CARET. The tray is the input
// block's first row, so a press that fell through to the box would put the caret
// in the middle of a sentence somebody was still writing (draftclick.go).
func TestClickingTheChipOpensTheLadderAndLeavesTheCaretAlone(t *testing.T) {
	_, a := dialled(t)
	typeInto(t, a, "what changed in the relay")
	drive(t, a, key("left"), key("left"))
	caret := a.input.cursor

	a.chipStrip(a.width - len(inputPad))
	x := len(inputPad) + a.effortSpan.from
	drive(t, a, clickAt(x, trayRow(a)))

	if !a.effPick.open {
		t.Fatal("the press on the chip did not open the ladder")
	}
	if a.input.cursor != caret {
		t.Fatalf("the press moved the caret to %d, want %d", a.input.cursor, caret)
	}
	// And the second press on the same cell puts it away, because a control that
	// ignored it would be one with no way back through the gesture that opened it.
	drive(t, a, clickAt(x, trayRow(a)))
	if a.effPick.open {
		t.Fatal("the second press on the chip did not close the ladder")
	}
}

// THE LADDER IS FIVE ROWS, CHEAPEST FIRST, WITH THE RUNG IN FORCE MARKED — the
// ground ladder's chosen step, which is the same idiom every other list on this
// surface marks the current thing with.
func TestTheLadderDrawsFiveRungsCheapestFirstWithTheCurrentOneChosen(t *testing.T) {
	_, a := dialled(t)
	a.openEffortMenu()

	rows := a.effPick.rows(a.width, a.effPick.height(), a.pal, -1)
	if len(rows) != len(effort.Rungs)+effortFrameRows {
		t.Fatalf("the ladder drew %d rows, want %d", len(rows), len(effort.Rungs)+effortFrameRows)
	}
	at := 0
	for _, rung := range effort.Rungs {
		found := -1
		for i := at; i < len(rows); i++ {
			if strings.Contains(plain(rows[i]), rung.String()) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("the ladder is missing %q:\n%s", rung, strings.Join(rows, "\n"))
		}
		at = found + 1
	}
	// `high` is in force, so its row wears the selected ground and no other does.
	ground := paintPrefix(a.pal.background("x", 0, a.pal.ramp.selected))
	chosen := 0
	for _, row := range rows {
		if strings.Contains(row, ground) {
			chosen++
		}
	}
	if chosen != 1 {
		t.Fatalf("%d rows wear the chosen step, want exactly one", chosen)
	}
	if !strings.Contains(plain(rows[a.effPick.cursor+1]), "high") {
		t.Fatalf("the cursor did not open on the rung in force:\n%s", strings.Join(rows, "\n"))
	}
}

// ENTER PICKS, ESC CLOSES, AND A PLAIN LETTER DOES NOT TYPE. The ladder is a
// fixed list with no filter under it, so a letter falling through to the box
// would be a letter somebody has to find and delete afterwards.
func TestTheLadderTakesEveryKeyAndPicksWithEnter(t *testing.T) {
	agent, a := dialled(t)
	typeInto(t, a, "steady")
	a.openEffortMenu()

	drive(t, a, key("x"))
	if a.input.String() != "steady" {
		t.Fatalf("a letter typed into the box under the ladder: %q", a.input.String())
	}
	drive(t, a, key("down"), key("enter"))
	if got := agent.ConversationEffort(); got != "xhigh" {
		t.Fatalf("enter picked %q, want xhigh", got)
	}
	if a.effPick.open {
		t.Fatal("the ladder stayed up after a pick")
	}

	a.openEffortMenu()
	drive(t, a, key("esc"))
	if a.effPick.open {
		t.Fatal("esc did not close the ladder")
	}
	if a.input.String() != "steady" {
		t.Fatalf("closing the ladder disturbed the draft: %q", a.input.String())
	}
}

// A CLICK ON A ROW MEANS WHAT ENTER MEANS, and a click on the two sentences
// around them means nothing at all.
func TestClickingALadderRowPicksThatRung(t *testing.T) {
	agent, a := dialled(t)
	a.openEffortMenu()

	// The header is the ladder's first row and carries no rung.
	head := overlayRowY(a, 0)
	drive(t, a, clickAt(2, head))
	if !a.effPick.open {
		t.Fatal("a press on the header closed the ladder")
	}
	if len(agent.sets) != 0 {
		t.Fatalf("a press on the header set a rung: %v", agent.sets)
	}
	// The cheapest rung is the row under it.
	drive(t, a, clickAt(2, head+1))
	if got := agent.ConversationEffort(); got != "low" {
		t.Fatalf("the press picked %q, want low", got)
	}
	if a.effPick.open {
		t.Fatal("the ladder stayed up after a press picked a rung")
	}
}

// THE ROUTER REACHES THE CHORD WITH A DRAFT IN PROGRESS, asserted at the router
// rather than at the wire: ctrl+v is a single byte (0x16) that every terminal
// sends the same way, so what is worth pinning is that none of the seventeen
// claims above the plain switch swallows it while somebody is typing.
func TestTheRouterReachesTheChordWithADraftInProgress(t *testing.T) {
	_, a := dialled(t)
	typeInto(t, a, "/hel")
	if !a.menu.open {
		t.Fatal("the command list is not up, so this asserts nothing")
	}
	drive(t, a, key(effortKey))
	if !a.effortFlashing() {
		t.Fatal("the command list swallowed the chord")
	}
}
