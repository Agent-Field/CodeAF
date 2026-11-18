package factorytest

import (
	"errors"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE REPLAYS. Each is one stall found in a real job or in the code, driven
// again on a whole factory: the job is taken into the stall and the
// properties are held there ([Scenario.Check], after every event). A replay
// fails when the job sits with no named owner, or with an owner and no action
// that works.
//
// The first ten are the stalls the reliability spec lists. Those after them
// are stalls the random runs found; each was fixed in the door that caused it
// and is replayed here so it stays fixed.

// both runs a replay in Manual and in Auto.
func both(t *testing.T, run func(t *testing.T, mode string)) {
	for _, mode := range []string{factory.AutonomyManual, factory.AutonomyAuto} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			run(t, mode)
		})
	}
}

// in runs a replay in the one mode that shows its stall best: the stall and
// its doors are the same in the other, and every scenario costs the package
// seconds. SCENARIO_BOTH runs it in both.
func in(t *testing.T, mode string, run func(t *testing.T, mode string)) {
	if os.Getenv("SCENARIO_BOTH") != "" {
		both(t, run)
		return
	}
	t.Run(mode, func(t *testing.T) {
		t.Parallel()
		run(t, mode)
	})
}

// 1. JOB 31 (2026-11-17): a conflict fix that passed its checks here could not
// be pushed. The job had landed with Follow PR waiting for approval while the
// pull request still read conflicted, because the fix that resolves the
// conflict was the thing not yet pushed; the conflict outranked the approval,
// so the page offered no Approve, the manager's approve found nothing waiting
// and Auto never published it.
//
// In Manual the job must wait on its person to approve the push, and the
// approve must be taken. In Auto the service approves it itself.
func TestReplayAVerifiedConflictFixWaitsOnlyToBePushed(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		it := s.Do("the base moves and the pull request conflicts", func() {
			s.GitHub.Conflict(true)
			s.Poll()
		})
		if mode == factory.AutonomyAuto {
			// Auto approved the checked repair and pushed it.
			if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit || s.Delegate.Rounds(factory.FollowPRStep) != 1 {
				t.Fatalf("Auto did not push the conflict fix: state %s\n%s", it.State, s.Story())
			}
			if it = s.Do("GitHub is read", s.Poll); it.PullRequest.Conflict {
				t.Fatalf("the pushed fix still reads conflicted\n%s", s.Story())
			}
			return
		}
		if !factory.FollowUpUpdateWaiting(it) || !it.PullRequest.Conflict {
			t.Fatalf("not the stall: state %s, follow %+v\n%s", it.State, it.FollowUp, s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish {
			t.Fatalf("wait = %s, want you:approve_publish\n%s", waitKey(it), s.Story())
		}
		// GitHub is read again while the fix waits: the conflict is still
		// there, and still must not take the approval's place.
		it = s.Do("GitHub is read again", s.Poll)
		if it.Wait.Action != factory.ActApprovePublish {
			t.Fatalf("a read of the conflict took the approval's place: %s\n%s", waitKey(it), s.Story())
		}
		it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
		if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
			t.Fatalf("the fix was not pushed: state %s\n%s", it.State, s.Story())
		}
		it = s.Do("GitHub is read", s.Poll)
		if it.PullRequest.Conflict {
			t.Fatalf("the pushed fix still reads conflicted\n%s", s.Story())
		}
	})
}

// followRounds is how many Follow PR rounds the delegate has run.
func followRounds(s *Scenario) int { return s.Delegate.Rounds(factory.FollowPRStep) }

