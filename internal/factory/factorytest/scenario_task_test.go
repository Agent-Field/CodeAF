package factorytest

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE EXTRA TASK, on a whole factory. The job's main agent, or its person,
// sends a delegate into the job's checkout with plain instructions whatever
// step the job is on (internal/factory's task.go). Each scenario takes a job
// to one place a task can find it, sends the task through the service's own
// door, and holds the properties after every event.

const taskWords = "Add a note on the ledger's rounding to the README."

// sendTask presses the task door as by.
func sendTask(s *Scenario, by string, now bool) Pressed {
	return s.Post("task", api.Task{Words: taskWords, Now: now, By: by})
}

// mustTask is sendTask for a task the scenario expects the door to take. It
// answers the step the task runs as.
func mustTask(s *Scenario, by string, now bool) string {
	s.T.Helper()
	got := sendTask(s, by, now)
	if !got.OK() {
		s.T.Fatalf("the task was refused: %s\n%s", got, s.Story())
	}
	return wireField(got.Body, `"step":"`)
}

// wireField is the string value after key in a JSON body.
func wireField(body, key string) string {
	_, rest, ok := strings.Cut(body, key)
	if !ok {
		return ""
	}
	value, _, _ := strings.Cut(rest, `"`)
	return value
}

// held scripts step's next round to wait until it is released, and answers
// the round's start and its release. round is what the round does once
// released.
func heldRound(s *Scenario, step string, round Round) (entered chan struct{}, release func()) {
	entered, hold := make(chan struct{}), make(chan struct{})
	round.entered, round.hold = entered, hold
	s.Delegate.Next(step, round)
	var once bool
	return entered, func() {
		if !once {
			once = true
			close(hold)
		}
	}
}

// begun waits for a held round to start, with the service settling as an
// open job page settles it.
func begun(s *Scenario, entered chan struct{}) {
	s.T.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-entered:
			return
		case <-deadline:
			s.T.Fatalf("the round the scenario holds never began\n%s", s.Story())
		case <-time.After(tickEvery):
			s.Tick()
		}
	}
}

// yesUntil says yes for the person until the job reaches where the scenario
// wants it.
func yesUntil(s *Scenario, it factory.Item, there func(factory.Item) bool) factory.Item {
	s.T.Helper()
	for step := 0; step < 12; step++ {
		if there(it) {
			return it
		}
		if it.Wait.Who != factory.WaitYou {
			break
		}
		it = s.Do("the person says yes", func() { s.Yes() })
	}
	if !there(it) {
		s.T.Fatalf("the job did not get there: %s\n%s", waitKey(it), s.Story())
	}
	return it
}

// phaseNames is the run's steps in order, each with its state.
func phaseNames(it factory.Item) string {
	var out []string
	for _, ph := range it.Stream.Phases {
		out = append(out, ph.Name+":"+string(ph.State))
	}
	return strings.Join(out, " ")
}

// ranIn says the delegate's rounds hold want in that order, with nothing of
// those steps between them.
func ranIn(s *Scenario, want ...string) bool {
	return slices.Equal(s.Delegate.ran(), want)
}

// worktree is the job's checkout on this computer.
func worktree(s *Scenario) string {
	s.T.Helper()
	dir, err := s.workdirs.For(s.Item(), s.checkout)
	if err != nil {
		s.T.Fatalf("the job's checkout: %v", err)
	}
	return dir
}

