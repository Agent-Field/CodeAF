package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// taskItem is a job at its write step, plan done before it and test and
// proof after.
func taskItem(state factory.State, write factory.PhaseState) factory.Item {
	return factory.Item{ID: 7, Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: state, Cap: 10,
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, Until: factory.UntilDone, Max: 2, On: true},
			{Name: "write", Kind: factory.StageChat, Until: factory.UntilDone, Max: 2, On: true},
			{Name: "test", Kind: factory.StageChat, Until: factory.UntilNoNewFailures, Max: 3, On: true},
			{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven, Max: 3, On: true},
		},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "write", Kind: factory.StageChat, State: write, Round: 1},
			{Name: "test", Kind: factory.StageChat, State: factory.PhasePending},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
}

func phasesOf(it factory.Item) string {
	var out []string
	for _, ph := range it.Stream.Phases {
		out = append(out, ph.Name+":"+string(ph.State))
	}
	return strings.Join(out, " ")
}

// THE DOOR'S WRITE, by where the task finds the job. Each row is the job
// after the task was added: its steps in order, its state, the step it is on.
func TestAddTaskPutsTheTaskWhereTheJobIs(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		item      func() factory.Item
		held, now bool
		phases    string
		state     factory.State
		cur       string
		check     func(t *testing.T, it factory.Item)
	}{
		{"nobody holds the job: before its step, and queued", func() factory.Item { return taskItem(factory.StateQueued, factory.PhasePending) }, false, false,
			"plan:done task:pending write:pending test:pending proof:pending", factory.StateQueued, "task", nil},
		{"a round is running: right after it, and nothing else moves", func() factory.Item { return taskItem(factory.StateRunning, factory.PhaseRunning) }, true, false,
			"plan:done write:running task:pending test:pending proof:pending", factory.StateRunning, "write", nil},
		{"asked for now: before the step, which keeps its round", func() factory.Item { return taskItem(factory.StateRunning, factory.PhaseRunning) }, true, true,
			"plan:done task:pending write:running test:pending proof:pending", factory.StateQueued, "task", func(t *testing.T, it factory.Item) {
				if it.Stream.Phases[2].Round != 1 {
					t.Errorf("the cut step's round is %d, want 1", it.Stream.Phases[2].Round)
				}
			}},
		{"a step out of rounds gets one more after the task", func() factory.Item {
			it := taskItem(factory.StateNeedsYou, factory.PhaseWaiting)
			it.Stream.Phases[1].Round, it.Stream.Phases[1].RoundLimit = 2, 2
			it.Question, it.QKind = "write is out of rounds · try again?", factory.QKindOutOfAttempts
			return it
		}, true, false, "plan:done task:pending write:pending test:pending proof:pending", factory.StateQueued, "task", func(t *testing.T, it factory.Item) {
			if ph := it.Stream.Phases[2]; ph.Round != 3 || ph.RoundLimit != 3 || it.Question != "" || it.QKind != "" {
				t.Errorf("write is at round %d of %d with question %q, want round 3 of 3 and none", ph.Round, ph.RoundLimit, it.Question)
			}
		}},
		{"a failed step runs again after the task", func() factory.Item {
			it := taskItem(factory.StateNew, factory.PhaseFailed)
			f := &factory.Failure{Step: "write", Code: "error", Detail: "the delegate crashed", Next: factory.FailureStopped}
			it.Failure, it.Stream.Phases[1].Failure, it.Stream.Ended = f, f, now
			return it
		}, false, false, "plan:done task:pending write:pending test:pending proof:pending", factory.StateQueued, "task", func(t *testing.T, it factory.Item) {
			if it.Failure != nil || it.Stream.Phases[2].Round != 2 || !it.Stream.Ended.IsZero() {
				t.Errorf("failure %+v, write round %d, ended %v", it.Failure, it.Stream.Phases[2].Round, it.Stream.Ended)
			}
		}},
		{"a question a round asked goes with that round", func() factory.Item {
			it := taskItem(factory.StateNeedsYou, factory.PhaseWaiting)
			it.Question, it.QKind = "write asks: which table?", factory.QKindStep
			it.Asking = &factory.Asked{Stage: "write", Question: "Which table?", With: factory.AskedYou}
			return it
		}, false, false, "plan:done task:pending write:running test:pending proof:pending", factory.StateQueued, "task", func(t *testing.T, it factory.Item) {
			if it.Asking != nil || it.Question != "" {
				t.Errorf("the question is still on the job: %q", it.Question)
			}
		}},
		{"a landed job: after its last finished step, its publish withdrawn", func() factory.Item {
			it := taskItem(factory.StateLanded, factory.PhaseDone)
			for i := range it.Stream.Phases {
				it.Stream.Phases[i].State = factory.PhaseDone
			}
			it.Stream.Cur, it.Stream.Ended = 3, now
			it.PublishError, it.PublishStatus = "push refused", &factory.Publishing{State: "failed", Code: "unknown", Attempt: 1}
			return it
		}, false, false, "plan:done write:done test:done proof:done task:pending", factory.StateQueued, "task", func(t *testing.T, it factory.Item) {
			if it.PublishError != "" || it.PublishStatus != nil || !it.Stream.Ended.IsZero() {
				t.Errorf("the failed publish is still on the job: %q %+v", it.PublishError, it.PublishStatus)
			}
		}},
		{"a held job stays held", func() factory.Item {
			it := taskItem(factory.StateRunning, factory.PhaseRunning)
			it.Stream.Paused = true
			return it
		}, false, false, "plan:done task:pending write:running test:pending proof:pending", factory.StateRunning, "task", func(t *testing.T, it factory.Item) {
			if !it.Stream.Paused {
				t.Error("the task resumed a job its person holds")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.item()
			added, err := AddTask(&it, factory.TaskByMainAgent, " Add the rounding note. ", c.held, c.now, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := phasesOf(it); got != c.phases {
				t.Fatalf("steps = %s\n        want %s", got, c.phases)
			}
			if it.State != c.state || it.Stream.Phases[it.Stream.Cur].Name != c.cur {
				t.Fatalf("state %s on %s, want %s on %s", it.State, it.Stream.Phases[it.Stream.Cur].Name, c.state, c.cur)
			}
			if added.Step != "task" || added.By != factory.TaskByMainAgent || !logHas(it, "Task from the main agent: Add the rounding note.") {
				t.Fatalf("added %+v, log %v", added, it.Stream.Log)
			}
			i, ok := factory.WaitingTask(it)
			if !ok || it.Stream.Phases[i].Task.Words != "Add the rounding note." {
				t.Fatalf("the task's instructions are not on its step: %+v", it.Stream.Phases)
			}
			if c.check != nil {
				c.check(t, it)
			}
		})
	}
}

// Tasks run in the order they were asked for: one asked for now goes behind
// the ones already waiting for the round it cuts, and all of them before the
// step.
func TestATaskNowGoesBehindTheTasksAlreadyWaiting(t *testing.T) {
	it := taskItem(factory.StateRunning, factory.PhaseRunning)
	now := time.Now()
	for _, words := range []string{"First.", "Second."} {
		if _, err := AddTask(&it, factory.TaskByYou, words, true, false, now); err != nil {
			t.Fatal(err)
		}
	}
	added, err := AddTask(&it, factory.TaskByMainAgent, "Third, now.", true, true, now)
	if err != nil || added.Step != "task3" {
		t.Fatalf("added %+v (%v)", added, err)
	}
	if got := phasesOf(it); got != "plan:done task:pending task2:pending task3:pending write:running test:pending proof:pending" || it.Stream.Phases[it.Stream.Cur].Name != "task" {
		t.Fatalf("the run is %s, on %s", got, it.Stream.Phases[it.Stream.Cur].Name)
	}
	// And one more, with nobody holding the job: behind the three in front.
	if _, err := AddTask(&it, factory.TaskByYou, "Fourth.", false, false, now); err != nil {
		t.Fatal(err)
	}
	if got := phasesOf(it); got != "plan:done task:pending task2:pending task3:pending task4:pending write:running test:pending proof:pending" || it.Stream.Phases[it.Stream.Cur].Name != "task" {
		t.Fatalf("the run is %s, on %s", got, it.Stream.Phases[it.Stream.Cur].Name)
	}
}

// A refused task changes nothing on the job.
func TestAddTaskRefusedChangesNothing(t *testing.T) {
	it := taskItem(factory.StateRunning, factory.PhaseRunning)
	it.Manual = true
	before := phasesOf(it)
	if _, err := AddTask(&it, factory.TaskByYou, "Add the note.", true, false, time.Now()); err == nil {
		t.Fatal("a task on a job its person took over was taken")
	}
	if phasesOf(it) != before || len(it.Stream.Log) != 0 {
		t.Fatalf("the refused task changed the job: %s, log %v", phasesOf(it), it.Stream.Log)
	}
}

// A STEP EDIT NEVER DROPS A WAITING TASK. The edit lays the steps not begun
// again from the job's pipeline (RelayTail), and a task is no part of the
// pipeline: it stays where it was asked for, while the step the edit removed
// leaves the run.
func TestAStepEditKeepsAWaitingTask(t *testing.T) {
	recipe := factory.DefaultRecipe()
	for _, held := range []bool{true, false} {
		it := taskItem(factory.StateRunning, factory.PhaseRunning)
		it.Stages = append(it.Stages[:3:3], append([]factory.Stage{{Name: "review", Kind: factory.StageChat, Until: factory.UntilClean, Max: 3, On: true}}, it.Stages[3:]...)...)
		it.Stream.Phases = append(it.Stream.Phases[:3:3], append([]factory.Phase{{Name: "review", Kind: factory.StageChat, State: factory.PhasePending}}, it.Stream.Phases[3:]...)...)
		if _, err := AddTask(&it, factory.TaskByYou, "Add the note.", held, false, time.Now()); err != nil {
			t.Fatal(err)
		}
		lines, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, Remove: []string{"review"}, Why: "one line"}, recipe)
		if err != nil || len(lines) == 0 {
			t.Fatalf("edit: %v %v", lines, err)
		}
		want := "plan:done task:pending write:running test:pending proof:pending"
		if held {
			want = "plan:done write:running task:pending test:pending proof:pending"
		}
		if got := phasesOf(it); got != want {
			t.Fatalf("held %v: after the edit the run is %s\n        want %s", held, got, want)
		}
		if i, _ := factory.WaitingTask(it); it.Stream.Phases[i].Task == nil || it.Stream.Phases[i].Task.Words != "Add the note." {
			t.Fatalf("the task lost its instructions: %+v", it.Stream.Phases[i])
		}
	}
	// A task that is itself the last step begun still lets the tail be laid
	// again: the edit reaches the steps after it.
	it := taskItem(factory.StateRunning, factory.PhaseDone)
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByYou, "Add the note."))
	it.Stream.Phases[2].State, it.Stream.Phases[2].Round, it.Stream.Cur = factory.PhaseRunning, 1, 2
	if _, err := EditStepsItem(&it, factory.RunEdit{By: factory.ByYou, Skip: []string{"test"}, Why: "no tests here"}, factory.DefaultRecipe()); err != nil {
		t.Fatal(err)
	}
	if got := phasesOf(it); got != "plan:done write:done task:running proof:pending" {
		t.Fatalf("after an edit behind a running task the run is %s", got)
	}
	// The runner's own look at the tail between steps leaves the task too.
	it = taskItem(factory.StateRunning, factory.PhaseRunning)
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByYou, "Add the note."))
	if _, changed := tailPhases(it, it.Stream.Phases); changed {
		t.Fatal("the runner laid a tail again that only a task was added to")
	}
	it.Stages[2].On = false
	if tail, changed := tailPhases(it, it.Stream.Phases); !changed || phasesOf(factory.Item{Stream: &factory.Stream{Phases: tail}}) != "plan:done write:running task:pending proof:pending" {
		t.Fatalf("the runner did not lay the tail again around the task: %v", tail)
	}
}

