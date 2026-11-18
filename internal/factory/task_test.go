package factory

import (
	"strings"
	"testing"
	"time"
)

// taskJob is a job at its write step, with plan done before it and test and
// proof after: the place most tasks find a job.
func taskJob(state State, write PhaseState) Item {
	return Item{ID: 7, Repo: "acme/api", Kind: KindIssue, Num: 7, State: state, Cap: 10,
		Stages: []Stage{
			{Name: "plan", Kind: StageChat, Until: UntilDone, On: true},
			{Name: "write", Kind: StageChat, Until: UntilDone, On: true},
			{Name: "test", Kind: StageChat, Until: UntilNoNewFailures, On: true},
			{Name: "proof", Kind: StageChat, Until: UntilProven, On: true},
		},
		Stream: &Stream{Cur: 1, Phases: []Phase{
			{Name: "plan", Kind: StageChat, State: PhaseDone, Round: 1},
			{Name: "write", Kind: StageChat, State: write, Round: 1},
			{Name: "test", Kind: StageChat, State: PhasePending},
			{Name: "proof", Kind: StageChat, State: PhasePending},
		}}}
}

// taskPR is a job whose pull request is open and watched.
func taskPR() Item {
	it := taskJob(StateShipped, PhaseDone)
	for i := range it.Stream.Phases {
		it.Stream.Phases[i].State = PhaseDone
	}
	it.Stream.Phases = append(it.Stream.Phases, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting})
	it.Stream.Cur = len(it.Stream.Phases) - 1
	it.Publish, it.HeadCommit = "pr", "abc"
	it.PullRequest = &PullRequest{Number: 9, State: PROpen, HeadSHA: "abc", ChecksRead: true, ReviewsRead: true}
	it.FollowUp = &PRFollowUp{Kind: "pr", State: "watching", Limit: DefaultPRRepairAttempts, Reason: "Watching CI, review feedback and conflicts."}
	return it
}

