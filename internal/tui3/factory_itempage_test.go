package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ITEM PAGE: TOP BAR, LEFT COLUMN, THE REAL CHAT IN THE CENTER ────────
//
// The owner's layout of 2026-10-09 (factory_bar.go, factory_item.go,
// factory_host.go), and his interaction law: every clickable thing has a
// hover through the ground ladder, one at a time; every row and button acts on
// a click exactly as its key does; the keys move the cursor and the pointer
// the hover, and the two never fight; a letter typed into the focused chat
// box never fires a page key.

// factoryVerbsLab is the fake seam with the doors the fixture's fake leaves
// out (the item's conversation, its forge page and a fresh read), so every
// row of the column is there, at width, in the plain palette.
func factoryVerbsLab(t *testing.T, f *factoryFake, width int) *app {
	t.Helper()
	a := factoryVerbLab(t, f)
	s := f.seam()
	s.Talk = func(context.Context, int) (string, error) { return "", f.rec("Talk") }
	s.Open = func(id int) (string, error) { return "", f.rec("Open", id) }
	s.Refresh = func(_ context.Context, id int) error { return f.rec("Refresh", id) }
	a.factory = s
	a.pal = newPalette(tokens.NoColor, false)
	a.width = width
	return a
}

// factoryVerbsOpen opens item id's page and draws one frame, so the column's
// rows and their places are the frame's.
func factoryVerbsOpen(t *testing.T, a *app, id int) {
	t.Helper()
	factoryOn(t, a, id)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatalf("enter on item %d opened no page", id)
	}
	frame(a)
}

// factoryLineOf is the screen row the last draw put item-page row at on the
// left column, and fails when it drew none.
func factoryLineOf(t *testing.T, a *app, at int) int {
	t.Helper()
	g := a.fp.geo
	for i, r := range g.lines {
		if r == at {
			return g.leftY + i
		}
	}
	t.Fatalf("the left column drew no line for row %d (lines %v)", at, g.lines)
	return -1
}

// THE LEFT COLUMN IS issue, manager, steps (each step nested), log, settings,
// and under a blank line the item's own actions, each only where its door is.
func TestFactoryItemColumnIsTheOwnersOrder(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	factoryVerbsOpen(t, a, 2)
	it, _ := a.factoryCursorItem()
	want := []string{wordFacetIssue, wordFacetManager, wordFacetSteps}
	for _, st := range factoryStages(a.fp.snap, it) {
		want = append(want, "  "+st.Name)
	}
	want = append(want, wordFacetLog, wordFacetSettings, wordOpenGitHub, wordRefresh)
	if got := factoryFacetsOf(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the column is\n%q\nwant\n%q", got, want)
	}
	lines := a.factoryColumnLines(a.factoryItemRows(it))
	blank := -1
	for i, at := range lines {
		if at < 0 {
			blank = i
		}
	}
	if blank < 0 || a.factoryItemRows(it)[lines[blank+1]].kind != factoryPageAction {
		t.Fatalf("no blank line stands before the actions: %v", lines)
	}
	for _, word := range []string{"do", "also", wordChat, wordSelect} {
		for _, row := range factoryFrameLines(a) {
			left, _, _ := factorySplitAt(row)
			if strings.TrimSpace(left) == word {
				t.Fatalf("the retired rail's %q is still drawn:\n%s", word, strings.Join(factoryFrameLines(a), "\n"))
			}
		}
	}
	// A NEW ITEM CAN BE DISMISSED, AND SAYS SO UNDER THE FACETS.
	drive(t, a, key("esc"))
	factoryVerbsOpen(t, a, 4)
	got := factoryFacetsOf(a)
	if got[len(got)-1] != wordDismiss {
		t.Fatalf("a new item's column does not end on dismiss: %q", got)
	}
}