// 2. FOLLOW PR'S SCOPE QUESTION. A Follow PR round that reports needs_you
// parks the job on a question of kind "scope" (run/followup.go parkCI) and
// ends; no round is left waiting for the answer. The answer door took the
// answer, kept it as a note for "the step's next round", and left the
// question, the job's state and Follow PR's stop where they were. No next
// round was queued, and a second answer was refused ("is not waiting on
// you").
//
// Answering it must start the round the answer is for.
func TestReplayFollowPRsScopeQuestionCanBeAnswered(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Result: NeedsYou("The failing dependency audit predates this change; it needs a scope decision.")})
		it := s.Do("a check fails and Follow PR asks what is in scope", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.QKind != "scope" || it.FollowUp == nil || it.FollowUp.State != "needs_you" {
			t.Fatalf("not the stall: question kind %q, follow %+v\n%s", it.QKind, it.FollowUp, s.Story())
		}
		if it.Wait.Who != factory.WaitYou {
			t.Fatalf("wait = %s, want the person\n%s", waitKey(it), s.Story())
		}
		before := followRounds(s)
		it = s.Do("the person answers", func() { s.MustPress(factory.WaitMoves(it)[0]) })
		if followRounds(s) != before+1 {
			t.Fatalf("the answer started no Follow PR round (%d before, %d after)\n%s", before, followRounds(s), s.Story())
		}
	})
}

// repair is one Follow PR episode: GitHub changes, the poller reads it, the
// delegate repairs it, and in Manual the person approves the push.
func repair(s *Scenario, name string, change func()) factory.Item {
	s.T.Helper()
	it := s.Do(name, func() {
		change()
		s.Poll()
	})
	if it.Wait.Who == factory.WaitYou && it.Wait.Action == factory.ActApprovePublish {
		it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
	}
	return it
}

