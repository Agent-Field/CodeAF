package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The compact view is a reading of work, never a replacement for an answer.
func TestCompactLiveKeepsBothTheEarlierAnswerAndTheArrivingAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn, a.state = 2, stateWorking
	a.entries = []entry{
		{kind: entryUser, text: "The earlier question", turn: 1},
		{kind: entryAssistant, text: "The earlier answer stays readable.", turn: 1, settled: true},
		{kind: entryUser, text: "Start the next task", turn: 2},
		{kind: entryThinking, text: "private working text", turn: 2, open: true, settled: true},
		{kind: entryAssistant, text: "Reading the project setup.", turn: 2, settled: true},
		{kind: entryTool, tool: "read", text: "private-setup-path", turn: 2, status: toolOK, ended: time.Unix(101, 0)},
		{kind: entryAssistant, text: "The new answer is arriving.", turn: 2, settled: true},
	}
	a.touch()
	page := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"The earlier answer stays readable.", "Start the next task", "The new answer is arriving."} {
		if !strings.Contains(page, want) {
			t.Fatalf("compact work hid %q:\n%s", want, page)
		}
	}
	for _, hidden := range []string{"private working text", "private-setup-path"} {
		if strings.Contains(page, hidden) {
			t.Fatalf("compact work leaked %q:\n%s", hidden, page)
		}
	}
}

// An old failure remains useful after enough later steps arrive to fill the window.
func TestCompactLiveKeepsAnEarlierFailureAndThePersonsCorrection(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn, a.state = 1, stateWorking
	a.entries = []entry{
		{kind: entryUser, text: "Run the checks", turn: 1},
		{kind: entryTool, tool: "bash", text: "first check", turn: 1, status: toolFailed, open: true,
			detail: toolDetail{Output: "build failed: missing target"}},
		{kind: entrySteer, turn: 1, steer: &steerElbow{words: "Keep the existing configuration", consumed: true}},
	}
	for i, words := range []string{"Reading the configuration", "Checking the source", "Inspecting the imports", "Preparing the correction", "Running the final check"} {
		began := time.Unix(int64(110+i*2), 0)
		a.entries = append(a.entries,
			entry{kind: entryAssistant, text: words, turn: 1, settled: true},
			entry{kind: entryTool, tool: "read", text: "hidden-successful-call", turn: 1, status: toolOK, began: began, ended: began.Add(time.Second)},
		)
	}
	a.entries[len(a.entries)-1].status = toolRunning
	a.entries[len(a.entries)-1].ended = time.Time{}
	// The failed step was already opened to read its error.
	a.setCapOpen(a.conversation(), 1, true)
	a.touch()
	page := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"build failed: missing target", "Keep the existing configuration", "Running the final check"} {
		if !strings.Contains(page, want) {
			t.Fatalf("compact work hid %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "hidden-successful-call") {
		t.Fatalf("compact work exposed ordinary call details:\n%s", page)
	}
}

// The whole-work key must still close a turn after the reader asks for all its calls.
func TestCompactLiveWholeDisclosureStillWorksAfterShowingAllCalls(t *testing.T) {
	a := liveStepsApp(t)
	a.unfold(1)
	if !a.toggleLatestWorkfold() {
		t.Fatal("the whole-work key stopped responding after showing all calls")
	}
	page := livePage(a)
	for _, raw := range []string{"needle-pattern", "git status --porcelain"} {
		if strings.Contains(page, raw) {
			t.Fatalf("the whole-work key left raw calls visible: %s", page)
		}
	}
	if !strings.Contains(page, "Checking what changed") {
		t.Fatalf("the compact steps did not return: %s", page)
	}
}

// Opening the outline keeps the live batch's existing tool-row budget.
func TestCompactLiveExpandedFrontierKeepsTheExistingCallWindow(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn, a.state = 1, stateWorking
	a.entries = []entry{{kind: entryUser, text: "Check the source", turn: 1}, {kind: entryAssistant, text: "Reading the source files", turn: 1, settled: true}}
	for i := 0; i < toolWindow+2; i++ {
		a.entries = append(a.entries, entry{kind: entryTool, tool: "read", turn: 1, status: toolRunning, began: time.Unix(100, 0)})
	}
	a.touch()
	if !a.toggleLatestWorkfold() {
		t.Fatal("the running work has no disclosure")
	}
	calls := 0
	for _, r := range rows(a) {
		if r.hit == hitTool {
			calls++
		}
	}
	if calls != toolWindow {
		t.Fatalf("opening the live outline drew %d tool rows, want the existing %d-row budget", calls, toolWindow)
	}
}

// The parent's click closes its children even if a thought was opened inside it.
func TestCompactLiveClickingTheWholeDoorHidesAnOpenedThought(t *testing.T) {
	a := liveStepsApp(t)
	a.height = 60
	a.touch()
	click := func(match func(row) bool) {
		t.Helper()
		for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
			if r, ok := a.rowAt(y); ok && match(r) {
				drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
				drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
				return
			}
		}
		t.Fatalf("the expected disclosure is not visible: %s", livePage(a))
	}
	click(func(r row) bool { return r.hit == hitWorkFold && r.turn == 1 })
	click(func(r row) bool { return r.entry == 1 })
	if !strings.Contains(livePage(a), "probably reading the whole tree") {
		t.Fatal("clicking the thought did not open its text")
	}
	click(func(r row) bool { return r.hit == hitWorkFold && r.turn == 1 })
	page := livePage(a)
	if strings.Contains(page, "probably reading the whole tree") || strings.Contains(page, "git status --porcelain") {
		t.Fatalf("closing the whole work left a child visible: %s", page)
	}
	if !strings.Contains(page, "Checking what changed") {
		t.Fatalf("the compact view did not return: %s", page)
	}
}