// A STEP ROW CARRIES ITS LOOP IN ONE LINE: `↻ 1/2` once it runs, `↻ 2`
// before, what it loops until where that fits, and `+` where the manager
// added it.
func TestFactoryItemStepRowsCarryTheirLoop(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.Stages = append([]factory.Stage(nil), it.Stages...)
		last := len(it.Stages) - 1
		it.Stages = append(append(append([]factory.Stage(nil), it.Stages[:last]...), factory.Stage{Name: "docs", Ask: "write it down", On: true, By: factory.ByManager}), it.Stages[last])
	})
	a := factoryVerbsLab(t, f, 150)
	factoryVerbsOpen(t, a, 2)
	loop := a.icon(tokens.GLoop)
	var review, test, docs string
	for _, row := range factoryFrameLines(a) {
		left, _, _ := factorySplitAt(row)
		switch f := strings.Fields(left); {
		case len(f) > 1 && f[1] == "review":
			review = left
		case len(f) > 1 && f[1] == "test":
			test = left
		case len(f) > 1 && f[1] == "docs":
			docs = left
		}
	}
	if !strings.Contains(review, loop+" 1/2") || !strings.Contains(review, wordUntil+" clean") {
		t.Fatalf("the running review does not say its loop: %q", review)
	}
	if !strings.Contains(test, loop) {
		t.Fatalf("test, of two rounds, draws no loop: %q", test)
	}
	if !strings.Contains(docs, a.factoryAddedMark()) {
		t.Fatalf("the step the manager added is not marked: %q", docs)
	}
}

// factoryHeldItem is item 1 held at an approve step: a gate step between plan
// and write, waiting for the person.
func factoryHeldItem(f *factoryFake) {
	factoryShapeItem(f, 1, func(it *factory.Item) {
		stages := append([]factory.Stage(nil), it.Stages[:1]...)
		stages = append(stages, factory.Stage{Name: "approve", Kind: factory.StageGate, On: true})
		it.Stages = append(stages, it.Stages[1:]...)
		s := *it.Stream
		phases := []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}}
		for _, ph := range s.Phases[1:] {
			ph.State = factory.PhasePending
			phases = append(phases, ph)
		}
		s.Phases = phases
		it.Stream = &s
		it.Question = ""
		it.QKind = ""
	})
}

// AN APPROVE STEP HOLDING THE RUN says `waiting for you` on its row, the
// bar's control says `continue`, and the control (its key and a press on it)
// answers yes through the Answer door.
func TestFactoryItemApproveHoldSaysContinue(t *testing.T) {
	f := &factoryFake{}
	factoryHeldItem(f)
	a := factoryVerbsLab(t, f, 150)
	factoryVerbsOpen(t, a, 1)
	lines := factoryFrameLines(a)
	all := strings.Join(lines, "\n")
	held := false
	for _, row := range lines {
		left, _, _ := factorySplitAt(row)
		if strings.Contains(left, "approve") && strings.Contains(left, wordWaitingForYou) {
			held = true
		}
	}
	if !held {
		t.Fatalf("the approve row does not say it waits for you:\n%s", all)
	}
	bar := lines[a.fp.geo.barY]
	if !strings.Contains(bar, wordContinue) {
		t.Fatalf("the bar's control is not continue: %q", bar)
	}
	f.said()
	drive(t, a, factoryKeyPress(keyControl))
	if got := f.said(); len(got) != 1 || got[0] != "Answer(1,true,)" {
		t.Fatalf("space on the hold asked %v", got)
	}
	frame(a)
	drive(t, a, tea.MouseClickMsg{X: a.fp.geo.ctlX0, Y: a.fp.geo.barY, Button: tea.MouseLeft})
	if got := f.said(); len(got) != 1 || got[0] != "Answer(1,true,)" {
		t.Fatalf("a press on continue asked %v", got)
	}
}

