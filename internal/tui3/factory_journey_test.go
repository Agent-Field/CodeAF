package tui3

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE OWNER'S JOURNEY (2026-10-09) ────────────────────────────────────────
//
// Opening an issue runs nothing; the manager row opens as an idle chat at
// once; the manager shapes the steps only on ▶ run of an unshaped item, on
// words in its chat, or on `shape steps`; and the run/pause button sends the
// intent it shows.

// shapeLab is the fake floor with a Shape door that counts its asks by item
// and answers line and err.
type shapeLab struct {
	mu    sync.Mutex
	asked map[int]int
	line  string
	err   error
}

func (s *shapeLab) hang(a *app) {
	seam := a.factory
	seam.Shape = func(_ context.Context, id int) (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.asked == nil {
			s.asked = map[int]int{}
		}
		s.asked[id]++
		return s.line, s.err
	}
	a.factory = seam
}

func (s *shapeLab) count(id int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[id]
}

// OPENING AN ISSUE RUNS NOTHING: no shaping turn, nothing thinking.
func TestFactoryOpeningAnIssueRunsNothing(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	lab := &shapeLab{line: "manager set review: read it for security"}
	lab.hang(a)
	factoryOn(t, a, 8)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatal("enter opened no page")
	}
	if lab.count(8) != 0 || len(a.fp.shaping) != 0 {
		t.Fatalf("opening the page asked the Shape door %d times (shaping %v)", lab.count(8), a.fp.shaping)
	}
	for _, c := range f.said() {
		if strings.HasPrefix(c, "Launch") {
			t.Fatalf("opening the page launched: %v", c)
		}
	}
}