// A TASK ASKED FOR WHILE A STEP IS RUNNING WAITS FOR THAT ROUND. The round
// ends, the task runs, and the step then carries on at its next round with
// its count as it was: nothing of the step is lost, and the task does not
// wait for the whole step.
func TestTaskRunsWhenTheRoundRunningNowEnds(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "proof")
	entered, release := heldRound(s, "write", Round{Result: &factory.StageResult{Done: false, Output: "Half of the change is in."}})
	defer release()
	s.Events = append(s.Events, "start, and a task is sent while write's first round runs")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	if step := mustTask(s, api.TaskByMainAgent, false); step != "task" {
		t.Fatalf("the task runs as %q, want task", step)
	}
	it := s.Item()
	if got := phaseNames(it); !strings.HasPrefix(got, "write:running task:pending test:pending") {
		t.Fatalf("the task is not right after the running step: %s\n%s", got, s.Story())
	}
	if it.Wait.Who != factory.WaitWorking || it.Wait.What != "write" || it.Wait.Words == "" {
		t.Fatalf("wait = %s %q, want working:write with the task said\n%s", waitKey(it), it.Wait.Words, s.Story())
	}
	if !logSays(it, "Task from the main agent: "+taskWords) {
		t.Fatalf("the job's log does not say who sent the task\n%s", s.Story())
	}
	s.Check()
	release()
	it = s.Do("the round ends", nil)
	if !ranIn(s, "write#1", "task#1", "write#2", "test#1", "proof#1") {
		t.Fatalf("rounds ran as %v\n%s", s.Delegate.ran(), s.Story())
	}
	if it.PullRequest == nil {
		t.Fatalf("the job did not go on to open its pull request: %s\n%s", waitKey(it), s.Story())
	}
}

// TWO TASKS RUN IN THE ORDER THEY WERE ASKED FOR, and a round that finishes
// its step leaves them to run before the next step.
func TestTwoTasksRunInOrder(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "proof")
	entered, release := heldRound(s, "write", Round{})
	defer release()
	s.Events = append(s.Events, "start, and two tasks are sent while write runs")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	first, second := mustTask(s, api.TaskByMainAgent, false), mustTask(s, api.TaskByYou, false)
	if first != "task" || second != "task2" {
		t.Fatalf("the tasks run as %q and %q, want task and task2", first, second)
	}
	s.Check()
	release()
	s.Do("the round ends", nil)
	if !ranIn(s, "write#1", "task#1", "task2#1", "test#1", "proof#1") {
		t.Fatalf("rounds ran as %v\n%s", s.Delegate.ran(), s.Story())
	}
}

// A TASK ASKED FOR NOW ENDS THE ROUND RUNNING AND RUNS AT ONCE. What the cut
// round left in the checkout is still there for the task and for the step,
// which runs the same round again after it.
func TestTaskNowCutsTheRoundAndKeepsItsWork(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "proof")
	entered, release := heldRound(s, "write", Round{})
	defer release()
	s.Events = append(s.Events, "start, and a task is sent now while write's first round runs")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	// The round's work so far: a file it wrote before it was cut.
	half := filepath.Join(worktree(s), "half_written.go")
	s.write(half, "package ledger\n\n// half written when the task came\n")
	got := sendTask(s, api.TaskByMainAgent, true)
	if !got.OK() || wireField(got.Body, `"runs":"`) != api.TaskRunsNow {
		t.Fatalf("the task was not taken to run now: %s %s\n%s", got, got.Body, s.Story())
	}
	it := s.Do("the task runs at once", nil)
	if !ranIn(s, "write#1", "task#1", "write#1", "test#1", "proof#1") {
		t.Fatalf("rounds ran as %v\n%s", s.Delegate.ran(), s.Story())
	}
	if _, err := os.Stat(half); err != nil {
		t.Fatalf("the cut round's work is gone from the checkout: %v\n%s", err, s.Story())
	}
	if out, err := exec.Command("git", "-C", worktree(s), "show", "--stat", "--format=", it.HeadCommit).CombinedOutput(); err != nil || !strings.Contains(string(out), "half_written.go") {
		t.Fatalf("the cut round's work is not in the job's commit: %v\n%s\n%s", err, out, s.Story())
	}
}