// 3. THREE UNRELATED EVENTS USE UP FOLLOW PR'S THREE ATTEMPTS. The attempts
// are counted over the pull request's whole life and reset only on a read
// that finds nothing to do (followup.go: f.Attempt >= f.Limit). A failed
// check, then a review, then a conflict are three different events, each
// repaired at the first try, and the fourth event finds the limit reached.
//
// That count is replaced by a per-event one in a later phase. Here the job
// must stop with its person named and a door that starts Follow PR again.
func TestReplayThreeUnrelatedEventsUseUpFollowPRsAttempts(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		repair(s, "a check fails", func() { s.GitHub.SetChecks(1, 1, 0) })
		repair(s, "a reviewer requests changes", func() { s.GitHub.Review("mei", "CHANGES_REQUESTED", "Name the constant.", s.Now()) })
		repair(s, "the base moves and the pull request conflicts", func() { s.GitHub.Conflict(true) })
		if got := followRounds(s); got != 3 {
			t.Fatalf("%d Follow PR rounds ran, want 3\n%s", got, s.Story())
		}
		it := s.Do("a check fails again", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.FollowUp.State != "needs_you" || it.FollowUp.BlockedBy != factory.FollowBlockedAttempts {
			t.Fatalf("not the stall: follow %+v\n%s", it.FollowUp, s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActRetry {
			t.Fatalf("wait = %s, want you:retry\n%s", waitKey(it), s.Story())
		}
		it = s.Do("the person starts Follow PR again", func() { s.MustPress(factory.DoResumePipeline) })
		if got := followRounds(s); got != 4 {
			t.Fatalf("the retry ran no round: %d rounds\n%s", got, s.Story())
		}
	})
}

// 4. A LATER FOLLOW PR EPISODE GETS ONE ROUND, NOT TWO. Follow PR's step has
// two rounds, and every round saves the limit it ran under on the step
// (run/loop.go: ph.RoundLimit = limit). The next episode starts on the next
// round with that saved limit, so its first round is already its last: a
// round that reports "not done yet" stops the job on an out-of-attempts
// question instead of going again.
//
// The carried limit goes in a later phase. Here the question must be the
// person's, and try once more must be taken.
func TestReplayALaterFollowPREpisodeGetsOneRound(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		repair(s, "a check fails", func() { s.GitHub.SetChecks(1, 1, 0) })
		notYet := &factory.StageResult{Done: false, Output: "One failure is left.", Repair: &factory.CIRepairResult{Decision: "needs_repair", Reason: "One failure is left."}}
		s.Delegate.Next(factory.FollowPRStep, Round{Result: notYet, Edit: true})
		it := s.Do("a check fails on the pushed repair", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.QKind != factory.QKindOutOfAttempts || followRounds(s) != 2 {
			t.Fatalf("not the stall: question kind %q after %d rounds\n%s", it.QKind, followRounds(s), s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer {
			t.Fatalf("wait = %s, want you:answer\n%s", waitKey(it), s.Story())
		}
		it = s.Do("the person says try once more", func() { s.MustPress(factory.DoAnswer) })
		if followRounds(s) != 3 {
			t.Fatalf("try once more ran no round: %d rounds\n%s", followRounds(s), s.Story())
		}
	})
}

// 5. A STEP ERROR DURING FOLLOW PR LEAVES IT MARKED RUNNING. A round that
// errors ends the run with a failure on the job (run/loop.go failStop), and
// nothing takes Follow PR off "running". The poller queues nothing while it
// reads running, so no later read of GitHub can start the next round.
//
// The job must wait on its person with a door that runs Follow PR again.
func TestReplayAStepErrorDuringFollowPRLeavesAWayOut(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		// The error is one Auto does not retry by itself, so both modes stop.
		s.Delegate.Always(factory.FollowPRStep, Round{Err: errors.New("the model refused the request: invalid tool schema")})
		it := s.Do("a check fails and the Follow PR round errors", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.Failure == nil || it.Failure.Step != factory.FollowPRStep {
			t.Fatalf("not the stall: failure %+v, state %s\n%s", it.Failure, it.State, s.Story())
		}
		if it.Wait.Who != factory.WaitYou {
			t.Fatalf("wait = %s, want the person\n%s", waitKey(it), s.Story())
		}
		// GitHub is read again: the read alone must not strand the job.
		it = s.Do("GitHub is read again", s.Poll)
		s.Delegate.Forget(factory.FollowPRStep)
		before := followRounds(s)
		it = s.Do("the person runs the step again", func() { s.MustPress(factory.WaitMoves(it)[0]) })
		if followRounds(s) != before+1 {
			t.Fatalf("the retry ran no round\n%s", s.Story())
		}
	})
}

// 6. AN AUTO APPROVAL THAT WAITS WITH NO TIMER. Auto approves a checked
// repair in the service's settle pass, and the settle pass runs when a
// request arrives or a saved deadline falls due (serve/jobs.go jobTimerDue:
// a recovery, an approval already approving, a merge already asked). An
// approval that is only waiting has no deadline, so with no request nothing
// approves it: the job reads Auto and sits on its person.
//
// The replay switches a Manual job that waits for its push approval to Auto
// while its runner is offline and nothing else calls the service.
//
// WHAT THE WATCHDOG MUST CATCH: the wait is "you: approve_publish" on a job
// in Auto, and the same approve is offered to the main agent. That is trip 6
// of the spec ("you for an act that Auto lets the main agent do"): the
// watchdog's own timer must settle it, because no other clock will.
func TestReplayAnAutoApprovalWaitsWithNoTimer(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyManual)
	it := s.Do("a check fails and the repair waits for its push approval", func() {
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
	})
	if !factory.FollowUpUpdateWaiting(it) {
		t.Fatalf("the repair is not waiting to be pushed: %+v\n%s", it.FollowUp, s.Story())
	}
	s.StopRunner()
	s.Quiet = true
	it = s.Do("the runner goes offline and the person switches the job to Auto", func() {
		if got := s.Post("control", map[string]any{"mode": factory.AutonomyAuto}); !got.OK() {
			t.Fatalf("switch to Auto: %s", got)
		}
	})
	if !factory.Automatic(it) || it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("not the stall: automatic %v, wait %s\n%s", factory.Automatic(it), waitKey(it), s.Story())
	}
	if !factory.Offered(it, factory.OfferByMainAgent, factory.DoApprove) {
		t.Fatalf("the approve Auto may give is not offered to the main agent: %v\n%s", it.Offers, s.Story())
	}
	if !it.Wait.Deadline.IsZero() {
		t.Fatalf("the wait has a deadline; this replay's comment is out of date: %v", it.Wait.Deadline)
	}
	// Any request settles it: Auto approves, and the publish waits for a runner.
	s.Quiet = false
	it = s.Do("a request reaches the service", s.Tick)
	if it.Approval == nil || !it.Approval.Automatic || it.Approval.State != "approved" {
		t.Fatalf("Auto did not approve on the request: %+v\n%s", it.Approval, s.Story())
	}
	it = s.Do("the runner comes back", s.StartRunner)
	if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
		t.Fatalf("the approved repair was not pushed: state %s\n%s", it.State, s.Story())
	}
}

