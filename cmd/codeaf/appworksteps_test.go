package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A TASK'S WORK REACHES THE PAGE AS TOOL ENTRIES WITH THEIR FACTS.
//
// This is the defect in one test. A real task — a normal native one, seven steps
// and sixty-two tests in twenty-two seconds — reached tasks.get as seven QUIET
// NOTICES, because the page's log wrapped the engine's worded lines in
// [appwire.EntryNotice] whatever they were. An app that reads its work log by
// `kind == "tool"` therefore found nothing: no steps, a blank log, and a footer
// with nothing to count. Every fact needed to group, time and sort those steps
// was in the engine and was being spent on composing a sentence.
func TestATasksStepsReachThePageAsToolEntriesCarryingTheirFacts(t *testing.T) {
	started := time.Date(2026, 10, 15, 14, 20, 0, 0, time.UTC)
	exit := 0
	ran := session.AppTaskStep{
		ID: "c1", Call: "c1", Tool: "bash", Category: session.ActionRun,
		Command: "go test ./internal/reconciler/...", Args: `{"command":"go test ./internal/reconciler/..."}`,
		Detail: "ok internal/reconciler 22.004s\n62 tests passed", Tail: []string{"62 tests passed"},
		StartedAt: started, EndedAt: started.Add(22 * time.Second), Took: 22 * time.Second,
		ExitCode: &exit, OK: boolAt(true), Batch: "c1", Source: session.AppStepLive,
	}
	read := session.AppTaskStep{
		ID: "c2", Call: "c2", Tool: "read", Category: session.ActionRead,
		Target: "read internal/retry/backoff.go", Args: `{"path":"internal/retry/backoff.go"}`,
		StartedAt: started.Add(23 * time.Second), OK: boolAt(true), Source: session.AppStepLive,
	}
	conv := &fakeConversation{appTasks: []session.AppTaskView{{
		ID: "1", Title: "Fix the reconciler", State: "done", Result: "62 tests pass",
		Steps: []session.AppTaskStep{ran, read}, Step: 7, StepsKnown: true,
	}}}
	backend, _, agent := workBackend(t, conv)

	page, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	tools := entriesOfKind(page.Log, appwire.EntryTool)
	if len(tools) != 2 {
		t.Fatalf("the page carries %d tool entries out of %d log rows, want both steps: %+v",
			len(tools), len(page.Log), page.Log)
	}
	if notices := entriesOfKind(page.Log, appwire.EntryNotice); len(notices) != 0 {
		t.Fatalf("a typed step was drawn as a quiet notice: %+v", notices)
	}
	command := tools[0]
	if command.ToolFacts.Kind != appwire.ToolRun {
		t.Fatalf("the command's kind = %q, want the engine's run family", command.ToolFacts.Kind)
	}
	if command.Name != "bash" || command.Title != "go test ./internal/reconciler/..." {
		t.Fatalf("the command's row = name %q title %q", command.Name, command.Title)
	}
	if command.Call != "c1" || command.Batch != "c1" {
		t.Fatalf("the command lost its call id: %+v", command)
	}
	if command.StartedAt == nil || !command.StartedAt.Equal(started) ||
		command.EndedAt == nil || command.EndedAt.Sub(*command.StartedAt) != 22*time.Second {
		t.Fatalf("the command's clock = %v..%v, want the recorded 22s", command.StartedAt, command.EndedAt)
	}
	if command.ExitCode == nil || *command.ExitCode != 0 || command.OK == nil || !*command.OK {
		t.Fatalf("the command's outcome = exit %v ok %v", command.ExitCode, command.OK)
	}
	if command.Input == nil || command.Input.Text != "go test ./internal/reconciler/..." {
		t.Fatalf("the command's input = %+v", command.Input)
	}
	if len(command.Tail) != 1 || command.Tail[0] != "62 tests passed" {
		t.Fatalf("the command's tail = %q", command.Tail)
	}
	if command.ToolFacts.Task != agent.ID+":1" {
		t.Fatalf("a step names task %q, want its own task", command.ToolFacts.Task)
	}
	// A STEP WITH NO RECORDED END CLAIMS NONE. The second step was never
	// settled with a clock, and "now" must not be written in where the record
	// kept nothing.
	if tools[1].ToolFacts.Kind != appwire.ToolRead {
		t.Fatalf("the read's kind = %q, want the engine's read family", tools[1].ToolFacts.Kind)
	}
	if tools[1].EndedAt != nil {
		t.Fatalf("a step with no recorded end was given %s", tools[1].EndedAt)
	}
	if tools[1].ExitCode != nil {
		t.Fatalf("a read was given exit code %d; only a command reports one", *tools[1].ExitCode)
	}
	// AND THE FOOTER COUNTS THE TASK'S OWN STEPS, which is the figure the engine
	// recorded and not however many rows survived a filter.
	if page.Task.Steps == nil || *page.Task.Steps != 7 {
		t.Fatalf("the page's step count = %v, want the seven the engine recorded", page.Task.Steps)
	}
	// Two reads of the same record give a row the same id, so a polling page
	// does not grow a second copy of a step it already has.
	again, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	if ids(again.Log) != ids(page.Log) {
		t.Fatalf("a second read renamed the steps: %s then %s", ids(page.Log), ids(again.Log))
	}
}