// `shape steps` IS AN ACTION LIKE THE OTHERS: a row under the facets, on the
// `?` sheet, its key `p`, a press on its row and a hover on it, and each asks
// the Shape door once and says its line. Nothing is launched.
func TestFactoryShapeStepsIsAKeyARowAndAPress(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	a.height = 60
	lab := &shapeLab{line: "manager set review: read it for security"}
	lab.hang(a)
	factoryVerbsOpen(t, a, 8)
	it, _ := a.factoryCursorItem()
	at := -1
	for i, r := range a.factoryItemRows(it) {
		if r.kind == factoryPageAction && r.verb.word == wordShapeSteps {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no %q row under the facets: %q", wordShapeSteps, factoryFacetsOf(a))
	}
	sheet := false
	for _, g := range a.factorySheet() {
		for _, r := range g.rows {
			if r.key == keyShapeSteps && r.word == wordShapeSteps {
				sheet = true
			}
		}
	}
	if !sheet {
		t.Fatal("the `?` sheet does not name `p shape steps`")
	}
	drive(t, a, key(keyShapeSteps))
	if lab.count(8) != 1 || a.pageMsg != lab.line {
		t.Fatalf("`p` asked the door %d times and said %q", lab.count(8), a.pageMsg)
	}
	frame(a)
	y := factoryLineOf(t, a, at)
	drive(t, a, tea.MouseMotionMsg{X: 3, Y: y})
	if a.fp.hot.kind != factoryHotRow || a.fp.hot.row != at {
		t.Fatalf("the pointer on %q is %+v", wordShapeSteps, a.fp.hot)
	}
	drive(t, a, tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
	if lab.count(8) != 2 {
		t.Fatalf("a press on %q asked the door %d times in all", wordShapeSteps, lab.count(8))
	}
	for _, c := range f.said() {
		if strings.HasPrefix(c, "Launch") {
			t.Fatalf("`shape steps` launched: %v", c)
		}
	}
	// A TURN THAT FAILED SAYS WHY, never nothing.
	lab.line, lab.err = "the manager did not answer · the recipe stands", context.DeadlineExceeded
	drive(t, a, key(keyShapeSteps))
	if a.pageMsg != lab.line {
		t.Fatalf("a failed turn said %q", a.pageMsg)
	}
}

// journeyHostLab is item 8 with a window that can open conversations and a
// Talk door that answers chat, counting its asks.
func journeyHostLab(t *testing.T) (*app, *factoryFake, *fakeAgent, string) {
	t.Helper()
	chat := factoryStageChat(t)
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	a.height = 60
	seam := a.factory
	seam.Talk = func(_ context.Context, id int) (string, error) { return chat, f.rec("Talk", id) }
	a.factory = seam
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	a.open = func(where, file string) (Conversation, error) {
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	}
	factoryVerbsOpen(t, a, 8)
	f.said()
	return a, f, agent, chat
}

// THE MANAGER ROW OPENS AS A CHAT AT ONCE: resting the cursor on it asks the
// Talk door once (which writes the issue into a new conversation and asks no
// model anything), and the conversation is in the center, idle. Walking off
// and back asks nothing more.
func TestFactoryManagerRowOpensAnIdleChatAtOnce(t *testing.T) {
	a, f, agent, _ := journeyHostLab(t)
	factoryWalkToKind(t, a, factoryPageManager)
	if !a.factoryHosting() {
		t.Fatalf("the manager row did not bring its chat into the center (host %+v)", a.fp.host)
	}
	if got := f.said(); len(got) != 1 || got[0] != "Talk(8)" {
		t.Fatalf("the manager row asked %v", got)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("opening the manager's chat sent the model %q", agent.sent)
	}
	if w := a.factoryHostWaitWords(); w != "" {
		t.Fatalf("the center still says %q", w)
	}
	drive(t, a, key("up"), key("down"))
	if got := f.said(); len(got) != 0 {
		t.Fatalf("walking back to the manager asked %v", got)
	}
}

// ▶ RUN ON AN ITEM NEVER SHAPED RUNS FROM THE MANAGER'S CHAT: the cursor goes
// to the manager row, the chat is made and brought in front, and only then
// is the item launched, so the runner's shaping turn streams where the person
// is looking.
func TestFactoryRunOnAnUnshapedItemOpensTheManagersChatFirst(t *testing.T) {
	a, f, _, _ := journeyHostLab(t)
	a.factoryStageSelect(0)
	drive(t, a, factoryKeyPress(keyControl))
	got := f.said()
	talk, launch := -1, -1
	for i, c := range got {
		switch c {
		case "Talk(8)":
			talk = i
		case "Launch(8)":
			launch = i
		}
	}
	if talk < 0 || launch < 0 || talk > launch {
		t.Fatalf("▶ run asked %v, want Talk(8) and then Launch(8)", got)
	}
	it, _ := a.factoryCursorItem()
	if r, ok := a.factoryPageRowAt(it); !ok || r.kind != factoryPageManager || !a.factoryHosting() {
		t.Fatalf("▶ run left the cursor on %+v, hosting %v", r, a.factoryHosting())
	}
}

// AN ITEM THE MANAGER ALREADY SHAPED RUNS AT ONCE, where the cursor stands.
func TestFactoryRunOnAShapedItemRunsAtOnce(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 8, func(it *factory.Item) {
		it.Adapted = append(it.Adapted, factory.ByManager+" kept the recipe")
	})
	a := factoryVerbsLab(t, f, 150)
	factoryVerbsOpen(t, a, 8)
	stage := a.fp.stage
	f.said()
	drive(t, a, factoryKeyPress(keyControl))
	if got := f.said(); len(got) == 0 || got[0] != "Launch(8)" || a.fp.stage != stage {
		t.Fatalf("▶ run on a shaped item asked %v and moved the cursor to %d", got, a.fp.stage)
	}
}

// THE BUTTON SENDS THE INTENT IT SHOWS, through the Hold door: pause while it
// shows pause, pressed twice, holds twice and never toggles back; run on a
// paused item goes on.
func TestFactoryTheControlSendsItsIntent(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	seam := a.factory
	seam.Hold = func(id int, on bool) error { return f.rec("Hold", id, on) }
	a.factory = seam
	factoryVerbsOpen(t, a, 2)
	f.said()
	drive(t, a, factoryKeyPress(keyControl), factoryKeyPress(keyControl))
	var holds []string
	for _, c := range f.said() {
		if strings.HasPrefix(c, "Hold") || strings.HasPrefix(c, "Pause") {
			holds = append(holds, c)
		}
	}
	if strings.Join(holds, " ") != "Hold(2,true) Hold(2,true)" {
		t.Fatalf("pause pressed twice asked %v", holds)
	}
	// A PAUSED RUN SHOWS RUN, AND RUN GOES ON.
	g := &factoryFake{}
	factoryShapeItem(g, 3, func(it *factory.Item) {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}}, Paused: true}
	})
	b := factoryVerbsLab(t, g, 150)
	seam = b.factory
	seam.Hold = func(id int, on bool) error { return g.rec("Hold", id, on) }
	b.factory = seam
	factoryVerbsOpen(t, b, 3)
	g.said()
	drive(t, b, factoryKeyPress(keyControl))
	holds = nil
	for _, c := range g.said() {
		if strings.HasPrefix(c, "Hold") || strings.HasPrefix(c, "Pause") {
			holds = append(holds, c)
		}
	}
	if strings.Join(holds, " ") != "Hold(3,false)" {
		t.Fatalf("run on a paused item asked %v", holds)
	}
}

// factoryWalkToKind walks the cursor onto the first row of kind, one key at a
// time as a person does, so every sync runs.
func factoryWalkToKind(t *testing.T, a *app, kind factoryPageKind) {
	t.Helper()
	it, _ := a.factoryCursorItem()
	for i, r := range a.factoryItemRows(it) {
		if r.kind == kind {
			for a.fp.stage > i {
				drive(t, a, key("up"))
			}
			for a.fp.stage < i {
				drive(t, a, key("down"))
			}
			return
		}
	}
	t.Fatalf("no row of kind %d", kind)
}