// THE CONTROL SAYS WHAT A PRESS DOES WHERE THE RUN STANDS: run on a new item,
// pause on a moving run, run again on a paused one, and nothing on a landed
// item; its key and a press on it ask the same door.
func TestFactoryItemControlByState(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 3, func(it *factory.Item) {
		it.State = factory.StateRunning
		s := factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}}, Paused: true}
		it.Stream = &s
	})
	a := factoryVerbsLab(t, f, 150)
	for _, c := range []struct {
		id   int
		want factoryControl
		door string
	}{
		{2, factoryControlPause, "Pause(2)"},
		{3, factoryControlResume, "Pause(3)"},
		{9, factoryControlNone, ""},
	} {
		factoryVerbsOpen(t, a, c.id)
		it, _ := a.factoryCursorItem()
		if got := a.factoryControlOf(it); got != c.want {
			t.Fatalf("item %d's control is %d, want %d", c.id, got, c.want)
		}
		bar := factoryFrameLines(a)[a.fp.geo.barY]
		if c.want == factoryControlNone {
			if a.fp.geo.ctlX0 >= 0 {
				t.Fatalf("a landed item draws a control: %q", bar)
			}
			drive(t, a, key("esc"))
			continue
		}
		if !strings.Contains(bar, a.factoryControlLabel(c.want)) {
			t.Fatalf("item %d's bar does not draw %q: %q", c.id, a.factoryControlLabel(c.want), bar)
		}
		f.said()
		drive(t, a, tea.MouseClickMsg{X: a.fp.geo.ctlX0 + 1, Y: a.fp.geo.barY, Button: tea.MouseLeft})
		if got := f.said(); len(got) != 1 || got[0] != c.door {
			t.Fatalf("a press on item %d's control asked %v, want %s", c.id, got, c.door)
		}
		drive(t, a, factoryKeyPress(keyControl))
		if got := f.said(); len(got) != 1 || got[0] != c.door {
			t.Fatalf("space on item %d asked %v, want %s", c.id, got, c.door)
		}
		drive(t, a, key("esc"))
	}
}

// EVERY ROW OF THE LEFT COLUMN IS A STOP, at every width the audit draws: the
// arrows walk all of them in order and skip nothing, and a press on each
// lands the cursor on it (an action acts on the press, as its key does).
func TestFactoryItemEveryRowIsWalkedAndPressed(t *testing.T) {
	for _, width := range factoryAlignWidths {
		f := &factoryFake{}
		a := factoryVerbsLab(t, f, width)
		a.height = 60
		factoryVerbsOpen(t, a, 2)
		it, _ := a.factoryCursorItem()
		rows := a.factoryItemRows(it)
		a.factoryStageSelect(0)
		for want := 1; want < len(rows); want++ {
			drive(t, a, key("down"))
			if a.fp.stage != want {
				t.Fatalf("at %d ↓ landed on row %d, want %d", width, a.fp.stage, want)
			}
		}
		drive(t, a, key("down"))
		if a.fp.stage != len(rows)-1 {
			t.Fatalf("at %d ↓ off the last row moved to %d", width, a.fp.stage)
		}
		for want := len(rows) - 2; want >= 0; want-- {
			drive(t, a, key("up"))
			if a.fp.stage != want {
				t.Fatalf("at %d ↑ landed on row %d, want %d", width, a.fp.stage, want)
			}
		}
		for at, r := range rows {
			frame(a)
			y := factoryLineOf(t, a, at)
			f.said()
			drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
			if a.fp.stage != at {
				t.Fatalf("at %d a press on row %d (%s) left the cursor on %d", width, at, factoryPageRowWord(r), a.fp.stage)
			}
			if r.kind == factoryPageAction {
				switch r.verb.word {
				case wordRefresh:
					if got := f.said(); len(got) != 1 || got[0] != "Refresh(2)" {
						t.Fatalf("at %d a press on refresh asked %v", width, got)
					}
				case wordOpenGitHub:
					if got := f.said(); len(got) != 1 || got[0] != "Open(2)" {
						t.Fatalf("at %d a press on open on github asked %v", width, got)
					}
				}
			}
			a.fp.act = factoryActs{}
		}
	}
}

