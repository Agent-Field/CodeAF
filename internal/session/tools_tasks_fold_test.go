package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ── ANOTHER WINDOW'S FAMILY, THROUGH THE TOOL'S OWN DOOR (#894) ──────────────
//
// The <elsewhere> block folds a family onto its head (#879) and the tasks tool
// did not, so the same window's work read as one piece of work in one place and
// as four unrelated jobs in the other, in the same turn. This is the whole of
// what this file pins: that the tool's answer and the block are the same
// sentence about the same window.

// anotherWindow lays a second window's presence file beside this
// conversation's own, under the same project and the same workspace, holding
// one quick task with three quick parts.
func anotherWindow(t *testing.T, root string) string {
	t.Helper()
	started := time.Now().Add(-5 * time.Minute)
	parts := []PresenceTask{{
		ID: "4", Title: "Survey the config loaders", State: string(TaskRunning),
		StartedAt: started, Files: []string{"internal/config/load.go"},
	}}
	for id := 5; id <= 7; id++ {
		parts = append(parts, PresenceTask{
			ID: json.Number(intToString(id)).String(), Title: "read loader " + intToString(id),
			Parent: "4", Kind: TaskKindQuick, State: string(TaskRunning),
			StartedAt: started, Files: []string{"notes/" + intToString(id) + ".md"},
		})
	}
	return machineSession(t, root, "here", "theirs", "docs pass", "/work/aforge", 4242, time.Second, parts...)
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// THE TOOL'S ANSWER CARRIES ONE ROW FOR THAT FAMILY, spelled the way the block
// spells it — the family's own title, then `3 quick parts running` — and no row
// of its own for any part. Asserted on the exact string of the tool's answer.
func TestTheTasksToolFoldsAnotherWindowsPartsOntoTheirHead(t *testing.T) {
	root := t.TempDir()
	mine := machineSession(t, root, "here", "mine", "this chat", "/work/aforge", 4242, time.Second)
	anotherWindow(t, root)
	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}

	answer, _, err := agent.tasksTool().Execute(t.Context(), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "another window · Survey the config loaders · running · running for 5m 0s · 3 quick parts running"
	if !strings.Contains(answer, want) {
		t.Fatalf("the tool's answer is missing the family's one row %q:\n%s", want, answer)
	}
	// NO ROW OF ITS OWN FOR ANY PART. The parts are the family's, and a row each
	// is the four-unrelated-jobs reading the block stopped drawing on 2026-09-11.
	if strings.Contains(answer, "read loader ") {
		t.Fatalf("the tool's answer drew a part of its own:\n%s", answer)
	}
	// The family's files are the family's, not the head's alone.
	if !strings.Contains(answer, "files so far: internal/config/load.go, notes/5.md, notes/6.md, notes/7.md") {
		t.Fatalf("the family's files did not ride on its row:\n%s", answer)
	}
	if got := strings.Count(answer, "another window ·"); got != 1 {
		t.Fatalf("the answer drew %d rows for one family:\n%s", got, answer)
	}
}

// THE OVERFLOW LINE COUNTS FAMILIES, NOT PARTS, in both away sections — a
// window with work handed out cannot push a different window's whole task out
// of the answer by the number of its parts alone.
func TestTheOverflowLineCountsFamiliesAndNotParts(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	var rows []ElsewhereTask
	// One window running one task with three parts, then enough other windows
	// to cross the cap: taskSearchLimit families, no parts anywhere.
	rows = append(rows, ElsewhereTask{SessionID: "one", Session: "a", Task: PresenceTask{
		ID: "4", Title: "Survey the config loaders", State: string(TaskRunning), StartedAt: started}})
	for id := 5; id <= 7; id++ {
		rows = append(rows, ElsewhereTask{SessionID: "one", Session: "a", Task: PresenceTask{
			ID: intToString(id), Title: "read loader " + intToString(id), Parent: "4",
			Kind: TaskKindQuick, State: string(TaskRunning), StartedAt: started}})
	}
	for window := 0; window < taskSearchLimit+3; window++ {
		rows = append(rows, ElsewhereTask{SessionID: "w" + intToString(window), Task: PresenceTask{
			ID: "1", Title: "task " + intToString(window), State: string(TaskRunning), StartedAt: started}})
	}
	out := taskElsewhereText(rows, "", time.Now())
	if got := strings.Count(out, "another window ·"); got != taskSearchLimit {
		t.Fatalf("the section drew %d rows, want the %d families:\n%s", got, taskSearchLimit, out)
	}
	if !strings.Contains(out, "· 3 quick parts running") {
		t.Fatalf("a family with parts was drawn without its count:\n%s", out)
	}
}