// A STEP WHOSE RECORD HAS NO TOOL PAYLOAD IS STILL A LINE, drawn as the line it
// is and never classified into a kind it never declared. The gates check that a
// kind is present; nothing checks that a claim around it is true, so a legacy
// command dressed as a tool row would be a page asserting which hands were used
// on no evidence at all.
func TestALegacyStepStaysALineAndIsNeverDressedAsAToolRow(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{
		ID: "1", Title: "An older run", State: "done", StepsKnown: true, Step: 2,
		Steps: []session.AppTaskStep{
			{ID: "trajectory:1", Command: "$ echo one", Text: "$ echo one", Step: 1,
				Legacy: true, Source: session.AppStepTrajectory},
			{ID: "trajectory:2", Tool: "bash", Category: session.ActionRun, Command: "go vet ./...",
				Step: 2, Source: session.AppStepTrajectory},
		},
	}}}
	backend, _, agent := workBackend(t, conv)
	page, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Log) != 2 {
		t.Fatalf("the page drew %d rows, want both steps: %+v", len(page.Log), page.Log)
	}
	if page.Log[0].Kind != appwire.EntryNotice || !page.Log[0].Quiet {
		t.Fatalf("a legacy step was dressed as %q", page.Log[0].Kind)
	}
	if page.Log[0].Kind == appwire.EntryNotice && page.Log[0].ToolFacts.Kind != "" {
		t.Fatalf("a legacy line was given the kind %q it never declared", page.Log[0].ToolFacts.Kind)
	}
	if !strings.Contains(page.Log[0].Text, "echo one") {
		t.Fatalf("the legacy line lost its words: %q", page.Log[0].Text)
	}
	if page.Log[1].Kind != appwire.EntryTool || page.Log[1].ToolFacts.Kind != appwire.ToolRun {
		t.Fatalf("the typed step beside it = %+v", page.Log[1])
	}
	// NO RECORDED EXIT IS NOT A PASS, and neither step had one.
	if page.Log[1].OK != nil || page.Log[1].ExitCode != nil {
		t.Fatalf("a step with no recorded outcome was settled: %+v", page.Log[1])
	}
}

