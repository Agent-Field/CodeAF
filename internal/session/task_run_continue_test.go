package session

// CARRYING ON A RUN NOTHING IS DRIVING.
//
// The first test is the one this cell exists for, and it is a test about
// DESTRUCTION rather than about a door: work is put in a run's copy, the run is
// carried on, and the work is still there afterwards. The road this door could
// have taken instead deletes it — task_run_copy_test.go shows that road doing
// it — so this test is the difference between the feature and a data loss with
// a friendly sentence on it.
//
// The rest pin the four refusals. Each asserts TWICE: that the door said no in
// words that name what is in the way, and that NOTHING STARTED. The second half
// is the half that matters, because a door that refuses in words and seats
// workers anyway has spent the money either way.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// continueAgent is a conversation with a repository, a place to keep a plan in,
// and the engine double seated — everything the door reads and nothing else.
func continueAgent(t *testing.T) (*Agent, *TaskGraph, string, *beltRunDouble) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run carried on")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("carried on")}}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
	})
	agent.taskNow = (&fakeClock{at: time.Date(2026, time.September, 20, 9, 0, 0, 0, time.UTC)}).now

	g := agent.graph()
	if g == nil {
		t.Fatal("the conversation has no graph, so there are no run rows to carry on")
	}
	return agent, g, repo, double
}

// keepInterruptedRun puts a row in the graph the way a conversation read back
// from disk holds one: interrupted, with whatever copy it was recorded with.
func keepInterruptedRun(t *testing.T, agent *Agent, g *TaskGraph, row uint64, title string, record *TaskCopyRecord) {
	t.Helper()
	agent.publishRunRow(g, TaskNotice{
		ID: row, Title: title, State: TaskInterrupted, Copy: record,
		StartedAt: agent.taskClockNow(),
	})
}

// nothingStarted is the half of every refusal that costs money if it is wrong.
func nothingStarted(t *testing.T, agent *Agent, double *beltRunDouble) {
	t.Helper()
	if double.didRun() {
		t.Fatal("the door refused in words and started a run anyway, which has spent the money either way")
	}
	agent.beltMu.Lock()
	live := agent.beltRun
	agent.beltMu.Unlock()
	if live != nil {
		t.Fatalf("the door refused and installed a run on the conversation anyway: %d", live.row)
	}
}

// THE WORK IN THE COPY SURVIVES BEING CARRIED ON. This is the whole cell. A run
// is recorded as working in a real copy, a worker's file is put in it, and the
// run is carried on: the file is there afterwards, and the run picked up in the
// SAME directory rather than in a fresh one.
//
// The control is in task_run_copy_test.go, which shows the other road clearing
// that directory. If this door ever reaches for it, this test goes red on the
// first line that reads the file back.
func TestCarryingOnARunKeepsEveryStepOfTheWorkInItsCopy(t *testing.T) {
	agent, g, repo, double := continueAgent(t)

	tree, err := prepareTaskTree(agent.config.Place, repo, "1111bbbb1111bbbb", 7, "port the parser")
	if err != nil {
		t.Fatalf("cutting the run's copy: %v", err)
	}
	work := filepath.Join(tree.dir, "the-work.txt")
	if err := os.WriteFile(work, []byte("every step of it\n"), 0o644); err != nil {
		t.Fatalf("writing the run's work: %v", err)
	}
	keepInterruptedRun(t, agent, g, 7, "port the parser", runCopyOf(tree))

	answer, err := agent.ContinueRun(context.Background(), 7)
	if err != nil {
		t.Fatalf("carrying on a run whose copy is right there: %v", err)
	}
	if !strings.Contains(answer, "carrying on") {
		t.Fatalf("the answer does not say what happened: %q", answer)
	}

	beltRunWaitFor(t, "the carried-on run to start", double.didRun)
	kept, readErr := os.ReadFile(work)
	if readErr != nil {
		t.Fatalf("the work in the run's copy is gone: %v", readErr)
	}
	if string(kept) != "every step of it\n" {
		t.Fatalf("the work in the run's copy was rewritten: %q", kept)
	}

	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	if workspace != tree.dir {
		t.Fatalf("the carried-on run is typing in %q, not in the copy its work is in (%q)", workspace, tree.dir)
	}

	// AND THE ROW IS RUNNING AGAIN, STILL NAMING ITS COPY, so a second interruption
	// can be carried on the same way. A row that came back without it would be a
	// run nobody can pick up twice.
	rows := g.runRows(7)
	if len(rows) != 1 {
		t.Fatalf("%d rows kept for one run", len(rows))
	}
	if rows[0].State != TaskRunning {
		t.Fatalf("the carried-on row reads %s", rows[0].State)
	}
	if rows[0].Copy == nil || rows[0].Copy.Dir != tree.dir {
		t.Fatalf("the carried-on row lost the copy it is working in: %+v", rows[0].Copy)
	}

	endBeltRun(t, agent, double)
}

