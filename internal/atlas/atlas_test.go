package atlas

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// newTestModel is the model at a size, having drawn once so the boxes have
// their on-screen rectangles.
func newTestModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(Pairing, w, h)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.drawFrame()
	return m
}

// send runs one key through the model the way the program would.
func send(t *testing.T, m *Model, key tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	next, cmd := m.Update(key)
	if _, ok := next.(*Model); !ok {
		t.Fatalf("Update answered something other than the atlas model")
	}
	return cmd
}

// sendMouse runs one mouse event through the model the way the program would.
func sendMouse(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	next, _ := m.Update(msg)
	if _, ok := next.(*Model); !ok {
		t.Fatalf("Update answered something other than the atlas model")
	}
}

// plain strips the styling off a frame, for assertions about words.
func plain(s string) string {
	return strings.ReplaceAll(stripANSI(s), "\n", "\n")
}

// stripANSI removes escape sequences, which the styled frame is full of.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && !isFinalByte(s[i]) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isFinalByte(b byte) bool { return b >= 0x40 && b <= 0x7e }

// frameAt draws the model at a size and hands back the plain text and the
// styled frame.
func frameAt(t *testing.T, m *Model) (string, string) {
	t.Helper()
	styled := m.drawFrame()
	return stripANSI(styled), styled
}

// ── the data the map draws ───────────────────────────────────────────────────

// TestEveryBoxCarriesItsShortLine checks the roomy layout leaves room for the
// one-line subtitle: a box whose subtitle is longer than its label must be
// wide enough for both, or the subtitle is clipped in every frame.
func TestEveryBoxCarriesItsShortLine(t *testing.T) {
	for _, n := range Pairing.Nodes {
		w, _ := nodeSize(n, false)
		if len(n.Short) > w-4 {
			t.Errorf("node %s: the box is %d cells wide but the subtitle %q needs %d", n.ID, w, n.Short, len(n.Short)+4)
		}
	}
}

// ── dragging ─────────────────────────────────────────────────────────────────

// TestDraggingMovesTheBoxAndReroutesItsArrows is the one thing mouse cell
// motion is here for: press inside a box, move, release, and the box is where
// the mouse left it with every arrow that touches it redrawn to follow.
func TestDraggingMovesTheBoxAndReroutesItsArrows(t *testing.T) {
	m := newTestModel(t, 120, 35)
	const id = "pair"
	before, _ := m.nodeRect(id)

	// Where the arrows ran before the drag.
	var edge Edge
	for _, e := range Pairing.Edges {
		if e.ID == "pair-hosted" {
			edge = e
		}
	}
	oldRects := snapshotRects(m)
	oldPath := edgeCells(oldRects[edge.From], oldRects[edge.To], 0, 0)

	sendMouse(t, m, tea.MouseClickMsg{X: before.X + 2, Y: before.Y + 1, Button: tea.MouseLeft})
	sendMouse(t, m, tea.MouseMotionMsg{X: before.X + 22, Y: before.Y + 9, Button: tea.MouseLeft})
	sendMouse(t, m, tea.MouseReleaseMsg{X: before.X + 22, Y: before.Y + 9, Button: tea.MouseLeft})

	if m.detail != "" {
		t.Fatalf("a drag opened the detail pane of %q", m.detail)
	}
	// The rectangles come from the last drawn frame, so draw the one the
	// drag produced before reading where the box landed.
	frameAt(t, m)
	after, _ := m.nodeRect(id)
	if after.X != before.X+20 || after.Y != before.Y+8 {
		t.Fatalf("the box moved to (%d,%d); the drag was 20 cells right and 8 down", after.X, after.Y)
	}

	// And the arrow between the same two boxes runs somewhere else now. The
	// boxes on the map can overlap one another by design, so the arrow's own
	// path is the honest thing to compare, not the cells around the label.
	m.drawFrame()
	newRects := snapshotRects(m)
	newPath := edgeCells(newRects[edge.From], newRects[edge.To], 0, 0)
	if samePath(oldPath, newPath) {
		t.Fatal("the box moved but the arrow between it and the hosted relay still runs through the same cells")
	}
}

