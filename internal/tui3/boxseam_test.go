package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The box seam's acceptance tests: one rule over every box a person types to
// an agent in, saying the same four things in the same spelling, with the
// same doors on it — and the draft's pins landing on the conversation the box
// opens (boxseam.go).

// draftAgent is a [fakeAgent] that can say what a fresh conversation on this
// install would think at and run under — the two halves the draft's rule asks
// of the window's session — and that can take a rung and a posture the way the
// conversation the box opens does.
type draftAgent struct {
	*fakeAgent
	installed    effort.Rung
	conversation effort.Rung
	standing     string
	stored       string
	door         bool
	sets         []string
}

func (d *draftAgent) DefaultEffort() string           { return d.installed.String() }
func (d *draftAgent) ConversationEffort() string      { return d.conversation.String() }
func (d *draftAgent) StandingApprovalPosture() string { return d.standing }
func (d *draftAgent) ApprovalDial() bool              { return d.door }

func (d *draftAgent) ResolvedEffort() string {
	return effort.Resolve(effort.Scope{Conversation: d.conversation, Default: d.installed}).String()
}

func (d *draftAgent) SetConversationEffort(rung string) bool {
	parsed, ok := effort.Parse(rung)
	if !ok {
		return false
	}
	d.conversation = parsed
	return true
}

func (d *draftAgent) ResolvedApprovalPosture() string {
	if d.stored != "" && d.stored != session.PostureAuto {
		return d.stored
	}
	return d.standing
}

func (d *draftAgent) SetApprovalPosture(posture string) error {
	d.sets = append(d.sets, posture)
	d.stored = posture
	return nil
}

// drafting is [placeApp] on a session that can say the install's rung and the
// rows' posture, wide enough for the whole rule.
func drafting(t *testing.T) (*draftAgent, *app) {
	t.Helper()
	a := placeApp(t)
	agent := &draftAgent{fakeAgent: &fakeAgent{model: "m"}, installed: effort.High, standing: session.PostureAsk, door: true}
	a.agent = agent
	a.width, a.height = 200, 30
	return agent, a
}

// draftPlaces is every place whose box is a draft for a conversation.
var draftPlaces = []page{pageHome, pageTasks, pageStanding, pageMemory, pageSpend, pageSearch}

// ── 1. one rule, every place ────────────────────────────────────────────────

// THE RULE OVER EVERY DRAFT BOX SAYS THE SAME FOUR THINGS, in the cells the
// conversation's own seam uses: where, what, how hard, and what runs without
// asking. Settings, whose box is a value editor, has no draft and no rule of
// this shape.
func TestTheRuleOverEveryDraftBoxSaysTheSameFourThings(t *testing.T) {
	_, a := drafting(t)
	rung, gate := a.effortChip(effort.High.String()), a.approvalChip(session.PostureAsk)
	for _, id := range draftPlaces {
		a.showPage(id)
		text := placeFrameText(a)
		for _, want := range []string{targetLeadWord, "m", rung, gate} {
			if !strings.Contains(text, want) {
				t.Fatalf("the %s place's rule does not say %q:\n%s", id.word(), want, text)
			}
		}
	}
	a.showPage(pageSettings)
	if text := placeFrameText(a); strings.Contains(text, targetLeadWord) || strings.Contains(text, gate) {
		t.Fatalf("settings, whose box edits a row, drew the draft's rule:\n%s", text)
	}
}

// AND THE CELLS ARE SPELLED EXACTLY AS THE CONVERSATION SPELLS THEM: one
// function per cell, asked by both lines.
func TestTheDraftsCellsAreTheSeamsOwnSpelling(t *testing.T) {
	agent, a := gated(t)
	agent.stored = session.PostureGuardian
	if got, want := a.approvalChipText(), a.approvalChip(session.PostureGuardian); got != want {
		t.Fatalf("the seam spells the gate %q and the draft would spell it %q", got, want)
	}
	dial, b := dialled(t)
	dial.conversation = effort.Max
	if got, want := b.effortChipText(), b.effortChip(effort.Max.String()); got != want {
		t.Fatalf("the seam spells the rung %q and the draft would spell it %q", got, want)
	}
}