// A RUN THAT WAS CARRIED ON IS STILL ONE A PERSON CAN STOP, by the number its
// row wears and through the ordinary stop road.
//
// This is the stop law's own demand of any function that publishes a running row
// (stoplaw_test.go), and it is the right demand: a run restarted by a door whose
// owner nothing could reach would be work nobody can end, which is worse than
// work nobody can start.
func TestAStopReachesARunThatWasCarriedOn(t *testing.T) {
	agent, g, repo, double := continueAgent(t)
	// The double ends the way the real engine ends when its context is cut,
	// rather than waiting on a release nobody is going to send.
	double.honoursStop = true

	tree, err := prepareTaskTree(agent.config.Place, repo, "2222cccc2222cccc", 7, "port the parser")
	if err != nil {
		t.Fatalf("cutting the run's copy: %v", err)
	}
	keepInterruptedRun(t, agent, g, 7, "port the parser", runCopyOf(tree))
	if _, err := agent.ContinueRun(context.Background(), 7); err != nil {
		t.Fatalf("carrying the run on: %v", err)
	}
	beltRunWaitFor(t, "the carried-on run to start", double.didRun)

	line, err := agent.Cancel(CancelTask + ":7")
	if err != nil {
		t.Fatalf("the stop a surface sends for a carried-on run's row was refused: %v", err)
	}
	if !strings.HasPrefix(line, "stopping ") {
		t.Fatalf("the stop answered %q, want the sentence every stopped task answers", line)
	}
	if word := carriesMachinery(line); word != "" {
		t.Fatalf("the stop's sentence %q says %q to a person", line, word)
	}
	beltRunWaitFor(t, "the carried-on run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	double.mu.Lock()
	cut := double.ctx != nil && double.ctx.Err() != nil
	double.mu.Unlock()
	if !cut {
		t.Fatal("the run ended and the context its workers and their calls run under was never cut")
	}
}

// A RUN WHOSE COPY WAS NEVER WRITTEN DOWN IS REFUSED, IN THE READING'S OWN
// SENTENCE. Every run row on disk today predates the record, so this refusal is
// the first thing a person will meet, and it is the answer forever.
func TestARunWithNoCopyIsRefusedInTheSameSentenceTheRowShows(t *testing.T) {
	agent, g, _, double := continueAgent(t)
	keepInterruptedRun(t, agent, g, 7, "port the parser", nil)

	_, err := agent.ContinueRun(context.Background(), 7)
	if err == nil {
		t.Fatal("a run with no copy written down was carried on, which can only mean a fresh copy was cut")
	}
	// THE DOOR AND THE ROW SAY ONE SENTENCE, NOT TWO. A person reads the row's
	// words before they answer and the door's words after; two spellings of the
	// same fact is how a reading drifts from what actually happens.
	why := runCannotContinue(nil, "")
	if why == "" {
		t.Fatal("the row shows no reason at all, so the offer would read as available")
	}
	if err.Error() != why {
		t.Fatalf("the door refuses with %q and the row says %q", err.Error(), why)
	}
	nothingStarted(t, agent, double)
}

// AND A COPY THAT IS GONE IS REFUSED NAMING ITS BRANCH, which is the one thing
// left that a person can act on: the work is on it.
func TestARunWhoseCopyIsGoneIsRefusedAndNamesTheBranch(t *testing.T) {
	agent, g, _, double := continueAgent(t)
	keepInterruptedRun(t, agent, g, 7, "port the parser", &TaskCopyRecord{
		Dir:    filepath.Join(t.TempDir(), "not-here"),
		Branch: "task/port-the-parser-9c1a2f",
	})

	_, err := agent.ContinueRun(context.Background(), 7)
	if err == nil {
		t.Fatal("a run whose copy is not on disk was carried on, so something was made rather than adopted")
	}
	if !strings.Contains(err.Error(), "task/port-the-parser-9c1a2f") {
		t.Fatalf("the refusal does not name the branch the work is on: %v", err)
	}
	nothingStarted(t, agent, double)
}

// A ROW THAT SAID ITS LAST WORD HAS NOTHING TO CARRY ON, and each refusal says
// which word it was rather than a general no.
func TestARunThatSettledIsRefusedAndSaysWhichWordItSettledOn(t *testing.T) {
	for _, state := range []TaskState{TaskDone, TaskFailed, TaskUnverified} {
		t.Run(string(state), func(t *testing.T) {
			agent, g, _, double := continueAgent(t)
			agent.publishRunRow(g, TaskNotice{
				ID: 7, Title: "port the parser", State: state,
				Copy: &TaskCopyRecord{Dir: t.TempDir(), Branch: "task/port-the-parser-9c1a2f"},
			})

			_, err := agent.ContinueRun(context.Background(), 7)
			if err == nil {
				t.Fatalf("a %s run was carried on", state)
			}
			if !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("the refusal does not say the run is %s: %v", state, err)
			}
			nothingStarted(t, agent, double)
		})
	}
}

