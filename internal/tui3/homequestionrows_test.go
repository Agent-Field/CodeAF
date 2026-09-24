package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestTasksSpendAndSettingsAdvertiseEscapeClose(t *testing.T) {
	for _, place := range everyPlaceTable() {
		if place.id != pageTasks && place.id != pageSpend && place.id != pageSettings {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			for _, width := range []int{180, 80, 44} {
				a.width = width
				rows := strings.Split(placeFrameText(a), "\n")
				if last := rows[len(rows)-1]; !strings.Contains(last, "esc close") {
					t.Fatalf("at width %d, footer = %q", width, last)
				}
			}
			drive(t, a, key("esc"))
			if !a.at(pageNone) {
				t.Fatal("Escape did not return to the conversation")
			}
		})
	}
}

func TestSinceYouLeftHeadingOpensMemory(t *testing.T) {
	for _, width := range []int{80, 120, 180} {
		a := newSwitchLab(t).open(width, 45)
		homeText(a)
		x, y, ok := homeHeadingAt(a, "since you left")
		if !ok {
			t.Fatalf("width %d has no since you left heading", width)
		}
		drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		if !a.at(pageMemory) {
			t.Fatalf("width %d opened %s", width, a.page.word())
		}
	}
}

func TestHomeQuestionDecoratesConversationWithoutDuplicate(t *testing.T) {
	a, files := homeTabsFixture(t)
	setWaiting := func(waiting bool) {
		for pi := range a.home.world.Projects {
			for si := range a.home.world.Projects[pi].Sessions {
				row := &a.home.world.Projects[pi].Sessions[si]
				if row.Transcript != files[1] {
					continue
				}
				row.Live = waiting
				row.Presence = session.SessionPresence{}
				if waiting {
					row.Presence = session.SessionPresence{State: session.PresenceWaiting, UpdatedAt: a.home.world.Read,
						Question: consentQuestionAt(7, "allow the command?", a.home.world.Read)}
				}
			}
		}
		a.home.build()
	}
	setWaiting(true)
	count, found := 0, homeNoLine
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == files[1] && line.cell.panel == panelSessions {
			count++
			found = i
		}
	}
	if count != 1 || found < 0 {
		t.Fatalf("waiting conversation appears %d times", count)
	}
	line := a.home.lines[found]
	if line.cell.panel != panelSessions || line.cell.mark != cellMarkNeeds {
		t.Fatalf("question did not stay on its conversation row: %+v", line.cell)
	}
	if got := plain(a.homeConversationBullet(line.cell, a.pal)); got != a.pal.glyph(tokens.GNeedsHuman) {
		t.Fatalf("question bullet = %q", got)
	}
	if strings.Contains(homeText(a), "needs you") {
		t.Fatal("Home still draws the needs you heading")
	}
	assertHomeTabParity(t, a)
	answered := false
	a.leaveAnswer = func(_ string, _ session.QuestionKind, id uint64, answer string) error {
		answered = id == 7 && answer == "1"
		return nil
	}
	a.home.cursor = found
	_, took := a.homeGridAnswer("1")
	if !took || !answered {
		t.Fatal("the decorated conversation lost its answer action")
	}
	setWaiting(false)
	for _, line := range a.home.lines {
		if line.row.Transcript == files[1] && line.cell != nil && line.cell.mark == cellMarkNeeds {
			t.Fatal("resolved question kept its question mark")
		}
	}
}

func TestHomeTaskQuestionStaysOnTaskRow(t *testing.T) {
	lab := newLiveLab(t)
	lab.landed("4", "fix the flaky sieve", 30*time.Minute)
	a := lab.openAt(180, 45)
	count := 0
	for _, line := range a.home.lines {
		if line.task == nil || line.task.ID != "4" {
			continue
		}
		count++
		if line.cell.panel != panelNeeds || line.cell.mark != cellMarkNeeds || line.cell.answers == "" {
			t.Fatalf("task lost its question or answers: %+v", line.cell)
		}
	}
	if count != 1 {
		t.Fatalf("task question appears %d times", count)
	}
}

func TestHomeTaskDecisionDoesNotStealAnotherTasksQuestion(t *testing.T) {
	now := time.Now()
	row := session.SessionRow{ID: "chat", Transcript: "/tmp/chat/transcript.jsonl", Live: true,
		Presence: session.SessionPresence{State: session.PresenceWaiting, UpdatedAt: now,
			Question: consentQuestionAt(7, "continue task 2?", now)}}
	row.Presence.Question.Full = &session.Question{Subject: session.SubjectRef{Kind: session.SubjectNode, ID: 2}}
	row.Tasks.Rows = []session.TaskIndexEntry{
		{ID: "2", SessionID: "chat", Title: "second task", Status: string(session.TaskRunning), StartedAt: now},
		{ID: "3", SessionID: "chat", Title: "third task", Status: string(session.TaskUnverified), EndedAt: now},
	}
	chat := switcherRow{kind: switcherConversation, session: row, needs: true, title: "conversation"}
	in := homeGridInput{now: now, rows: []switcherRow{chat}, openChats: []switcherRow{chat},
		world: session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}}
	in.calls = needsCalls(in.world, now)
	conversations := (sessionsPanel{homePanelBase{panelSessions}}).rows(&in)
	if conversations.lines[0].cell.answers != "" {
		t.Fatal("task answer actions were put on the conversation instead")
	}
	tasks := (needsPanel{homePanelBase{panelNeeds}}).rows(&in)
	if len(tasks.lines) != 2 {
		t.Fatalf("got %d question rows", len(tasks.lines))
	}
	for _, line := range tasks.lines {
		if line.cell.mark != cellMarkNeeds || line.cell.answers == "" {
			t.Fatalf("question lost its actions: %+v", line.cell)
		}
	}

}

func TestPhoneTaskQuestionMarksItsConversation(t *testing.T) {
	a, files := homeTabsFixture(t)
	a.width, a.height = 50, 40
	homeText(a)
	for pi := range a.home.world.Projects {
		for si := range a.home.world.Projects[pi].Sessions {
			row := &a.home.world.Projects[pi].Sessions[si]
			if row.Transcript != files[1] {
				continue
			}
			row.Live = true
			row.Presence = session.SessionPresence{State: session.PresenceWaiting, UpdatedAt: a.home.world.Read,
				Question: consentQuestionAt(7, "continue the task?", a.home.world.Read)}
			row.Presence.Question.Full = &session.Question{Subject: session.SubjectRef{Kind: session.SubjectNode, ID: 2}}
		}
	}
	a.home.build()
	count := 0
	for _, line := range a.home.lines {
		if line.kind != homeSession || line.row.Transcript != files[1] {
			continue
		}
		count++
		if line.cell == nil || line.cell.mark != cellMarkNeeds || line.cell.answers == "" {
			t.Fatalf("compact conversation lost its task question: %+v", line.cell)
		}
	}
	if count != 1 {
		t.Fatalf("compact task question appears %d times", count)
	}
}