// 7. "NEEDS YOU" WITH NO CARD. Follow PR at its limit, or out of budget,
// stops with the job's own state still shipped: the job reads needs_you only
// through Follow PR's state, it asks no question and holds no approval, so
// the page had nothing to draw a card from.
//
// The stored wait must name the person and the action, and a door must work.
func TestReplayNeedsYouWithNoCard(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		// The limit, reached the short way: the job's own count says three
		// repairs are spent. No door writes the count, so the test does.
		s.Change(func(it *factory.Item) { it.FollowUp.Attempt = it.FollowUp.Limit })
		it := s.Do("a check fails with the attempts spent", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.State != factory.StateShipped || it.Question != "" || it.FollowUp.BlockedBy != factory.FollowBlockedAttempts {
			t.Fatalf("not the stall: state %s, question %q, follow %+v\n%s", it.State, it.Question, it.FollowUp, s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActRetry || it.Wait.Words == "" {
			t.Fatalf("wait = %s %q, want you:retry with words\n%s", waitKey(it), it.Wait.Words, s.Story())
		}
	})
}

// 8. TEST REFUSED OVER FAILURES THAT ALSO FAIL ON THE BASE. The test step is
// done when it leaves no new failures; a round that reports failures the
// change did not cause is still "not done", so three rounds are spent and the
// job stops on an out-of-attempts question (job codeaf#1758 sat there until
// a person accepted the step).
//
// Known base failures become an accepted finding in a later phase. Here the
// question must be the person's and every choice it offers must be taken.
func TestReplayTestRefusedOverFailuresOnTheBase(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		base := &factory.StageResult{Done: false, Output: "4 tests fail. All four fail on the base branch too.", Findings: 4,
			Tests: &factory.TestCounts{Command: "go test ./...", Passed: 96, Failed: 4}}
		s.Delegate.Always("test", Round{Result: base})
		it := s.Do("start", func() { s.MustPress(factory.DoStart) })
		for step := 0; step < 10 && it.QKind != factory.QKindOutOfAttempts; step++ {
			if it.Wait.Who != factory.WaitYou {
				t.Fatalf("the job left its person before test ran out: %s\n%s", waitKey(it), s.Story())
			}
			it = s.Do("the person says yes", func() { s.Yes() })
		}
		if it.QKind != factory.QKindOutOfAttempts || it.Asking == nil && it.Question == "" {
			t.Fatalf("not the stall: question kind %q\n%s", it.QKind, s.Story())
		}
		if got := s.Delegate.Rounds("test"); got != 3 {
			t.Fatalf("test ran %d rounds before it asked, want its 3\n%s", got, s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer {
			t.Fatalf("wait = %s, want you:answer\n%s", waitKey(it), s.Story())
		}
		if !factory.Offered(it, factory.OfferByYou, factory.DoAcceptStep) {
			t.Fatalf("the step cannot be accepted as it stands: %v\n%s", it.Offers, s.Story())
		}
		it = s.Do("the person accepts the step as it stands", func() { s.MustPress(factory.DoAcceptStep) })
		if s.Delegate.Rounds("proof") == 0 && it.Wait.Who != factory.WaitYou {
			t.Fatalf("accepting did not carry the job on\n%s", s.Story())
		}
	})
}

// 9. A JOB PINNED TO A COMPUTER THAT IS OFFLINE. A job's worktree lives on
// the computer that made it, and only that computer is offered the job again
// (lease/lease.go eligible: WorktreeMachine). With that computer offline and
// another one online, a queued Follow PR round is taken by nobody and the job
// sat in the queue with no clock.
//
// The job must wait on a runner with a deadline, so the watchdog has a time
// to look at it; and the computer coming back must take it.
func TestReplayAJobPinnedToAnOfflineComputer(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		it := s.Item()
		if it.WorktreeMachine != scenarioMachine {
			t.Fatalf("the job is not pinned to its computer: %q", it.WorktreeMachine)
		}
		s.SwitchComputer("desktop")
		it = s.Do("the laptop is offline, a desktop is online, and a check fails", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.State != factory.StateQueued || followRounds(s) != 0 {
			t.Fatalf("not the stall: state %s after %d rounds\n%s", it.State, followRounds(s), s.Story())
		}
		if it.Wait.Who != factory.WaitRunner || it.Wait.Deadline.IsZero() {
			t.Fatalf("wait = %s with deadline %v, want a runner and a deadline\n%s", waitKey(it), it.Wait.Deadline, s.Story())
		}
		it = s.Do("the laptop comes back", func() { s.SwitchComputer(scenarioMachine) })
		if followRounds(s) != 1 {
			t.Fatalf("the laptop did not take its job back\n%s", s.Story())
		}
	})
}

