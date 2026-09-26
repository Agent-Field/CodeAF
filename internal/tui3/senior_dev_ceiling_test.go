package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSeniorDevProposalCardNamesEffectiveCeiling(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.proposeTask(session.Event{Kind: session.EventTaskProposal, Tool: "propose_task", Task: &session.TaskNotice{
		ID: 7, Title: "Repair the parser", Brief: "repair it", Program: "senior-dev", Ceiling: "up to $1.00 and 30m",
	}})
	card := a.cardFor(7)
	if card == nil || !strings.Contains(a.taskMetaWord(card, 80), "up to $1.00 and 30m") {
		t.Fatalf("proposal card lost its effective ceiling: %+v", card)
	}
}

// A landing while the task page is open must still enter the conversation.
func TestSeniorDevLandingWhileTaskPageOpenPostsAnEndedCard(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.Update(taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 7, Title: "Repair the parser", State: session.TaskRunning, Program: "senior-dev",
	}}})
	a.openRoom(7, "Repair the parser")
	a.Update(taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{
		ID: 7, Title: "Repair the parser", State: session.TaskDone, Program: "senior-dev", Report: "done",
	}}})
	if at := a.doneEntryFor(7); at < 0 || a.entries[at].done == nil || a.entries[at].done.program != "senior-dev" {
		t.Fatalf("landing with page open posted no senior-dev card: %+v", a.entries)
	}
}

func TestSeniorDevLimitNoticeAppearsOnTheOpenConversation(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	line := "senior-dev stopped at the conversation's $1.00 limit · spent $1.20 · its work is on branch task/repair"
	a.taskEvent(session.Event{Kind: session.EventNotice, Text: line})
	for _, entry := range a.entries {
		if entry.kind == entryNote && entry.text == line {
			return
		}
	}
	t.Fatal("the live conversation did not paint the limit notice")
}