// A NUMBER THAT IS NOT A RUN IN THIS CONVERSATION IS REFUSED BY NUMBER, because
// the number is all the person typed and it is what they will check.
func TestANumberThatIsNotARunHereIsRefusedByItsNumber(t *testing.T) {
	agent, _, _, double := continueAgent(t)

	_, err := agent.ContinueRun(context.Background(), 41)
	if err == nil {
		t.Fatal("a number with no run behind it was carried on")
	}
	if !strings.Contains(err.Error(), "41") {
		t.Fatalf("the refusal does not name the number that was asked for: %v", err)
	}
	nothingStarted(t, agent, double)
}

// A RUN THAT IS ALREADY GOING IS NOT CARRIED ON, and the live run is left
// exactly as it was. A conversation drives one run at a time; the honest answer
// to "carry this on" for work that never stopped is that it never stopped.
func TestARunThatIsAlreadyGoingIsLeftAloneAndSaysSo(t *testing.T) {
	agent, g, _, double := continueAgent(t)
	keepInterruptedRun(t, agent, g, 7, "port the parser", &TaskCopyRecord{Dir: t.TempDir()})

	live := &beltRun{row: 4, title: "rewrite the lexer"}
	agent.beltMu.Lock()
	agent.beltRun = live
	agent.beltMu.Unlock()
	t.Cleanup(func() {
		agent.beltMu.Lock()
		agent.beltRun = nil
		agent.beltMu.Unlock()
	})

	// The run that is going, asked for by its own number.
	if _, err := agent.ContinueRun(context.Background(), 4); err == nil {
		t.Fatal("a run that never stopped was carried on, which would be two runs on one store")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("the refusal does not say the run never stopped: %v", err)
	}

	// And a different, genuinely interrupted run, while that one is going.
	_, err := agent.ContinueRun(context.Background(), 7)
	if err == nil {
		t.Fatal("a second run was started beside a live one")
	}
	if !strings.Contains(err.Error(), "one run at a time") {
		t.Fatalf("the refusal does not say why not: %v", err)
	}
	if !strings.Contains(err.Error(), "rewrite the lexer") {
		t.Fatalf("the refusal does not name the run that is in the way: %v", err)
	}

	if double.didRun() {
		t.Fatal("a run was started beside the live one")
	}
	agent.beltMu.Lock()
	still := agent.beltRun
	agent.beltMu.Unlock()
	if still != live {
		t.Fatal("the live run was replaced by the one that was refused")
	}
}

// A PROGRAM'S RUN IS NEVER CARRIED ON, and the door and its row say so in one
// sentence rather than blaming a copy it never had.
func TestAProgramsRunIsNeverCarriedOn(t *testing.T) {
	agent, g, _, double := continueAgent(t)
	agent.publishRunRow(g, TaskNotice{
		ID: 7, Title: "port the parser", State: TaskInterrupted, Program: "senior-dev",
		StartedAt: agent.taskClockNow(),
	})
	_, err := agent.ContinueRun(context.Background(), 7)
	if err == nil {
		t.Fatal("a program's run was carried on by codeaf's own workers")
	}
	if want := programNotCarriedOn("senior-dev"); err.Error() != want {
		t.Fatalf("the door refuses with %q, want %q", err.Error(), want)
	}
	row, _ := runRowOf(g, 7)
	if got := row.StatusFacts().CannotContinue; got != err.Error() {
		t.Fatalf("the row says %q and the door %q", got, err.Error())
	}
	nothingStarted(t, agent, double)
}
