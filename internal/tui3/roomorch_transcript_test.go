package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

func adaptiveRailApp(t *testing.T) *app {
	t.Helper()
	agent := &orchFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		snaps:     map[string]orchestrate.Snapshot{"r1": orchRun4()},
	}
	a, _, _ := taskApp(t)
	a.agent = agent
	a.width, a.height = 140, 30
	a.taskUpdate(update(10, "answer the retry question", session.TaskRunning,
		session.TaskNotice{Run: "r1"}))
	a.taskUpdate(update(11, "read the three RFCs", session.TaskRunning,
		session.TaskNotice{Run: "r1", Node: "rfcs", Parent: 10}))
	if a.tasks[10].run != "r1" || a.tasks[11].node != "rfcs" {
		t.Fatalf("adaptive door fields were not retained: root=%+v node=%+v", a.tasks[10], a.tasks[11])
	}
	return a
}

func clickRailDoor(t *testing.T, a *app, node int) {
	t.Helper()
	head := a.bodyTop()
	seen, at := 0, -1
	for y := head; y < head+a.viewHeight(); y++ {
		row := a.railNodeAt(y)
		if row == nil || (y > head && a.railNodeAt(y-1) == row) {
			continue
		}
		if seen == node {
			at = y
			break
		}
		seen++
	}
	if at < 0 {
		t.Fatalf("the roster has no node row %d", node)
	}
	drive(t, a, tea.MouseClickMsg{
		X: a.bodyWidth() + ansi.StringWidth(railSeam) + 6, Y: at, Button: tea.MouseLeft,
	})
}

func TestRunRailNodeDoorsOpenItsFocusedCard(t *testing.T) {
	a := adaptiveRailApp(t)
	a.railWhere = railSpot{id: 11}
	a.railHold = true
	drive(t, a, key("enter"))
	if !a.orchShowing("r1") || a.orchOf().card != "rfcs" {
		t.Fatalf("enter opened %+v, want run r1 focused on rfcs", a.orchOf())
	}
	if got := roomText(a); !strings.Contains(got, "read the three RFCs") {
		t.Fatalf("the focused node card is not on the run page:\n%s", got)
	}

	a = adaptiveRailApp(t)
	clickRailDoor(t, a, 1)
	if !a.orchShowing("r1") || a.orchOf().card != "rfcs" {
		t.Fatalf("click opened %+v, want run r1 focused on rfcs", a.orchOf())
	}
}

func TestRunRailRootDoorOpensTheRunPage(t *testing.T) {
	a := adaptiveRailApp(t)
	clickRailDoor(t, a, 0)
	if !a.orchShowing("r1") {
		t.Fatalf("root click did not open run r1: %+v", a.orchOf())
	}
	if a.orchOf().card != "" {
		t.Fatalf("root click focused node %q, want the run page", a.orchOf().card)
	}
}

func TestOrdinaryRailRowStillOpensItsTaskRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRailDoor(t, a, 0)
	if a.room == nil || a.room.id != 7 || a.room.orch != nil {
		t.Fatalf("ordinary row opened %+v, want task room 7", a.room)
	}
}

func TestRunNodeTranscriptDoorOpensAndEscReturns(t *testing.T) {
	snap := orchRun4()
	snap.Nodes[1].State = orchestrate.Done
	a, agent := orchApp(t, snap)
	path := filepath.Join(t.TempDir(), "rfcs.jsonl")
	journal := "{\"type\":\"message\",\"role\":\"user\",\"content\":\"read the retry RFCs\"}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"RFC 7231 answers it.\"}\n"
	if err := os.WriteFile(path, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.journals = map[string]string{"r1:rfcs": path}
	a.orchCardOpen("rfcs")
	if got := roomText(a); !strings.Contains(got, "transcript · enter opens it") {
		t.Fatalf("node card has no transcript door:\n%s", got)
	}
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	if got := roomText(a); !strings.Contains(got, "read the retry RFCs") || !strings.Contains(got, "RFC 7231 answers it") {
		t.Fatalf("journal did not render as a conversation:\n%s", got)
	}
	drive(t, a, key("esc"))
	if got := roomText(a); !strings.Contains(got, "transcript · enter opens it") {
		t.Fatalf("esc did not return to the node card:\n%s", got)
	}
}

func TestLiveRunNodeTranscriptFollowsTheJournal(t *testing.T) {
	a, agent := orchApp(t, orchRun4())
	path := filepath.Join(t.TempDir(), "rfcs.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"start here\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.journals = map[string]string{"r1:rfcs": path}
	a.orchCardOpen("rfcs")
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"new live line\"}\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	orchPollNow(t, a)
	if got := roomText(a); !strings.Contains(got, "new live line") {
		t.Fatalf("live journal did not follow the poll:\n%s", got)
	}
}

func TestRunNodeWithoutJournalSaysSo(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.orchCardOpen("client")
	a.orchOf().link = len(a.orchCardLinks()) - 1
	drive(t, a, key("enter"))
	if got := roomText(a); !strings.Contains(got, "no transcript yet") || strings.Contains(strings.ToLower(got), "error") {
		t.Fatalf("missing journal was not an honest empty page:\n%s", got)
	}
}