// A TASK THAT CHANGES FILES SENDS THE CHECKS THAT HAD PASSED ROUND AGAIN, from
// the first one the change could undo. A task that changes nothing leaves the
// job where it was.
func TestTaskThatChangesFilesRunsThePassedChecksAgain(t *testing.T) {
	t.Parallel()
	for _, changes := range []bool{true, false} {
		name := "changes nothing"
		if changes {
			name = "changes files"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := NewScenario(t, factory.AutonomyAuto, "write", "test", "review", "proof")
			entered, release := heldRound(s, "review", Round{})
			defer release()
			s.Delegate.Next("task", Round{Edit: changes})
			s.Events = append(s.Events, "start, and a task is sent after test passed, while review runs")
			s.MustPress(factory.DoStart)
			begun(s, entered)
			mustTask(s, api.TaskByMainAgent, false)
			s.Check()
			release()
			it := s.Do("the round ends", nil)
			want := []string{"write#1", "test#1", "review#1", "task#1", "proof#1"}
			if changes {
				want = []string{"write#1", "test#1", "review#1", "task#1", "test#2", "review#2", "proof#1"}
			}
			if !ranIn(s, want...) {
				t.Fatalf("rounds ran as %v, want %v\n%s", s.Delegate.ran(), want, s.Story())
			}
			if again := logSays(it, "task changed files. Checking again from test."); again != changes {
				t.Fatalf("the log's word on checking again is %v, want %v\n%s", again, changes, s.Story())
			}
		})
	}
}

// A TASK ON A LANDED JOB WITHDRAWS THE APPROVAL IT WAITED FOR. The task runs,
// the job lands again on a new commit, and that commit needs its own
// approval: the person's in Manual, Auto's own in Auto.
func TestTaskOnALandedJobWithdrawsTheApproval(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := NewScenario(t, factory.AutonomyManual, "write", "proof")
		it := s.Do("start", func() { s.MustPress(factory.DoStart) })
		it = yesUntil(s, it, func(it factory.Item) bool { return it.State == factory.StateLanded })
		if it.Wait.Action != factory.ActApprovePublish || !factory.Offered(it, factory.OfferByMainAgent, factory.DoTask) {
			t.Fatalf("the landed job does not wait for its approval with a task on offer: %s %v\n%s", waitKey(it), it.Offers, s.Story())
		}
		landed := it.HeadCommit
		entered, release := heldRound(s, "task", Round{Edit: true})
		defer release()
		s.Events = append(s.Events, "a task is sent to the landed job")
		mustTask(s, api.TaskByMainAgent, false)
		begun(s, entered)
		if it = s.Item(); it.State == factory.StateLanded || it.Wait.Who != factory.WaitWorking || it.Wait.What != "task" {
			t.Fatalf("the task did not take the job off its approval: state %s, wait %s\n%s", it.State, waitKey(it), s.Story())
		}
		if mode == factory.AutonomyAuto {
			if got := s.Post("control", map[string]any{"mode": factory.AutonomyAuto}); !got.OK() {
				t.Fatalf("switch to Auto: %s", got)
			}
		}
		release()
		it = s.Do("the task ends", nil)
		if it.HeadCommit == landed || it.HeadCommit == "" {
			t.Fatalf("the job did not land on a new commit: %s then %s\n%s", landed, it.HeadCommit, s.Story())
		}
		if mode == factory.AutonomyAuto {
			if pull, open := s.GitHub.PullRequest(); !open || pull.HeadSHA != it.HeadCommit {
				t.Fatalf("Auto did not approve and publish the new commit: %s\n%s", waitKey(it), s.Story())
			}
			return
		}
		if it.State != factory.StateLanded || it.Wait.Action != factory.ActApprovePublish || s.GitHub.Open() {
			t.Fatalf("the new commit does not wait for the person's approval: state %s, wait %s\n%s", it.State, waitKey(it), s.Story())
		}
		it = s.Do("the person approves", func() { s.MustPress(factory.DoApprove) })
		if pull, open := s.GitHub.PullRequest(); !open || pull.HeadSHA != it.HeadCommit {
			t.Fatalf("the approved commit was not published: %s\n%s", waitKey(it), s.Story())
		}
	})
}

