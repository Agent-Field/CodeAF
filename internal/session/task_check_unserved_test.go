package session

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// run21Stop is the sentence internal/run now ends run21's rota check on: its
// checker's provider refused every shape of the request, which is a fact about
// the checker and never about the rota (internal/run's checkStopSentence).
const run21Stop = `the check could not finish: the provider would not serve its model google/gemini-4-carbon and said ` +
	`"Your request contains encrypted reasoning or compaction content that can only be processed by the provider that created it (google-ai-studio), but your request's provider.ignore preference excludes that provider."`

// run21Guess is what run21's reading made up when the failed check's row
// carried no reason, and what the manager then told the person.
const run21Guess = "The run ended failed — the Rota tab holds 14 shift rows, but role cells are left reading Open."

// run21Fixture is the rota run through the real belt door: the worker built the
// rota and landed it, and the engine answers done with the check's stop
// carried typed. The reading the run buys at its landing is scripted to say
// run21's guess, so the test shows the run's own account reaching the
// conversation ahead of whatever a reading says.
func run21Fixture(t *testing.T) (*Agent, uint64, *scriptedCompleter) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := t.TempDir()
	client := &scriptedCompleter{steps: []step{finalText("what: a rota for next week\nsince: nothing\nnow: " + run21Guess + "\nnext: Nothing needs you.")}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.Place = Place{Dir: place}
		c.AskConsent = false
	})
	g := agent.graph()
	id := g.reserve()
	store, err := plandb.Open(g.planPath(), "run21", strconv.FormatUint(id, 10), "Build next week's shift rota", "shift rota for next week", g.planChat())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	result := "Rota sheet tabs Rota Coverage Hours built. Rota: header plus 14 shift rows, five role cells each a name or Open"
	if err := store.CompleteRoot(result); err != nil {
		t.Fatal(err)
	}
	agent.publishRunRow(g, TaskNotice{ID: id, Title: "Build next week's shift rota", State: TaskRunning, StartedAt: time.Now().Add(-5 * time.Minute), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
	run := &beltRun{store: store, root: strconv.FormatUint(id, 10), row: id, title: "Build next week's shift rota"}
	agent.driveBeltRun(context.Background(), landingRunDouble{
		summary: RunSummary{Outcome: beltRunOutcomeDone, Result: result, Unchecked: run21Stop, USD: 0.09},
	}, run, RunSpec{})
	return agent, id, client
}

// THE RUN21 ROW: the rota was built, its check's model could not be served,
// and the chat drew "Build next week's shift rota · failed" in red. The row
// now asks the check's question — your call, nobody could check it — led by
// the real stop.
func TestRun21FinishedWorkWhoseCheckCouldNotBeServedIsYourCall(t *testing.T) {
	agent, id, _ := run21Fixture(t)
	kept, found := runRowOf(agent.graph(), id)
	if !found {
		t.Fatal("the run's row is gone")
	}
	if kept.State != TaskUnverified || !kept.Unchecked {
		t.Fatalf("row = %s unchecked=%v (%q), want your call over landed work", kept.State, kept.Unchecked, kept.Report)
	}
	if !strings.HasPrefix(kept.Report, run21Stop) {
		t.Fatalf("report = %q, want the real stop as its lead", kept.Report)
	}
	project := ProjectTask(kept.StatusFacts())
	if project.Word != taskWordYourCall || project.Ask.Kind != TaskAskCheck || project.Ask.Reason != taskAskCheckReason {
		t.Fatalf("person reads %q / %+v, want your call · %s", project.Word, project.Ask, taskAskCheckReason)
	}
	if strings.Contains(strings.ToLower(project.Word+" "+project.Ask.Reason), "fail") {
		t.Fatalf("finished work still reads as failed: %+v", project)
	}
	// The check is not asked again from here, and the refusal says why in the
	// stop's own terms rather than the ceiling's.
	if err := agent.ResolveUnverified(id, TaskReaudit, ""); err == nil || !strings.Contains(err.Error(), "could not finish") {
		t.Fatalf("check again = %v, want a refusal saying the check could not finish", err)
	}
	if err := agent.ResolveUnverified(id, TaskAccept, "the rota looks right"); err != nil {
		t.Fatal(err)
	}
	if kept, _ := runRowOf(agent.graph(), id); kept.State != TaskDone {
		t.Fatalf("accepted row = %s, want done", kept.State)
	}
}

// THE MANAGER IS HANDED THE REAL REASON. The note a run's ending puts in the
// conversation is what the manager reads and repeats; in run21 it carried
// only "ran and did not finish" and a reading's guess. It now says what the
// row says — your call — and the check's own recorded stop, and a reading's
// line about how the run ended no longer rides beside a recorded one.
func TestRun21TheManagerIsToldTheRecordedStopNotAGuess(t *testing.T) {
	agent, _, _ := run21Fixture(t)
	var note string
	for _, entry := range agent.Transcript() {
		if entry.RoutineCompletion {
			note = entry.Text
		}
	}
	if note == "" {
		t.Fatal("the run's ending reached the conversation with no note")
	}
	t.Logf("the note the manager reads: %s", note)
	if !strings.HasPrefix(note, taskWordYourCall+" · ") {
		t.Fatalf("note = %q, want it to open on the row's own word", note)
	}
	if !strings.Contains(note, run21Stop) {
		t.Fatalf("note = %q, want the check's recorded stop in it", note)
	}
	if strings.Contains(note, run21Guess) {
		t.Fatalf("note = %q, a reading's guess still rode beside the recorded stop", note)
	}
	if strings.Contains(note, "did not finish ·") || strings.HasPrefix(note, "done") {
		t.Fatalf("note = %q, want neither failed nor done words over unchecked work", note)
	}
}

// AND THE READING IS FED THE STOP. A row that ended without a result now
// carries the reason it recorded, so the model writing the run's lines has a
// cause to name and is told never to supply one of its own.
func TestRun21TheReadingSeesTheStopAFailedRowRecorded(t *testing.T) {
	check := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "y4xjr9", Title: "check: Build next week's shift rota"}, Status: plandb.StatusFailed,
		Error: "after 0 retries: no provider can serve google/gemini-4-carbon — refused after 4 attempts"}
	work := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "1", Title: "Build next week's shift rota"}, Status: plandb.StatusDone, Result: "Rota sheet tabs Rota Coverage Hours built"}
	input := runSummaryInput([]*plandb.Task{work, check}, nil, "1", time.Time{}, time.Now(), RunPlanSummary{})
	if !strings.Contains(input, "check: Build next week's shift rota · failed · stopped: after 0 retries: no provider can serve google/gemini-4-carbon") {
		t.Fatalf("reading input = %q, want the failed row's recorded stop on it", input)
	}
	if !strings.Contains(runSummaryPrompt, "never supply a cause") {
		t.Fatal("the reading's page no longer forbids a cause no row records")
	}
}

// A run that did not finish, whose ending names no reason of its own, says the
// stop its failed task recorded — and a run that recorded none says none.
func TestARunThatDidNotFinishSaysTheStopItsTaskRecorded(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.db", "run", "1", "Build next week's shift rota", "shift rota")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failed := RunSummary{Outcome: "ran and did not finish", Result: "Rota built"}
	if got := runRecordedStop(store, store.RootID(), failed); got != "" {
		t.Fatalf("stop = %q from a store that recorded none, want nothing invented", got)
	}
	if err := store.EndRoot("the provider refused the request"); err != nil {
		t.Fatal(err)
	}
	note := beltRunOutcomeNote(store, store.RootID(), failed, RunLanding{}, 0)
	if note != "ran and did not finish · Rota built · the provider refused the request" {
		t.Fatalf("note = %q, want the recorded stop said once after the result", note)
	}
	failed.Failure = "the run's own words"
	if got := runRecordedStop(store, store.RootID(), failed); got != "" {
		t.Fatalf("stop = %q beside an ending that already says why, want it said once", got)
	}
}
