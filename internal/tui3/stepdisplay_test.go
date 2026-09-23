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
			// The line is the one these parts were read from, and the parts carry
			// their spans into it, the way the session hands them over.
			var line strings.Builder
			for _, part := range test.parts {
				line.WriteString(part.Command + part.Separator)
			}
			parts := spannedParts(t, line.String(), test.parts...)
			if got := planDisplayCommand(line.String(), parts); got != test.want {
				t.Fatalf("planDisplayCommand(%q) = %q, want %q", line.String(), got, test.want)
			}
		})
	}
}

// spannedParts reads a command the way the session hands it over: each part
// with the span it was typed in and the boundary after it, found in order.
func spannedParts(t *testing.T, command string, parts ...session.PlanCommandPart) []session.PlanCommandPart {
	t.Helper()
	at := 0
	for i := range parts {
		found := strings.Index(command[at:], parts[i].Command)
		if found < 0 {
			t.Fatalf("%q is not in %q after byte %d", parts[i].Command, command, at)
		}
		parts[i].Start = at
		end := at + found + len(parts[i].Command)
		if parts[i].Separator == "" {
			parts[i].End, parts[i].SepEnd = len(command), len(command)
		} else {
			parts[i].End = end + strings.Index(command[end:], parts[i].Separator)
			parts[i].SepEnd = parts[i].End + len(parts[i].Separator)
		}
		at = parts[i].SepEnd
	}
	return parts
}

// WHAT IS DRAWN IS CUT FROM THE LINE THAT RAN, NEVER RETYPED. A line with
// nothing left out is drawn exactly as recorded, whatever its spacing and
// however it groups; a line with a part left out keeps every other byte as it
// was typed, and never ends on a boundary.
func TestPlanStepDisplayCutsTheRecordedLineAndNeverRetypesIt(t *testing.T) {
	const copy = "/conversation/trees/1"
	for _, test := range []struct {
		name    string
		command string
		parts   []session.PlanCommandPart
		want    string
	}{
		{"nothing left out, tight boundaries", "ls;go test ./...&&go vet ./...",
			[]session.PlanCommandPart{displayPart("ls", ";", false, false), displayPart("go test ./...", "&&", false, false), displayPart("go vet ./...", "", false, false)},
			"ls;go test ./...&&go vet ./..."},
		{"nothing left out, a substitution keeps its brackets", "echo $(date) > stamp",
			[]session.PlanCommandPart{displayPart("echo", "$(", false, false), displayPart("date", ")", false, false), displayPart("> stamp", "", false, false)},
			"echo $(date) > stamp"},
		{"the copy left out, the work as typed", "cd " + copy + "  &&  ls -la   &&   go test ./...",
			[]session.PlanCommandPart{displayPart("cd "+copy, "&&", false, true), displayPart("ls -la", "&&", false, false), displayPart("go test ./...", "", false, false)},
			"ls -la   &&   go test ./..."},
		{"the record left out of the middle", "go build ./... && plandb note t-1 'built' && go test ./...",
			[]session.PlanCommandPart{displayPart("go build ./...", "&&", false, false), displayPart("plandb note t-1 'built'", "&&", true, false), displayPart("go test ./...", "", false, false)},
			"go build ./... && go test ./..."},
		{"the record left off the end, and no boundary dangles", "go test ./... ; plandb done t-1 | head -3",
			[]session.PlanCommandPart{displayPart("go test ./...", ";", false, false), displayPart("plandb done t-1", "|", true, false), displayPart("head -3", "", true, false)},
			"go test ./..."},
		{"only the record", "plandb task overview",
			[]session.PlanCommandPart{displayPart("plandb task overview", "", true, false)},
			""},
	} {
		t.Run(test.name, func(t *testing.T) {
			parts := spannedParts(t, test.command, test.parts...)
			if got := planDisplayCommand(test.command, parts); got != test.want {
				t.Fatalf("planDisplayCommand(%q) = %q, want %q", test.command, got, test.want)
			}
		})
	}
}