// 10. A BUDGET STOP IN MANUAL THAT STAYS AFTER THE BUDGET IS RAISED. Follow
// PR out of budget parks the job on "raise the budget" (followup.go
// parkFollowBudget). The next read of GitHub released that stop only for a
// job in Auto, so in Manual the person raised the budget and the job went on
// saying "raise the budget".
//
// Raising the budget must move the job, in both modes.
func TestReplayABudgetStopLiftsWhenTheBudgetIsRaised(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		// The budget is spent where no step spends it: in the job's own chat
		// with its manager, while Follow PR watches. No model runs here, so
		// the test writes what that chat cost.
		s.Change(func(it *factory.Item) { it.Stream.Spent = it.Cap })
		it := s.Do("a check fails with the budget spent", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.FollowUp.State != "needs_you" || it.FollowUp.BlockedBy != factory.FollowBlockedBudget {
			t.Fatalf("not the stall: follow %+v\n%s", it.FollowUp, s.Story())
		}
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActBudget {
			t.Fatalf("wait = %s, want you:budget\n%s", waitKey(it), s.Story())
		}
		it = s.Do("the person raises the budget and GitHub is read", func() {
			s.MustPress(factory.DoRaiseBudget)
			s.Poll()
		})
		if followRounds(s) != 1 {
			t.Fatalf("the raise started no Follow PR round: wait %s, follow %+v\n%s", waitKey(it), it.FollowUp, s.Story())
		}
	})
}

// 11. THE FIRST ROUND AFTER THE PULL REQUEST WENT READY LOST ITS RESULTS. The
// random runs found this one. Once the final review had marked the pull
// request ready, the next Follow PR round (a comment, a failing check, a
// conflict) had its first report refused by the service ("ready requires the
// recorded publication transition"): the report carried the follow-up's
// readiness record unchanged, and the service read "ready" as a new move to
// ready with no publication behind it. The job stopped on "results not saved"
// with its runner still holding it.
//
// A round after the pull request is ready must run and report like any other.
func TestReplayARoundAfterThePullRequestWentReadyKeepsItsResults(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		it := s.Do("the checks pass and the final review finds the pull request ready", func() {
			s.GitHub.SetChecks(2, 0, 0)
			s.Poll()
		})
		if it.Wait.Who == factory.WaitYou && it.Wait.Action == factory.ActApproveReady {
			it = s.Do("the person approves marking it ready", func() { s.MustPress(factory.DoApprove) })
		}
		if it.PullRequest.Draft || it.FollowUp.Readiness == nil || it.FollowUp.Readiness.State != "ready" {
			t.Fatalf("the pull request was not marked ready: draft %v, readiness %+v\n%s", it.PullRequest.Draft, it.FollowUp.Readiness, s.Story())
		}
		before := followRounds(s)
		it = repair(s, "a review comment arrives", func() { s.GitHub.AddComment("Could this be a method instead?") })
		if it.QKind == "results_not_saved" || followRounds(s) != before+1 {
			t.Fatalf("the round after ready did not run and report: question kind %q, %d rounds\n%s", it.QKind, followRounds(s)-before, s.Story())
		}
		if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
			t.Fatalf("the repair was not pushed: state %s\n%s", it.State, s.Story())
		}
	})
}

