package tui3

// THE TAB BAR KEEPS WORKING WHATEVER IS OPEN UNDER IT. These tests drive the
// surface's own Update loop and read what the FRAME shows after each gesture:
// how many tabs are drawn selected, whether Home is on the bar, and whether a
// page is still covering whatever the gesture chose.
//
// An ordinary task's room is the contrast: it is drawn under the
// conversation's own strip, with that conversation's tab the one selected tab
// and Home beside it, and `esc`, a press on the conversation's tab and a press
// on Home all leave it. The rooms the belt switch (CODEAF_TASK_BELT) opens — the
// work tab's, and a run's task opened from the side list — used to be pages
// drawn over the conversation, and are held to the same laws here. A
// program's task is a room of its own and has its own file
// (programtab_test.go).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// tabShot is one frame as a person reads its top: whether the conversation's
// strip was drawn on THIS frame, which of its tabs are drawn selected, whether
// Home is on it, and the whole frame's text.
type tabShot struct {
	drawn    bool
	selected []string
	home     bool
	hits     []tabHit
	text     string
}

// shootTabs draws one frame and reads the strip the frame itself laid out. The
// strip's hit map is cleared first, so a frame that drew no strip reads as
// none rather than as the last strip some earlier frame drew — and it is put
// back as it was when no strip was drawn, because the surface itself never
// clears it and a press is answered against whatever it holds.
func shootTabs(a *app) tabShot {
	held := a.chatTabHits
	a.chatTabHits = nil
	f, _, _ := a.frame()
	shot := tabShot{text: plain(f), hits: append([]tabHit(nil), a.chatTabHits...)}
	shot.drawn = len(shot.hits) > 0
	if !shot.drawn {
		a.chatTabHits = held
	}
	for _, hit := range shot.hits {
		switch hit.kind {
		case tabHere:
			shot.selected = append(shot.selected, hit.tab.word)
		case tabHome:
			shot.home = true
		}
	}
	return shot
}

// tabLab is a window in one conversation, "the run", with the strip as a
// person last saw it drawn.
type tabLab struct {
	a    *app
	last []tabHit
}

func (l *tabLab) shoot() tabShot {
	shot := shootTabs(l.a)
	if shot.drawn {
		l.last = shot.hits
	}
	return shot
}

