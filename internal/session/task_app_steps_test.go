package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A RUNNING NODE'S STEPS ARRIVE AS FACTS, NOT AS SENTENCES. This is the whole
// defect: the recorder kept worded lines, so the one surface that groups and
// times steps was handed prose and could say nothing about any of them. The
// tool, the family it belongs to, the command as it was spelled, the clock and
// the exit code all have to be readable off the step itself.
func TestALiveNodesStepsCarryTheirToolCommandClockAndExit(t *testing.T) {
	live := &taskLive{}
	started := time.Now()
	live.record(Event{Kind: EventToolBegin, Tool: "bash", CallID: "c1", Batch: "c1",
		Hint: "bash go test ./internal/reconciler/…", Args: `{"command":"go test ./internal/reconciler/..."}`})
	live.record(Event{Kind: EventToolFinished, Tool: "bash", CallID: "c1", Took: 22 * time.Second})
	exit := 0
	live.record(Event{Kind: EventToolEnd, Tool: "bash", CallID: "c1", Exit: &exit,
		Output: "ok  \tinternal/reconciler\t22.004s\n62 tests passed\n"})

	state := live.state(taskLiveMaxTail)
	if len(state.Calls) != 1 {
		t.Fatalf("the recorder kept %d typed steps, want the one call: %+v", len(state.Calls), state.Calls)
	}
	step := state.Calls[0]
	if step.Tool != "bash" || step.Category != ActionRun {
		t.Fatalf("the step's tool and family = %q/%q, want bash as a run", step.Tool, step.Category)
	}
	if step.Command != "go test ./internal/reconciler/..." {
		t.Fatalf("the command = %q, want the one the model spelled", step.Command)
	}
	if step.Call != "c1" || step.ID != "c1" || step.Batch != "c1" {
		t.Fatalf("the step's identity = %+v, want the provider's own call id", step)
	}
	if step.Took != 22*time.Second {
		t.Fatalf("the duration = %s, want the 22s the engine reported", step.Took)
	}
	if step.OK == nil || !*step.OK || step.Running {
		t.Fatalf("a call that ended cleanly reads ok=%v running=%v", step.OK, step.Running)
	}
	if step.ExitCode == nil || *step.ExitCode != 0 {
		t.Fatalf("exit code = %v, want the zero the result reported", step.ExitCode)
	}
	if step.StartedAt.Before(started) || step.EndedAt.Before(step.StartedAt) {
		t.Fatalf("the step's clock = %s..%s, want one that moves forward from %s", step.StartedAt, step.EndedAt, started)
	}
	if len(step.Tail) == 0 || !strings.Contains(strings.Join(step.Tail, "\n"), "62 tests passed") {
		t.Fatalf("the tail = %q, want what the command printed", step.Tail)
	}
	if step.Source != AppStepLive {
		t.Fatalf("the step says it came from %q, want the live ring", step.Source)
	}
	// AND THE WORDED TAIL IS STILL THERE. A model asking what a task is doing
	// reads sentences; both records are written from the same events.
	if len(state.Lines) == 0 {
		t.Fatal("the worded ring lost its lines when the typed one was added")
	}
}

// A STEP STILL RUNNING ASSERTS NOTHING ABOUT HOW IT WENT, and a refused one is
// a settled failure the record names as refused rather than as a sentence.
func TestARunningStepIsUnsettledAndARefusedOneIsNamed(t *testing.T) {
	live := &taskLive{}
	live.record(Event{Kind: EventToolBegin, Tool: "read", CallID: "c1", Hint: "read internal/retry/backoff.go"})
	running := live.state(taskLiveMaxTail).Calls
	if len(running) != 1 || !running[0].Running || running[0].OK != nil || running[0].Settled() {
		t.Fatalf("a running step reads %+v, want no outcome at all", running)
	}
	if running[0].ExitCode != nil {
		t.Fatalf("a running step was given exit code %v; unknown is not zero", *running[0].ExitCode)
	}

	live.record(Event{Kind: EventToolFailed, Tool: "bash", CallID: "c2",
		Hint: `denied by the person: critical command "rm*"`, HarnessMade: true, Refused: true})
	steps := live.state(taskLiveMaxTail).Calls
	if len(steps) != 2 {
		t.Fatalf("the recorder kept %d steps, want both calls", len(steps))
	}
	refused := steps[1]
	if !refused.Refused || !refused.HarnessMade {
		t.Fatalf("the refusal lost its marks: %+v", refused)
	}
	if !refused.Failed() {
		t.Fatalf("a refused call reads ok=%v, want a settled failure", refused.OK)
	}
	if refused.ExitCode != nil {
		t.Fatal("a call a door refused was given an exit code it never earned")
	}
}