// 12. RESUME PIPELINE OVER A ROUND PARKED ON A QUESTION. The random runs
// found this one. A step that asks its person keeps its runner's hold while
// it waits. Resume pipeline took the question down and queued a Follow PR
// round without a word to that runner: the job read queued, the one runner
// that could take it sat parked on an answer nobody could give any more, and
// its hold was renewed for ever.
//
// Resume pipeline must let the parked round go, and the queued round must run.
func TestReplayResumePipelineLetsGoOfARoundParkedOnAQuestion(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Ask: "Rename the public constant too?"})
		it := s.Do("a check fails and the Follow PR round asks its person a question", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.Asking == nil || !s.held() {
			t.Fatalf("not the stall: asking %+v, held %v\n%s", it.Asking, s.held(), s.Story())
		}
		if !factory.Offered(it, factory.OfferByYou, factory.DoResumePipeline) {
			t.Fatalf("resume_pipeline is not offered: %v\n%s", it.Offers, s.Story())
		}
		before := followRounds(s)
		it = s.Do("the person resumes the pipeline with words instead of answering", func() {
			if got := s.Post("control", map[string]any{"mode": "resume_pipeline", "words": "Keep the public names as they are."}); !got.OK() {
				t.Fatalf("resume_pipeline was refused: %s", got)
			}
		})
		if followRounds(s) != before+1 || it.State == factory.StateQueued {
			t.Fatalf("the resumed round did not run: %d rounds, state %s\n%s", followRounds(s)-before, it.State, s.Story())
		}
	})
}

// 13. A RERUN AFTER SOMEONE ELSE PUSHED LOST ITS RESULTS. The random runs
// found this one. A Follow PR round that failed is run again by its person
// after the poller has read a commit someone else pushed to the branch. The
// rerun keeps the head its follow-up was made on; the runner reads the forge,
// finds the head moved and reports its follow-up moved. Because the service
// already held that head, the report carried no pull request, and the service
// refused it ("follow-up identity changed"): results not saved, and neither
// a second send nor a rerun ("step has not run yet") was taken.
//
// The rerun must take the new commits in and report like any other round.
func TestReplayARerunAfterSomeoneElsePushedKeepsItsResults(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Err: errors.New("the model refused the request: invalid tool schema")})
		it := s.Do("a check fails and the Follow PR round errors", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		if it.Failure == nil {
			t.Fatalf("the round did not fail\n%s", s.Story())
		}
		it = s.Do("someone else pushes to the branch and GitHub is read", func() {
			s.OutsidePush()
			s.Poll()
		})
		if it.PullRequest.HeadSHA == it.FollowUp.HeadSHA {
			t.Fatalf("not the stall: the follow-up already names the pushed head\n%s", s.Story())
		}
		before := followRounds(s)
		it = s.Do("the person runs the step again", func() { s.MustPress(factory.DoRerunStep) })
		if it.QKind == "results_not_saved" || followRounds(s) != before+1 {
			t.Fatalf("the rerun did not run and report: question kind %q, %d rounds\n%s", it.QKind, followRounds(s)-before, s.Story())
		}
		if it.Wait.Who == factory.WaitYou && it.Wait.Action == factory.ActApprovePublish {
			it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
		}
		if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
			t.Fatalf("the repair was not pushed over the other commit: state %s\n%s", it.State, s.Story())
		}
	})
}

// 14. A REPAIR THE PULL REQUEST MOVED PAST. The random runs found this one.
// Someone else pushes to the branch while a checked repair waits to be
// pushed. The approval is given, the publish reads the forge, finds a head
// that is not the one the repair was made on, and fails. The job then read
// "retry publishing": a retry that fails the same way every time, and after
// a Resume pipeline one the service refused outright ("the commit changed;
// approve again") because the failed publish was still on the job.
//
// The wait must name Follow PR, which takes the new commits in, and that
// door must lead to the repair being pushed.
func TestReplayAPublishThePullRequestMovedPastNamesFollowPR(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyManual)
	it := s.Do("a check fails and the repair waits for its push approval", func() {
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
	})
	if !factory.FollowUpUpdateWaiting(it) {
		t.Fatalf("the repair is not waiting to be pushed: %+v\n%s", it.FollowUp, s.Story())
	}
	s.Do("someone else pushes to the branch and GitHub is read", func() {
		s.OutsidePush()
		s.Poll()
	})
	it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
	if it.PublishError == "" {
		t.Fatalf("not the stall: the publish did not fail\n%s", s.Story())
	}
	if it.Wait.Action != factory.ActRetry || factory.Offered(it, factory.OfferByYou, factory.DoRetryPublish) {
		t.Fatalf("wait = %s offering %v, want you:retry and no retry_publish\n%s", waitKey(it), it.Offers, s.Story())
	}
	it = s.Do("the person resumes the pipeline", func() { s.MustPress(factory.DoResumePipeline) })
	if it.PublishError != "" || it.PublishStatus != nil && it.PublishStatus.State == "failed" {
		t.Fatalf("the failed publish is still on the job after the pipeline resumed\n%s", s.Story())
	}
	if it.Wait.Who == factory.WaitYou && it.Wait.Action == factory.ActApprovePublish {
		it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
	}
	if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
		t.Fatalf("the repair was not pushed over the other commit: state %s\n%s", it.State, s.Story())
	}
}

