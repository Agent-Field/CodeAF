package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The thinking chip's acceptance tests: what the SEAM says beside the model,
// what the chord and the press do to it, and what `/effort` opens.
//
// Each asserts the FACT the behaviour exists for. The cell must name what will
// actually happen and not what somebody chose; the chord must reach the ladder
// with a sentence half typed and leave that sentence alone; the press must walk
// one rung the way a press on a task's thinking row walks that task's; and the
// rung must be given up whole rather than cut when the line runs out of cells.

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

// dialled is an app with an explicit high install setting to exercise the dial.
// It is drawn at a width the whole seam fits on, because the rung is the third
// thing that line gives up when it does not (foot.go's [app.seamIdentity]) and
// every test below but the narrow one is about the rung being there.
func dialled(t *testing.T) (*effortAgent, *app) {
	t.Helper()
	agent := &effortAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4"}, installed: effort.High}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	a.model, a.title = "deepseek/deepseek-v4", "porting the parser"
	return agent, a
}

// trayRow is the screen row the tray is drawn on, seamRow is the legend above
// the box, and overlayRowY is where one row of the open list landed. All three
// are found by asking [app.chromeAt] what is on each row, which is the same
// question the pointer asks — an arithmetic of their own would be a second copy
// of the layout for the test to be wrong in.
func trayRow(a *app) int { return markedRowY(a, chromeDraft, 0) }

func seamRowY(a *app) int { return markedRowY(a, chromeLegend, 0) }

// seamLine is the seam as a person reads it, painted off and the frame drawn
// first — the span the press resolves against is written by the layout, so a
// test that read the span without drawing would be reading the frame before.
func seamLine(t *testing.T, a *app) string {
	t.Helper()
	rows := strings.Split(plain(frame(a)), "\n")
	y := seamRowY(a)
	if y < 0 || y >= len(rows) {
		t.Fatalf("no seam on the frame:\n%s", strings.Join(rows, "\n"))
	}
	return rows[y]
}

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

// ── 1. the rung on the seam ─────────────────────────────────────────────────

// THE CELL NAMES THE RUNG THE NEXT TURN WILL ACTUALLY ASK FOR, not the rung
// somebody chose. On a session where nobody has chosen anything the two are
// different — the stored rung is absence — and it is the resolved one a person
// needs. It is written beside the model, because it is a fact about the model.
func TestTheSeamNamesTheResolvedThinkingRung(t *testing.T) {
	agent, a := dialled(t)

	if got := agent.ConversationEffort(); got != "" {
		t.Fatalf("the session started with a chosen rung: %q", got)
	}
	line := seamLine(t, a)
	if !strings.Contains(line, "deepseek-v4 · "+glyphEffort+" high") {
		t.Fatalf("the seam does not name the configured rung beside the model: %q", line)
	}
	// AND THE COLON SPELLING IS GONE. The level used to ride the model id —
	// `deepseek-v4:high` — which said only the picker-dialled level while the
	// chip beside it said the resolved rung: one ladder, two spellings, one line.
	if strings.Contains(line, "deepseek-v4:") {
		t.Fatalf("the seam still spells a level onto the model id: %q", line)
	}

	// And it follows the resolver rather than remembering anything: a rung set on
	// the conversation moves the word on the next frame.
	agent.conversation = effort.Max
	if line := seamLine(t, a); !strings.Contains(line, glyphEffort+" max") {
		t.Fatalf("the seam kept the old rung: %q", line)
	}
}

// THE EMPTINESS LAW: a session that asks for no thinking at all has nothing to
// report, and a cell saying "off" would be a permanent reminder of an absence.
func TestAnInstallWithThinkingOffDrawsNoRungAtAll(t *testing.T) {
	agent, a := dialled(t)
	agent.installed = effort.None

	if line := seamLine(t, a); strings.Contains(line, glyphEffort) {
		t.Fatalf("the seam drew a rung nobody asked for: %q", line)
	}
	if a.seamEffortSpan.pressable() {
		t.Fatal("a rung nobody asked for is still a press target")
	}
	// The chord still works from there, which is what puts the rung back.
	drive(t, a, key(effortKey))
	if line := seamLine(t, a); !strings.Contains(line, glyphEffort+" low") {
		t.Fatalf("the chord did not bring the rung back: %q", line)
	}
}

// A SESSION THAT CANNOT SAY HOW HARD IT THINKS HAS NO RUNG — the design law
// that a capability with nothing behind it is absent rather than broken. The
// plain scripted agent is one, and so is a connection to an engine that has
// never heard of the ladder ([effortDialer]'s own comment).
func TestASessionWithNoDialDrawsNoRung(t *testing.T) {
	_, a := wired(nil)
	a.width, a.height = 120, 24
	if line := seamLine(t, a); strings.Contains(line, glyphEffort) {
		t.Fatalf("a session with no dial drew a rung: %q", line)
	}
	if a.seamEffortSpan.pressable() {
		t.Fatal("a session with no dial recorded a press target")
	}
	if _, ok := a.effortDial(); ok {
		t.Fatal("the plain scripted agent claimed a thinking dial")
	}
}