// A TASK ON AN OPEN PULL REQUEST IS A FOLLOW PR ROUND. Its commit goes
// through the approval every repair push goes through: nothing is pushed in
// Manual until the person says so.
func TestTaskOnAnOpenPullRequestIsAFollowPRRound(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		opened, _ := s.GitHub.PullRequest()
		published := opened.HeadSHA
		rounds := followRounds(s)
		var step string
		// The pull request has only just been opened and GitHub not read
		// since: the task waits on the job for that read, which queues its
		// round.
		it := s.Do("a task is sent, and GitHub is read", func() {
			step = mustTask(s, api.TaskByMainAgent, false)
			s.Poll()
		})
		if step != factory.FollowPRStep || followRounds(s) != rounds+1 {
			t.Fatalf("the task ran as %q with %d Follow PR rounds, want one more than %d\n%s", step, followRounds(s), rounds, s.Story())
		}
		if slices.ContainsFunc(s.Delegate.ran(), func(r string) bool { return strings.HasPrefix(r, "task") }) {
			t.Fatalf("the task ran as a step of its own on an open pull request: %v\n%s", s.Delegate.ran(), s.Story())
		}
		if mode == factory.AutonomyManual {
			if !factory.FollowUpUpdateWaiting(it) || it.Wait.Action != factory.ActApprovePublish {
				t.Fatalf("the task's commit does not wait for the push approval: %s\n%s", waitKey(it), s.Story())
			}
			if pull, _ := s.GitHub.PullRequest(); pull.HeadSHA != published {
				t.Fatalf("the task's commit was pushed before its approval\n%s", s.Story())
			}
			it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
		}
		if pull, _ := s.GitHub.PullRequest(); it.State != factory.StateShipped || pull.HeadSHA != it.HeadCommit || it.HeadCommit == published {
			t.Fatalf("the task's commit was not pushed: state %s\n%s", it.State, s.Story())
		}
	})
}

// A TASK ASKED FOR WHILE A FOLLOW PR ROUND RUNS IS THE NEXT ROUND'S REQUEST.
// The round running ends first, and its checked repair is not pushed ahead of
// the task that will change its commit.
func TestTaskWaitsForTheFollowPRRoundRunningNow(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		entered, release := heldRound(s, factory.FollowPRStep, Round{})
		defer release()
		s.Events = append(s.Events, "a check fails, and a task is sent while the repair round runs")
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
		begun(s, entered)
		rounds := followRounds(s)
		got := sendTask(s, api.TaskByMainAgent, false)
		if !got.OK() || wireField(got.Body, `"runs":"`) != api.TaskRunsAfterRound {
			t.Fatalf("the task was not taken to run after the round: %s %s\n%s", got, got.Body, s.Story())
		}
		if it := s.Item(); it.FollowUp.Next == "" || followRounds(s) != rounds {
			t.Fatalf("the task did not wait for the round running now: %+v\n%s", it.FollowUp, s.Story())
		}
		s.Check()
		release()
		it := s.Do("the round ends", nil)
		if followRounds(s) != rounds+1 || it.FollowUp.Next != "" {
			t.Fatalf("the task's round did not run after the repair's: %d rounds, follow %+v\n%s", followRounds(s), it.FollowUp, s.Story())
		}
		if !factory.FollowUpUpdateWaiting(it) {
			t.Fatalf("the two rounds' work does not wait to be pushed: %s\n%s", waitKey(it), s.Story())
		}
	})
}

