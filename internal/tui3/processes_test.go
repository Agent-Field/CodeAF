package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestProcessesKeepsFinishedLogsOffSidebarAndOpensExactClickedResult(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 110, 32
	first := jobLogFile(t, "compiler output for first process")
	second := jobLogFile(t, "tests passed for second process")
	a.jobUpdate(jobNotice(3, session.JobDone, "compile", first))
	a.jobUpdate(jobNotice(9, session.JobDone, "test", second))
	if lines := a.jobSection(32, 20); len(lines) != 0 || a.jobSectionMin() != 0 {
		t.Fatal("completed process still occupies sidebar")
	}
	a.showProcesses("")
	rows, hits, _, _ := a.jobPageFrame(110, 32)
	if text := plain(strings.Join(rows, "\n")); !strings.Contains(text, "Processes") || !strings.Contains(text, "9 · done") {
		t.Fatal(text)
	}
	at := -1
	for i, hit := range hits {
		if hit == 1001 {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("first process has no mouse target")
	}
	cmd := a.jobPagePress(8, at)
	if a.jobPage != 3 || cmd == nil {
		t.Fatalf("clicked row opened %d rather than process 3", a.jobPage)
	}
	takeJobLog(t, a, cmd)
	rows, _, _, _ = a.jobPageFrame(110, 32)
	if text := plain(strings.Join(rows, "\n")); !strings.Contains(text, "compiler output for first process") || strings.Contains(text, "tests passed for second process") {
		t.Fatal(text)
	}
	a.jobPageKey("esc")
	if a.jobPage != processListPage {
		t.Fatal("Escape did not return to process list")
	}
	a.jobPageKey("esc")
	if a.jobPageOpen() || a.jobList {
		t.Fatal("Escape did not return to conversation")
	}
}

func TestProcessesKeyboardAndConversationSwitchPreserveOwnership(t *testing.T) {
	a, _, _ := taskApp(t)
	a.jobUpdate(jobNotice(3, session.JobDone, "compile", jobLogFile(t, "output")))
	a.showProcesses("")
	if cmd := a.jobPageKey("enter"); a.jobPage != 3 || cmd == nil {
		t.Fatal("Enter did not open process log")
	}
	a.dropJobs()
	if a.jobPageOpen() || a.jobList || len(a.processRows()) != 0 {
		t.Fatal("process page survived conversation switch")
	}
	a.showProcesses("")
	rows, _, _, _ := a.jobPageFrame(80, 24)
	if !strings.Contains(plain(strings.Join(rows, "\n")), "when you start background work") {
		t.Fatal("empty processes page has no explanation")
	}
}