// THE RING IS BOUNDED AND ITS INDEX STAYS CORRECT. A node that takes thousands
// of steps must not hold them all, and the step that falls off the front must
// not leave its call id pointing at somebody else's row.
func TestTheTypedStepRingIsBoundedAndKeepsItsIndexTrue(t *testing.T) {
	live := &taskLive{}
	for i := 0; i < appTaskStepsMax+50; i++ {
		id := "c" + strconv.Itoa(i)
		live.record(Event{Kind: EventToolBegin, Tool: "read", CallID: id, Hint: "read a.go"})
		live.record(Event{Kind: EventToolEnd, Tool: "read", CallID: id, Output: "a"})
	}
	steps := live.state(taskLiveMaxTail).Calls
	if len(steps) != appTaskStepsMax {
		t.Fatalf("the ring holds %d steps, want its bound of %d", len(steps), appTaskStepsMax)
	}
	if steps[len(steps)-1].ID != "c"+strconv.Itoa(appTaskStepsMax+49) {
		t.Fatalf("the newest step is %q, want the last call", steps[len(steps)-1].ID)
	}
	live.mu.Lock()
	for call, at := range live.stepAt {
		if at < 0 || at >= len(live.calls) {
			t.Fatalf("call %s indexes step %d, outside the %d the ring holds", call, at, len(live.calls))
		}
		if live.calls[at].step.Call != call {
			t.Fatalf("call %s indexes a row belonging to %s", call, live.calls[at].step.Call)
		}
	}
	live.mu.Unlock()
}

// A RESTORED NODE READS ITS OWN JOURNAL AS FACTS. This is what a task page has
// after a restart: no recorder at all, and a durable file that has kept the
// call's tool, arguments, result and duration all along.
func TestAJournaledCallComesBackAsAStepAndNotAsAGluedLine(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	record := Record{Entries: []DisplayEntry{
		{Role: "assistant", Text: "I will run the tests."},
		{Role: "tool", Tool: "bash", CallID: "c1", Hint: "bash go test ./…", At: at, Took: 22 * time.Second,
			Args: `{"command":"go test ./..."}`, Output: "62 tests passed\n", Answered: true},
		{Role: "tool", Tool: "read", CallID: "c2", Hint: "read a.go", At: at, Args: `{"path":"a.go"}`},
	}}
	steps := appStepsOfRecord(record)
	if len(steps) != 2 {
		t.Fatalf("the record shaped %d steps, want its two calls", len(steps))
	}
	ran := steps[0]
	if ran.Tool != "bash" || ran.Command != "go test ./..." || ran.Took != 22*time.Second {
		t.Fatalf("the journaled command = %+v, want its tool, command and duration", ran)
	}
	if ran.OK == nil || !*ran.OK {
		t.Fatalf("an answered call reads ok=%v, want the result the file holds", ran.OK)
	}
	if !ran.StartedAt.Equal(at) || !ran.EndedAt.Equal(at.Add(22*time.Second)) {
		t.Fatalf("the journaled clock = %s..%s, want it derived from the instant the file kept", ran.StartedAt, ran.EndedAt)
	}
	if ran.Source != AppStepJournal {
		t.Fatalf("the step says it came from %q, want the journal", ran.Source)
	}
	// AN UNANSWERED CALL IS UNSETTLED, NOT FAILED, and it claims no end.
	unanswered := steps[1]
	if unanswered.Settled() {
		t.Fatalf("a call with no result in the file reads ok=%v, want nothing", unanswered.OK)
	}
	if !unanswered.EndedAt.IsZero() {
		t.Fatalf("a call with no duration was given the end %s", unanswered.EndedAt)
	}
}

