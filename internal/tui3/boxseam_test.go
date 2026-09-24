package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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

// draftPlaces is every place whose box is a draft for a conversation: home,
// and home alone (pages.go's [place.box]).
var draftPlaces = []page{pageHome}

// ── 1. one rule, every place ────────────────────────────────────────────────

// THE RULE OVER HOME'S BOX SAYS THE SAME FOUR THINGS AS A CONVERSATION'S SEAM:
// where, what, how hard, and what runs without asking. No other place draws
// it — only home starts things (pages.go's [place.box]).
func TestTheRuleOverHomesBoxSaysTheSameFourThingsAsTheSeam(t *testing.T) {
	_, a := drafting(t)
	rung, gate := a.effortChip(effort.High.String()), a.approvalChip(session.PostureAsk)
	a.showPage(pageHome)
	text := placeFrameText(a)
	for _, want := range []string{targetProjectLead, "m", rung, gate} {
		if !strings.Contains(text, want) {
			t.Fatalf("home's rule does not say %q:\n%s", want, text)
		}
	}
	for _, id := range []page{pageTasks, pageSpend, pageSearch, pageSettings} {
		a.showPage(id)
		if text := placeFrameText(a); strings.Contains(text, targetProjectLead) || strings.Contains(text, gate) {
			t.Fatalf("the %s place drew the draft's rule:\n%s", id.word(), text)
		}
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

// `alt+a` AND `alt+e` WALK THE DRAFT'S GATE AND RUNG ON HOME, one stop per
// press, on the conversation's own wheels: the gate never lands on `refuses`,
// and the rung comes back to `auto` off the top.
func TestTheDraftsChordsWalkTheRungAndTheGateOnHome(t *testing.T) {
	for _, id := range draftPlaces {
		_, a := drafting(t)
		a.showPage(id)
		if hint := a.homeHint(); !strings.Contains(hint, "alt+e effort · alt+a approvals") {
			t.Fatalf("home does not name its approval control: %q", hint)
		}
		drive(t, a, key("alt+a"))
		if got, _ := a.targetApproval(); got != session.PostureGuardian {
			t.Fatalf("%s: one alt+a from ask lands on %q, want guardian", id.word(), got)
		}
		drive(t, a, key("alt+a"))
		if got, _ := a.targetApproval(); got != session.PostureAllow {
			t.Fatalf("%s: two alt+a from ask land on %q, want allow", id.word(), got)
		}
		if text := placeFrameText(a); !strings.Contains(text, a.approvalChip(session.PostureAllow)) {
			t.Fatalf("%s: the rule does not say the open gate:\n%s", id.word(), text)
		}
		drive(t, a, key("alt+a"))
		if got, _ := a.targetApproval(); got != session.PostureAsk {
			t.Fatalf("%s: the wheel went to %q past YOLO, want ask and never refuses", id.word(), got)
		}
		before, _ := a.targetEffort()
		drive(t, a, key("ctrl+v"))
		if got, _ := a.targetEffort(); got != before {
			t.Fatalf("the retired effort chord changed the home draft from %q to %q", before, got)
		}
		drive(t, a, key("alt+e"))
		if got, _ := a.targetEffort(); got != effortNextClearing(effort.High).String() {
			t.Fatalf("%s: one alt+e from the install's high lands on %q, want %s", id.word(), got, effortNextClearing(effort.High))
		}
	}
}

// AND THE CELLS ARE DOORS UNDER THE POINTER, at the columns the frame drew
// them — a press on the rung is `alt+e` and a press on the gate is `alt+a`.
func TestTheDraftsCellsAreDoorsUnderThePointer(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageHome)
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
	a.showPage(pageHome)
	text := placeFrameText(a)
	if strings.Contains(text, glyphPermTool+" ") || strings.Contains(text, ":auto") {
		t.Fatalf("a session with no dial drew a cell it cannot move:\n%s", text)
	}
	if !strings.Contains(text, targetProjectLead) {
		t.Fatalf("the folder and the model still belong on the rule:\n%s", text)
	}
	if hint := a.homeHint(); strings.Contains(hint, "alt+a") {
		t.Fatalf("home advertises an unavailable approval control: %q", hint)
	}
	drive(t, a, key("alt+a"), key("alt+e"))
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
	drive(t, a, key("alt+a"), key("alt+a"), key("alt+e"))
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

// ── 4. the ladder ───────────────────────────────────────────────────────────

// The draft rule carries the model, the rung and the gate, and no longer the
// project: that is the keys row's since 2026-09-22 (hometiplayout_test.go
// proves the row and its door).
func TestTheDraftRuleKeepsTheModelAndNotTheProject(t *testing.T) {
	_, a := drafting(t)
	a.showPage(pageHome)
	a.model = "moonshotai/kimi-k3"
	a.target.where = "/tmp/landing-test"
	line, drew := a.targetLegend(120, a.pal)
	text := ansi.Strip(line)
	if !drew || !strings.HasPrefix(text, "─ moonshotai/kimi-k3:high · ") || strings.Contains(text, targetProjectLead) {
		t.Fatalf("the draft seam has the wrong shape: %q", text)
	}
	if a.targetModelSpan.from != 2 {
		t.Fatalf("the seam's model door did not move with it: %+v", a.targetModelSpan)
	}
	for width := 1; width <= 120; width++ {
		line, _ := a.targetLegend(width, a.pal)
		if ansi.StringWidth(line) > width {
			t.Fatalf("at %d cells the seam overflowed: %q", width, ansi.Strip(line))
		}
	}
}

// A conversation's seam no longer names its workspace: the project is at the
// right end of the keys row under the box (chattip_test.go proves the row),
// and a pin for the next conversation must not relabel this one anywhere.
func TestConversationProjectHasLeftTheSeam(t *testing.T) {
	_, a := gated(t)
	a.tilde, a.workspace = "/home/person", "/home/person/projects/parser"
	a.target.where = "/tmp/next-project"
	for width := 1; width <= 240; width++ {
		line := ansi.Strip(a.legend(width))
		if ansi.StringWidth(line) > width {
			t.Fatalf("at %d cells the conversation seam overflowed: %q", width, line)
		}
		if strings.Contains(line, targetProjectLead) {
			t.Fatalf("at %d cells the seam still names the project: %q", width, line)
		}
	}
	if text := ansi.Strip(a.hintRow(240)); strings.Contains(text, "next-project") {
		t.Fatalf("the keys row names the next conversation's folder: %q", text)
	}
}

// The model stays identifiable before anyone pins it or points at it.
func TestTheCurrentModelIsBoldAndBrightOnBothSeams(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		_, home := drafting(t)
		_, conversation := gated(t)
		for _, a := range []*app{home, conversation} {
			a.pal = newPalette(tokens.TrueColor, false)
			a.model = "deepseek/deepseek-v4.1-flash"
			if catalog {
				a.sources = modelsource.NewSet(modelsource.Connected{Source: modelsource.Source{ID: modelsource.DefaultID}})
			}
		}
		for _, pinned := range []string{"", "moonshotai/kimi-k3"} {
			home.target.model = pinned
			line, ok := home.targetLegend(200, home.pal)
			want := home.targetModel()
			if !ok || !strings.Contains(line, home.pal.bold(home.pal.data(want))) {
				t.Fatalf("home's full model is not bold and bright (catalog %v, pin %q): %q", catalog, pinned, line)
			}
			if got := ansi.Cut(plain(line), home.targetModelSpan.from, home.targetModelSpan.to); got != want {
				t.Fatalf("home's model click span covers %q, want %q", got, want)
			}
		}
		line := conversation.legend(200)
		if !strings.Contains(line, conversation.pal.bold(conversation.pal.data(conversation.model))) {
			t.Fatalf("the conversation's full model is not bold and bright (catalog %v): %q", catalog, line)
		}
	}
}

// The idle footer shares home's control order and keeps its way home clickable
// after the commands door, including on the phone deck.
func TestConversationControlsMatchHomeAndKeepTheHomeDoor(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "here", "/tmp/alpha", now)
	a := lab.door(mine)
	agent, _ := drafting(t)
	a.agent = agent
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/other.jsonl"}, &aside{since: a.now()})
	a.chords.meta = chordMetaWord
	a.notices.enabled = false
	a.branch = "dev"
	want := "opt+e effort · opt+a approvals · opt+k chats · / commands · space space home"
	if got := a.footHint(200); got != want {
		t.Fatalf("conversation controls = %q, want %q", got, want)
	}
	for _, width := range []int{200, 100, 80, 60, 44, 30, 15} {
		a.width, a.height = width, 30
		frame, _, _ := a.frame()
		if strings.Contains(plain(a.legend(width)), " · dev") {
			t.Fatalf("the seam carries a branch at %d columns", width)
		}
		if !strings.Contains(plain(frame), microcopy) {
			t.Fatalf("commands vanished at %d columns:\n%s", width, plain(frame))
		}
		if !a.homeDoor.pressable() {
			continue
		}
		row := -1
		for y := 0; y < a.height; y++ {
			if mark, ok := a.chromeAt(y); ok && mark.kind == a.hintRowKind() {
				row = y
			}
		}
		if row < 0 {
			t.Fatal("no hint row")
		}
		line := strings.Split(plain(frame), "\n")[row]
		if got := ansi.Cut(line, a.homeDoor.from, a.homeDoor.to); got != homeDoorWord {
			t.Fatalf("home hit target covers %q at %d columns: %q", got, width, line)
		}
		if _, took := a.homeDoorPress(a.homeDoor.from, row); !took || !a.at(pageHome) {
			t.Fatalf("home hint did not open home at %d columns", width)
		}
		a.closeHome()
	}
}

