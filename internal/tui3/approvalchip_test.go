package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The approvals chip's acceptance tests: what the SEAM says about the gate,
// what the chord, the press and the command do to it, and what a session with
// no dial gets instead.
//
// Each asserts the FACT the behaviour exists for. The cell must be there at
// every posture, because a control invisible until it has been used is not a
// control; the open gate must be loud, because that is the one posture a
// person must not be able to forget; the wheel must never land on `refuses`
// by a press; and over a connection the cell must be a reading and not a knob.

// approvalAgent is a [fakeAgent] that can say and move what runs without
// asking. It is a separate double for [approvalDialer]'s reason: a session
// with no door has no chip, and the suite needs both of those sessions.
type approvalAgent struct {
	*fakeAgent
	// stored is the posture this conversation was set to; standing is what
	// the settings rows amount to; launch is what --yolo handed down.
	stored, standing, launch string
	// door is whether the session has a dial at all.
	door bool
	// sets is every word the surface handed to SetApprovalPosture, in order.
	sets []string
	// refuse makes the door fail, for the test about a rebuild that cannot.
	refuse error
}

func (g *approvalAgent) ApprovalDial() bool { return g.door }

func (g *approvalAgent) ResolvedApprovalPosture() string {
	switch {
	case g.stored != "" && g.stored != session.PostureAuto:
		return g.stored
	case g.stored == "" && g.launch != "":
		return g.launch
	}
	return g.standing
}

func (g *approvalAgent) SetApprovalPosture(posture string) error {
	g.sets = append(g.sets, posture)
	if g.refuse != nil {
		return g.refuse
	}
	g.stored = posture
	return nil
}

// gated is an app on a session whose rows say ask, at a width the whole seam
// fits on, for [dialled]'s reason.
func gated(t *testing.T) (*approvalAgent, *app) {
	t.Helper()
	agent := &approvalAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4"}, standing: session.PostureAsk, door: true}
	a := newTestApp(agent)
	a.width, a.height = 140, 24
	a.model, a.title = "deepseek/deepseek-v4", "porting the parser"
	return agent, a
}

// ── 1. the cell on the seam ─────────────────────────────────────────────────

// THE CELL IS THERE AT EVERY POSTURE, and it says the posture in force in a
// person's words — after the rung, because it is a fact about what the model
// may do.
func TestTheSeamSaysWhatRunsWithoutAskingAtEveryPosture(t *testing.T) {
	agent, a := gated(t)
	cases := map[string]string{
		session.PostureAsk:      approvalAsksWord,
		session.PostureGuardian: approvalGuardianWord,
		session.PostureAllow:    approvalYoloWord,
		session.PostureDeny:     approvalRefusesWord,
	}
	for posture, word := range cases {
		agent.stored = posture
		line := seamLine(t, a)
		if !strings.Contains(line, glyphPermTool+" "+word) {
			t.Fatalf("at %q the seam reads %q, want %q on it", posture, line, word)
		}
	}
	// AND THE OLD BADGE IS OFF THE STATUS ROW at the one posture it used to
	// draw, because the chip is saying it a line above.
	agent.stored = session.PostureAllow
	if strings.Contains(plain(a.status(200)), approvalYoloWord) {
		t.Fatalf("the status row still carries the badge beside the chip:\n%q", plain(a.status(200)))
	}
	if got := a.approvalSegment(); got != "" {
		t.Fatalf("the row's segment says %q while the seam carries the chip", got)
	}
}