// EVERY CLICKABLE THING ON THE PAGE IS WHERE THE DRAW SAYS, at every width
// the audit draws: the control, each crumb and each row of the column answer
// a hit test at their own cells, and only there.
func TestFactoryItemHitTestsAtEveryWidth(t *testing.T) {
	for _, width := range factoryAlignWidths {
		a := factoryVerbsLab(t, &factoryFake{}, width)
		a.height = 60
		factoryVerbsOpen(t, a, 2)
		lines := factoryFrameLines(a)
		g := a.fp.geo
		bar := []rune(lines[g.barY])
		if g.ctlX0 < 0 || !strings.Contains(string(bar[g.ctlX0:g.ctlX1]), wordPause) {
			t.Fatalf("at %d the control's cells do not hold pause: %q", width, string(bar))
		}
		if hot := a.factoryItemHotAt(g.ctlX0, g.barY); hot.kind != factoryHotControl {
			t.Fatalf("at %d the control's first cell is not the control", width)
		}
		if hot := a.factoryItemHotAt(g.ctlX1, g.barY); hot.kind == factoryHotControl {
			t.Fatalf("at %d the cell past the control is the control", width)
		}
		for _, c := range a.fp.crumbHits {
			if hot := a.factoryItemHotAt(c.x0, g.barY); hot.kind != factoryHotCrumb || hot.crumb != c.crumb {
				t.Fatalf("at %d crumb %d is not hit at its own cell", width, c.crumb)
			}
		}
		if string(bar[a.fp.crumbHits[0].x0:a.fp.crumbHits[0].x1]) != wordFloorCrumb {
			t.Fatalf("at %d the first crumb's cells hold %q", width, string(bar[a.fp.crumbHits[0].x0:a.fp.crumbHits[0].x1]))
		}
		it, _ := a.factoryCursorItem()
		for at, r := range a.factoryItemRows(it) {
			y := factoryLineOf(t, a, at)
			if hot := a.factoryItemHotAt(1, y); hot.kind != factoryHotRow || hot.row != at {
				t.Fatalf("at %d row %d (%s) is not hit on its line", width, at, factoryPageRowWord(r))
			}
			if !strings.Contains(lines[y], factoryPageRowWord(r)) {
				t.Fatalf("at %d row %d's line does not say %q: %q", width, at, factoryPageRowWord(r), lines[y])
			}
			if hot := a.factoryItemHotAt(g.centerX+2, y); hot.kind == factoryHotRow {
				t.Fatalf("at %d the center beside row %d is the row", width, at)
			}
		}
		// A PRESS ON A CRUMB IS ITS ROAD: `Factory` puts the floor back.
		drive(t, a, tea.MouseClickMsg{X: a.fp.crumbHits[0].x0, Y: g.barY, Button: tea.MouseLeft})
		if a.fp.open {
			t.Fatalf("at %d a press on %s left the page open", width, wordFloorCrumb)
		}
	}
}

// ONE THING WEARS THE POINTER'S GROUND AT A TIME, the cursor's row keeps the
// selected ground, the pointer never moves the cursor, and the ground goes
// when the pointer leaves.
func TestFactoryItemHoverIsOneThingAndNeverTheCursor(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	a.pal = newPalette(tokens.TrueColor, false)
	factoryVerbsOpen(t, a, 2)
	ground := strings.SplitN(a.pal.cursor("§", 1), "§", 2)[0]
	selected := strings.SplitN(a.pal.selected("§", 1), "§", 2)[0]
	if ground == "" || selected == "" || ground == selected {
		t.Fatalf("the palette's pointer and selected grounds are not two: %q %q", ground, selected)
	}
	cursor := a.fp.stage
	it, _ := a.factoryCursorItem()
	other := 0
	if cursor == 0 {
		other = 1
	}
	frame(a)
	y := factoryLineOf(t, a, other)
	drive(t, a, tea.MouseMotionMsg{X: 3, Y: y})
	if a.fp.stage != cursor {
		t.Fatalf("the pointer moved the cursor from %d to %d", cursor, a.fp.stage)
	}
	lines := strings.Split(frame(a), "\n")
	count := 0
	for _, l := range lines {
		if strings.Contains(l, ground) {
			count++
		}
	}
	if !strings.Contains(lines[y], ground) || count != 1 {
		t.Fatalf("the hovered row wears the ground %v, and %d rows wear it", strings.Contains(lines[y], ground), count)
	}
	if !strings.Contains(lines[factoryLineOf(t, a, cursor)], selected) {
		t.Fatal("the cursor's row lost its selected ground under the hover")
	}
	// THE CONTROL TAKES THE GROUND, AND THE ROW GIVES IT UP.
	drive(t, a, tea.MouseMotionMsg{X: a.fp.geo.ctlX0 + 1, Y: a.fp.geo.barY})
	lines = strings.Split(frame(a), "\n")
	if strings.Contains(lines[y], ground) || !strings.Contains(lines[a.fp.geo.barY], ground) {
		t.Fatal("the hover did not move from the row to the control")
	}
	// A CRUMB TOO.
	c := a.fp.crumbHits[0]
	drive(t, a, tea.MouseMotionMsg{X: c.x0, Y: a.fp.geo.barY})
	if a.fp.hot.kind != factoryHotCrumb {
		t.Fatalf("the pointer on a crumb is %+v", a.fp.hot)
	}
	// AND OFF EVERYTHING, NOTHING WEARS IT.
	drive(t, a, tea.MouseMotionMsg{X: a.fp.geo.centerX + 10, Y: a.fp.geo.centerY + 5})
	for _, l := range strings.Split(frame(a), "\n") {
		if strings.Contains(l, ground) {
			t.Fatalf("a ground stayed after the pointer left: %q", plain(l))
		}
	}
	// A KEY LETS A HOVER GO.
	drive(t, a, tea.MouseMotionMsg{X: 3, Y: y})
	drive(t, a, key("down"))
	if a.fp.hot != (factoryItemHot{}) {
		t.Fatalf("a key kept the hover %+v", a.fp.hot)
	}
	_ = it
}