// A CALL WITH NO TOOL IS A LINE AND STAYS ONE. The oldest records kept words and
// nothing else; classifying one would mean guessing which hands were used from a
// sentence, which is how a page comes to assert what its record never held.
//
// AND A RESULT MESSAGE IS NOT A STEP. The shaping keeps a row for a call's
// result with no id and no name; counted as a step it doubles every call a
// record holds.
func TestARecordWithNoToolPayloadStaysTheLineItIs(t *testing.T) {
	steps := appStepsOfRecord(Record{Entries: []DisplayEntry{
		{Role: "tool", CallID: "c1", Hint: "ran the suite", Text: "all green", Answered: true},
		{Role: "tool", Text: "all green"},
	}})
	if len(steps) != 1 {
		t.Fatalf("the record shaped %d steps, want its one line and not its result row", len(steps))
	}
	if !steps[0].Legacy {
		t.Fatalf("a call with no tool was classified: %+v", steps[0])
	}
	if steps[0].Category != ActionWork || steps[0].Tool != "" {
		t.Fatalf("a legacy line was given the tool %q and family %q", steps[0].Tool, steps[0].Category)
	}
	if !strings.Contains(steps[0].Text, "ran the suite") {
		t.Fatalf("the legacy line lost its words: %q", steps[0].Text)
	}
}