// 15. A RUNNER THAT DIES IN THE MIDDLE OF A ROUND. The random runs found two
// stalls here. In Manual the job stopped on "Runner went offline mid-step",
// and the job offered an answer the answer door refused ("not waiting on
// you"): only a lost publish takes one. In Auto the service plans its own
// retry a few seconds on, and for those seconds the job read "retry" and
// offered a rerun that the retry then refused as "not run yet".
//
// MANUAL NOW RETRIES AS AUTO DOES: running an interrupted step again is never
// the person's (factory.RecoveryBlockReason, the owner's authority table of
// 2026-11-17). In both modes the job must wait on a runner, with a deadline,
// and the retry must run by itself. What a lost step offers once its retries
// are used up is held by the rules' own tests (factory's wait_test.go).
func TestReplayARunnerDiesInTheMiddleOfARound(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Quiet = true
		var killed bool
		it := s.Do("a check fails and the runner dies in the middle of the round", func() {
			killed = s.KillRunnerMidRound(func() {
				s.GitHub.SetChecks(1, 1, 0)
				s.Poll()
			})
		})
		if !killed {
			t.Fatalf("no round was under way to kill\n%s", s.Story())
		}
		if it.Failure == nil || it.Failure.Code != factory.CodeMachineLost {
			t.Fatalf("not the stall: failure %+v\n%s", it.Failure, s.Story())
		}
		// Held still, the job is seen in the seconds before its retry.
		if it.Wait.Who != factory.WaitRunner || it.Wait.Deadline.IsZero() {
			t.Fatalf("wait = %s with deadline %v, want a runner and a deadline\n%s", waitKey(it), it.Wait.Deadline, s.Story())
		}
		s.Quiet = false
		it = s.Do("the retry's wait is over", nil)
		if followRounds(s) != 2 || it.Failure != nil {
			t.Fatalf("the retry did not run: %d rounds, failure %+v\n%s", followRounds(s), it.Failure, s.Story())
		}
	})
}

// 16. A ROUND THAT CHANGED NOTHING AFTER SOMEONE ELSE PUSHED. The random runs
// found this one. Follow PR takes the other person's commits into the
// checkout before its round, so the job's head becomes a commit nobody
// approved. A round that then finds nothing to change ends "watching" and
// tells the service it is done, and the service refused the done ("job
// requires proof approval"): a done is a publish only beside an approval of
// the job's head. The runner dropped the refusal and never gave its hold
// back; a minute later the hold ran out as "Runner went offline mid-step",
// and Auto ran the same round again.
//
// A round with nothing to publish must end watching, with its hold given back.
func TestReplayARoundThatChangedNothingAfterSomeoneElsePushed(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Result: &factory.StageResult{Done: true,
			Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "The other commit already does what the comment asks."}}})
		it := s.Do("someone else pushes to the branch, a comment arrives, and the round finds nothing to change", func() {
			s.OutsidePush()
			s.GitHub.AddComment("Could this be a method instead?")
			s.Poll()
		})
		if followRounds(s) != 1 {
			t.Fatalf("%d rounds ran, want 1\n%s", followRounds(s), s.Story())
		}
		if it.State != factory.StateShipped || it.FollowUp.State != "watching" || s.held() {
			t.Fatalf("the round did not end watching with its hold given back: state %s, follow %s, held %v\n%s", it.State, it.FollowUp.State, s.held(), s.Story())
		}
		if it.Wait.Who != factory.WaitGitHub {
			t.Fatalf("wait = %s, want GitHub\n%s", waitKey(it), s.Story())
		}
	})
}
