package factory

import (
	"testing"
	"time"
)

func waitRun(state State, phase Phase) Item {
	return Item{ID: 7, Repo: "acme/api", Kind: KindIssue, Num: 7, State: state, Cap: 10,
		Stream: &Stream{Phases: []Phase{phase}, Cur: 0}}
}

func TestWaitOf(t *testing.T) {
	open := &PullRequest{Number: 9, State: PROpen, HeadSHA: "abc", ChecksRead: true, ReviewsRead: true}
	cases := []struct {
		name   string
		item   func() Item
		want   string
		offers []string
	}{
		{"a job nobody started waits in the backlog", func() Item { return Item{State: StateNew} }, "backlog", []string{DoStart}},
		{"a running step is working", func() Item {
			return waitRun(StateRunning, Phase{Name: "write", Kind: StageChat, State: PhaseRunning})
		}, "working:write", nil},
		{"a queued job waits on a runner", func() Item {
			return waitRun(StateQueued, Phase{Name: "write", Kind: StageChat, State: PhasePending})
		}, "runner:write", nil},
		{"a held job waits on its person, put away", func() Item {
			it := waitRun(StateRunning, Phase{Name: "write", Kind: StageChat, State: PhaseRunning})
			it.Stream.Paused = true
			return it
		}, "you:resume:put away", []string{DoResume}},
		{"a question the manager holds is the manager's", func() Item {
			it := waitRun(StateRunning, Phase{Name: "write", Kind: StageChat, State: PhaseRunning})
			it.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedManager}
			return it
		}, "main_agent:write", nil},
		{"a step's question sent on waits on the person's answer", func() Item {
			it := waitRun(StateNeedsYou, Phase{Name: "write", Kind: StageChat, State: PhaseWaiting})
			it.Question, it.QKind = "write asks: which table?", QKindStep
			it.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedYou}
			return it
		}, "you:answer", []string{DoAnswer}},
		{"a landed job waits on the approval that publishes it", func() Item {
			it := waitRun(StateLanded, Phase{Name: "proof", Kind: StageChat, State: PhaseDone})
			it.Proof = []Claim{{Text: "works", OK: true}}
			return it
		}, "you:approve_publish", []string{DoApprove}},
		{"a conflict fix waiting to be pushed outranks the conflict (job 31)", func() Item {
			it := waitRun(StateLanded, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseDone})
			pr := *open
			pr.Conflict = true
			it.PullRequest = &pr
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "waiting_approval", Reason: "Repair and proof passed locally; updating the existing PR."}
			return it
		}, "you:approve_publish", []string{DoApprove}},
		{"a watched pull request waits on GitHub", func() Item {
			it := waitRun(StateShipped, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseDone})
			pr := *open
			pr.Checks = PRChecks{Pending: 2}
			it.PullRequest = &pr
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "watching", Reason: "Watching CI, review feedback and conflicts."}
			return it
		}, "github:checks", nil},
		{"Follow PR out of attempts waits on the person's retry", func() Item {
			it := waitRun(StateShipped, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting})
			pr := *open
			it.PullRequest = &pr
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "needs_you", BlockedBy: FollowBlockedAttempts, Reason: "Follow PR reached its repair limit."}
			return it
		}, "you:retry", []string{DoResumePipeline}},
		{"a budget stop with the budget raised and the attempts spent waits on a retry", func() Item {
			it := waitRun(StateShipped, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting})
			pr := *open
			it.PullRequest = &pr
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "needs_you", BlockedBy: FollowBlockedBudget, Attempt: 3, Limit: 3, Reason: "Job budget reached."}
			return it
		}, "you:retry", []string{DoResumePipeline}},
		{"a budget stop with the budget spent waits on the raise", func() Item {
			it := waitRun(StateShipped, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting})
			pr := *open
			it.PullRequest = &pr
			it.Stream.Spent = it.Cap
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "needs_you", BlockedBy: FollowBlockedBudget, Attempt: 3, Limit: 3, Reason: "Job budget reached."}
			return it
		}, "you:budget", []string{DoRaiseBudget}},
		{"a conflict nobody is repairing is the person's to decide", func() Item {
			it := waitRun(StateShipped, Phase{Name: "proof", Kind: StageChat, State: PhaseDone})
			pr := *open
			pr.Conflict = true
			it.PullRequest = &pr
			return it
		}, "you:decide", []string{DoResumePipeline}},
		{"a failed step waits on the person's retry", func() Item {
			it := waitRun(StateNew, Phase{Name: "test", Kind: StageChat, State: PhaseFailed})
			it.Failure = &Failure{Step: "test", Code: "error", Detail: "the worker crashed"}
			return it
		}, "you:retry", []string{DoRerunStep}},
		{"results the service refused wait on a resend, with or without a pull request", func() Item {
			it := waitRun(StateNeedsYou, Phase{Name: "write", Kind: StageChat, State: PhaseWaiting})
			it.Question, it.QKind = "The factory refused this step's results (HTTP 422).", "results_not_saved"
			return it
		}, "you:retry", []string{DoRerunStep, DoResumePipeline}},
		{"a failed step the service is about to retry waits on a runner", func() Item {
			it := waitRun(StateNew, Phase{Name: "test", Kind: StageChat, State: PhaseFailed})
			it.Autonomy = AutonomyAuto
			it.Failure = &Failure{Step: "test", Code: CodeTransportError, Detail: "connection reset by peer", Next: FailureStopped}
			it.Recovery = &StepRecovery{Step: "test", State: "waiting", Limit: RecoveryLimit, Reason: "connection reset by peer"}
			return it
		}, "runner:test", nil},
		{"a repair the pull request moved past waits on Follow PR, not on a retry of the publish", func() Item {
			it := waitRun(StateLanded, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseDone})
			pr := *open
			pr.HeadSHA = "def"
			it.PullRequest = &pr
			it.FollowUp = &PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: "abc"}
			it.PublishError = "the existing PR closed or its head moved; review it before publishing the repair"
			it.PublishStatus = &Publishing{State: "failed", Code: "unknown", Attempt: 1}
			return it
		}, "you:retry", []string{DoResumePipeline}},
		{"a merged pull request is done", func() Item {
			it := waitRun(StateShipped, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseDone})
			pr := *open
			pr.State = PRMerged
			it.PullRequest = &pr
			return it
		}, "done", nil},
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.item()
			SettleWait(Item{}, &it, now)
			if got := it.Wait.key(); got != c.want {
				t.Fatalf("wait = %s, want %s", got, c.want)
			}
			for _, action := range c.offers {
				if !Offered(it, OfferByYou, action) {
					t.Errorf("does not offer %s; offers %v", action, it.Offers)
				}
			}
			if err := WaitFault(it); err != nil {
				t.Errorf("fault: %v", err)
			}
		})
	}
}