// THE BADGE COMES BACK ON THE FRAMES THE SEAM IS NOT ON. The welcome box
// stands where the conversation will be, and it is exactly where a person who
// typed --yolo reads whether it took; a room replaces the seam's left with the
// way out. On both the row says `YOLO` when the gate is open, and only then.
func TestTheRowCarriesTheBadgeWhereTheSeamHasNoChip(t *testing.T) {
	agent, a := gated(t)
	agent.stored = session.PostureAllow
	a.welcome.open = true
	if got := a.approvalSegment(); got != approvalYoloWord {
		t.Fatalf("with the welcome up the row says %q, want the badge", got)
	}
	if !strings.Contains(plain(a.status(200)), approvalYoloWord) {
		t.Fatalf("the welcome frame does not say the gate is open:\n%q", plain(a.status(200)))
	}
	agent.stored = session.PostureAsk
	if got := a.approvalSegment(); got != "" {
		t.Fatalf("with the welcome up and the gate asking the row says %q", got)
	}
	a.welcome.open = false
	agent.stored = session.PostureAllow
	if got := a.approvalSegment(); got != "" {
		t.Fatalf("with the seam back the row still says %q", got)
	}
}

// THE OPEN GATE IS LOUD, AND ASKING IS FURNITURE. The bad hue is on the cell
// for as long as the gate is open, which is what the badge bought and the one
// thing its absence from the row must not cost.
func TestTheOpenGateWearsTheBadHueAndAskingDoesNot(t *testing.T) {
	agent, a := gated(t)
	rows := strings.Split(frame(a), "\n")
	row := rows[seamRowY(a)]
	if strings.Contains(row, a.pal.bad(glyphPermTool+" "+approvalAsksWord)) {
		t.Fatalf("the asking posture is painted as a warning:\n%q", row)
	}
	agent.stored = session.PostureAllow
	rows = strings.Split(frame(a), "\n")
	row = rows[seamRowY(a)]
	if !strings.Contains(row, a.pal.bad(glyphPermTool+" "+approvalYoloWord)) {
		t.Fatalf("the open gate is not painted as one:\n%q", row)
	}
}

// A SESSION WITH NO DIAL DRAWS NO CELL, and the chord says so rather than
// doing nothing.
func TestASessionWithNoDialDrawsNoChipAndTheChordSaysSo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "deepseek/deepseek-v4"})
	a.width, a.height = 140, 24
	a.model = "deepseek/deepseek-v4"
	if line := seamLine(t, a); strings.Contains(line, glyphPermTool) {
		t.Fatalf("a session with no dial drew a chip: %q", line)
	}
	drive(t, a, key(approvalKey))
	if !strings.Contains(plain(frame(a)), approvalUnavailableWord) {
		t.Fatalf("the chord on a dial-less session said nothing:\n%s", plain(frame(a)))
	}
}

