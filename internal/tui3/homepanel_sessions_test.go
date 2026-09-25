package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// An untitled chat on home used to read as its session id with the first
// letter raised (`D53cceead3f99593`). The sessions place already calls that
// row `new conversation`. Home reads the name from the same [homeName].
func TestHomeSessionsCallsAnUntitledChatNewConversation(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	id := "d53cceead3f99593"
	transcript := "/chat/" + id + "/transcript.jsonl"
	titled := session.SessionRow{
		ID: "927d303242f9d00e", Transcript: "/chat/927d303242f9d00e/transcript.jsonl",
		Title: "Porting the Picker", At: now.Add(-time.Hour),
	}
	for _, title := range []string{id, "D53cceead3f99593"} {
		row := session.SessionRow{ID: id, Transcript: transcript, Title: title, At: now, Live: true}
		in := homeGridInput{now: now, rows: []switcherRow{
			{kind: switcherConversation, session: row, title: homeName(row)},
			{kind: switcherConversation, session: titled, title: homeName(titled)},
		}}
		got := (sessionsPanel{homePanelBase{panelSessions}}).rows(&in)
		var untitledTitle, titledTitle string
		for _, line := range got.lines {
			if line.cell == nil {
				continue
			}
			switch line.row.ID {
			case id:
				untitledTitle = line.cell.title
			case titled.ID:
				titledTitle = line.cell.title
			}
		}
		if untitledTitle != unnamedConversationWord {
			t.Fatalf("title %q reads %q, want %q", title, untitledTitle, unnamedConversationWord)
		}
		if titledTitle != "Porting the Picker" {
			t.Fatalf("a titled chat reads %q", titledTitle)
		}
	}
}

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

func TestHomeConversationsAppearOnlyOnceUnderSessions(t *testing.T) {
	for _, width := range []int{48, 80, 120, 180} {
		a, files := homeTabsFixture(t)
		a.width, a.height = width, 70
		homeText(a)
		seen := map[string]bool{}
		for _, line := range a.home.lines {
			if line.kind != homeSession {
				continue
			}
			if line.cell == nil || line.cell.panel != panelSessions {
				t.Fatalf("width %d: conversation outside Sessions", width)
			}
			if seen[line.row.Transcript] {
				t.Fatalf("width %d: duplicate conversation %s", width, line.row.Transcript)
			}
			seen[line.row.Transcript] = true
		}
		if len(seen) != len(files) {
			t.Fatalf("width %d: got %d conversations, want %d", width, len(seen), len(files))
		}
	}
}