func TestSettleWaitKeepsTheClockOfAWaitThatGoesOn(t *testing.T) {
	first := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	it := waitRun(StateLanded, Phase{Name: "proof", Kind: StageChat, State: PhaseDone})
	it.Proof = []Claim{{Text: "works", OK: true}}
	SettleWait(Item{}, &it, first)
	before := it
	it.Notes = append(it.Notes, "a note changes nothing about the wait")
	SettleWait(before, &it, first.Add(time.Hour))
	if !it.Wait.Since.Equal(first) {
		t.Fatalf("since = %v, want the first write's %v", it.Wait.Since, first)
	}
	before = it
	it.State = StateRunning
	it.Stream.Phases[0].State = PhaseRunning
	SettleWait(before, &it, first.Add(2*time.Hour))
	if !it.Wait.Since.Equal(first.Add(2 * time.Hour)) {
		t.Fatalf("a new wait kept the old clock: %v", it.Wait.Since)
	}
}

func TestWaitFaultNamesAWaitWithNoWayOut(t *testing.T) {
	it := waitRun(StateNeedsYou, Phase{Name: "write", Kind: StageChat, State: PhaseWaiting})
	SettleWait(Item{}, &it, time.Now())
	if it.Wait.Who != WaitYou {
		t.Fatalf("wait = %s", it.Wait.key())
	}
	if err := WaitFault(it); err == nil {
		t.Fatalf("a job that needs its person with no question, and offers %v, passed", it.Offers)
	}
}