// TestADragStaysInsideTheMap: a drag towards the edge stops at the edge of
// the map rather than pushing the box off screen.
func TestADragStaysInsideTheMap(t *testing.T) {
	m := newTestModel(t, 120, 35)
	const id = "machineB"
	before, _ := m.nodeRect(id)
	sendMouse(t, m, tea.MouseClickMsg{X: before.X + 2, Y: before.Y + 1, Button: tea.MouseLeft})
	sendMouse(t, m, tea.MouseMotionMsg{X: 500, Y: 500, Button: tea.MouseLeft})
	sendMouse(t, m, tea.MouseReleaseMsg{X: 500, Y: 500, Button: tea.MouseLeft})
	after, _ := m.nodeRect(id)
	g := m.geometry()
	if after.X+after.W > g.mapW || after.Y+after.H > g.mapTop+g.mapH {
		t.Fatalf("the box was dragged off the map: %+v in a %d×%d map", after, g.mapW, g.mapH)
	}
}

func snapshotRects(m *Model) map[string]Rect {
	out := make(map[string]Rect, len(m.rects))
	for k, r := range m.rects {
		out[k] = r
	}
	return out
}

func samePath(a, b []pt) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── the detail pane ──────────────────────────────────────────────────────────

// TestTabThenEnterOpensTheDetailPane is the keyboard road to what a click
// does: tab puts the selection on the first box, enter opens what it does,
// its files and what it talks to.
func TestTabThenEnterOpensTheDetailPane(t *testing.T) {
	m := newTestModel(t, 120, 35)
	first := Pairing.Nodes[0]
	send(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.detail != first.ID {
		t.Fatalf("tab then enter opened %q; the first node is %q", m.detail, first.ID)
	}
	text, _ := frameAt(t, m)
	if !strings.Contains(text, " esc close ") {
		t.Fatal("the open detail pane never says how to close it")
	}
	if !strings.Contains(text, first.Summary[:20]) {
		t.Fatalf("the detail pane does not tell what the part does; it should name %q", first.Summary[:20])
	}
}

// TestClickingABoxOpensItsDetail: the mouse road to the same pane, a press
// and a release without movement in between.
func TestClickingABoxOpensItsDetail(t *testing.T) {
	m := newTestModel(t, 120, 35)
	first := Pairing.Nodes[0]
	r, _ := m.nodeRect(first.ID)
	sendMouse(t, m, tea.MouseClickMsg{X: r.X + 2, Y: r.Y + 1, Button: tea.MouseLeft})
	sendMouse(t, m, tea.MouseReleaseMsg{X: r.X + 2, Y: r.Y + 1, Button: tea.MouseLeft})
	if m.detail != first.ID {
		t.Fatalf("clicking the box opened %q, not %q", m.detail, first.ID)
	}
}

// ── the flows ────────────────────────────────────────────────────────────────

// TestSteppingAFlowHighlightsTheRightStep: 1 opens the pairing flow, the
// arrows walk it, and the step the map is telling is the one that draws.
func TestSteppingAFlowHighlightsTheRightStep(t *testing.T) {
	m := newTestModel(t, 120, 35)
	flow := Pairing.Flows[0]
	send(t, m, tea.KeyPressMsg{Code: '1', Text: "1"})
	if m.flow != 0 || m.step != 0 {
		t.Fatalf("pressing 1 opened flow %d at step %d", m.flow, m.step)
	}
	text, _ := frameAt(t, m)
	if !strings.Contains(text, "step 1/"+itoa(len(flow.Steps))) {
		t.Fatalf("the flow panel does not say where the story stands:\n%s", text)
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.step != 1 {
		t.Fatalf("the right arrow took the story to step %d", m.step)
	}
	text, _ = frameAt(t, m)
	if !strings.Contains(text, "step 2/"+itoa(len(flow.Steps))) {
		t.Fatalf("after stepping, the panel still tells step 1:\n%s", text)
	}
	if !strings.Contains(text, "2. "+flow.Steps[1].Message[:20]) {
		t.Fatalf("the active step's message is not drawn:\n%s", text)
	}
	// The active arrow is the one the step travels, drawn last over the rest.
	if _, inFlow := m.activeStep(); !inFlow {
		t.Fatal("no step is active after opening a flow")
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.step != 0 {
		t.Fatalf("the left arrow took the story to step %d", m.step)
	}
}

// TestTheLetterKeysWalkTheFlows: f cycles through the flows the way the
// number keys step through them one by one.
func TestTheLetterKeysWalkTheFlows(t *testing.T) {
	m := newTestModel(t, 120, 35)
	send(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.flow != 0 {
		t.Fatalf("f opened flow %d; the first flow was expected", m.flow)
	}
	send(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	send(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.flow != 2 {
		t.Fatalf("three presses of f landed on flow %d", m.flow)
	}
	send(t, m, tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.flow != 0 {
		t.Fatalf("f does not wrap: it landed on flow %d", m.flow)
	}
	send(t, m, tea.KeyPressMsg{Code: '0', Text: "0"})
	if m.flow != -1 {
		t.Fatal("0 did not go back to the overview")
	}
}

// TestPlayTogglesAndSteps: p starts the story and p stops it again; while it
// plays, each beat steps it forward.
func TestPlayTogglesAndSteps(t *testing.T) {
	m := newTestModel(t, 120, 35)
	send(t, m, tea.KeyPressMsg{Code: '1', Text: "1"})
	send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !m.playing {
		t.Fatal("p did not start the flow playing")
	}
	next, _ := m.Update(tickMsg{})
	if _, ok := next.(*Model); !ok {
		t.Fatal("a tick answered something other than the atlas model")
	}
	if m.step != 1 {
		t.Fatalf("one beat of play took the story to step %d", m.step)
	}
	send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.playing {
		t.Fatal("p did not pause the flow")
	}
}

// ── help, escape, quitting ───────────────────────────────────────────────────

// TestQuestionMarkShowsTheLegend: ? opens the help over the map, naming what
// each colour of box is.
func TestQuestionMarkShowsTheLegend(t *testing.T) {
	m := newTestModel(t, 120, 35)
	send(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	if !m.help {
		t.Fatal("? did not open the help")
	}
	text, _ := frameAt(t, m)
	if !strings.Contains(text, "Legend") {
		t.Fatalf("the help never draws its legend:\n%s", text)
	}
	if !strings.Contains(text, "codeaf (Go)") {
		t.Fatal("the legend does not name the kinds of box")
	}
}

// TestEscapeBacksOutOneLayer: help first, then the detail pane, then the
// flow, then the selection — the way the help pane promises.
func TestEscapeBacksOutOneLayer(t *testing.T) {
	m := newTestModel(t, 120, 35)
	send(t, m, tea.KeyPressMsg{Code: '1', Text: "1"})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	send(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help {
		t.Fatal("esc did not close the help")
	}
	if m.detail == "" {
		t.Fatal("esc closed the detail pane at the same time as the help")
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.detail != "" {
		t.Fatal("esc did not close the detail pane")
	}
	if m.flow < 0 {
		t.Fatal("esc closed the flow at the same time as the pane")
	}
	send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.flow >= 0 {
		t.Fatal("esc did not go back to the overview")
	}
}

// TestQQuits: q is the door out, and it answers a command that ends the
// program — the same door ctrl+c takes.
func TestQQuits(t *testing.T) {
	m := newTestModel(t, 120, 35)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		cmd := send(t, m, key)
		if cmd == nil {
			t.Fatalf("%v answered no command; q and ctrl+c must both quit", key)
		}
		if msg := cmd(); msg != tea.Quit() {
			t.Fatalf("%v answered %v; the program was not told to quit", key, msg)
		}
	}
}

// ── the frame itself ─────────────────────────────────────────────────────────

// TestTheFrameFitsEightyByTwentyFour: the smallest terminal the map promises
// to work at. Nothing panics, nothing draws wider than the screen, and every
// box still lands inside it.
func TestTheFrameFitsEightyByTwentyFour(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {200, 50}} {
		m := newTestModel(t, size[0], size[1])
		styled := m.drawFrame()
		rows := strings.Split(styled, "\n")
		if len(rows) > size[1] {
			t.Errorf("at %dx%d the frame is %d rows tall", size[0], size[1], len(rows))
		}
		for i, row := range rows {
			if w := lipgloss.Width(row); w > size[0] {
				t.Errorf("at %dx%d row %d is %d cells wide", size[0], size[1], i, w)
			}
		}
		plainText := stripANSI(styled)
		for _, n := range Pairing.Nodes {
			if !strings.Contains(plainText, n.Label) {
				t.Errorf("at %dx%d the box %q is not drawn at all", size[0], size[1], n.Label)
			}
			r := m.rects[n.ID]
			if r.X < 0 || r.X+r.W > size[0] || r.Y < 0 || r.Y+r.H > size[1] {
				t.Errorf("at %dx%d the box %q sits at %+v, outside the screen", size[0], size[1], n.ID, r)
			}
		}
	}
}

// TestTheOverviewDrawsItsTitle: the first thing on screen names itself.
func TestTheOverviewDrawsItsTitle(t *testing.T) {
	m := newTestModel(t, 120, 35)
	text, _ := frameAt(t, m)
	if !strings.Contains(text, Pairing.Title) {
		t.Fatalf("the overview does not draw its title %q:\n%s", Pairing.Title, text)
	}
	if !strings.Contains(text, "◀") && !strings.Contains(text, "▶") {
		t.Fatal("the overview draws no arrowheads")
	}
}