// A rerun stands the steps after it pending again; a task waiting among them
// keeps its instructions.
func TestARerunKeepsAWaitingTasksInstructions(t *testing.T) {
	it := taskItem(factory.StateNew, factory.PhaseFailed)
	f := &factory.Failure{Step: "write", Code: "error", Detail: "crashed", Next: factory.FailureStopped}
	it.Failure, it.Stream.Phases[1].Failure = f, f
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByMainAgent, "Add the note."))
	if err := StepControl(&it, "write", true, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if ph := it.Stream.Phases[2]; ph.Task == nil || ph.Task.Words != "Add the note." || ph.State != factory.PhasePending {
		t.Fatalf("after the rerun the task is %+v", ph)
	}
}

// A RUNNER'S REPORT OF A TASK IS A REPORT OF ONE OF THE JOB'S STEPS, and so is
// its report of a request for changes' prove. Any other step the job's
// pipeline does not name is still refused.
func TestAReportOfATaskIsTaken(t *testing.T) {
	now := time.Now()
	it := taskItem(factory.StateRunning, factory.PhaseDone)
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByYou, "Add the note."))
	it.Stream.Phases = append(it.Stream.Phases, factory.Phase{Name: "prove", Kind: factory.StageChat, State: factory.PhasePending, Note: "show it"},
		factory.Phase{Name: "stray", Kind: factory.StageChat, State: factory.PhasePending})
	res := factory.StageResult{Done: true, Output: "added the note"}
	if err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "task", State: factory.PhaseDone, Round: 1, Result: &res}}}, now); err != nil {
		t.Fatalf("a task's report was refused: %v", err)
	}
	if ph := it.Stream.Phases[2]; ph.State != factory.PhaseDone || ph.Result == nil || ph.Task == nil {
		t.Fatalf("the task after its report: %+v", ph)
	}
	if err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "prove", State: factory.PhaseRunning, Round: 1}}}, now); err != nil {
		t.Fatalf("a prove step's report was refused: %v", err)
	}
	if err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{Name: "stray", State: factory.PhaseRunning, Round: 1}}}, now); err == nil {
		t.Fatal("a report of a step neither the pipeline nor the factory made was taken")
	}
}

