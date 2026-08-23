package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func workRailApp(t *testing.T, kids ...session.WorkNode) (*app, *taskFake) {
	t.Helper()
	a, agent, _ := taskApp(t)
	a.taskUpdate(update(7, "Build the rail", session.TaskRunning, session.TaskNotice{}))
	agent.work = []session.WorkNode{{ID: "7", Title: "Build the rail", State: session.WorkRunning, Children: kids}}
	a.paints = 0
	return a, agent
}

func TestANilWorkTreeLeavesTheRailByteIdentical(t *testing.T) {
	a, agent, _ := taskApp(t)
	a.taskUpdate(update(7, "Build the rail", session.TaskRunning, session.TaskNotice{}))
	withDoor := strings.Join(a.railRows(12), "\n")
	a.agent = agent.fakeAgent
	withoutDoor := strings.Join(a.railRows(12), "\n")
	if withDoor != withoutDoor {
		t.Fatalf("a nil work tree changed the rail:\nwith door:\n%s\nwithout door:\n%s", withDoor, withoutDoor)
	}
}

func TestWorkingChildrenStartUnfoldedAndRememberAFold(t *testing.T) {
	a, _ := workRailApp(t,
		session.WorkNode{ID: "hand-1", Title: "Read the law", State: session.WorkWaiting},
		session.WorkNode{ID: "hand-2", Title: "Write the tests", State: session.WorkRunning},
	)
	text := strings.Join(railText(a, 14), "\n")
	for _, title := range []string{"Read the law", "Write the tests"} {
		if !strings.Contains(text, title) {
			t.Fatalf("the default fold hid %q:\n%s", title, text)
		}
	}
	a.railSetOpen(a.tasks[7], false)
	for pass := 0; pass < 2; pass++ {
		text = strings.Join(railText(a, 14), "\n")
		if strings.Contains(text, "Read the law") || strings.Contains(text, "Write the tests") {
			t.Fatalf("the remembered fold reopened on pass %d:\n%s", pass, text)
		}
	}
}

func TestWorkingChildrenStopAtFiveAndOfferTheRoom(t *testing.T) {
	kids := make([]session.WorkNode, 7)
	for i := range kids {
		kids[i] = session.WorkNode{ID: itoa(i + 1), Title: "hand " + itoa(i+1), State: session.WorkWaiting}
	}
	a, _ := workRailApp(t, kids...)
	text := strings.Join(railText(a, 18), "\n")
	if strings.Contains(text, "hand 6") || strings.Contains(text, "hand 7") {
		t.Fatalf("the preview exceeded five children:\n%s", text)
	}
	if !strings.Contains(text, "view more · +2") {
		t.Fatalf("the preview did not count its hidden children:\n%s", text)
	}
}

func TestTheTasksHeadCountsOnlyTwoOrMoreWorking(t *testing.T) {
	a, agent := workRailApp(t)
	for n := 0; n <= 3; n++ {
		kids := make([]session.WorkNode, n)
		for i := range kids {
			kids[i] = session.WorkNode{ID: itoa(i), Title: "hand", State: session.WorkRunning}
		}
		agent.work[0].Children = kids
		head := plain(a.marginHead(28)[0].text)
		total := n + 1
		if total <= 1 && head != marginTasksWord {
			t.Fatalf("%d working drew a payload: %q", total, head)
		}
		if total > 1 && head != marginTasksWord+" · "+itoa(total)+" working" {
			// The root is itself a running worker, and CountWorking counts every depth.
			t.Fatalf("%d working drew the wrong head count: %q", total, head)
		}
	}
}

func TestOnlyOneWorkingChildSpins(t *testing.T) {
	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	a, _ := workRailApp(t,
		session.WorkNode{ID: "old", Title: "older hand", State: session.WorkRunning, Born: base},
		session.WorkNode{ID: "new", Title: "newer hand", State: session.WorkRunning, Born: base.Add(time.Second)},
	)
	text := strings.Join(railText(a, 14), "\n")
	if got := strings.Count(text, tokens.Spinner(0)); got != 1 {
		t.Fatalf("exactly one row should spin, got %d spinners:\n%s", got, text)
	}
	if !strings.Contains(text, glyphRunASCII+" older hand") {
		t.Fatalf("the other running child did not hold still:\n%s", text)
	}
}