// A HANDED-OVER RUN'S TRAJECTORY KEEPS MORE THAN A COMMAND, and the read now
// takes all of it: before this, the one field read back was the command, so a
// run's page lost which tool was used, whether the step ran, and its exit.
func TestARunsTrajectoryIsReadBackWholeAndNeverInvented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trajectory.jsonl")
	lines := strings.Join([]string{
		`{"kind":"begin","exits_recorded":true}`,
		`{"kind":"step","step":1,"tool":"bash","command":"go test ./...","observation":"62 tests passed","exit_code":0}`,
		`{"kind":"notes","notes":["n1"]}`,
		`{"kind":"step","step":2,"tool":"bash","command":"rm -rf dist","not_run":true,"refused":true}`,
		`{"kind":"step","step":3,"tool":"bash","command":"go vet ./..."}`,
		`{"kind":"step","step":4,"command":"an older record's command"}`,
		`{"kind":"end","steps":4,"result":"done"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	steps, reads := appTrajectorySteps(path, appTaskStepsMax)
	if len(steps) != 4 {
		t.Fatalf("the trajectory read %d steps, want its four: %+v", len(steps), steps)
	}
	if steps[0].Tool != "bash" || steps[0].Command != "go test ./..." || steps[0].Step != 1 {
		t.Fatalf("the first step = %+v, want its tool, command and number", steps[0])
	}
	if steps[0].ExitCode == nil || *steps[0].ExitCode != 0 || steps[0].OK == nil || !*steps[0].OK {
		t.Fatalf("a recorded zero exit read as %+v", steps[0])
	}
	if !steps[1].Refused || !steps[1].Failed() {
		t.Fatalf("a refused step read as %+v, want a settled failure", steps[1])
	}
	// NO RECORDED EXIT IS NOT A PASS. A record written before exits were kept
	// says the step happened and nothing about how it came out.
	if steps[2].Settled() || steps[2].ExitCode != nil {
		t.Fatalf("a step with no recorded exit was settled as %+v", steps[2])
	}
	if !steps[3].Legacy || steps[3].Text != "an older record's command" {
		t.Fatalf("a trajectory line with no tool was classified: %+v", steps[3])
	}
	if reads["n1"] != 1 {
		t.Fatalf("the note was read at step %d, want 1", reads["n1"])
	}
}

// THE PROJECTION IS CACHED AGAINST THE FILE'S OWN FINGERPRINT. A task page
// polls, and reshaping a whole journal every second for a page that has not
// changed is the cost that makes a correct projection unusable.
func TestAJournalIsReshapedOnlyWhenTheFileItselfMoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &appStepStore{parse: map[string]appStepParse{}}
	parses := 0
	read := func() []AppTaskStep {
		return store.read(path, func() []AppTaskStep {
			parses++
			return []AppTaskStep{{ID: "only"}}
		})
	}
	for i := 0; i < 5; i++ {
		if steps := read(); len(steps) != 1 {
			t.Fatalf("the cached read answered %+v", steps)
		}
	}
	if parses != 1 {
		t.Fatalf("an unchanged file was reshaped %d times, want once", parses)
	}
	// The file moves underneath it: the parse is not reused.
	if err := os.WriteFile(path, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	read()
	if parses != 2 {
		t.Fatalf("a changed file was reshaped %d times, want a second pass", parses)
	}
	// A FILE WITH NO FINGERPRINT IS NEVER SERVED FROM A PARSE. Nothing may hand
	// back a reading about a file it cannot see.
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	for i := 0; i < 2; i++ {
		store.read(missing, func() []AppTaskStep { parses++; return nil })
	}
	if parses != 4 {
		t.Fatalf("a file with no fingerprint was cached: %d parses", parses)
	}
}

// THE NODE'S VIEW PREFERS ITS LIVE RING AND FALLS BACK TO ITS JOURNAL, and the
// two are never merged: a restarted task whose steps were counted twice would
// have no field able to say which copy is which.
func TestATasksStepCountSaysKnownOrNothingAndNeverAFalseZero(t *testing.T) {
	// A node with no record of its work at all.
	var view AppTaskView
	appTaskSteps(&view, taskLiveState{}, "")
	if view.StepsKnown || view.Step != 0 || len(view.Steps) != 0 {
		t.Fatalf("a task with no record claims %+v, want nothing at all", view)
	}

	// A running node: the recorder's own count, which is exact.
	live := taskLiveState{Steps: 7, Calls: []AppTaskStep{{ID: "c1", Tool: "bash", Source: AppStepLive}}}
	view = AppTaskView{}
	appTaskSteps(&view, live, "")
	if !view.StepsKnown || view.Step != 7 || len(view.Steps) != 1 {
		t.Fatalf("a running node reads %+v, want its recorder's seven", view)
	}

	// A restored node: no recorder, and a journal that holds the work.
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	writeStepJournal(t, path)
	view = AppTaskView{}
	appTaskSteps(&view, taskLiveState{}, path)
	if !view.StepsKnown || view.Step != 2 || len(view.Steps) != 2 {
		t.Fatalf("a restored node reads %+v, want the two steps its file holds", view)
	}
	if view.Steps[0].Source != AppStepJournal {
		t.Fatalf("a restored node's steps came from %q, want its journal", view.Steps[0].Source)
	}
}

// writeStepJournal writes a two-call session file in the SHAPE THE PRODUCT
// WRITES, through the journal's own line type rather than a JSON string spelled
// out here, so the fixture cannot drift away from the format the reader reads.
// It asserts its own reading, because a fixture the reader finds empty would
// make every test below it pass by saying nothing.
func writeStepJournal(t *testing.T, path string) {
	t.Helper()
	at := time.Now().UTC().Format(time.RFC3339Nano)
	ask := func(call, tool, args string) sessionEntry {
		return sessionEntry{Type: "message", Role: "assistant", Timestamp: at,
			ToolCalls: []ai.ToolCall{{ID: call, Type: "function",
				Function: ai.ToolCallFunction{Name: tool, Arguments: args}}}}
	}
	answer := func(call, text string) sessionEntry {
		return sessionEntry{Type: "message", Role: "tool", ToolCallID: call, Content: text, Timestamp: at}
	}
	var body strings.Builder
	for _, entry := range []sessionEntry{
		ask("c1", "bash", `{"command":"go test ./..."}`),
		answer("c1", "62 tests passed"),
		ask("c2", "read", `{"path":"a.go"}`),
		answer("c2", "package main"),
	} {
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(raw)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if steps := appJournalSteps(path, appTaskStepsMax); len(steps) != 2 {
		t.Fatalf("the written journal shaped %d steps, want two — the fixture is wrong, not the reader", len(steps))
	}
}