// A runner that reshapes its steps before a task reached it does not take
// the task off the job: the factory's copy keeps it after the step it
// followed.
func TestARunnersReshapeKeepsATaskItHasNotHeardOf(t *testing.T) {
	it := taskItem(factory.StateRunning, factory.PhaseRunning)
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByYou, "Add the note."))
	runner := taskItem(factory.StateRunning, factory.PhaseRunning)
	runner.Stages = append(runner.Stages[:2:2], append([]factory.Stage{{Name: "lint", Kind: factory.StageChat, Until: factory.UntilDone, On: true}}, runner.Stages[2:]...)...)
	runner.Stream.Phases = append(runner.Stream.Phases[:2:2], append([]factory.Phase{{Name: "lint", Kind: factory.StageChat, State: factory.PhasePending}}, runner.Stream.Phases[2:]...)...)
	if err := applyPipeline(&it, &factory.PipelineChange{Stages: runner.Stages, Phases: runner.Stream.Phases}); err != nil {
		t.Fatal(err)
	}
	if got := phasesOf(it); got != "plan:done write:running task:pending lint:pending test:pending proof:pending" {
		t.Fatalf("after the reshape the run is %s", got)
	}
	// Once the runner has placed the task its order is the job's.
	runner.Stream.Phases = factory.InsertPhase(runner.Stream.Phases, 1, factory.Phase{Name: "task", Kind: factory.StageChat, State: factory.PhasePending})
	if err := applyPipeline(&it, &factory.PipelineChange{Stages: runner.Stages, Phases: runner.Stream.Phases}); err != nil {
		t.Fatal(err)
	}
	if got := phasesOf(it); got != "plan:done task:pending write:running lint:pending test:pending proof:pending" || it.Stream.Phases[1].Task == nil {
		t.Fatalf("after the runner placed the task the run is %s", got)
	}
}