// The same effort control names the platform's modifier on both message boxes
// and on the ladder, so a Mac never shows an Alt hint beside Option hints.
func TestEffortHintsUseThePlatformModifier(t *testing.T) {
	for _, modifier := range []string{chordAltWord, chordMetaWord} {
		t.Run(modifier, func(t *testing.T) {
			_, a := drafting(t)
			a.chords.meta = modifier
			a.showPage(pageHome)
			if got := a.homeHint(); !strings.Contains(got, modifier+"e effort") {
				t.Fatalf("home effort hint: %q", got)
			}
			if got := a.idleHint(); !strings.Contains(got, modifier+"e effort") {
				t.Fatalf("conversation effort hint: %q", got)
			}
			a.effPick.open = true
			if got := a.hintWord(); !strings.Contains(got, modifier+"e next rung") {
				t.Fatalf("effort picker hint: %q", got)
			}
		})
	}
}

// The model picker's draft level must be visible on the seam before a new
// conversation exists, while the message-box effort pin keeps precedence.
func TestHomeEffortSeamReflectsDraftPickerReasoning(t *testing.T) {
	_, a := drafting(t)
	a.target.model = "org/model"
	a.target.levels = map[string]string{session.ReasoningKey("org/model"): "low"}
	if got, ok := a.targetEffort(); !ok || got != "low" {
		t.Fatalf("picker effort reached the seam as %q, supported=%v; want low", got, ok)
	}
	a.target.effort = "medium"
	if got, ok := a.targetEffort(); !ok || got != "medium" {
		t.Fatalf("explicit draft effort reached the seam as %q, supported=%v; want medium", got, ok)
	}
}