// THE WHEEL OVER A LONG LEFT COLUMN MOVES ITS WINDOW, and never the cursor.
func TestFactoryItemWheelScrollsTheColumn(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	a.height = 16
	factoryVerbsOpen(t, a, 2)
	frame(a)
	cursor := a.fp.stage
	first := a.fp.geo.first
	drive(t, a, tea.MouseWheelMsg{X: 3, Y: a.fp.geo.leftY + 1, Button: tea.MouseWheelDown})
	frame(a)
	if a.fp.geo.first == first || a.fp.stage != cursor {
		t.Fatalf("the wheel left the column at %d (was %d) and the cursor at %d (was %d)", a.fp.geo.first, first, a.fp.stage, cursor)
	}
}

// ── the hosted chat ─────────────────────────────────────────────────────────

// factoryHostLab is item 2 with its review step's chat on disk and a window
// that can open conversations, the page open on review and one frame drawn.
func factoryHostLab(t *testing.T) (*app, *factoryFake, *fakeAgent) {
	t.Helper()
	chat := factoryStageChat(t)
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		s := *it.Stream
		s.Phases = append([]factory.Phase(nil), s.Phases...)
		s.Phases[4].Chat = chat
		it.Stream = &s
	})
	a := factoryVerbsLab(t, f, 150)
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	a.open = func(where, file string) (Conversation, error) {
		return Conversation{Agent: agent, SessionFile: file, Workspace: where}, nil
	}
	factoryVerbsOpen(t, a, 2)
	factoryRowNamed(t, a, "review")
	drive(t, a, key("down"), key("up"))
	if !a.factoryHosting() {
		t.Fatalf("selecting review did not bring its chat into the center (host %+v)", a.fp.host)
	}
	f.said()
	return a, f, agent
}

// SELECTING A STEP WITH A CHAT PUTS THAT CHAT IN THE CENTER: the real chat
// frame beside the left column, under the bar, the chat laid out in the
// cells it is drawn in, and the page still standing.
func TestFactoryItemCenterHostsTheStepsChat(t *testing.T) {
	a, _, _ := factoryHostLab(t)
	if !a.at(pageFactory) || !a.fp.open {
		t.Fatal("hosting the chat left the page")
	}
	if w, _ := a.size(); w != a.width-factoryItemColW-factoryRuleW {
		t.Fatalf("the hosted chat is laid out at %d cells, want %d", w, a.width-factoryItemColW-factoryRuleW)
	}
	lines := factoryFrameLines(a)
	if !a.fp.geo.hosted {
		t.Fatal("the frame did not draw the hosted shape")
	}
	if !strings.Contains(lines[a.fp.geo.barY], wordPause) {
		t.Fatalf("the bar is not over the hosted chat: %q", lines[a.fp.geo.barY])
	}
	review := factoryLineOf(t, a, a.fp.stage)
	if left, _, _ := factorySplitAt(lines[review]); !strings.Contains(left, "review") {
		t.Fatalf("the left column does not stand beside the chat: %q", lines[review])
	}
	if a.caret {
		t.Fatal("the chat's caret is drawn while the keys walk the page")
	}
}