// A job that lands with a task still waiting does not wait for its approval:
// the task was asked for as the last round ended, and it runs first.
func TestALandingWithAWaitingTaskRunsTheTaskFirst(t *testing.T) {
	it := taskItem(factory.StateRunning, factory.PhaseDone)
	for i := range it.Stream.Phases {
		it.Stream.Phases[i].State = factory.PhaseDone
	}
	it.Stream.Phases = factory.InsertPhase(it.Stream.Phases, 2, factory.NamedTaskPhase("task", factory.TaskByYou, "Add the note."))
	it.Proof = []factory.Claim{{Text: "works", OK: true, Evidence: "ran it"}}
	if err := FinishReport(&it, factory.StateLanded, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := phasesOf(it); it.State != factory.StateQueued || got != "plan:done write:done test:done proof:done task:pending" || it.Stream.Cur != 4 {
		t.Fatalf("state %s, run %s, on %d", it.State, got, it.Stream.Cur)
	}
}

// taskRun records the rounds a rig's executor ran, and what each was handed.
type taskRun struct {
	mu   sync.Mutex
	ran  []string
	jobs map[string]Job
}

func (r *taskRun) note(job Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := job.Stage.Name + "#" + string(rune('0'+job.Round))
	r.ran = append(r.ran, key)
	if r.jobs == nil {
		r.jobs = map[string]Job{}
	}
	r.jobs[key] = job
}

func (r *taskRun) rounds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ran...)
}