// WHEN A TASK MAY BE ADDED. Each row is one state a task can find a job in:
// the refusal's words, or none, and whether the job offers a task there. The
// offer and the door read the same rule, so they never disagree.
func TestTaskRefusal(t *testing.T) {
	stopped := true
	cases := []struct {
		name string
		item func() Item
		want string
	}{
		{"a running step takes a task", func() Item { return taskJob(StateRunning, PhaseRunning) }, ""},
		{"a queued job that has begun takes a task", func() Item { return taskJob(StateQueued, PhasePending) }, ""},
		{"a held job takes a task, for when it is resumed", func() Item {
			it := taskJob(StateRunning, PhaseRunning)
			it.Stream.Paused = true
			return it
		}, ""},
		{"a landed job takes a task", func() Item {
			it := taskJob(StateLanded, PhaseDone)
			it.Proof = []Claim{{Text: "works", OK: true}}
			return it
		}, ""},
		{"a step out of rounds takes a task", func() Item {
			it := taskJob(StateNeedsYou, PhaseWaiting)
			it.Question, it.QKind = "write is out of rounds · try again?", QKindOutOfAttempts
			return it
		}, ""},
		{"a failed step takes a task", func() Item {
			it := taskJob(StateNew, PhaseFailed)
			it.Failure = &Failure{Step: "write", Code: "error", Detail: "the delegate crashed"}
			return it
		}, ""},
		{"a step's own question takes a task", func() Item {
			it := taskJob(StateNeedsYou, PhaseWaiting)
			it.Question, it.QKind = "write asks: which table?", QKindStep
			it.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedYou}
			return it
		}, ""},
		{"an open pull request takes a task", taskPR, ""},
		{"Follow PR stopped at its limit takes a task", func() Item {
			it := taskPR()
			it.FollowUp.State, it.FollowUp.BlockedBy, it.FollowUp.Attempt = "needs_you", FollowBlockedAttempts, 3
			return it
		}, ""},
		{"a repair waiting to be pushed takes a task", func() Item {
			it := taskPR()
			it.State = StateLanded
			it.FollowUp.State = "waiting_approval"
			return it
		}, ""},

		{"no instructions", func() Item { return taskJob(StateRunning, PhaseRunning) }, "Say what the task is."},
		{"a job nobody started", func() Item { return Item{ID: 7, Kind: KindIssue, State: StateNew, Cap: 10} }, "Start the job first."},
		{"a job queued for its first step", func() Item {
			it := taskJob(StateQueued, PhasePending)
			it.Stream.Cur = 0
			it.Stream.Phases[0] = Phase{Name: "plan", Kind: StageChat, State: PhasePending}
			it.Stream.Phases[1].Round = 0
			return it
		}, "Start the job first."},
		{"a merged job", func() Item {
			it := taskPR()
			it.PullRequest.State = PRMerged
			return it
		}, "This job is finished"},
		{"a dropped job", func() Item {
			it := taskJob(StateDismissed, PhaseRunning)
			it.DoneHow = "dropped"
			return it
		}, DroppedRefusal},
		{"a job its person stopped", func() Item {
			it := taskJob(StateNew, PhaseFailed)
			it.PersonStopped = &stopped
			return it
		}, "You stopped this job."},
		{"a decision put away", func() Item {
			it := taskJob(StateNeedsYou, PhaseWaiting)
			it.Question, it.QKind = "write is out of rounds · try again?", QKindOutOfAttempts
			it.PausedRecoveryQuestion = pausedDecisionPrefix + "write"
			return it
		}, "A decision on this job was put away."},
		{"a job its person took over", func() Item {
			it := taskJob(StateNeedsYou, PhaseRunning)
			it.Manual, it.QKind = true, "manual"
			return it
		}, "You have taken this job over."},
		{"the budget is reached", func() Item {
			it := taskJob(StateRunning, PhaseRunning)
			it.Stream.Spent = it.Cap
			return it
		}, "Job budget reached."},
		{"a pull request job with no budget", func() Item {
			it := taskPR()
			it.Cap = 0
			return it
		}, "Choose a budget"},
		{"a publish in flight", func() Item {
			it := taskJob(StateQueued, PhaseDone)
			it.Publishing = true
			return it
		}, "A push is in flight."},
		{"an approval inside its undo window", func() Item {
			it := taskJob(StateLanded, PhaseDone)
			it.Approval = &Approval{Kind: "proof", State: "approving", UndoUntil: time.Now().Add(time.Minute)}
			return it
		}, "A push is in flight."},
		{"a merge in flight", func() Item {
			it := taskPR()
			it.Merging = &MergeRun{State: MergeQueued, Commit: "abc"}
			return it
		}, "A merge is in flight."},
		{"a plan waiting for its approval", func() Item {
			it := taskJob(StateNeedsYou, PhasePending)
			it.Stages = append(it.Stages[:1:1], append([]Stage{{Name: ApproveName, Kind: StageGate, On: true}}, it.Stages[1:]...)...)
			it.Stream.Phases = append(it.Stream.Phases[:1:1], append([]Phase{{Name: ApproveName, Kind: StageGate, State: PhaseWaiting}}, it.Stream.Phases[1:]...)...)
			it.Stream.Phases[2].Round = 0
			it.Question, it.QKind = ApproveQuestion("plan"), QKindApprove
			return it
		}, "The plan is waiting for approval."},
		{"a job stopped after its step, as its person asked", func() Item {
			it := taskJob(StateNeedsYou, PhaseDone)
			it.Question, it.QKind = "Stopped spending after write as you asked. Carry on?", QKindSpendStopped
			return it
		}, "stopped after its last step"},
		{"results the service refused", func() Item {
			it := taskJob(StateNeedsYou, PhaseWaiting)
			it.Question, it.QKind = "The factory refused this step's results (HTTP 422).", "results_not_saved"
			return it
		}, "results were not saved"},
		{"a job an AgentField reasoner runs", func() Item {
			it := taskJob(StateRunning, PhaseRunning)
			it.RunOn = "node.reasoner"
			return it
		}, "runs on AgentField"},
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.item()
			words := "Add the rounding note."
			if c.name == "no instructions" {
				words = "  "
			}
			err := TaskRefusal(it, words)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("refusal = %v, want one saying %q", err, c.want)
			}
			SettleWait(Item{}, &it, now)
			offered := c.want == "" || c.name == "no instructions"
			for _, by := range []string{OfferByYou, OfferByMainAgent} {
				if Offered(it, by, DoTask) != offered {
					t.Errorf("offers %s a task: %v, want %v (offers %v)", by, !offered, offered, it.Offers)
				}
			}
			// A task is a move of no wait, so offering one never makes a wait
			// valid or invalid by itself.
			without := it
			without.Offers = nil
			for _, o := range it.Offers {
				if o.Action != DoTask {
					without.Offers = append(without.Offers, o)
				}
			}
			if (WaitFault(it) == nil) != (WaitFault(without) == nil) {
				t.Errorf("the task offer changed the wait's fault: %v with it, %v without", WaitFault(it), WaitFault(without))
			}
		})
	}
}

