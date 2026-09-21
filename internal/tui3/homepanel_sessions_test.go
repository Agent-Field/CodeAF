package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestHomeSessionsShowsFifteenMostRecentConversations(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	in := homeGridInput{now: now}
	for i := 19; i >= 0; i-- {
		row := session.SessionRow{ID: itoa(i), Transcript: "/chat/" + itoa(i), Title: "Conversation " + itoa(i), At: now.Add(-time.Duration(i) * time.Hour)}
		row.Tasks.Rows = []session.TaskIndexEntry{{ID: "task", Label: "individual task"}}
		in.rows = append(in.rows, switcherRow{kind: switcherConversation, session: row, title: row.Title})
	}
	in.opened, in.openedOn = panelSessions, true
	got := (sessionsPanel{homePanelBase{panelSessions}}).rows(&in)
	if len(got.lines) != homeSessionsLimit || got.more != 0 {
		t.Fatalf("rows=%d more=%d", len(got.lines), got.more)
	}
	for i, line := range got.lines {
		if line.kind != homeSession || line.row.ID != itoa(i) || line.cell.row.task != nil {
			t.Fatalf("row %d: %+v", i, line)
		}
	}
}

func TestHomeSessionsHeadingOpensRenamedTab(t *testing.T) {
	for _, width := range []int{80, 120, 180} {
		a := newLiveLab(t).openAt(width, 60)
		x, y, ok := homeHeadingAt(a, sessionsWord)
		if !ok {
			t.Fatalf("width %d: no sessions heading", width)
		}
		drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if !a.at(pageTasks) || a.page.word() != "sessions" {
			t.Fatalf("opened %s", a.page.word())
		}
	}
}

func TestHomeSessionsKeepsClosedHistoryAndFreshTabTitles(t *testing.T) {
	a, files := homeTabsFixture(t)
	in := a.home.gridInput()
	in.closedChats = []switcherRow{in.openChats[1]}
	in.openChats = in.openChats[:1]
	in.openChats[0].title = "Fresh tab title"
	got := (sessionsPanel{homePanelBase{panelSessions}}).rows(&in)
	found := false
	for _, line := range got.lines {
		if line.row.Transcript == files[0] && line.cell.title != "Fresh tab title" {
			t.Fatal("stale title")
		}
		if line.row.Transcript == files[1] {
			found = line.cell.closed
		}
	}
	if !found {
		t.Fatal("closed history is absent or not dimmed")
	}
	if got.lines[0].sameRow((recentPanel{homePanelBase{panelRecent}}).rows(&in).lines[0]) {
		t.Fatal("separate panel rows share selection identity")
	}
}

func TestHomeSessionsNeverRendersIndividualTasks(t *testing.T) {
	a := newSwitchLab(t).open(180, 60)
	for _, line := range panelLines(a, panelSessions) {
		if line.cell.kind == cellRow && (line.kind != homeSession || strings.Contains(line.cell.title, "read 40 filings")) {
			t.Fatalf("individual task remains: %+v", line)
		}
	}
	if pageTasks.lookKey() != "tasks" {
		t.Fatal("renaming the tab discarded its saved visit stamp")
	}
}

func TestHomeSessionsHeadingOpensOnCompactScreens(t *testing.T) {
	l := newLiveLab(t)
	a := phoneHome(t, l.homeLab, l.mine)
	_, hits, _, _ := a.homePhoneFrame(a.width, a.height)
	for y, at := range hits {
		if at >= 0 && at < len(a.home.lines) {
			line := a.home.lines[at]
			if line.kind == homePhoneSection && line.project == sessionsWord {
				a.homePhonePress(1, y)
				if !a.at(pageTasks) {
					t.Fatal("compact heading did not open sessions")
				}
				return
			}
		}
	}
	t.Fatal("no compact sessions heading")
}