// ── 2. the chords, on every place ───────────────────────────────────────────

// `alt+y` AND `ctrl+v` WALK THE DRAFT'S GATE AND RUNG ON EVERY PLACE, one stop
// per press, on the conversation's own wheels: the gate never lands on
// `refuses`, and the rung comes back to `auto` off the top.
func TestTheDraftsChordsWalkTheRungAndTheGateOnEveryPlace(t *testing.T) {
	for _, id := range draftPlaces {
		_, a := drafting(t)
		a.showPage(id)
		drive(t, a, key("alt+y"))
		if got, _ := a.targetApproval(); got != session.PostureGuardian {
			t.Fatalf("%s: one alt+y from ask lands on %q, want guardian", id.word(), got)
		}
		drive(t, a, key("alt+y"))
		if got, _ := a.targetApproval(); got != session.PostureAllow {
			t.Fatalf("%s: two alt+y from ask land on %q, want allow", id.word(), got)
		}
		if text := placeFrameText(a); !strings.Contains(text, a.approvalChip(session.PostureAllow)) {
			t.Fatalf("%s: the rule does not say the open gate:\n%s", id.word(), text)
		}
		drive(t, a, key("alt+y"))
		if got, _ := a.targetApproval(); got != session.PostureAsk {
			t.Fatalf("%s: the wheel went to %q past YOLO, want ask and never refuses", id.word(), got)
		}
		drive(t, a, key("ctrl+v"))
		if got, _ := a.targetEffort(); got != effortNextClearing(effort.High).String() {
			t.Fatalf("%s: one ctrl+v from the install's high lands on %q, want %s", id.word(), got, effortNextClearing(effort.High))
		}
	}
}

// AND THE CELLS ARE DOORS UNDER THE POINTER, at the columns the frame drew
// them — a press on the rung is `ctrl+v` and a press on the gate is `alt+y`.
func TestTheDraftsCellsAreDoorsUnderThePointer(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageSpend)
	placeFrameText(a)
	if !a.targetEffortSpan.pressable() || !a.targetApprovalSpan.pressable() || a.targetRow < 1 {
		t.Fatalf("the rule recorded no doors: rung %+v, gate %+v, row %d", a.targetEffortSpan, a.targetApprovalSpan, a.targetRow)
	}
	if _, took := a.placeTargetPress(a.targetApprovalSpan.from, a.targetRow); !took {
		t.Fatal("a press on the gate's cell was not taken")
	}
	if got, _ := a.targetApproval(); got != session.PostureGuardian {
		t.Fatalf("a press on the gate lands on %q, want guardian", got)
	}
	if _, took := a.placeTargetPress(a.targetEffortSpan.from, a.targetRow); !took {
		t.Fatal("a press on the rung's cell was not taken")
	}
	if got, _ := a.targetEffort(); got != effortNextClearing(effort.High).String() {
		t.Fatalf("a press on the rung lands on %q", got)
	}
}

// A WINDOW WHOSE SESSION CANNOT SAY DRAWS NEITHER CELL, and the chords do
// nothing: a control with nothing behind it is absent, not broken.
func TestADraftWithNoDialDrawsNoRungAndNoGate(t *testing.T) {
	a := placeApp(t)
	a.width = 200
	a.showPage(pageSpend)
	text := placeFrameText(a)
	if strings.Contains(text, glyphPermTool+" ") || strings.Contains(text, glyphEffort+" ") {
		t.Fatalf("a session with no dial drew a cell it cannot move:\n%s", text)
	}
	if !strings.Contains(text, targetLeadWord) {
		t.Fatalf("the folder and the model still belong on the rule:\n%s", text)
	}
	drive(t, a, key("alt+y"), key("ctrl+v"))
	if a.target.approval != "" || a.target.effort != "" {
		t.Fatalf("a chord pinned something the rule never offered: gate %q, rung %q", a.target.approval, a.target.effort)
	}
}

// ── 3. the pins ride onto the conversation ──────────────────────────────────