func TestTaskPageOmitsOwnRecordIDsAndRunCopyPath(t *testing.T) {
	const copy = "/private/conversation/trees/1"
	row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "done", Folder: copy}
	steps := []session.PlanStep{
		{Step: 1, Command: "ls; plandb task overview", Parts: []session.PlanCommandPart{displayPart("ls", "; ", false, false), displayPart("plandb task overview", "", true, false)}, Observation: "files", ObservationHeadWithheld: true},
		{Step: 2, Command: "plandb done t-1 --agent 1", Parts: []session.PlanCommandPart{displayPart("plandb done t-1 --agent 1", "", true, false)}, Observation: "✓ t-1 done [0/0]"},
		{Step: 3, Command: "cd " + copy + " && go test ./...", Parts: []session.PlanCommandPart{displayPart("cd "+copy, " && ", false, true), displayPart("go test ./...", "", false, false)}, Observation: "ok"},
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"t-alpha": {Row: row, Folder: copy, Steps: steps}})
	if !openTaskPlaceWithRows(a) {
		t.Fatal("task place did not open")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	page := taskSheetText(a)
	// `files` IS ON THE NEVER LIST, AND IT USED TO BE WANTED. Step 1's row left
	// out a part addressed to the run's record, so the head cannot be told from
	// that part's print: one shell, one interleaved observation, and the head
	// belongs to the first part that PRINTED, not the first part shown. The step
	// carries the fact the engine sets for that shape ([session.PlanStep]'s
	// ObservationHeadWithheld), and the page draws no dim line under it. Step 3
	// left out only the change into the run's copy, joined so that its failure
	// ends the line, and keeps its `ok`.
	for _, never := range []string{copy, "t-1", "--agent 1", "✓ t-1", "files"} {
		if strings.Contains(page, never) {
			t.Fatalf("page contains %q:\n%s", never, page)
		}
	}
	shell := a.actionLead(session.ActionRun, true)
	for _, want := range []string{shell + "ls", shell + "go test ./...", "ok"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
}

func TestTaskPageDrawsObservationHeadOnlyWhenOmittedPartsCannotWriteIt(t *testing.T) {
	const copy = "/private/conversation/trees/1"
	for _, test := range []struct {
		name     string
		command  string
		parts    []session.PlanCommandPart
		head     string
		wantHead bool
	}{
		{
			name:    "record before work",
			command: "plandb task overview; printf work",
			parts:   []session.PlanCommandPart{displayPart("plandb task overview", "; ", true, false), displayPart("printf work", "", false, false)},
			head:    "record-before-print",
		},
		{
			name:    "record after work",
			command: "printf work; plandb task overview",
			parts:   []session.PlanCommandPart{displayPart("printf work", "; ", false, false), displayPart("plandb task overview", "", true, false)},
			head:    "record-after-print",
		},
		{
			name:    "record pipeline before sequenced work",
			command: "plandb task overview | head -1; printf work",
			parts:   []session.PlanCommandPart{displayPart("plandb task overview", " | ", true, false), displayPart("head -1", "; ", true, false), displayPart("printf work", "", false, false)},
			head:    "record-pipeline-print",
		},
		{
			name:     "only leading run copy omitted",
			command:  "cd " + copy + " && printf work",
			parts:    []session.PlanCommandPart{displayPart("cd "+copy, " && ", false, true), displayPart("printf work", "", false, false)},
			head:     "copy-safe-head",
			wantHead: true,
		},
		{
			name:     "nothing omitted",
			command:  "printf work",
			parts:    []session.PlanCommandPart{displayPart("printf work", "", false, false)},
			head:     "whole-row-head",
			wantHead: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "done", Folder: copy}
			step := session.PlanStep{Step: 1, Command: test.command, Parts: spannedParts(t, test.command, test.parts...), Observation: test.head, ObservationHeadWithheld: !test.wantHead}
			a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"t-alpha": {Row: row, Folder: copy, Steps: []session.PlanStep{step}}})
			if !openTaskPlaceWithRows(a) {
				t.Fatal("task place did not open")
			}
			drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
			page := taskSheetText(a)
			if strings.Contains(page, test.head) != test.wantHead {
				t.Fatalf("head presence = %v, want %v:\n%s", strings.Contains(page, test.head), test.wantHead, page)
			}
		})
	}
}