// hostedDial is a connection whose far engine says at the door whether it has a
// dial at all — which is the only honest reading over a wire, since every
// *remote.Agent carries the three methods and "" is a real rung.
type hostedDial struct {
	*effortAgent
	known bool
}

func (h *hostedDial) EffortSupported() bool { return h.known }

// A HOSTED CONVERSATION HAS THE DIAL WHERE THE ENGINE HAS ONE, AND NONE WHERE IT
// HAS NOT. Before the wire carried it, `--host` drew no rung and answered the
// chord with nothing on every engine alike.
func TestAHostedConversationDrawsTheRungItsEngineAdmitsTo(t *testing.T) {
	for _, known := range []bool{true, false} {
		agent := &effortAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4"}, installed: effort.High}
		a := newTestApp(&hostedDial{effortAgent: agent, known: known})
		a.width, a.height = 120, 24
		a.model, a.title = "deepseek/deepseek-v4", "porting the parser"

		line := seamLine(t, a)
		if drew := strings.Contains(line, glyphEffort+" high"); drew != known {
			t.Fatalf("an engine that says known=%v drew rung=%v: %q", known, drew, line)
		}
		drive(t, a, key(effortKey))
		if wrote := len(agent.sets) > 0; wrote != known {
			t.Fatalf("an engine that says known=%v took %d rungs from the chord", known, len(agent.sets))
		}
	}
}

// THE RUNG IS ANCHORED TO THE MODEL AND NOT TO THE END OF THE LINE. The `via`
// rider comes and goes on a sighting's own clock, so a rung drawn after it would
// slide sideways under a hand that had just learned where it was.
func TestTheRungKeepsItsColumnsWhenTheRiderComesAndGoes(t *testing.T) {
	_, a := dialled(t)
	bare := seamLine(t, a)
	at := a.now()
	pinSighting(t, provider.Sighting{
		Model: "deepseek/deepseek-v4", Provider: "quicksilver", At: at.Add(-time.Second),
	}, true)
	served := seamLine(t, a)

	if !strings.Contains(served, glyphEffort+" high · via quicksilver") {
		t.Fatalf("the rider does not follow the rung: %q", served)
	}
	if strings.Index(bare, glyphEffort) != strings.Index(served, glyphEffort) {
		t.Fatalf("the rung moved when the rider arrived:\n%q\n%q", bare, served)
	}
}

