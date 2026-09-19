package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func displayPart(command, separator string, record, copyPrefix bool) session.PlanCommandPart {
	return session.PlanCommandPart{Command: command, Separator: separator, RecordAddressed: record, RunCopyPrefix: copyPrefix}
}

func TestPlanStepDisplayUsesTheFiveRecordedShapes(t *testing.T) {
	const copy = "/…/trees/1"
	for _, test := range []struct {
		name  string
		parts []session.PlanCommandPart
		want  string
	}{
		{"mixed work and record", []session.PlanCommandPart{displayPart("ls", "; ", false, false), displayPart("ls *.go 2>/dev/null", "; ", false, false), displayPart("plandb task overview 2>/dev/null | head -30", "", true, false)}, "ls; ls *.go 2>/dev/null"},
		{"work only", []session.PlanCommandPart{displayPart("cat calc.go go.mod notes.txt", "", false, false)}, "cat calc.go go.mod notes.txt"},
		{"record only", []session.PlanCommandPart{displayPart("plandb done t-1 --agent 1 --result 'Added Mul and Div in muldiv.go …'", "", true, false)}, ""},
		{"copy then work", []session.PlanCommandPart{displayPart("cd "+copy, " && ", false, true), displayPart("ls", " && ", false, false), displayPart("cat muldiv.go", " && ", false, false), displayPart("go vet ./...", " && ", false, false), displayPart("go test -count=1 ./...", "", false, false)}, "ls && cat muldiv.go && go vet ./... && go test -count=1 ./..."},
		{"copy then record", []session.PlanCommandPart{displayPart("cd "+copy, " && ", false, true), displayPart("plandb done t-1 --agent 1 --result 'Mul and Div in …'", "", true, false)}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := planDisplayCommand("", test.parts); got != test.want {
				t.Fatalf("planDisplayCommand() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTaskPageOmitsOwnRecordIDsAndRunCopyPath(t *testing.T) {
	const copy = "/private/conversation/trees/1"
	row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "done", Folder: copy}
	steps := []session.PlanStep{
		{Step: 1, Command: "ls; plandb task overview", Parts: []session.PlanCommandPart{displayPart("ls", "; ", false, false), displayPart("plandb task overview", "", true, false)}, Observation: "files"},
		{Step: 2, Command: "plandb done t-1 --agent 1", Parts: []session.PlanCommandPart{displayPart("plandb done t-1 --agent 1", "", true, false)}, Observation: "✓ t-1 done [0/0]"},
		{Step: 3, Command: "cd " + copy + " && go test ./...", Parts: []session.PlanCommandPart{displayPart("cd "+copy, " && ", false, true), displayPart("go test ./...", "", false, false)}, Observation: "ok"},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"t-alpha": {Row: row, Folder: copy, Steps: steps}})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("task place did not open")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	page := taskSheetText(a)
	for _, never := range []string{copy, "t-1", "--agent 1", "✓ t-1"} {
		if strings.Contains(page, never) {
			t.Fatalf("page contains %q:\n%s", never, page)
		}
	}
	for _, want := range []string{"1  ls", "2  go test ./...", "files", "ok"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
}
