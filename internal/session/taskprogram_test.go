package session

// A program's work says which program has it (task_contract.go's
// [TaskNotice.Program]): on the proposal a person answers, on the first row its
// run publishes and every row after it, in the checkpoint a reopened
// conversation redraws from, in the project's index every other reader draws
// from, and in the tasks tool's own words. The surfaces draw a badge from it
// (internal/tui3's programbadge.go); these tests hold the engine to carrying it.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// THE PROPOSAL NAMES THE PROGRAM, so the card a person approves can say where the
// work is going; the question every reader of a proposal gets says it in words;
// and a task no program is handed asks exactly the sentence it always asked.
func TestAProposalNamesTheProgramItIsGoingTo(t *testing.T) {
	spec := taskSpec{title: "rewrite the auth middleware", summary: "swap the session store", via: "senior-dev"}
	question := newTaskQuestion(7, spec, "", time.Time{}, Config{})
	if question.notice.Program != "senior-dev" {
		t.Fatalf("the proposal's notice names program %q, want senior-dev", question.notice.Program)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	asked := agent.proposalQuestion(7, question.notice)
	if want := "wants to start a [senior-dev] task: rewrite the auth middleware"; asked.Head != want {
		t.Fatalf("the proposal asks %q, want %q", asked.Head, want)
	}
	if asked.Subject.Name != "rewrite the auth middleware" {
		t.Fatalf("the proposal's subject is %q, want the task's title alone", asked.Subject.Name)
	}

	spec.via = ""
	ordinary := newTaskQuestion(8, spec, "", time.Time{}, Config{})
	if ordinary.notice.Program != "" {
		t.Fatalf("an ordinary proposal names program %q", ordinary.notice.Program)
	}
	if got := agent.proposalQuestion(8, ordinary.notice).Head; got != TaskProposalLead+"rewrite the auth middleware" {
		t.Fatalf("an ordinary proposal asks %q", got)
	}
	if ProgramBadge("  ") != "" || ProgramBadge("doc-writer") != "[doc-writer]" {
		t.Fatalf("the badge's spelling is %q / %q", ProgramBadge("  "), ProgramBadge("doc-writer"))
	}
}

// A TYPED `/<name>` RUN NAMES ITS PROGRAM FROM ITS FIRST ROW TO ITS LAST, and so
// does its row in the project's index: the hand-off's running row, and the row
// that settles it — which the landing publishes without restating the program,
// so it reaches the settled row only because [Agent.publishRunRow] carries it.
// Another conversation's tasks tool says it in words.
func TestAProgramsRunNamesItsProgramFromTheHandOffToTheIndex(t *testing.T) {
	double := newBeltRunDouble("submitted and verified")
	registerBeltRunEngine(t, double)
	bucket := t.TempDir()
	workspace := newTestRepo(t)
	conversation := func(name string) *Agent {
		dir := filepath.Join(bucket, name)
		agent, _ := newTestAgent(t, beltRunCompleter{text: "submitted and verified"}, func(config *Config) {
			config.Workspace = workspace
			config.Place = Place{Dir: dir}
			config.SessionFile = filepath.Join(dir, placeTranscript)
			config.AskConsent = false
			config.Delegates = testPrograms("fake")
		})
		return agent
	}
	runner, other := conversation("runner"), conversation("other")

	id, _, _, err := runner.StartDelegate(context.Background(), "fake", "add two files to the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	key := strconv.FormatUint(id, 10)
	row, ok := runRowOf(runner.graph(), id)
	if !ok || row.Program != "fake" || row.State != TaskRunning {
		t.Fatalf("the run's first row = %+v, want it running and naming fake", row)
	}
	if entry := indexRowFor(t, runner, key); entry.Program != "fake" {
		t.Fatalf("the index's running row = %+v, want it naming fake", entry)
	}
	found, failed := runTool(t, other, "tasks", `{"query":"two files"}`)
	if failed || !strings.Contains(found, "via fake") {
		t.Fatalf("the other conversation's tasks tool answered %q (failed %v), want the program named", found, failed)
	}

	endBeltRun(t, runner, double)
	row, ok = runRowOf(runner.graph(), id)
	if !ok || !row.State.settled() || row.Program != "fake" {
		t.Fatalf("the run's settled row = %+v, want it still naming fake", row)
	}
	if entry := indexRowFor(t, runner, key); !TaskState(entry.Status).settled() || entry.Program != "fake" {
		t.Fatalf("the index's closing row = %+v, want it settled and naming fake", entry)
	}
}

// EVERY PUBLISH AFTER THE FIRST CARRIES THE PROGRAM FORWARD, the way it carries
// the copy: a stop, a landing and a carry-on each publish a row that knows
// nothing about which program had the work, and the row they replace is the only
// place that fact was. A row that names a program of its own is not overruled,
// and a run no program had gains none.
func TestAPublishCarriesTheProgramForward(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	g := agent.graph()
	agent.publishRunRow(g, TaskNotice{ID: 11, Title: "the program's run", State: TaskRunning, Program: "fake"})
	agent.publishRunRow(g, TaskNotice{ID: 11, Title: "the program's run", State: TaskDone})
	if row, _ := runRowOf(g, 11); row.Program != "fake" {
		t.Fatalf("the settled row = %+v, want the program carried forward", row)
	}
	agent.publishRunRow(g, TaskNotice{ID: 12, Title: "a bash worker's run", State: TaskRunning})
	agent.publishRunRow(g, TaskNotice{ID: 12, Title: "a bash worker's run", State: TaskDone})
	if row, _ := runRowOf(g, 12); row.Program != "" {
		t.Fatalf("a run no program had names %q", row.Program)
	}
}

// THE CHECKPOINT KEEPS THE PROGRAM, so a conversation reopened tomorrow redraws
// its program's run wearing its badge before any plan row is read; and a record
// written before the field existed reads back as no program at all.
func TestARunsRecordKeepsItsProgram(t *testing.T) {
	notice := TaskNotice{ID: 7, Title: "rewrite the auth middleware", State: TaskDone, Program: "senior-dev"}
	record := runRowRecord(notice)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"program":"senior-dev"`) {
		t.Fatalf("the record is %s, want the program written down", encoded)
	}
	var read runRecord
	if err := json.Unmarshal(encoded, &read); err != nil {
		t.Fatal(err)
	}
	if back := runRowNotice(read); back.Program != "senior-dev" {
		t.Fatalf("the restored row = %+v, want it naming senior-dev", back)
	}
	var older runRecord
	if err := json.Unmarshal([]byte(`{"id":7,"title":"x","state":"done"}`), &older); err != nil {
		t.Fatal(err)
	}
	if runRowNotice(older).Program != "" {
		t.Fatal("a record with no program read back as one")
	}
	if ordinary, _ := json.Marshal(runRowRecord(TaskNotice{ID: 8, State: TaskDone})); strings.Contains(string(ordinary), "program") {
		t.Fatalf("an ordinary run's record is %s, which writes an empty program", ordinary)
	}
}

// THE TASKS TOOL SAYS WHICH PROGRAM HAS THE WORK, as the last fact on a row's
// first line, in the word propose_task hands work to one with — and says
// nothing of the kind about a task no program had.
func TestTheTasksToolSaysWhichProgramHasTheWork(t *testing.T) {
	entry := TaskIndexEntry{ID: "7", Name: "rewrite-the-auth", Title: "rewrite the auth middleware", Status: string(TaskDone), Program: "senior-dev"}
	first, _, _ := strings.Cut(taskRowText(entry), "\n")
	if !strings.HasSuffix(first, " · via senior-dev") || !strings.HasPrefix(first, "7 · rewrite-the-auth · ") {
		t.Fatalf("a program's row reads %q, want it ending in the program", first)
	}
	entry.Program = ""
	if text := taskRowText(entry); strings.Contains(text, "via") {
		t.Fatalf("an ordinary row reads %q", text)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	listing := agent.planTasksText([]PlanTaskRow{
		{ID: "t-7", Title: "rewrite the auth middleware", Status: "running", Program: "senior-dev"},
		{ID: "t-8", Title: "an ordinary task", Status: "done"},
	}, "")
	lines := strings.Split(strings.TrimSpace(listing), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "running · via senior-dev") || strings.Contains(lines[1], "via") {
		t.Fatalf("the run's listing reads:\n%s", listing)
	}
}
