package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// stoppableBeltRun starts one run on a committed repository with a double that
// ends when its context is cut, the way the real engine does, and answers the
// agent, the double, the folder the conversation is in and the store's folder.
func stoppableBeltRun(t *testing.T, row uint64) (*Agent, *beltRunDouble, string, string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("unused")
	double.real, double.honoursStop = true, true
	double.early = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "half.txt"), []byte("half made\n"), 0o644); err != nil {
			t.Errorf("the run's own write: %v", err)
		}
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	if err := agent.startKnownTaskRun(context.Background(), row, "make the change", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	t.Cleanup(func() {
		// A RED TEST STILL ENDS ITS RUN, or the fixture's own folder is removed
		// under a goroutine that is still writing into it.
		select {
		case <-double.finished:
		default:
			close(double.release)
		}
		beltRunWaitFor(t, "the run to end", func() bool {
			agent.beltMu.Lock()
			defer agent.beltMu.Unlock()
			return agent.beltRun == nil
		})
	})
	return agent, double, conversation, dir
}

// stopMachineryWords are the words of how this program is built. A sentence a
// person reads about stopping their work carries none of them.
var stopMachineryWords = []string{"harness", "root", "supervisor", "context", "engine", "store", "in this session"}

func carriesMachinery(sentence string) string {
	lower := strings.ToLower(sentence)
	for _, word := range stopMachineryWords {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}

// A PERSON'S STOP ON A RUN'S OWN ROW ENDS THE RUN. The row a run is published
// under wears a task's number, so the stop a surface sends for it is a task's
// stop, and the task graph has never heard of it: measured on the real binary
// 2026-09-19, the card's "stop it" answered "there is no task 1 in this
// session" four times of four and the run carried on to its own landing. The
// run was started on a context nothing could cut and no cancel was kept.
func TestAStopOnARunsOwnRowEndsTheRun(t *testing.T) {
	agent, double, conversation, dir := stoppableBeltRun(t, 71)

	line, err := agent.Cancel(CancelTask + ":71")
	if err != nil {
		t.Fatalf("the stop a surface sends for a run's row was refused: %v", err)
	}
	if !strings.HasPrefix(line, "stopping ") || !strings.Contains(line, "its branch is kept") {
		t.Fatalf("the stop answered %q, want the sentence every stopped task answers", line)
	}
	if word := carriesMachinery(line); word != "" {
		t.Fatalf("the stop's sentence %q says %q to a person", line, word)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	double.mu.Lock()
	cut := double.ctx.Err() != nil
	double.mu.Unlock()
	if !cut {
		t.Fatal("the run ended and the context its workers and their calls run under was never cut")
	}

	// THE STORE SAYS THE RUN IS OVER, so the next hand-off cannot adopt it.
	if root := beltRunTaskAt(t, dir, "71"); root == nil || root.Status != plandb.StatusCancelled {
		t.Fatalf("the run's own task after a stop = %+v, want cancelled", root)
	}

	// NOTHING WENT INTO THE PERSON'S FOLDER, and what the run had made is on the
	// branch the sentence promised.
	if _, err := os.Stat(filepath.Join(conversation, "half.txt")); !os.IsNotExist(err) {
		t.Fatalf("a stopped run's half-made work went into the person's folder: %v", err)
	}
	rows := agent.graph().runRows(71)
	if len(rows) != 1 || rows[0].State != TaskFailed || !rows[0].Stopped || rows[0].EndedAt.IsZero() {
		t.Fatalf("the run's row after a stop = %+v, want ended, failed and stopped by a person", rows)
	}
	if rows[0].Branch == "" || rows[0].Merge != mergeAborted {
		t.Fatalf("the run's row names branch %q, merge %q, want the branch it was stopped on", rows[0].Branch, rows[0].Merge)
	}
	if out, err := git(conversation, "show", rows[0].Branch+":half.txt"); err != nil || out != "half made\n" {
		t.Fatalf("the kept branch %s does not hold what the run had made: %q, %v", rows[0].Branch, out, err)
	}
	if got := double.lands(); got != 0 {
		t.Fatalf("a stopped run was landed %d times, want never", got)
	}

	// THE PERSON IS TOLD, ONCE, WHERE THE WORK IS, in the run's page and in the
	// conversation, and neither sentence is about how the program is built.
	where := "its work so far is kept on " + rows[0].Branch + " and did not go into " + canonicalPath(conversation) +
		" · merge that branch to bring it in, or delete it to drop it"
	if got := conversationNotes(agent, where); got != 1 {
		t.Fatalf("the conversation was told where the stopped work is %d times, want once", got)
	}
	said := strings.Join(beltRunNotes(t, dir, "71"), "\n")
	if !strings.Contains(said, "stopped") || !strings.Contains(said, where) {
		t.Fatalf("the run's page does not say it was stopped and where its work is:\n%s", said)
	}
	if word := carriesMachinery(said); word != "" {
		t.Fatalf("the run's page says %q to a person:\n%s", word, said)
	}

	// TWO PRESSES ARE ONE STOP.
	again, err := agent.Cancel(CancelTask + ":71")
	if err != nil || !strings.Contains(again, "nothing to stop") {
		t.Fatalf("a stop on a run that is already over answered %q, %v", again, err)
	}
}

// A RUN STOPPED BEFORE IT CHANGED ANYTHING NAMES NO BRANCH. A branch named over
// no work sends a person looking for something that is not there.
func TestAStoppedRunThatChangedNothingSaysSo(t *testing.T) {
	agent, double, _, dir := stoppableBeltRun(t, 71)
	double.mu.Lock()
	workspace := double.spec.Workspace
	double.mu.Unlock()
	if err := os.Remove(filepath.Join(workspace, "half.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	said := strings.Join(beltRunNotes(t, dir, "71"), "\n")
	if !strings.Contains(said, "stopped · it had changed nothing") || strings.Contains(said, "kept on") {
		t.Fatalf("a run stopped before it changed anything says:\n%s", said)
	}
	if rows := agent.graph().runRows(71); len(rows) != 1 || rows[0].Branch != "" || !rows[0].Stopped {
		t.Fatalf("a run stopped before it changed anything draws %+v, want a stopped row naming no branch", rows)
	}
}

// A HAND-OFF AFTER A STOPPED RUN IS A FRESH RUN. A store whose run task is open
// is adopted by the next hand-off, so a stop that only cut a context would hand
// the stopped work to the next run's workers.
func TestAHandoffAfterAStoppedRunNeverPicksTheStoppedWorkBackUp(t *testing.T) {
	agent, _, conversation, dir := stoppableBeltRun(t, 71)
	if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	second := newBeltRunDouble("the second run's work")
	registerBeltRunEngine(t, second)
	if err := agent.startKnownTaskRun(context.Background(), 72, "another change", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-second.entered
	store := beltRunStoreAt(t, dir)
	root := store.RootID()
	_ = store.Close()
	endBeltRun(t, agent, second)
	if root != "72" {
		t.Fatalf("the hand-off after a stop runs under %q, want a fresh run of its own", root)
	}
}

// A STOP ON WORK THAT JOINED A RUN ENDS THAT WORK AND LEAVES THE RUN GOING. A
// joined hand-off is a row of its own with a number of its own, and the same
// stop a surface sends for any row has to reach it.
func TestAStopOnAJoinedRowEndsThatWorkAndLeavesTheRun(t *testing.T) {
	agent, double, conversation, dir := stoppableBeltRun(t, 71)
	if err := agent.startKnownTaskRun(context.Background(), 72, "a second piece", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	line, err := agent.Cancel(CancelTask + ":72")
	if err != nil {
		t.Fatalf("the stop on a joined row was refused: %v", err)
	}
	if word := carriesMachinery(line); word != "" || !strings.HasPrefix(line, "stopped ") {
		t.Fatalf("the stop answered %q (machinery word %q)", line, word)
	}
	if task := beltRunTaskAt(t, dir, strconv.Itoa(72)); task == nil || task.Status != plandb.StatusCancelled {
		t.Fatalf("the joined work after its stop = %+v, want cancelled", task)
	}
	rows := agent.graph().runRows(72)
	if len(rows) != 1 || rows[0].State != TaskFailed || !rows[0].Stopped || rows[0].EndedAt.IsZero() {
		t.Fatalf("the joined row after its stop = %+v, want ended and stopped by a person", rows)
	}
	double.mu.Lock()
	cut := double.ctx.Err() != nil
	double.mu.Unlock()
	if cut {
		t.Fatal("a stop on one joined piece of work cut the whole run")
	}
	if root := beltRunTaskAt(t, dir, "71"); root == nil || root.Status == plandb.StatusCancelled {
		t.Fatalf("a stop on one joined piece of work ended the run: %+v", root)
	}
}

// THE PAGE'S OWN STOP ON THE RUN'S OWN TASK IS THE SAME STOP. The store refuses
// every verb on the run's task, so the page's `x` answered with the store's
// sentence about who owns what, which is no sentence for a person.
func TestThePagesStopOnTheRunsOwnTaskEndsTheRun(t *testing.T) {
	agent, double, _, dir := stoppableBeltRun(t, 71)
	if err := agent.PlanCancel("71"); err != nil {
		t.Fatalf("the page's stop on the run's own task was refused: %v", err)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	double.mu.Lock()
	cut := double.ctx.Err() != nil
	double.mu.Unlock()
	if root := beltRunTaskAt(t, dir, "71"); !cut || root == nil || root.Status != plandb.StatusCancelled {
		t.Fatalf("after the page's stop the run's context cut = %t, its task = %+v", cut, root)
	}
}

// A RUN NOBODY IS RUNNING ANY MORE IS STILL ENDED BY ITS PAGE'S STOP. A store
// can hold an open run with no run going in this conversation (the program
// ended under it), and its page still offers the stop. The store refuses its
// own cancel on that task for every caller, so the page answered with the
// store's sentence about who owns what. The person's stop ends it in the store,
// which is also what keeps the next hand-off from adopting it.
func TestThePagesStopEndsARunNobodyIsRunningAnyMore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	workspace := t.TempDir()
	store, err := OpenRunPlan(workspace, "the run", "the hand-off's own words")
	if err != nil {
		t.Fatal(err)
	}
	root := store.RootID()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "part", Title: "a part", Description: "its own words"}}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = workspace
	})
	if err := agent.PlanCancel(root); err != nil {
		t.Fatalf("the page's stop on a run nobody is running was refused: %v", err)
	}
	after, err := plandb.Open(PlanStorePath(workspace), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if task := after.Task(root); task == nil || task.Status != plandb.StatusCancelled {
		t.Fatalf("the run's own task after the page's stop = %+v, want cancelled", task)
	}
	if task := after.Task("part"); task == nil || task.Status != plandb.StatusCancelled {
		t.Fatalf("the run's part after the page's stop = %+v, want cancelled", task)
	}
}

// countingCompleter counts the model calls a conversation makes.
type countingCompleter struct {
	beltRunCompleter
	calls atomic.Int64
}

func (c *countingCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	c.calls.Add(1)
	return c.beltRunCompleter.CompleteWithMessages(ctx, messages, options...)
}

// A RUN A PERSON STOPPED BUYS NO FURTHER READING. A surface asks for the run's
// summary again whenever its rows move, and a stop moves them, so on the real
// binary one model call was made about twenty seconds after every stop taken
// from the run's page (the usage ledger, 2026-09-19, three of three). The person
// ended the spend; the last reading the run had stands.
func TestAStoppedRunBuysNoFurtherSummary(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := t.TempDir()
	double := newBeltRunDouble("unused")
	double.real, double.honoursStop = true, true
	registerBeltRunEngine(t, double)
	counting := &countingCompleter{beltRunCompleter: beltRunCompleter{text: "done"}}
	agent, _ := newTestAgent(t, counting, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	if _, err := agent.Cancel(CancelTask + ":71"); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the run to end", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})
	before := counting.calls.Load()
	for _, id := range []string{"71", "t-71"} {
		agent.RefreshRunSummary(context.Background(), id, time.Time{})
	}
	if got := counting.calls.Load() - before; got != 0 {
		t.Fatalf("a stopped run's summary was asked of a model %d times, want never", got)
	}
}