// WHERE A TASK GOES, by what the job is doing and whether a runner holds it.
func TestTaskRoad(t *testing.T) {
	landed := taskJob(StateLanded, PhaseDone)
	asking := taskJob(StateNeedsYou, PhaseWaiting)
	asking.Question, asking.QKind = "write asks: which table?", QKindStep
	asking.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedYou}
	stuck := taskJob(StateNeedsYou, PhaseWaiting)
	stuck.Question, stuck.QKind = "write is out of rounds · try again?", QKindOutOfAttempts
	following := taskPR()
	following.State, following.FollowUp.State = StateRunning, "running"
	cases := []struct {
		name      string
		item      Item
		held, now bool
		want      string
	}{
		{"a round is running: after it", taskJob(StateRunning, PhaseRunning), true, false, TaskRunsAfter},
		{"a round is running and the task is asked for now: before the step", taskJob(StateRunning, PhaseRunning), true, true, TaskRunsBefore},
		{"nobody holds the job: before the step", taskJob(StateQueued, PhaseRunning), false, false, TaskRunsBefore},
		{"the step has not begun its round: before it", taskJob(StateRunning, PhasePending), true, false, TaskRunsBefore},
		{"a round waits on its own question: after it", asking, true, false, TaskRunsAfter},
		{"a step out of rounds has no round running: before it", stuck, true, false, TaskRunsBefore},
		{"a landed job: after its last finished step", landed, false, false, TaskRunsLanded},
		{"an open pull request: the next Follow PR round", taskPR(), false, false, TaskRunsFollow},
		{"a Follow PR round is running: the round after it", following, true, false, TaskRunsFollowNext},
		{"a Follow PR round is running and the task is asked for now", following, true, true, TaskRunsFollow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TaskRoad(c.item, c.held, c.now); got != c.want {
				t.Fatalf("road = %s, want %s", got, c.want)
			}
		})
	}
}

// A task's step is named task, then task2, task3: no step or phase of the
// job shares its name, and its instructions are kept whole beside a note
// that may change.
func TestTaskPhaseIsNamedApartFromEveryStep(t *testing.T) {
	it := taskJob(StateRunning, PhaseRunning)
	first := TaskPhase(it, TaskByMainAgent, "Add the note.\nKeep it short.")
	if first.Name != "task" || first.Note != "Add the note." || first.Task.Words != "Add the note.\nKeep it short." || first.Task.By != TaskByMainAgent {
		t.Fatalf("first task = %+v %+v", first, first.Task)
	}
	it.Stream.Phases = InsertPhase(it.Stream.Phases, AfterRound(it.Stream.Phases, 1), first)
	second := TaskPhase(it, "someone", "And this.")
	if second.Name != "task2" || second.Task.By != TaskByYou {
		t.Fatalf("second task = %+v %+v", second, second.Task)
	}
	it.Stream.Phases = InsertPhase(it.Stream.Phases, AfterRound(it.Stream.Phases, 1), second)
	it.Stages = append(it.Stages, Stage{Name: "task3", Kind: StageChat, On: true})
	if third := TaskPhase(it, TaskByYou, "And that."); third.Name != "task4" {
		t.Fatalf("third task = %s, want task4 past the step named task3", third.Name)
	}
	var names []string
	for _, ph := range it.Stream.Phases {
		names = append(names, ph.Name)
	}
	if got := strings.Join(names, " "); got != "plan write task task2 test proof" {
		t.Fatalf("phases = %s, want the tasks after the running step in the order asked", got)
	}
	st, index := PhaseStage(it, it.Stream.Phases[2])
	if index != -1 || st.Ask != "Add the note.\nKeep it short." || st.Until != UntilDone || st.Max != TaskRounds || st.By != ByManager {
		t.Fatalf("task stage = %+v at %d", st, index)
	}
	if i, ok := WaitingTask(it); !ok || i != 2 {
		t.Fatalf("waiting task = %d %v, want 2", i, ok)
	}
}