// THE RUNG IS GIVEN UP WHOLE OR NOT AT ALL, and it is given up before the name
// is cut: half a rung word is a word somebody reads as another rung.
func TestANarrowSeamDropsTheRungRatherThanCuttingIt(t *testing.T) {
	_, a := dialled(t)
	for width := 120; width >= 40; width-- {
		a.width = width
		line := seamLine(t, a)
		if !strings.Contains(line, glyphEffort) {
			continue
		}
		found := false
		for _, rung := range effort.Rungs {
			if strings.Contains(line, glyphEffort+" "+rung.String()) {
				found = true
			}
		}
		if !found {
			t.Fatalf("at %d columns the seam drew a cut rung: %q", width, line)
		}
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
		if line := seamLine(t, a); !strings.Contains(line, glyphEffort+" "+rung) {
			t.Fatalf("press %d drew %q, want %q", at+1, line, rung)
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

// THE MOMENT IT CHANGES IS THE ONE MOMENT THIS CELL IS ACCENT. Before the chord
// and after the flash it is furniture, in the dim tier the rest of the seam
// wears — THE ACCENT BUDGET is one lit element per screen and a rung that sat
// lit forever would have spent it on a fact that changes once a week.
func TestTheRungIsEmphasizedOnlyWhileItsChangeIsFresh(t *testing.T) {
	_, a := dialled(t)

	if a.effortFlashing() {
		t.Fatal("the rung opened already lit")
	}
	rest := a.legend(a.width)
	drive(t, a, key(effortKey))
	if !a.effortFlashing() {
		t.Fatal("the chord did not light the rung")
	}
	if lit := a.legend(a.width); lit == rest {
		t.Fatal("the lit rung is painted exactly like the resting one")
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

// ── 3. the press ────────────────────────────────────────────────────────────

// PRESSING THE RUNG WALKS IT ONE STEP AND MOVES NO CARET — the same gesture the
// room panel's thinking row makes on a task (roompanel.go), so one press means
// one step wherever a person meets a rung. The seam sits directly above the box,
// so a press that fell through would put the caret in the middle of a sentence
// somebody was still writing (draftclick.go).
func TestPressingTheRungWalksTheLadderOneStepAndLeavesTheCaretAlone(t *testing.T) {
	agent, a := dialled(t)
	typeInto(t, a, "what changed in the relay")
	drive(t, a, key("left"), key("left"))
	caret := a.input.cursor

	_ = frame(a)
	x, y := a.seamEffortSpan.from+1, seamRowY(a)
	if !a.seamEffortSpan.pressable() {
		t.Fatal("the rung recorded no columns to press")
	}
	drive(t, a, clickAt(x, y))

	if got := agent.ConversationEffort(); got != "xhigh" {
		t.Fatalf("the press left the conversation at %q, want xhigh", got)
	}
	if a.effPick.open {
		t.Fatal("the press opened a list instead of walking the ladder")
	}
	if a.input.cursor != caret {
		t.Fatalf("the press moved the caret to %d, want %d", a.input.cursor, caret)
	}
	// And the next press is the next step, which is what makes it a wheel.
	drive(t, a, clickAt(a.seamEffortSpan.from+1, seamRowY(a)))
	if got := agent.ConversationEffort(); got != "max" {
		t.Fatalf("the second press left the conversation at %q, want max", got)
	}
}

// THE SET THAT LIGHTS IS THE SET THE PRESS ACTS ON (hover.go). The rung lights
// on exactly its own columns, and the model beside it lights as its own control
// — two cells, two lights, two different things done to them.
func TestTheRungLightsUnderThePointerOnItsOwnColumns(t *testing.T) {
	_, a := dialled(t)
	_ = frame(a)

	a.setHover(a.seamEffortSpan.from+1, seamRowY(a))
	if !a.hoveringEffort() {
		t.Fatal("the rung does not light under the pointer")
	}
	if a.hoveringStatusModel() {
		t.Fatal("the pointer on the rung lit the model as well")
	}
	hot := frame(a)
	a.dropHover()
	if cold := frame(a); hot == cold {
		t.Fatal("hovering the rung changed nothing on the frame")
	}
	// One cell to the left of the span is the separator, which is not a control.
	a.setHover(a.seamEffortSpan.from-1, seamRowY(a))
	if a.hoveringEffort() {
		t.Fatal("the rung lights from outside its own columns")
	}
	// And the model's own columns still open the picker rather than the ladder.
	a.setHover(a.seamModelSpan.from+1, seamRowY(a))
	if a.hoveringEffort() {
		t.Fatal("the model's columns light the rung")
	}
}

// ── 4. the ladder ───────────────────────────────────────────────────────────

// `/effort` IS THE LADDER'S DOOR now that the pointer's gesture on the cell is
// the wheel, and it TOGGLES: a door that opened a list and then ignored the same
// word typed again would be one with no way back through the gesture that got
// you there.
func TestTheEffortCommandOpensTheLadderAndSetsARungOutright(t *testing.T) {
	agent, a := dialled(t)

	a.slash("/effort")
	if !a.effPick.open {
		t.Fatal("/effort did not open the ladder")
	}
	a.slash("/effort")
	if a.effPick.open {
		t.Fatal("/effort a second time did not put the ladder away")
	}

	// A rung after it is the rung, through the same path the chord and the list
	// both take.
	a.slash("/effort max")
	if got := agent.ConversationEffort(); got != "max" {
		t.Fatalf("/effort max left the conversation at %q", got)
	}
	if a.effPick.open {
		t.Fatal("/effort with a rung opened the list as well")
	}

	// AND A WORD THAT IS NOT A RUNG CHANGES NOTHING AND SAYS THE FIVE. `off` is
	// among them: absence belongs to the settings row, never to this dial.
	for _, word := range []string{"harder", "off"} {
		before := agent.ConversationEffort()
		a.slash("/effort " + word)
		if got := agent.ConversationEffort(); got != before {
			t.Fatalf("/effort %s moved the rung to %q", word, got)
		}
		got := plain(frame(a))
		for _, rung := range effortRungWords() {
			if !strings.Contains(got, rung) {
				t.Fatalf("the refusal of /effort %s does not name %q:\n%s", word, rung, got)
			}
		}
	}
}

// AND ITS OTHER WORDS REACH IT. People say "thinking" because that is what the
// settings row calls the same ladder, and terminal fingers type the short one.
func TestTheOtherWordsForTheEffortCommandReachIt(t *testing.T) {
	for _, word := range []string{"/think", "/thinking"} {
		agent, a := dialled(t)
		a.slash(word + " low")
		if got := agent.ConversationEffort(); got != "low" {
			t.Fatalf("%s low left the conversation at %q", word, got)
		}
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
	// AND A SPACE IS A LETTER. It is called out beside the `x` because space is
	// the one unanswered key that means something on the surfaces around this
	// one, and a rung that let it through would put a character into a sentence
	// nobody is looking at just as surely as the `x` would.
	drive(t, a, key(" "))
	if a.input.String() != "steady" {
		t.Fatalf("a space fell through the ladder into the box: %q", a.input.String())
	}
	if !a.effPick.open {
		t.Fatal("a key the ladder does not answer to closed it")
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