// TYPING GOES TO THE BOX ONLY WHEN IT IS FOCUSED: `→` (or `tab`, or a press
// on the chat) puts the keys in it, every letter then types and none fires a
// page key, the arrows edit the words, and `esc` gives the keys back.
func TestFactoryItemFocusedBoxNeverFiresAPageKey(t *testing.T) {
	a, f, _ := factoryHostLab(t)
	drive(t, a, key("right"))
	if !a.fp.box {
		t.Fatal("→ did not put the keys in the chat's box")
	}
	before := a.fp.stage
	factoryType(t, a, "draft a dismissal; run it")
	if got := f.said(); len(got) != 0 {
		t.Fatalf("typing in the box asked the doors %v", got)
	}
	if got := a.input.String(); got != "draft a dismissal; run it" {
		t.Fatalf("the box holds %q", got)
	}
	drive(t, a, key("up"), key("down"), key("left"))
	if a.fp.stage != before || !a.fp.box {
		t.Fatalf("the arrows in the box walked the page (row %d, was %d; box %v)", a.fp.stage, before, a.fp.box)
	}
	frame(a)
	if !a.caret {
		t.Fatal("the focused box draws no caret")
	}
	drive(t, a, key("esc"))
	if a.fp.box || !a.fp.open {
		t.Fatalf("esc in the box did not give the keys back (box %v, open %v)", a.fp.box, a.fp.open)
	}
	drive(t, a, key("down"))
	if a.fp.stage == before {
		t.Fatal("↓ after esc did not walk the column")
	}
	// A PRESS ON THE CHAT TAKES THE KEYS AGAIN, and one on the column gives
	// them back.
	drive(t, a, key("up"))
	frame(a)
	drive(t, a, tea.MouseClickMsg{X: a.fp.geo.centerX + 5, Y: a.height - 3, Button: tea.MouseLeft})
	if !a.fp.box {
		t.Fatal("a press on the chat did not put the keys in its box")
	}
	drive(t, a, tea.MouseClickMsg{X: 3, Y: factoryLineOf(t, a, 0), Button: tea.MouseLeft})
	if a.fp.box || a.fp.stage != 0 {
		t.Fatalf("a press on the column left the box %v and the cursor on %d", a.fp.box, a.fp.stage)
	}
}

// THE MANAGER'S BOX, BEFORE THE MANAGER HAS A CHAT, IS PRESSABLE AND SENDS IN
// PLACE through the Say door: the page stays.
func TestFactoryItemManagerBoxSendsThroughSay(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	var said []string
	s := a.factory
	s.Say = func(_ context.Context, id int, words string) error {
		said = append(said, itoa(id)+":"+words)
		return nil
	}
	a.factory = s
	factoryVerbsOpen(t, a, 8)
	factoryRowNamed(t, a, wordFacetManager)
	frame(a)
	if a.fp.geo.boxY < 0 {
		t.Fatal("the manager row's center draws no box")
	}
	drive(t, a, tea.MouseClickMsg{X: a.fp.geo.centerX + 4, Y: a.fp.geo.boxY, Button: tea.MouseLeft})
	if !a.factoryBoxFocused() {
		t.Fatal("a press on the manager's box did not put the keys in it")
	}
	factoryType(t, a, "dismiss nothing")
	drive(t, a, key("enter"))
	if len(said) != 1 || said[0] != "8:dismiss nothing" {
		t.Fatalf("the Say door was asked %v", said)
	}
	if got := f.said(); strings.Contains(strings.Join(got, ","), "Dismiss") {
		t.Fatalf("typing in the manager's box fired a page key: %v", got)
	}
	if !a.at(pageFactory) || !a.fp.open {
		t.Fatal("sending to the manager left the page")
	}
}