// OVER A CONNECTION THE DIAL IS THE FAR ENGINE'S. An engine with the door
// moves its own posture and the chip says what it resolved; an engine without
// it leaves the chip a reading of the posture that travelled once, and every
// door onto moving it says where the rules actually are.
func TestAHostedConversationMovesTheFarGateOrSaysWhoseRulesDecide(t *testing.T) {
	agent := &approvalAgent{fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4"}, standing: session.PostureAsk, door: true}
	a := newTestApp(agent)
	a.width, a.height = 140, 24
	a.model = "deepseek/deepseek-v4"
	a.host, a.handedApproval = "devbox", "prompt"
	a.approval = a.approvalPosture()
	drive(t, a, key(approvalKey))
	if got := agent.stored; got != session.PostureGuardian {
		t.Fatalf("the chord on an engine with the door left it at %q, want guardian", got)
	}

	// The engine without the door: the chip is the far machine's word and the
	// chord is a sentence, not a move.
	agent.door, agent.sets = false, nil
	a.handedApproval = "allow"
	a.approval = a.approvalPosture()
	if line := seamLine(t, a); !strings.Contains(line, glyphPermTool+" "+approvalYoloWord) {
		t.Fatalf("a hosted session with the gate open drew %q", line)
	}
	drive(t, a, key(approvalKey))
	if len(agent.sets) != 0 {
		t.Fatalf("the chord moved a gate the engine has no door onto: %v", agent.sets)
	}
	if !strings.Contains(plain(frame(a)), "decided on the machine the conversation runs on") {
		t.Fatalf("the chord did not say whose rules decide:\n%s", plain(frame(a)))
	}
}

// ── 2. the chord and the press ──────────────────────────────────────────────

// THE WHEEL HAS THREE STOPS AND NEVER LANDS ON refuses: asks → guardian → YOLO
// → asks, from whatever the chip says.
func TestTheChordWalksAsksGuardianYoloAndNeverRefuses(t *testing.T) {
	agent, a := gated(t)
	want := []string{session.PostureGuardian, session.PostureAllow, session.PostureAsk, session.PostureGuardian}
	for at, posture := range want {
		drive(t, a, key(approvalKey))
		if got := agent.stored; got != posture {
			t.Fatalf("press %d left the conversation at %q, want %q", at+1, got, posture)
		}
	}
	// And from refuses — reached by name — the first press lands on the wheel.
	agent.stored = session.PostureDeny
	drive(t, a, key(approvalKey))
	if got := agent.stored; got != session.PostureAsk {
		t.Fatalf("a press from refuses landed on %q, want the wheel's first stop", got)
	}
	for _, word := range agent.sets {
		if word == session.PostureDeny {
			t.Fatal("the wheel walked onto refuses")
		}
	}
}

// THE CHORD SURVIVES A DRAFT: it is a chord, it carries no text, and it leaves
// the sentence and the caret exactly where they were.
func TestTheApprovalChordWorksMidDraftAndDisturbsNeitherTextNorCaret(t *testing.T) {
	agent, a := gated(t)
	typeInto(t, a, "what changed in the relay")
	drive(t, a, key("left"), key("left"))
	caret, text := a.input.cursor, a.input.String()
	drive(t, a, key(approvalKey))
	if got := agent.stored; got != session.PostureGuardian {
		t.Fatalf("the chord left the conversation at %q, want guardian", got)
	}
	if a.input.String() != text || a.input.cursor != caret {
		t.Fatalf("the chord disturbed the draft: %q at %d, want %q at %d", a.input.String(), a.input.cursor, text, caret)
	}
}

// THE PRESS ON THE CELL IS THE SAME STEP AS THE CHORD, on its own columns, and
// it leaves the caret alone.
func TestPressingTheChipWalksTheWheelOneStopOnItsOwnColumns(t *testing.T) {
	agent, a := gated(t)
	typeInto(t, a, "what changed")
	caret := a.input.cursor
	_ = frame(a)
	if !a.seamApprovalSpan.pressable() {
		t.Fatal("the chip recorded no columns to press")
	}
	if a.seamEffortSpan.pressable() && a.seamEffortSpan.to > a.seamApprovalSpan.from {
		t.Fatalf("the chip's columns %v overlap the rung's %v", a.seamApprovalSpan, a.seamEffortSpan)
	}
	drive(t, a, clickAt(a.seamApprovalSpan.from+1, seamRowY(a)))
	if got := agent.stored; got != session.PostureGuardian {
		t.Fatalf("the press left the conversation at %q, want guardian", got)
	}
	if a.input.cursor != caret {
		t.Fatalf("the press moved the caret to %d, want %d", a.input.cursor, caret)
	}
	// And a press on the rung beside it still walks the rung, not the gate.
	agent.sets = nil
	if a.seamEffortSpan.pressable() {
		drive(t, a, clickAt(a.seamEffortSpan.from+1, seamRowY(a)))
		if len(agent.sets) != 0 {
			t.Fatalf("a press on the rung moved the gate: %v", agent.sets)
		}
	}
}

// THE CELL WEARS ITS CHANGE FOR TWO SECONDS AND THEN SETTLES, for the thinking
// chip's reason.
func TestTheChipIsEmphasizedOnlyWhileItsChangeIsFresh(t *testing.T) {
	_, a := gated(t)
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	drive(t, a, key(approvalKey))
	if !a.approvalFlashing() {
		t.Fatal("the chip did not take the emphasis on the press")
	}
	now = now.Add(approvalFlashFor + 1)
	if a.approvalFlashing() {
		t.Fatal("the chip is still lit after the flash should have settled")
	}
}

// A DOOR THAT REFUSES IS SAID, in the door's own words, and the chip stays put.
func TestARefusedMoveIsSaidAndTheChipStaysPut(t *testing.T) {
	agent, a := gated(t)
	agent.refuse = errors.New("the rules do not read back")
	drive(t, a, key(approvalKey))
	if got := agent.stored; got != "" {
		t.Fatalf("a refused move left the conversation at %q", got)
	}
	if !strings.Contains(plain(frame(a)), "the rules do not read back") {
		t.Fatalf("the refusal was swallowed:\n%s", plain(frame(a)))
	}
}

// ── 3. the command ──────────────────────────────────────────────────────────

// `/approvals <word>` SETS THE POSTURE OUTRIGHT, under every word a person
// plausibly types for it, and the bare form prints the stops with the one in
// force marked.
func TestTheApprovalsCommandSetsAPostureUnderEveryWordForIt(t *testing.T) {
	agent, a := gated(t)
	cases := map[string]string{
		"yolo": session.PostureAllow, "allow": session.PostureAllow,
		"ask": session.PostureAsk, "prompt": session.PostureAsk,
		"guardian": session.PostureGuardian,
		"deny":     session.PostureDeny, "refuse": session.PostureDeny,
		"auto": session.PostureAuto, "off": session.PostureAuto,
	}
	for word, posture := range cases {
		spend(t, a, a.slash("/approvals "+word))
		if got := agent.stored; got != posture {
			t.Fatalf("/approvals %s left the conversation at %q, want %q", word, got, posture)
		}
	}
	// The alias is the flag's word.
	spend(t, a, a.slash("/yolo ask"))
	if got := agent.stored; got != session.PostureAsk {
		t.Fatalf("/yolo ask left the conversation at %q", got)
	}
	// An unknown word changes nothing and names the words.
	agent.sets = nil
	spend(t, a, a.slash("/approvals wide"))
	if len(agent.sets) != 0 {
		t.Fatalf("an unknown word moved the gate: %v", agent.sets)
	}
	screen := plain(frame(a))
	for _, word := range approvalCommandWords() {
		if !strings.Contains(screen, word) {
			t.Fatalf("the refusal does not name %q:\n%s", word, screen)
		}
	}
	// And the bare form lists every stop with what it buys.
	spend(t, a, a.slash("/approvals"))
	screen = plain(frame(a))
	for _, line := range approvalLines {
		if !strings.Contains(screen, line) {
			t.Fatalf("the list does not say %q:\n%s", line, screen)
		}
	}
}

// THE SEAM GIVES THE CHIP UP AFTER THE RUNG AND WHOLE, never cut: what may run
// without asking outranks how hard it thinks.
func TestANarrowSeamDropsTheRungBeforeTheChipAndNeverCutsIt(t *testing.T) {
	agent, a := gated(t)
	agent.stored = session.PostureAllow
	sawChipWithoutRung := false
	for width := 140; width >= 40; width-- {
		a.width = width
		line := seamLine(t, a)
		hasRung := strings.Contains(line, glyphEffort+" ")
		hasChip := strings.Contains(line, glyphPermTool+" "+approvalYoloWord)
		if strings.Contains(line, glyphPermTool) && !hasChip {
			t.Fatalf("at %d the chip was cut: %q", width, line)
		}
		if hasRung && !hasChip {
			t.Fatalf("at %d the seam kept the rung and gave up the chip: %q", width, line)
		}
		if hasChip && !hasRung {
			sawChipWithoutRung = true
		}
	}
	if !sawChipWithoutRung {
		t.Fatal("no width kept the chip after the rung had gone — the chip is not outranking the rung")
	}
}