// KeepTasks puts each waiting task back before the first step that followed
// it and is still in the tail, and last when none is.
func TestKeepTasksPutsEachTaskBackInItsPlace(t *testing.T) {
	task := func(name string) Phase { return NamedTaskPhase(name, TaskByYou, "do it") }
	step := func(name string) Phase { return Phase{Name: name, Kind: StageChat, State: PhasePending} }
	names := func(phases []Phase) string {
		var out []string
		for _, ph := range phases {
			out = append(out, ph.Name)
		}
		return strings.Join(out, " ")
	}
	cases := []struct {
		name      string
		old, laid []Phase
		want      string
	}{
		{"the tail is laid as it was", []Phase{task("task"), step("test"), step("proof")}, []Phase{step("test"), step("proof")}, "task test proof"},
		{"the step it ran before is removed", []Phase{step("test"), task("task"), step("review"), step("proof")}, []Phase{step("test"), step("proof")}, "test task proof"},
		{"every step after it is removed", []Phase{step("test"), task("task"), step("review")}, []Phase{step("test")}, "test task"},
		{"two tasks keep their order", []Phase{task("task"), task("task2"), step("test")}, []Phase{step("lint"), step("test")}, "lint task task2 test"},
		{"a step is added before it", []Phase{task("task"), step("proof")}, []Phase{step("security"), step("proof")}, "security task proof"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := names(KeepTasks(c.old, c.laid)); got != c.want {
				t.Fatalf("tail = %s, want %s", got, c.want)
			}
		})
	}
}

// A task on an open pull request is the next Follow PR round's request: the
// round is queued, a repair count at its limit gives this one more, and an
// approval the job waited for is withdrawn.
func TestQueueFollowTask(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	t.Run("a watched pull request", func(t *testing.T) {
		it := taskPR()
		QueueFollowTask(&it, TaskByMainAgent, "Add the note.", now)
		if it.State != StateQueued || it.FollowUp.State != "queued" || !strings.Contains(it.FollowUp.Request, "Add the note.") || !strings.Contains(it.FollowUp.Request, "Do not push") {
			t.Fatalf("state %s, follow %+v", it.State, it.FollowUp)
		}
	})
	t.Run("a repair waiting to be pushed", func(t *testing.T) {
		it := taskPR()
		it.State, it.FollowUp.State, it.FollowUp.Attempt = StateLanded, "waiting_approval", 3
		it.Approval = &Approval{Kind: "proof", State: "waiting"}
		QueueFollowTask(&it, TaskByYou, "Add the note.", now)
		if it.State != StateQueued || it.FollowUp.State != "queued" || it.FollowUp.Attempt != 3 || it.Approval != nil {
			t.Fatalf("state %s, follow %+v, approval %+v", it.State, it.FollowUp, it.Approval)
		}
	})
	t.Run("a pull request not read yet keeps the request for the read", func(t *testing.T) {
		it := taskPR()
		it.PullRequest.HeadSHA = ""
		QueueFollowTask(&it, TaskByYou, "Add the note.", now)
		if it.State != StateShipped || it.FollowUp.State != "watching" || it.FollowUp.Request == "" {
			t.Fatalf("state %s, follow %+v", it.State, it.FollowUp)
		}
		it.PullRequest.HeadSHA = "abc"
		if !QueueCIFollowUp(&it, now) || it.FollowUp.State != "queued" {
			t.Fatalf("the read did not queue the task's round: %+v", it.FollowUp)
		}
	})
	t.Run("a round queued and not begun takes the task with it", func(t *testing.T) {
		it := taskPR()
		it.State, it.FollowUp.State, it.FollowUp.Attempt, it.FollowUp.Request = StateQueued, "queued", 1, "Fix the failing check."
		QueueFollowTask(&it, TaskByYou, "Add the note.", now)
		if it.FollowUp.Attempt != 1 || !strings.HasPrefix(it.FollowUp.Request, "Fix the failing check.\n") || !strings.Contains(it.FollowUp.Request, "Add the note.") {
			t.Fatalf("follow %+v", it.FollowUp)
		}
	})
	t.Run("a task kept for the next round starts when the round is over", func(t *testing.T) {
		it := taskPR()
		it.State, it.FollowUp.State = StateRunning, "running"
		HoldFollowTask(&it, TaskByMainAgent, "Add the note.")
		HoldFollowTask(&it, TaskByYou, "And the changelog.")
		if strings.Count(it.FollowUp.Next, "An extra task from") != 2 || it.FollowUp.State != "running" {
			t.Fatalf("follow %+v", it.FollowUp)
		}
		it.State, it.FollowUp.State = StateLanded, "waiting_approval"
		if !StartHeldFollowTask(&it, now) || it.FollowUp.Next != "" || it.FollowUp.State != "queued" || it.State != StateQueued {
			t.Fatalf("state %s, follow %+v", it.State, it.FollowUp)
		}
		if at, second := strings.Index(it.FollowUp.Request, "Add the note."), strings.Index(it.FollowUp.Request, "And the changelog."); at < 0 || second < at {
			t.Fatalf("the two tasks are not in the round's request in order: %q", it.FollowUp.Request)
		}
		if StartHeldFollowTask(&it, now) {
			t.Fatal("a job with no task kept was changed")
		}
	})
	t.Run("a task kept waits while a push is in flight", func(t *testing.T) {
		it := taskPR()
		HoldFollowTask(&it, TaskByYou, "Add the note.")
		it.Publishing = true
		if StartHeldFollowTask(&it, now) || it.FollowUp.Next == "" {
			t.Fatalf("the task started during a push: %+v", it.FollowUp)
		}
	})
}