// A TASK ASKED FOR NOW ENDS THE FOLLOW PR ROUND RUNNING. The round the task
// asks for takes its place, with the cut round's own request ahead of the
// task's, and counts as the same repair.
func TestTaskNowCutsAFollowPRRound(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		entered, release := heldRound(s, factory.FollowPRStep, Round{})
		defer release()
		s.Events = append(s.Events, "a check fails, and a task is sent now while the repair round runs")
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
		begun(s, entered)
		attempt := s.Item().FollowUp.Attempt
		got := sendTask(s, api.TaskByMainAgent, true)
		if !got.OK() || wireField(got.Body, `"runs":"`) != api.TaskRunsNow {
			t.Fatalf("the task was not taken to run now: %s %s\n%s", got, got.Body, s.Story())
		}
		it := s.Do("the task's round runs", nil)
		if followRounds(s) != 2 || it.FollowUp.Attempt != attempt {
			t.Fatalf("%d Follow PR rounds at repair %d, want the cut round and the task's at repair %d\n%s", followRounds(s), it.FollowUp.Attempt, attempt, s.Story())
		}
		if !factory.FollowUpUpdateWaiting(it) {
			t.Fatalf("the task's commit does not wait to be pushed: %s\n%s", waitKey(it), s.Story())
		}
	})
}

// A TASK IS THE WORD FOR ONE MORE ROUND. Follow PR stopped at its repair
// limit runs the task's round, as it runs the round an answer asks for.
func TestTaskRunsOnAFollowPRStoppedAtItsLimit(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Change(func(it *factory.Item) { it.FollowUp.Attempt = it.FollowUp.Limit })
		it := s.Do("a check fails with the attempts spent", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.FollowUp.BlockedBy != factory.FollowBlockedAttempts || !factory.Offered(it, factory.OfferByMainAgent, factory.DoTask) {
			t.Fatalf("not the stop, or no task on offer: follow %+v, offers %v\n%s", it.FollowUp, it.Offers, s.Story())
		}
		rounds := followRounds(s)
		it = s.Do("a task is sent", func() { mustTask(s, api.TaskByMainAgent, false) })
		if followRounds(s) != rounds+1 {
			t.Fatalf("the task ran no Follow PR round at the limit\n%s", s.Story())
		}
		if it.FollowUp.BlockedBy == factory.FollowBlockedAttempts {
			t.Fatalf("the job still reads stopped at its limit: %+v\n%s", it.FollowUp, s.Story())
		}
	})
}

// A STEP OUT OF ROUNDS GETS ONE MORE AFTER THE TASK. The main agent meets the
// stop, sends a delegate to clear what stood in the step's way, and the step
// is judged again on the checkout the task left.
func TestTaskOnAStepOutOfRoundsRunsItAgain(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		short := &factory.StageResult{Done: false, Output: "2 tests fail on a fixture the change did not touch.", Findings: 2,
			Tests: &factory.TestCounts{Command: "go test ./...", Passed: 9, Failed: 2}}
		s.Delegate.Next("test", Round{Result: short}, Round{Result: short}, Round{Result: short})
		it := s.Do("start", func() { s.MustPress(factory.DoStart) })
		it = yesUntil(s, it, func(it factory.Item) bool { return it.QKind == factory.QKindOutOfAttempts })
		tests := s.Delegate.Rounds("test")
		it = s.Do("a task is sent", func() { mustTask(s, api.TaskByMainAgent, false) })
		ran := s.Delegate.ran()
		at := slices.Index(ran, "task#1")
		if at < 0 || at+1 >= len(ran) || !strings.HasPrefix(ran[at+1], "test#") || s.Delegate.Rounds("test") != tests+1 {
			t.Fatalf("test did not run once more after the task: %v\n%s", ran, s.Story())
		}
		if it.QKind == factory.QKindOutOfAttempts {
			t.Fatalf("the job still asks about the rounds the task answered\n%s", s.Story())
		}
	})
}