// Results a runner could not save are not a question: an answer would take
// the notice down and send nothing, so the job does not offer one.
func TestRefusedResultsOfferNoAnswer(t *testing.T) {
	it := waitRun(StateNeedsYou, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting})
	it.PullRequest = &PullRequest{Number: 9, State: PROpen, HeadSHA: "abc"}
	it.Question, it.QKind = "The factory refused this step's results (HTTP 422).", "results_not_saved"
	SettleWait(Item{}, &it, time.Now())
	if Offered(it, OfferByYou, DoAnswer) || !Offered(it, OfferByYou, DoResumePipeline) {
		t.Fatalf("offers %v, want resume_pipeline and no answer", it.Offers)
	}
	// The rerun always moves the job; a second send is refused again for most
	// results. With the budget spent only the send is left.
	if got := WaitMoves(it); len(got) != 2 || got[0] != DoRerunStep {
		t.Fatalf("the wait names %v, want rerun_step first", got)
	}
	it.Stream.Spent = it.Cap
	SettleWait(Item{}, &it, time.Now())
	if got := WaitMoves(it); len(got) != 1 || got[0] != DoResumePipeline || !Offered(it, OfferByYou, DoRaiseBudget) {
		t.Fatalf("with the budget spent the wait names %v and offers %v", got, it.Offers)
	}
	if err := WaitFault(it); err != nil {
		t.Fatal(err)
	}
}

// A step that lost its runner asks nothing: only a lost publish takes an
// answer. Its doors are the rerun and, on an open pull request, Resume
// pipeline.
func TestALostRunnersStepOffersNoAnswer(t *testing.T) {
	it := waitRun(StateNeedsYou, Phase{Name: FollowPRStep, Kind: StageChat, State: PhaseFailed})
	it.PullRequest = &PullRequest{Number: 9, State: PROpen, HeadSHA: "abc"}
	it.Question, it.QKind = "Runner went offline mid-step", NeedsRunnerOffline
	it.Failure = &Failure{Step: FollowPRStep, Code: CodeMachineLost, Detail: "Runner went offline mid-step", Next: FailureStopped}
	SettleWait(Item{}, &it, time.Now())
	if it.Wait.Action != ActComputer {
		t.Fatalf("wait = %s, want you:computer", it.Wait.key())
	}
	if Offered(it, OfferByYou, DoAnswer) || !Offered(it, OfferByYou, DoRerunStep) || !Offered(it, OfferByYou, DoResumePipeline) {
		t.Fatalf("offers %v, want rerun_step and resume_pipeline and no answer", it.Offers)
	}
	if err := WaitFault(it); err != nil {
		t.Fatal(err)
	}
	// A lost publish is the one notice that takes an answer.
	it.Publishing = true
	SettleWait(Item{}, &it, time.Now())
	if !Offered(it, OfferByYou, DoAnswer) {
		t.Fatalf("a lost publish offers %v, want answer", it.Offers)
	}
}

// An approval the person already gave is the service's to carry out when its
// undo window closes. A landed job in that window read "you: approve" with
// nothing to press.
func TestAnApprovalInItsUndoWindowIsTheServicesAct(t *testing.T) {
	until := time.Now().Add(time.Minute)
	it := waitRun(StateLanded, Phase{Name: "proof", Kind: StageChat, State: PhaseDone})
	it.Approval = &Approval{State: "approving", UndoUntil: until}
	SettleWait(Item{}, &it, time.Now())
	if it.Wait.key() != "working:approving" || !it.Wait.Deadline.Equal(until) {
		t.Fatalf("wait = %s, deadline %v; want working:approving until the window closes", it.Wait.key(), it.Wait.Deadline)
	}
	if err := WaitFault(it); err != nil {
		t.Fatal(err)
	}
}

// A failed step with the budget spent waits on the budget first: the rerun is
// refused until it is raised. Raised, it reads "retry" and offers the rerun.
func TestAFailedStepWithTheBudgetSpentWaitsOnTheBudget(t *testing.T) {
	it := waitRun(StateNew, Phase{Name: "write", Kind: StageChat, State: PhaseFailed})
	it.Failure = &Failure{Step: "write", Code: "error", Detail: "the worker crashed"}
	it.Stream.Spent = it.Cap
	SettleWait(Item{}, &it, time.Now())
	if it.Wait.key() != "you:budget" || !Offered(it, OfferByYou, DoRaiseBudget) {
		t.Fatalf("wait = %s, offers %v; want you:budget with raise_budget", it.Wait.key(), it.Offers)
	}
	if err := WaitFault(it); err != nil {
		t.Fatal(err)
	}
	it.Cap = 20
	SettleWait(Item{}, &it, time.Now())
	if it.Wait.key() != "you:retry" || !Offered(it, OfferByYou, DoRerunStep) {
		t.Fatalf("with the budget raised: wait = %s, offers %v; want you:retry with rerun_step", it.Wait.key(), it.Offers)
	}
}