// A TASK HEARD WHILE A ROUND RUNS GOES BEFORE THE STEP'S NEXT ROUND. The
// step waits at the round it was about to start, runs it after the task with
// what its last round found, and the steps after read the task's report with
// the others.
func TestATaskHeardDuringARoundRunsBeforeTheNextRound(t *testing.T) {
	var run taskRun
	started, release := make(chan struct{}), make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			run.note(job)
			if job.Stage.Name == "write" && job.Round == 1 {
				close(started)
				<-release
				return factory.StageResult{Done: false, Output: "half of the change is in"}, nil
			}
			return done(job.Stage.Name + " went fine"), nil
		}),
	}, nil)
	write := chat("write")
	write.Max = 2
	id := g.add("one", write, chat("test"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := g.r.Task(id, "task", "Add the rounding note.", factory.TaskByMainAgent); err != nil {
		t.Fatal(err)
	}
	// The same task heard twice is one task.
	_ = g.r.Task(id, "task", "Add the rounding note.", factory.TaskByMainAgent)
	close(release)
	it := g.waitState(id, factory.StateLanded)
	if got := run.rounds(); !slices.Equal(got, []string{"write#1", "task#1", "write#2", "test#1"}) {
		t.Fatalf("rounds ran as %v", got)
	}
	if got := phasesOf(it); got != "task:done write:done test:done" {
		t.Fatalf("the run is %s", got)
	}
	task := run.jobs["task#1"]
	if task.Stage.Ask != "Add the rounding note." || task.Stage.Until != factory.UntilDone || task.Stage.Max != factory.TaskRounds {
		t.Fatalf("the task ran as %+v", task.Stage)
	}
	if brief := stageBrief(task); !strings.Contains(brief, "This is an extra task from the job's main agent, not one of the pipeline's steps.") || !strings.Contains(brief, "Do not push") || !strings.Contains(brief, "The task, whole:\nAdd the rounding note.") {
		t.Fatalf("the task's brief does not say what a task is:\n%s", brief)
	}
	if again := run.jobs["write#2"]; again.Last == nil || again.Last.Output != "half of the change is in" {
		t.Fatalf("write's second round was not handed its first round's result: %+v", again.Last)
	}
	if next := run.jobs["test#1"]; !slices.Equal(next.PriorNames, []string{"task", "write"}) || next.Prior[1].Output != "write went fine" {
		t.Fatalf("test was handed %v", next.PriorNames)
	}
}

// A task heard as a step finishes runs before the next step, and two run in
// the order they were asked for.
func TestTasksHeardAsAStepFinishesRunBeforeTheNextStep(t *testing.T) {
	var run taskRun
	started, release := make(chan struct{}), make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			run.note(job)
			if job.Stage.Name == "write" {
				close(started)
				<-release
			}
			return done(job.Stage.Name + " went fine"), nil
		}),
	}, nil)
	id := g.add("one", chat("write"), chat("test"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-started
	_ = g.r.Task(id, "task", "First.", factory.TaskByYou)
	_ = g.r.Task(id, "task2", "Second.", factory.TaskByYou)
	close(release)
	g.waitState(id, factory.StateLanded)
	if got := run.rounds(); !slices.Equal(got, []string{"write#1", "task#1", "task2#1", "test#1"}) {
		t.Fatalf("rounds ran as %v", got)
	}
}

// A STEP A TASK CUT IN FRONT OF CARRIES ON IN ITS OWN CONVERSATION. The task
// was put before a step whose round was under way; after the task the step
// runs that same round on, in the chat it had, and is told there that a task
// ran meanwhile.
func TestAStepCutByATaskCarriesOnInItsChat(t *testing.T) {
	var run taskRun
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			run.note(job)
			return done(job.Stage.Name + " went fine"), nil
		}),
	}, nil)
	id := g.add("one", chat("plan"), chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		// The job as a runner pulls it after a task was asked for now: the
		// task first, the step it cut still in its round.
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Paused: true, Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "the plan"}},
			factory.NamedTaskPhase("task", factory.TaskByMainAgent, "Add the rounding note."),
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1, Chat: "chat-of-write"},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	if got := run.rounds(); !slices.Equal(got, []string{"task#1", "write#1"}) {
		t.Fatalf("rounds ran as %v", got)
	}
	write := run.jobs["write#1"]
	if write.Resume != "chat-of-write" || !write.Continuing {
		t.Fatalf("write did not carry on in its chat: resume %q, continuing %v", write.Resume, write.Continuing)
	}
	if len(write.CarryOn) != 1 || !strings.Contains(write.CarryOn[0], "an extra task ran in this checkout: Add the rounding note.") || !strings.Contains(write.CarryOn[0], "It reported: task went fine") {
		t.Fatalf("write was not told of the task: %q", write.CarryOn)
	}
}