// A TASK NEVER APPROVES A PLAN BY THE BACK DOOR. A plan that waits for its
// approval takes no task and offers none: there is nothing agreed to work on
// until the person, or Auto, has passed it.
func TestTaskIsRefusedBeforeThePlanIsApproved(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyManual)
	it := s.Do("start", func() { s.MustPress(factory.DoStart) })
	if it.Wait.Action != factory.ActApprovePlan {
		t.Fatalf("wait = %s, want you:approve_plan\n%s", waitKey(it), s.Story())
	}
	if factory.Offered(it, factory.OfferByMainAgent, factory.DoTask) || factory.Offered(it, factory.OfferByYou, factory.DoTask) {
		t.Fatalf("a task is offered on a plan nobody approved: %v", it.Offers)
	}
	got := sendTask(s, api.TaskByMainAgent, false)
	if got.Status != http.StatusConflict || !strings.Contains(got.Error, "The plan is waiting for approval") {
		t.Fatalf("the task was not refused in plain words: %s", got)
	}
	if after := s.Do("the refusal", nil); phaseNames(after) != phaseNames(it) || after.Wait.Action != factory.ActApprovePlan {
		t.Fatalf("the refused task changed the job: %s\n%s", phaseNames(after), s.Story())
	}
}

// A FINISHED JOB TAKES NO TASK, and says so in a sentence.
func TestTaskIsRefusedOnAFinishedJob(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyManual)
	it := s.Do("the pull request is merged on GitHub", func() {
		s.GitHub.MergeOutside("mei", s.Now())
		s.Poll()
	})
	if it.Wait.Who != factory.WaitDone || len(it.Offers) != 0 {
		t.Fatalf("the merged job is not done with nothing on offer: %s %v\n%s", waitKey(it), it.Offers, s.Story())
	}
	got := sendTask(s, api.TaskByYou, false)
	if got.Status != http.StatusConflict || got.Error != "This job is finished, so it takes no more tasks." {
		t.Fatalf("the task was not refused in plain words: %s", got)
	}
	if got := s.Post("task", api.Task{Words: "  "}); got.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a task with no instructions answered %s", got)
	}
}

// A LATER STEP EDIT DOES NOT DROP A WAITING TASK. The edit lays the steps not
// begun again from the job's pipeline, which the task is no part of; the
// task still runs, and the step the edit removed does not.
func TestAStepEditKeepsAWaitingTask(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "review", "proof")
	entered, release := heldRound(s, "write", Round{})
	defer release()
	s.Events = append(s.Events, "start, a task is sent while write runs, and review is then taken off the job")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	mustTask(s, api.TaskByMainAgent, false)
	if got := press(s.handler, http.MethodPost, "/v1/jobs/"+strconv.Itoa(s.Job)+"/step-edits", api.StepEditsRequest{Remove: []string{"review"}, Why: "not needed for a one-line change"}); !got.OK() {
		t.Fatalf("the step edit was refused: %s\n%s", got, s.Story())
	}
	it := s.Item()
	if got := phaseNames(it); !strings.Contains(got, "task:pending") || strings.Contains(got, "review") {
		t.Fatalf("after the edit the run is %s, want the task kept and review gone\n%s", got, s.Story())
	}
	s.Check()
	release()
	s.Do("the run goes on", nil)
	ran := s.Delegate.ran()
	if !slices.Contains(ran, "task#1") || slices.ContainsFunc(ran, func(r string) bool { return strings.HasPrefix(r, "review") }) {
		t.Fatalf("rounds ran as %v, want the task and no review\n%s", ran, s.Story())
	}
	if slices.Index(ran, "task#1") > slices.Index(ran, "test#1") {
		t.Fatalf("the task ran after the step it was asked for before: %v\n%s", ran, s.Story())
	}
}