// press clicks the strip where a person sees the piece `want` picks out: on the
// strip this frame drew, or, when the frame drew none, where the last drawn
// strip had it — which is where a person who just watched it vanish aims.
func (l *tabLab) press(t *testing.T, want func(tabHit) bool) {
	t.Helper()
	for _, hit := range l.last {
		if want(hit) {
			x := hit.span.from + (hit.span.to-hit.span.from)/2
			drive(t, l.a, tea.MouseClickMsg{X: x, Y: placeTabRow, Button: tea.MouseLeft})
			drive(t, l.a, tea.MouseReleaseMsg{X: x, Y: placeTabRow, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no such piece on the strip: %+v", l.last)
}

// conversationTab is the conversation's own tab — selected while nothing is
// drawn over it, and a door back to it while something is. Every fixture here
// names the conversation "the run".
func conversationTab(hit tabHit) bool {
	return !hit.tab.work && (hit.kind == tabHere || hit.kind == tabOther) && hit.tab.word == "the run"
}
func homeTab(hit tabHit) bool      { return hit.kind == tabHome }
func workTabPiece(hit tabHit) bool { return hit.tab.work && hit.kind != tabClose }

// escOut is `esc` the way a person leaves a room: once, and once more when the
// first one only handed the keyboard back from the side list, which is what an
// ordinary task's room opened by `enter` on its row asks for too.
func escOut(t *testing.T, a *app) {
	t.Helper()
	held := a.railHold
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if held {
		drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	}
}

// tabWaysOut are the three gestures that leave any page inside a conversation.
var tabWaysOut = []struct {
	name  string
	leave func(t *testing.T, l *tabLab)
	home  bool
}{
	{"esc", func(t *testing.T, l *tabLab) { escOut(t, l.a) }, false},
	{"a press on the conversation's tab", func(t *testing.T, l *tabLab) { l.press(t, conversationTab) }, false},
	{"a press on Home", func(t *testing.T, l *tabLab) { l.press(t, homeTab) }, true},
}

// checkLeft asserts what the frame shows after a way out: Home drawn as Home,
// or the conversation under its own strip — and the page's words gone.
func checkLeft(t *testing.T, l *tabLab, way string, home bool, said string) {
	t.Helper()
	shot := l.shoot()
	if strings.Contains(shot.text, said) {
		t.Errorf("SYMPTOM: the page is still drawn after %s (page=%v room=%v)", way, l.a.page, l.a.roomOpen())
	}
	if home {
		if !l.a.at(pageHome) {
			t.Errorf("the press on Home did not open Home (page=%v)", l.a.page)
		}
		if shot.drawn && !shot.home {
			t.Errorf("SYMPTOM: the conversation's strip is still drawn and Home has left it:\n%s", firstRows(shot.text, 4))
		}
		if l.a.tabRow != placeTabRow {
			t.Errorf("SYMPTOM: Home is not what is drawn; the frame drew no place bar:\n%s", firstRows(shot.text, 4))
		}
		return
	}
	if l.a.pageShowing() {
		t.Errorf("%s landed on place %v, want the conversation", way, l.a.page)
	}
	if !shot.drawn || len(shot.selected) != 1 || !shot.home {
		t.Errorf("SYMPTOM: after %s the strip is drawn=%v with selected=%q and home=%v, want the conversation's own strip:\n%s",
			way, shot.drawn, shot.selected, shot.home, firstRows(shot.text, 4))
	}
}

func firstRows(text string, n int) string {
	rows := strings.Split(text, "\n")
	if len(rows) > n {
		rows = rows[:n]
	}
	return strings.Join(rows, "\n")
}

// AN ORDINARY TASK IS THE CONTRAST, and it holds: its room is drawn under the
// conversation's strip with one selected tab and Home, and `esc`, the tab press
// and Home all leave it.
func TestAnOrdinaryTasksRoomKeepsTheConversationsTab(t *testing.T) {
	for _, way := range tabWaysOut {
		t.Run(way.name, func(t *testing.T) {
			a, _ := railTaskPageApp(t, false)
			a.resume = func(string) (Agent, error) { return nil, nil }
			a.width, a.height = 160, 40
			l := &tabLab{a: a}
			l.shoot()
			clickRail(t, a, 0)
			if !a.roomOpen() {
				t.Fatal("the ordinary task did not open its room")
			}
			if shot := l.shoot(); !shot.drawn || len(shot.selected) != 1 || !shot.home {
				t.Fatalf("an ordinary room's strip: drawn=%v selected=%q home=%v", shot.drawn, shot.selected, shot.home)
			}
			way.leave(t, l)
			if !way.home && a.roomOpen() {
				t.Fatalf("%s did not leave the room", way.name)
			}
			checkLeft(t, l, way.name, way.home, "esc/← main")
		})
	}
}

// ── THE ROOMS THE BELT SWITCH OPENS ─────────────────────────────────────────

// beltTabLab is [workTabFixture] under the strip a person sees.
func beltTabLab(t *testing.T) *tabLab {
	t.Helper()
	a, _ := workTabFixture(t)
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.width, a.height = 160, 40
	l := &tabLab{a: a}
	l.shoot()
	l.shoot()
	return l
}

// THE WORK TAB IS THE ONE SELECTED TAB WHILE IT IS UP, and the strip leaves it:
// the conversation's own tab goes back to the conversation and Home goes Home.
// It drew both tabs selected, a press on the conversation's tab did nothing,
// and Home opened under the work tab, which then drew the strip without Home.
func TestTheWorkTabIsTheOneSelectedTabAndTheStripLeavesIt(t *testing.T) {
	l := beltTabLab(t)
	l.press(t, workTabPiece)
	if l.a.roomPlan() == nil {
		t.Fatal("the work tab did not open")
	}
	if shot := l.shoot(); len(shot.selected) != 1 || !shot.home {
		t.Errorf("SYMPTOM: with the work tab up the strip draws selected=%q home=%v, want one selected tab and Home", shot.selected, shot.home)
	}
	for _, way := range tabWaysOut {
		t.Run(way.name, func(t *testing.T) {
			l := beltTabLab(t)
			l.press(t, workTabPiece)
			l.shoot()
			way.leave(t, l)
			checkLeft(t, l, way.name, way.home, taskPlanNoteWord)
		})
	}
}

// LEAVING IS NEVER MODAL (input.go's first rung), and the work tab was read
// above that law: ctrl+c on it was handed to the page, which took nothing.
func TestCtrlCIsNeverTakenByTheWorkTab(t *testing.T) {
	l := beltTabLab(t)
	l.press(t, workTabPiece)
	if l.a.roomPlan() == nil {
		t.Fatal("the work tab did not open")
	}
	control := beltTabLab(t)
	want := control.a.key(key("ctrl+c")) != nil
	if got := l.a.key(key("ctrl+c")) != nil; got != want {
		t.Errorf("SYMPTOM: ctrl+c on the work tab answered a command=%v, on the conversation %v", got, want)
	}
}

// A PRESS NEVER REACHES WHAT A TASK'S PAGE COVERS. A run's part opened from the
// rail took the whole frame and drew no strip, but the pointer was still
// answered against the strip the conversation last drew: a press where that
// strip's ✕ had been closed the conversation's tab and moved the window to
// Home, under a page that went on covering both.
func TestAPressNeverReachesTheStripATaskPageCovers(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.width, a.height = 160, 40
	l := &tabLab{a: a}
	l.shoot()
	clickRail(t, a, 0)
	if a.roomPlan() == nil {
		t.Fatal("the run's row did not open its room")
	}
	if shot := l.shoot(); shot.drawn {
		t.Skip("the page draws the strip, so a press on it is a press on something drawn")
	}
	l.press(t, func(hit tabHit) bool { return hit.kind == tabClose && !hit.tab.work })
	if len(a.tabShut) != 0 || a.at(pageHome) || a.closingTab() {
		t.Errorf("SYMPTOM: a press on a strip nobody can see closed tabs %v, moved to place %v, raised the close card %v",
			a.tabShut, a.page, a.closingTab())
	}
}
