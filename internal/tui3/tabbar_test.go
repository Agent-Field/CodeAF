package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The place navigation sits on the wordmark row, while a conversation's tab
// strip is drawn only in a chat. A task room still belongs to that chat.
func TestAnOrdinaryTasksRoomKeepsTheConversationsTab(t *testing.T) {
	a, _ := railTaskPageApp(t, false)
	a.width, a.height = 160, 40
	frame(a)
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the ordinary task did not open its room")
	}
	frame(a)
	if a.tabRow != navRow || a.navLit() != pageChats {
		t.Fatalf("the task room moved the place navigation: row %d, place %v", a.tabRow, a.navLit())
	}
	selected := 0
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("the task room drew %d selected conversation tabs", selected)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.roomOpen() {
		t.Fatal("esc did not leave the task room")
	}
}

func TestTheWorkTabIsTheOneSelectedTabAndTheStripLeavesIt(t *testing.T) {
	a, _ := workTabFixture(t)
	a.width, a.height = 160, 40
	frame(a)
	var work *tabHit
	for i := range a.chatTabHits {
		hit := &a.chatTabHits[i]
		if hit.tab.work && hit.kind != tabClose {
			work = hit
			break
		}
	}
	if work == nil {
		t.Fatal("the work tab was not drawn")
	}
	x := work.span.from + (work.span.to-work.span.from)/2
	drive(t, a, tea.MouseClickMsg{X: x, Y: tabStripRow, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: tabStripRow, Button: tea.MouseLeft})
	if a.roomPlan() == nil {
		t.Fatal("the work tab did not open")
	}
	frame(a)
	selected := 0
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHere {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("the work tab drew %d selected tabs", selected)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.roomPlan() != nil {
		t.Fatal("esc did not leave the work tab")
	}
}

// tabShot reads the conversation strip and the place navigation from one frame.
// Home lives on the wordmark row even while the task page is open.
type tabShot struct {
	drawn    bool
	selected []string
	home     bool
	hits     []tabHit
	text     string
}

type tabLab struct {
	a    *app
	last []tabHit
}

func (l *tabLab) shoot() tabShot {
	text := frame(l.a)
	shot := tabShot{text: plain(text), hits: append([]tabHit(nil), l.a.chatTabHits...)}
	shot.drawn = len(shot.hits) > 0
	for _, hit := range shot.hits {
		if hit.kind == tabHere {
			shot.selected = append(shot.selected, hit.tab.word)
		}
	}
	for _, span := range l.a.tabs {
		if span.id == pageHome {
			shot.home = true
		}
	}
	if shot.drawn {
		l.last = shot.hits
	}
	return shot
}

func (l *tabLab) press(t *testing.T, want func(tabHit) bool) {
	t.Helper()
	for _, hit := range l.last {
		if want(hit) {
			x := hit.span.from + (hit.span.to-hit.span.from)/2
			drive(t, l.a, tea.MouseClickMsg{X: x, Y: tabStripRow, Button: tea.MouseLeft})
			drive(t, l.a, tea.MouseReleaseMsg{X: x, Y: tabStripRow, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no matching conversation tab: %+v", l.last)
}

func (l *tabLab) pressHome(t *testing.T) {
	t.Helper()
	frame(l.a)
	for _, span := range l.a.tabs {
		if span.id == pageHome {
			x := span.from + (span.to-span.from)/2
			drive(t, l.a, tea.MouseClickMsg{X: x, Y: navRow, Button: tea.MouseLeft})
			drive(t, l.a, tea.MouseReleaseMsg{X: x, Y: navRow, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatal("Home was not drawn on the place navigation")
}

var tabWaysOut = []struct {
	name  string
	leave func(*testing.T, *tabLab)
	home  bool
}{
	{"esc", func(t *testing.T, l *tabLab) {
		held := l.a.railHold
		drive(t, l.a, tea.KeyPressMsg{Code: tea.KeyEscape})
		if held {
			drive(t, l.a, tea.KeyPressMsg{Code: tea.KeyEscape})
		}
	}, false},
	{"conversation tab", func(t *testing.T, l *tabLab) {
		l.press(t, func(hit tabHit) bool { return !hit.tab.work && (hit.kind == tabHere || hit.kind == tabOther) })
	}, false},
	{"Home", func(t *testing.T, l *tabLab) { l.pressHome(t) }, true},
}

func checkLeft(t *testing.T, l *tabLab, way string, home bool, said string) {
	t.Helper()
	shot := l.shoot()
	if strings.Contains(shot.text, said) {
		t.Errorf("the task page is still drawn after %s", way)
	}
	if home {
		if !l.a.at(pageHome) || shot.drawn {
			t.Errorf("Home did not open without a conversation strip after %s", way)
		}
		return
	}
	if l.a.pageShowing() || !shot.drawn || len(shot.selected) != 1 {
		t.Errorf("the conversation did not reopen under its strip after %s", way)
	}
}