// THE PINS LAND ON THE CONVERSATION THE BOX OPENS, through the session's own
// doors, and THE GATE IS SPENT WHILE THE RUNG STICKS: an open gate is a safety
// claim about one conversation; how hard you think is how you work.
func TestThePinsRideOntoTheConversationAndTheGateIsSpent(t *testing.T) {
	_, a := drafting(t)
	next := &draftAgent{fakeAgent: &fakeAgent{model: "m"}, installed: effort.High, standing: session.PostureAsk, door: true}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: workspace + "/next/transcript.jsonl", Workspace: workspace}, nil
	}
	drive(t, a, key("alt+y"), key("alt+y"), key("ctrl+v"))
	rung, _ := a.targetEffort()

	typeHome(a, "why is the lexer allocating")
	spend(t, a, a.homeEnter())

	if got := next.sets; len(got) != 1 || got[0] != session.PostureAllow {
		t.Fatalf("the new conversation was handed %q, want the pinned allow once", got)
	}
	if got := next.ResolvedEffort(); got != rung {
		t.Fatalf("the new conversation thinks at %q, want the pinned %q", got, rung)
	}
	if a.target.approval != "" {
		t.Fatalf("the gate pin survived the conversation that used it: %q", a.target.approval)
	}
	if a.target.effort != rung {
		t.Fatalf("the rung pin was spent: %q, want %q", a.target.effort, rung)
	}
	// AND THE RULE ON HOME IS BACK AT THE ROWS' OWN WORD, so the next draft
	// starts safe and says so.
	runCmd(a.openHome())
	if got, _ := a.targetApproval(); got != session.PostureAsk {
		t.Fatalf("home's next draft opens at %q, want the rows' ask", got)
	}
}

// AND FROM EVERY OTHER PLACE TOO: `enter` on a sentence typed at spend opens a
// conversation carrying the same pins, because it is the same draft.
func TestThePinsRideFromAnyPlaceNotOnlyHome(t *testing.T) {
	_, a := drafting(t)
	next := &draftAgent{fakeAgent: &fakeAgent{model: "m"}, installed: effort.High, standing: session.PostureAsk, door: true}
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: next, SessionFile: workspace + "/next/transcript.jsonl", Workspace: workspace}, nil
	}
	a.showPage(pageSpend)
	drive(t, a, key("alt+y"))
	drive(t, a, key("c"), key("u"), key("t"), key("enter"))
	if got := next.sets; len(got) != 1 || got[0] != session.PostureGuardian {
		t.Fatalf("a conversation opened from spend was handed %q, want the pinned guardian", got)
	}
}

// ── 4. the ladder ───────────────────────────────────────────────────────────

// THE RUNG GOES BEFORE THE GATE, THE GATE BEFORE THE MODEL, AND THE FOLDER
// OUTLIVES THEM ALL — the seam's order for the cells under home's own ruling
// that the folder is the fact `enter` acts on.
func TestTheDraftsRuleGivesUpTheRungThenTheGateThenTheModel(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageSpend)
	rung, gate := a.effortChip(effort.High.String()), a.approvalChip(session.PostureAsk)
	left, _, _, _, _ := a.draftSeamLeft(1000)
	if !strings.Contains(left, rung) || !strings.Contains(left, gate) {
		t.Fatalf("a wide rule dropped a cell:\n%s", left)
	}
	// ONE CELL SHORT OF EVERYTHING, measured off the label the wide rule builds
	// and the arithmetic [legendRoom] reads backwards: the first thing the
	// ladder gives up has to be the rung, and it buys more than one cell.
	room := ansi.StringWidth(left) - 1 + 3 + legendGap + ansi.StringWidth(a.targetLegendRight()) + 3
	line, drew := a.targetLegend(room, a.pal)
	if !drew {
		t.Fatalf("a %d-column rule drew nothing", room)
	}
	got := ansi.Strip(line)
	if strings.Contains(got, rung) {
		t.Fatalf("the rung outlived the room for it:\n%s", got)
	}
	if !strings.Contains(got, gate) || !strings.Contains(got, "m") || !strings.Contains(got, targetLeadWord) {
		t.Fatalf("the gate, the model or the folder went before the rung:\n%s", got)
	}
}
