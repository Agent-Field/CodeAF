package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The standing task lane is the engine road: its landing arrives while the
// chat's wake turn is being drawn. The first frame and the settled reply must
// both show the program's card without opening the wake turn's work fold.
func TestSeniorDevEngineLandingStandsOutsideWakeFold(t *testing.T) {
	for _, roomOpen := range []bool{false, true} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.width, a.height = 110, 40
		a.workMode = config.WorkFold
		a.entries = []entry{{kind: entryUser, text: "start senior-dev", turn: 1},
			{kind: entryAssistant, text: "I will report when it finishes.", turn: 1, settled: true}}
		a.turn = 2
		a.Update(taskEventMsg{gen: a.taskGen, ev: update(7, "Repair the parser", session.TaskRunning,
			session.TaskNotice{Program: "senior-dev"})})
		if roomOpen {
			a.openRoom(7, "Repair the parser")
		}
		a.entries = append(a.entries, entry{kind: entryThinking, text: "reading the ending", turn: 2, settled: true})
		a.Update(taskEventMsg{gen: a.taskGen, ev: update(7, "Repair the parser", session.TaskDone,
			session.TaskNotice{Program: "senior-dev", Report: "submitted a change"})})
		if roomOpen {
			a.closeRoom()
		}
		first := taskText(a)
		if !strings.Contains(first, "senior-dev's ending went to the chat") {
			t.Fatalf("room open %t: the first conversation frame hid the landing:\n%s", roomOpen, first)
		}
		a.entries = append(a.entries,
			entry{kind: entryTool, tool: "read", turn: 2, status: toolOK},
			entry{kind: entryAssistant, text: "The change is on its branch.", turn: 2, settled: true})
		a.touch()
		settled := taskText(a)
		card := strings.Index(settled, "senior-dev's ending went to the chat")
		fold := strings.LastIndex(settled, "worked")
		answer := strings.Index(settled, "The change is on its branch.")
		if card < 0 || fold < 0 || answer < 0 || !(card < fold && fold < answer) {
			t.Fatalf("room open %t: the card, folded wake work and answer are out of order:\n%s", roomOpen, settled)
		}
		if strings.Contains(settled, "reading the ending") || strings.Contains(settled, "read") {
			t.Fatalf("room open %t: the wake work did not fold:\n%s", roomOpen, settled)
		}
		reopened := newTestApp(&fakeAgent{model: "m"})
		reopened.width, reopened.height = 110, 40
		reopened.workMode = config.WorkFold
		reopened.entries = append([]entry(nil), a.entries...)
		reopened.touch()
		again := taskText(reopened)
		if card, fold, answer := strings.Index(again, "senior-dev's ending went to the chat"),
			strings.LastIndex(again, "worked"), strings.Index(again, "The change is on its branch."); card < 0 || fold < 0 || answer < 0 || !(card < fold && fold < answer) {
			t.Fatalf("room open %t: reopened conversation changed the landing order:\n%s", roomOpen, again)
		}
	}
}

// The direct session event lane used by --no-host carries the same program
// landing and must draw the card before the reply on that road too.
func TestSeniorDevNoHostLandingStandsOutsideWakeFold(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 110, 40
	a.workMode = config.WorkFold
	a.turn = 2
	a.entries = []entry{{kind: entryUser, text: "start senior-dev", turn: 1},
		{kind: entryAssistant, text: "I will report when it finishes.", turn: 1, settled: true},
		{kind: entryThinking, text: "reading the ending", turn: 2, settled: true}}
	a.Update(streamEventMsg{gen: a.gen, ev: update(7, "Repair the parser", session.TaskRunning,
		session.TaskNotice{Program: "senior-dev"})})
	a.Update(streamEventMsg{gen: a.gen, ev: update(7, "Repair the parser", session.TaskDone,
		session.TaskNotice{Program: "senior-dev", Report: "submitted a change"})})
	a.entries = append(a.entries,
		entry{kind: entryTool, tool: "read", turn: 2, status: toolOK},
		entry{kind: entryAssistant, text: "The change is on its branch.", turn: 2, settled: true})
	a.touch()
	text := taskText(a)
	if card, fold, answer := strings.Index(text, "senior-dev's ending went to the chat"),
		strings.LastIndex(text, "worked"), strings.Index(text, "The change is on its branch."); card < 0 || fold < 0 || answer < 0 || !(card < fold && fold < answer) {
		t.Fatalf("direct session lane hid the landing or wake fold:\n%s", text)
	}
}

// An ordinary task still writes its landing after the starting turn and before
// the later chat turn, as it did before the program card needed a fold boundary.
func TestOrdinaryTaskLandingKeepsItsConversationPosition(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 110, 40
	a.workMode = config.WorkFold
	a.entries = []entry{{kind: entryUser, text: "start task", turn: 1},
		{kind: entryAssistant, text: "It is running.", turn: 1, settled: true}}
	a.turn = 1
	a.Update(taskEventMsg{gen: a.taskGen, ev: update(7, "Repair the parser", session.TaskRunning, session.TaskNotice{})})
	a.Update(taskEventMsg{gen: a.taskGen, ev: update(7, "Repair the parser", session.TaskDone,
		session.TaskNotice{Report: "repaired"})})
	at := a.doneEntryFor(7)
	if at != 2 || a.entries[at].turn != 1 {
		t.Fatalf("ordinary landing moved from its turn: index %d, entries %+v", at, a.entries)
	}
	a.turn = 2
	a.entries = append(a.entries, entry{kind: entryThinking, text: "checking", turn: 2, settled: true},
		entry{kind: entryAssistant, text: "The task is done.", turn: 2, settled: true})
	a.touch()
	text := taskText(a)
	card := strings.Index(text, "Repair the parser")
	answer := strings.Index(text, "The task is done.")
	if card < 0 || answer < 0 || card >= answer {
		t.Fatalf("ordinary card is not before the later answer:\n%s", text)
	}
}
