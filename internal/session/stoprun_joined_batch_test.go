package session

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A JOINED TASK ENDED BY ITS RUN'S STOP IS STOPPED, NOT FINISHED. Stopping a
// batch runs the stops concurrently, so whether a joined task's own stop
// reaches the store before or after the run's [plandb.Store.StopRoot] write
// is a race. StopRoot cancels every open task under the run under the stop's
// own reason, and a later stop on the joined row used to read only "terminal"
// off that write and answer "has already finished" — the sentence a task that
// landed gets — over work that was mid-flight when the run's stop killed it.
func TestStopJoinedRowEndedByRunsStopSaysAlreadyStoppedWithRun(t *testing.T) {
	store, err := OpenRunPlan(t.TempDir(), "task C", "the run's own brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "72", Title: "task D", Description: "its own words"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.StopRoot(stopBecause(taskStoppedWord, "")); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	run := &beltRun{store: store, root: store.RootID(), row: 71, title: "task C", joined: []uint64{72}}
	line, owned, err := agent.stopJoinedRow(run, 72, "")
	if err != nil || !owned {
		t.Fatalf("the stop on a joined row ended by its run's stop was refused: %q, %v", line, err)
	}
	if strings.Contains(line, "already finished") {
		t.Fatalf("a joined task its run's stop killed answered %q, want never \"already finished\"", line)
	}
	if want := "task 72 (task D) was already stopped with task 71 (task C), whose run it shared"; line != want {
		t.Fatalf("the stop answered %q, want %q", line, want)
	}
}

// A JOINED TASK THAT LANDED KEEPS THE FINISHED SENTENCE. The stop's reason is
// the only thing telling a task its run's stop ended apart from a task that
// came home on its own: a landed task is done, carries a result, and was
// never cancelled.
func TestStopJoinedRowThatLandedStillSaysAlreadyFinished(t *testing.T) {
	store, err := OpenRunPlan(t.TempDir(), "task C", "the run's own brief")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "72", Title: "task D", Description: "its own words", ParentID: store.RootID()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("72", "worker"); err != nil {
		task := store.Task("72")
		t.Fatalf("claim the joined task (status %+v): %v", task, err)
	}
	if _, err := store.Done("72", "worker", "landed with its letter", nil, nil); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	run := &beltRun{store: store, root: store.RootID(), row: 71, title: "task C", joined: []uint64{72}}
	line, owned, err := agent.stopJoinedRow(run, 72, "")
	if err != nil || !owned {
		t.Fatalf("the stop on a landed joined row was refused: %q, %v", line, err)
	}
	if want := "task 72 (task D) has already finished; there is nothing to stop"; line != want {
		t.Fatalf("the stop answered %q, want %q", line, want)
	}
}

// STOPPING THE RUN FIRST AND A JOINED TASK SECOND, THROUGH THE ONE DOOR a
// batch stop takes, answers the joined task as stopped. This is the
// replication's order made deterministic: the run's stop is issued and
// answered before the joined stop is sent, so the joined stop always arrives
// after StopRoot has written, whichever internal road it then takes.
func TestStoppingRunThenJoinedTaskSaysAlreadyStopped(t *testing.T) {
	agent, _, conversation, dir := stoppableBeltRun(t, 71)
	if err := agent.startKnownTaskRun(context.Background(), 72, "a second piece", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
		t.Fatal(err)
	}
	line, err := agent.Cancel(CancelTask + ":72")
	if err != nil {
		t.Fatalf("the stop on the joined row after the run's stop was refused: %v", err)
	}
	if strings.Contains(line, "already finished") {
		t.Fatalf("the joined stop after the run's stop answered %q, want never \"already finished\"", line)
	}
	if !strings.Contains(line, "already stopped") {
		t.Fatalf("the joined stop after the run's stop answered %q, want it to say the task was already stopped", line)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	if task := beltRunTaskAt(t, dir, "72"); task == nil || task.Status != plandb.StatusCancelled {
		t.Fatalf("the joined work after the batch stop = %+v, want cancelled", task)
	}
	rows := agent.graph().runRows(72)
	if len(rows) != 1 || rows[0].State != TaskFailed || !rows[0].Stopped {
		t.Fatalf("the joined row after the batch stop = %+v, want a row that reads stopped", rows)
	}
}

// A BATCH STOP ISSUED CONCURRENTLY ANSWERS EVERY JOINED TASK AS STOPPED, in
// whatever order the stops reach the store. Run under -race: each joined
// stop may win its own race (and stop its task itself), lose to the run's
// StopRoot, or arrive after the run has settled — all three answers are the
// stop's, and none of them is "already finished".
func TestStoppingBatchConcurrentlyNeverSaysAlreadyFinishedForJoined(t *testing.T) {
	agent, _, conversation, _ := stoppableBeltRun(t, 71)
	for _, id := range []uint64{72, 73} {
		if err := agent.startKnownTaskRun(context.Background(), id, "another piece", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
			t.Fatal(err)
		}
	}
	ids := []uint64{71, 72, 73}
	lines := make([]string, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lines[i], errs[i] = agent.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10))
		}()
	}
	wg.Wait()
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatalf("the batch stop on task %d was refused: %v (line %q)", id, errs[i], lines[i])
		}
		if strings.Contains(lines[i], "already finished") {
			t.Fatalf("the batch stop on task %d answered %q, want never \"already finished\"", id, lines[i])
		}
		if id != 71 && !strings.Contains(lines[i], "stopped") && !strings.Contains(lines[i], "stopping") {
			t.Fatalf("the batch stop on joined task %d answered %q, want a stopped sentence", id, lines[i])
		}
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
}