// A RUNNER THAT GOES AWAY DOES NOT TAKE A WAITING TASK WITH IT. The job
// itself holds the task, so the runner that takes the job next runs the step
// it finds cut short and then the task. A runner that quits with a report
// still unconfirmed keeps its hold until it runs out (worker's handBack); the
// scenario waits that out as the service does.
func TestARunnerRestartKeepsAWaitingTask(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "proof")
	entered, release := heldRound(s, "write", Round{})
	defer release()
	s.Events = append(s.Events, "start, a task is sent while write runs, and the runner restarts")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	mustTask(s, api.TaskByMainAgent, false)
	s.StopRunner()
	release()
	if s.held() {
		s.Advance(s.Desk.TTL() + time.Minute)
		s.Tick()
	}
	if got := phaseNames(s.Item()); !strings.Contains(got, "task:pending") {
		t.Fatalf("the task went with the runner: %s\n%s", got, s.Story())
	}
	s.StartRunner()
	s.Do("the runner comes back", nil)
	ran := s.Delegate.ran()
	if n := len(ran); n < 5 || !strings.HasPrefix(ran[n-4], "write#") || !slices.Equal(ran[n-3:], []string{"task#1", "test#1", "proof#1"}) {
		t.Fatalf("rounds ran as %v, want write again, then the task, test and proof\n%s", ran, s.Story())
	}
}

// A TASK ON A JOB WHOSE CHECKOUT IS ON ANOTHER COMPUTER WAITS FOR THAT
// COMPUTER. The task is on the job, not on a runner: with only another
// computer online the job waits in the queue with a deadline, and the
// computer that holds the checkout runs the task when it is back.
func TestTaskWaitsForTheComputerThatHoldsTheCheckout(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "test", "proof")
	entered, release := heldRound(s, "test", Round{})
	defer release()
	s.Events = append(s.Events, "start, and the laptop goes away while test runs")
	s.MustPress(factory.DoStart)
	begun(s, entered)
	s.SwitchComputer("desktop")
	release()
	if s.held() {
		s.Advance(s.Desk.TTL() + time.Minute)
		s.Tick()
	}
	it := s.Do("a task is sent with only the other computer online", func() { mustTask(s, api.TaskByMainAgent, false) })
	if it.Wait.Who != factory.WaitRunner || it.Wait.Deadline.IsZero() || slices.Contains(s.Delegate.ran(), "task#1") {
		t.Fatalf("the task did not wait for the computer with the checkout: %s, rounds %v\n%s", waitKey(it), s.Delegate.ran(), s.Story())
	}
	if got := phaseNames(it); !strings.Contains(got, "task:pending") {
		t.Fatalf("the task is not on the job: %s\n%s", got, s.Story())
	}
	s.SwitchComputer(scenarioMachine)
	s.Do("the laptop comes back", nil)
	ran := s.Delegate.ran()
	if n := len(ran); n < 4 || !slices.Contains(ran, "task#1") || ran[n-1] != "proof#1" || slices.Index(ran, "task#1") > slices.Index(ran, "proof#1") {
		t.Fatalf("rounds ran as %v, want the task on the laptop before proof\n%s", ran, s.Story())
	}
}

// REQUEST CHANGES ON A JOB A RUNNER RAN KEEPS ITS RESULTS. The prove step it
// adds is a step of the run and no step of the pipeline, as a task is, and
// the service used to refuse the runner's report of it as an unknown step:
// the job stopped on "results not saved".
func TestRequestChangesRunsItsProveStep(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyManual, "write", "proof")
	it := s.Do("start", func() { s.MustPress(factory.DoStart) })
	it = yesUntil(s, it, func(it factory.Item) bool { return it.State == factory.StateLanded })
	it = s.Do("the person requests changes", func() {
		if got := s.Post("request_changes", map[string]any{"words": "Show the rounding on a negative balance."}); !got.OK() {
			t.Fatalf("request changes: %s", got)
		}
	})
	it = yesUntil(s, it, func(it factory.Item) bool { return it.State == factory.StateLanded || it.QKind == "results_not_saved" })
	if it.QKind == "results_not_saved" || it.State != factory.StateLanded || !slices.Contains(s.Delegate.ran(), "prove#1") {
		t.Fatalf("the prove step did not run and land: state %s, question %q, rounds %v\n%s", it.State, it.Question, s.Delegate.ran(), s.Story())
	}
}

// logSays says the job's log holds a line with these words.
func logSays(it factory.Item, words string) bool {
	return slices.ContainsFunc(logOf(it), func(l factory.LogLine) bool { return strings.Contains(l.Text, words) })
}