// A TASK THAT FINISHED BEFORE A RESTART IS STILL CHECKED. The checkout as the
// task began was kept in the memory of the runner that ran it; a runner that
// takes the job on with the task finished and its check not yet run cannot
// tell what it changed, so it takes it as changed and sends the checks that
// had passed round again, rather than skipping them. A task this runner has
// already checked is not checked again on its next drive.
func TestATaskFinishedBeforeARestartIsStillChecked(t *testing.T) {
	var run taskRun
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			run.note(job)
			return done(job.Stage.Name + " went fine"), nil
		}),
	}, nil)
	review := chat("review")
	review.Until = factory.UntilClean
	id := g.add("one", chat("plan"), review, chat("write"))
	task := factory.NamedTaskPhase("task", factory.TaskByMainAgent, "Add the rounding note.")
	task.State, task.Round, task.Result = factory.PhaseDone, 1, &factory.StageResult{Done: true, Output: "added the note"}
	if err := g.st.Update(id, func(it *factory.Item) error {
		// The job as a runner pulls it after the one that ran the task went
		// away: the task finished, its check never ran.
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Paused: true, Cur: 3, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "the plan"}},
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "clean"}},
			task,
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if got := run.rounds(); !slices.Equal(got, []string{"review#2", "write#1"}) {
		t.Fatalf("rounds ran as %v; want the review checked again before write", got)
	}
	if !strings.Contains(fmt.Sprint(it.Stream.Log), "task changed files. Checking again from review.") {
		t.Fatalf("the log does not say the task was checked: %v", it.Stream.Log)
	}
}

// checkMissed is true only for a finished task nothing but tasks has finished
// after, on a run that goes on after it.
func TestCheckMissedOnlyForATaskWithNothingFinishedAfterIt(t *testing.T) {
	task := func(state factory.PhaseState) factory.Phase {
		ph := factory.NamedTaskPhase("task", factory.TaskByYou, "x")
		ph.State = state
		return ph
	}
	step := func(name string, state factory.PhaseState) factory.Phase {
		return factory.Phase{Name: name, Kind: factory.StageChat, State: state}
	}
	for name, c := range map[string]struct {
		phases []factory.Phase
		want   bool
	}{
		"next step pending":       {[]factory.Phase{step("review", factory.PhaseDone), task(factory.PhaseDone), step("write", factory.PhasePending)}, true},
		"the step it cut":         {[]factory.Phase{task(factory.PhaseDone), step("write", factory.PhaseRunning)}, true},
		"a task after it":         {[]factory.Phase{task(factory.PhaseDone), task(factory.PhaseDone), step("write", factory.PhasePending)}, true},
		"a step after it done":    {[]factory.Phase{task(factory.PhaseDone), step("write", factory.PhaseDone), step("test", factory.PhasePending)}, false},
		"checks sent round again": {[]factory.Phase{step("review", factory.PhasePending), task(factory.PhaseDone), step("write", factory.PhasePending)}, false},
	} {
		if got := checkMissed(c.phases, slices.IndexFunc(c.phases, factory.IsTask)); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// checkoutKey names the checkout's content: any change to a file git tracks,
// any file it does not, and any commit gives another key, and nothing else
// does.
func TestCheckoutKeyChangesWithTheCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	key := func() string {
		t.Helper()
		k, err := checkoutKey(dir)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	git("init", "-q", "-b", "main")
	write("ledger.go", "package ledger\n")
	git("add", "ledger.go")
	git("commit", "-q", "-m", "first")
	clean := key()
	if key() != clean {
		t.Fatal("the same checkout gave two keys")
	}
	write("ledger.go", "package ledger\n\nconst balanced = 1\n")
	edited := key()
	write("notes.txt", "one\n")
	untracked := key()
	write("notes.txt", "two\n")
	rewritten := key()
	git("add", ".")
	git("commit", "-q", "-m", "second")
	committed := key()
	seen := map[string]bool{}
	for _, k := range []string{clean, edited, untracked, rewritten, committed} {
		if seen[k] {
			t.Fatalf("two different checkouts gave one key: %v", []string{clean, edited, untracked, rewritten, committed})
		}
		seen[k] = true
	}
	if _, err := checkoutKey(""); err == nil {
		t.Fatal("a job with no checkout gave a key")
	}
}