// A TASK WHOSE WORK WAS NEVER COUNTED SAYS NOTHING ABOUT IT, and one that really
// took no steps says zero. They were arriving as the same missing field, so a
// page either claimed "0 steps" about unknown history or stayed silent about a
// task that genuinely has taken none.
func TestAnUncountedTasksStepsAreAbsentAndACountedZeroIsSaid(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{
		{ID: "1", Title: "Unknown history", State: "done"},
		{ID: "2", Title: "Nothing done yet", State: "running", StepsKnown: true, Step: 0},
	}}
	backend, _, agent := workBackend(t, conv)
	unknown, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Task.Steps != nil {
		t.Fatalf("a task with no record of its work claims %d steps", *unknown.Task.Steps)
	}
	counted, err := backend.GetTask(agent.ID + ":2")
	if err != nil {
		t.Fatal(err)
	}
	if counted.Task.Steps == nil || *counted.Task.Steps != 0 {
		t.Fatalf("a task counted at no steps answers %v, want a said zero", counted.Task.Steps)
	}
}

// WHO LEFT A NOTE TRAVELS WITH IT. Every note was reaching the app with no
// origin at all, so a surface defaulting to "yours" put a worker's own handoff —
// and the account of the work it handed back — into the person's bubble as words
// they never typed.
func TestATaskNoteCarriesWhoSaidItAndAnUnlabelledOneClaimsNobody(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{
		ID: "1", Title: "Fix the reconciler", State: "done", Result: "62 tests pass",
		Notes: []session.AppTaskNote{
			{Text: "keep it small", From: session.AppNoteFromPerson},
			{Text: "Both runs passed: 62 tests, exit 0.", From: session.AppNoteFromTask},
			{Text: "a line from a record that never said"},
		},
	}}}
	backend, _, agent := workBackend(t, conv)
	page, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notes) != 3 {
		t.Fatalf("the page carries %d notes, want all three: %+v", len(page.Notes), page.Notes)
	}
	if page.Notes[0].From != appwire.NoteFromYou {
		t.Fatalf("the person's own note reads from %q", page.Notes[0].From)
	}
	if page.Notes[1].From != appwire.NoteFromTask {
		t.Fatalf("the work's own line reads from %q, want the task", page.Notes[1].From)
	}
	// AN ABSENT ORIGIN IS UNKNOWN AND NOT YOURS.
	if page.Notes[2].From != "" {
		t.Fatalf("an unlabelled note was attributed to %q", page.Notes[2].From)
	}
	// The result is the task's ONE ANSWER and is not repeated as a note.
	if page.Result != "62 tests pass" {
		t.Fatalf("the page's result = %q", page.Result)
	}
	for _, note := range page.Notes {
		if strings.TrimSpace(note.Text) == page.Result {
			t.Fatalf("the answer was said a second time as a note: %+v", note)
		}
	}
}

// AN ENGINE LANE THAT HAS NOT FILLED THE TYPED STEPS STILL HAS LINES A PERSON
// CAN READ, and they are said as the lines they are rather than dropped.
func TestATaskWithOnlyWordedLinesStillDrawsThem(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{
		ID: "1", Title: "An older engine", State: "done",
		WorkLog: []string{"read cache.go", "", "edit cache.go"},
	}}}
	backend, _, agent := workBackend(t, conv)
	page, err := backend.GetTask(agent.ID + ":1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Log) != 2 {
		t.Fatalf("the page drew %d rows, want the two lines with words: %+v", len(page.Log), page.Log)
	}
	for _, entry := range page.Log {
		if entry.Kind != appwire.EntryNotice || !entry.Quiet {
			t.Fatalf("a worded line was dressed as %q", entry.Kind)
		}
	}
}

func boolAt(v bool) *bool { return &v }

func entriesOfKind(log []appwire.Entry, kind string) []appwire.Entry {
	var out []appwire.Entry
	for _, entry := range log {
		if entry.Kind == kind {
			out = append(out, entry)
		}
	}
	return out
}

func ids(log []appwire.Entry) string {
	out := make([]string, 0, len(log))
	for _, entry := range log {
		out = append(out, entry.ID)
	}
	return strings.Join(out, " ")
}