// A job with a task waiting behind the round running now says so in its
// wait's words; a task that is queued or running is the step the wait names.
func TestWaitNamesATask(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	it := taskJob(StateRunning, PhaseRunning)
	it.Stream.Phases = InsertPhase(it.Stream.Phases, AfterRound(it.Stream.Phases, 1), TaskPhase(it, TaskByYou, "Add the note."))
	SettleWait(Item{}, &it, now)
	if it.Wait.key() != "working:write" || it.Wait.Words != "A task runs when this round ends." {
		t.Fatalf("wait = %s %q", it.Wait.key(), it.Wait.Words)
	}
	it.Stream.Phases[1].State, it.Stream.Phases[2].State, it.Stream.Cur = PhaseDone, PhaseRunning, 2
	SettleWait(Item{}, &it, now)
	if it.Wait.key() != "working:task" || it.Wait.Words != "" {
		t.Fatalf("wait = %s %q", it.Wait.key(), it.Wait.Words)
	}
	it.State, it.Stream.Phases[2].State = StateQueued, PhasePending
	SettleWait(Item{}, &it, now)
	if it.Wait.key() != "runner:task" {
		t.Fatalf("wait = %s", it.Wait.key())
	}
	if err := WaitFault(it); err != nil {
		t.Fatal(err)
	}
}

// Manual's hold before a step never holds a task, and a no at an approval
// sends back the step the pipeline ran, never a task.
func TestATaskIsNotHeldAndNotSentBackTo(t *testing.T) {
	it := taskJob(StateQueued, PhasePending)
	it.Autonomy = AutonomyManual
	it.Stream.Phases[1].Round = 0
	if !AsksBeforeStep(it, 1) {
		t.Fatal("the fixture's write step is not held in Manual; the test proves nothing")
	}
	it.Stream.Phases = InsertPhase(it.Stream.Phases, 1, TaskPhase(it, TaskByYou, "Add the note."))
	if AsksBeforeStep(it, 1) {
		t.Fatal("Manual holds a task before it runs")
	}
	it.Stream.Phases[1].State = PhaseDone
	if prev := PreviousWork(it.Stream.Phases, 2); prev != 0 {
		t.Fatalf("the work before write is phase %d, want plan at 0", prev)
	}
}
