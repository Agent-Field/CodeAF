package factory

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// handRun is a job on its second step, with a budget, in mode.
func handRun(mode string) Item {
	return Item{ID: 7, Repo: "acme/api", Kind: KindIssue, Num: 7, State: StateRunning, Cap: 10, Autonomy: mode,
		Stages: []Stage{{Name: "write", Kind: StageChat, On: true, Until: UntilDone}, {Name: "test", Kind: StageChat, On: true, Until: UntilGreen}},
		Stream: &Stream{Cur: 1, Phases: []Phase{
			{Name: "write", Kind: StageChat, State: PhaseDone, Round: 1},
			{Name: "test", Kind: StageChat, State: PhaseRunning, Round: 2},
		}}}
}

// handOpen is a job whose pull request is open and watched, in mode.
func handOpen(mode string) Item {
	it := followItem()
	it.ID, it.Autonomy, it.Cap = 7, mode, 10
	return it
}

// handStored is the job as the store wrote it at watchT0, on no stop.
func handStored(it Item) Item {
	SettleWait(Item{Wait: &Wait{Who: WaitBacklog}}, &it, watchT0)
	return it
}

// handStop is a job the store wrote while it worked, then wrote again a
// minute later with stop applied: the write that makes a stop.
func handStop(make func() Item, stop func(*Item)) Item {
	before := handStored(make())
	it := make()
	stop(&it)
	SettleWait(before, &it, watchT0.Add(time.Minute))
	return it
}

func outOfRounds(it *Item) {
	it.State, it.QKind = StateNeedsYou, QKindOutOfAttempts
	it.Question = "test is not green after 2 rounds · try again, send it back, or stop the job?"
	it.Asking = &Asked{Stage: "test", Question: it.Question, With: AskedYou}
	it.Stream.Phases[1].State = PhaseWaiting
	it.Stream.Phases[1].Result = &StageResult{Findings: 2, Output: "two tests fail"}
}

func stepFailed(it *Item) {
	it.State = StateNew
	f := NewFailure("test", CodeToolError, "the delegate crashed", "", FailureStopped, 1, watchT0)
	StampFailure(it, f)
	it.Stream.Phases[1].State = PhaseFailed
	it.Stream.Ended = watchT0
}

func asksPerson(why string, person bool) func(*Item) {
	return func(it *Item) {
		it.State, it.QKind, it.Question = StateNeedsYou, QKindStep, "test asks: which table?"
		it.Asking = &Asked{Stage: "test", Question: "which table?", With: AskedYou, Why: why, Person: person}
		it.Stream.Phases[1].State = PhaseWaiting
	}
}

func followLimit(it *Item) {
	it.FollowUp.State, it.FollowUp.BlockedBy = "needs_you", FollowBlockedAttempts
	it.FollowUp.Attempt, it.FollowUp.Limit = 3, 3
	it.FollowUp.Reason = "Follow PR reached its repair limit. Retry Follow PR or take over."
}

func followScope(it *Item) {
	it.FollowUp.State, it.FollowUp.Reason = "needs_you", "The reviewer asks for a new endpoint, which is outside this issue."
	it.State, it.Question, it.QKind = StateNeedsYou, it.FollowUp.Reason, "scope"
}

func managerOffered(it Item) []string {
	var out []string
	for _, o := range it.Offers {
		// A task is offered beside the decisions (task_handoff_test.go).
		if o.By == OfferByMainAgent && o.Action != DoTask {
			out = append(out, o.Action)
		}
	}
	return out
}

// EVERY STOP THE MAIN AGENT MAY DECIDE IS ITS FIRST, in Manual and in Auto:
// the write that makes the stop hands it over, the job reads "main_agent"
// with the action its person would be asked for and a deadline, and the
// decisions that fit are offered to the main agent beside the person's own.
func TestANewStopGoesToTheMainAgentFirst(t *testing.T) {
	cases := []struct {
		name   string
		make   func(mode string) Item
		stop   func(*Item)
		kind   string
		what   string
		offers []string
	}{
		{"a step out of rounds", handRun, outOfRounds, StopRounds, ActAnswer,
			[]string{DecideAnotherRound, DecideSendBack, DecideRerun, DecideAccept, DecideHandToPerson}},
		{"a step whose verification is incomplete", handRun, func(it *Item) {
			outOfRounds(it)
			it.Stream.Phases[1].Verification = &StepVerification{State: "incomplete", Reason: "the check did not finish"}
		}, StopRounds, ActAnswer, []string{DecideAnotherRound, DecideSendBack, DecideRerun, DecideHandToPerson}},
		{"a step's question nobody answered", handRun, asksPerson("", false), StopQuestion, ActAnswer,
			[]string{DecideHandToPerson, DoAnswer}},
		{"a failed step", handRun, stepFailed, StopFailed, ActRetry, []string{DecideRerun, DecideHandToPerson}},
		{"results the service refused", handRun, func(it *Item) {
			it.State, it.QKind, it.Question = StateNeedsYou, "results_not_saved", "The factory refused this step's results."
			it.Asking = &Asked{Stage: "test", Question: it.Question, With: AskedYou}
		}, StopUnsaved, ActRetry, []string{DecideRerun, DecideResumePipeline, DecideHandToPerson}},
		{"a publish that failed after its approval", handRun, func(it *Item) {
			it.State, it.PublishError = StateLanded, "push rejected"
			it.PublishStatus = &Publishing{State: "failed", Attempt: 1}
			it.Stream.Phases[1].State = PhaseDone
		}, StopPublish, ActRetryPublish, []string{DecideRetryPublish, DecideHandToPerson}},
		{"Follow PR at its repair limit", handOpen, followLimit, StopFollow, ActRetry,
			[]string{DecideResumePipeline, DecideWaitGitHub, DecideHandToPerson}},
		{"Follow PR's scope question", handOpen, followScope, StopFollow, ActAnswer,
			[]string{DecideResumePipeline, DecideWaitGitHub, DecideHandToPerson}},
		{"a conflict nothing is following", handOpen, func(it *Item) {
			it.FollowUp = nil
			it.PullRequest.Conflict = true
		}, StopFollow, ActDecide, []string{DecideResumePipeline, DecideHandToPerson}},
	}
	for _, mode := range []string{AutonomyAuto, AutonomyManual} {
		for _, c := range cases {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				it := handStop(func() Item { return c.make(mode) }, c.stop)
				w := it.Wait
				if w.Who != WaitMainAgent || w.What != c.what || w.Action != "" {
					t.Fatalf("the stop reads %s, want main_agent:%s", w.key(), c.what)
				}
				if !w.Deadline.Equal(w.Since.Add(WaitMainAgentFor)) || !w.Since.Equal(watchT0.Add(time.Minute)) {
					t.Fatalf("the hold's clock is since %s, deadline %s", w.Since, w.Deadline)
				}
				h := it.Handoff
				if h == nil || h.Over || h.Turns != 0 || h.Wait != "you:"+c.what || !h.At.Equal(w.Since) {
					t.Fatalf("handoff = %+v", h)
				}
				if kind := ManagerStop(it); kind != c.kind {
					t.Fatalf("the stop is of kind %q, want %q", kind, c.kind)
				}
				if got := managerOffered(it); !slices.Equal(got, c.offers) {
					t.Fatalf("offered to the main agent: %v, want %v", got, c.offers)
				}
				under, held := HandedOver(it)
				if !held || under.Who != WaitYou || under.Action != c.what || under.Words != w.Words || w.Words == "" {
					t.Fatalf("underneath: %s %q, held %v; the hold says %q", under.key(), under.Words, held, w.Words)
				}
				// The person may still act: their offers are what they were.
				person := it
				person.Handoff = nil
				for _, o := range OffersOf(person) {
					if o.By == OfferByYou && !Offered(it, OfferByYou, o.Action) {
						t.Fatalf("the hold took %s away from the person", o.Action)
					}
				}
				if err := WaitFault(it); err != nil {
					t.Fatal(err)
				}
				// A task is offered beside the decisions wherever its own rules
				// take one (task_handoff_test.go).
				mine := "Accepted from you now: " + strings.Join(c.offers, ", ")
				if Offered(it, OfferByMainAgent, DoTask) {
					mine = "Accepted from you now: task, " + strings.Join(c.offers, ", ")
				}
				if line := WaitLine(it); !strings.Contains(line, "to decide this stop first") || !strings.Contains(line, mine) {
					t.Fatalf("the status line: %s", line)
				}
				// Handed back, it is the person's exactly as it always was.
				over := it.Handoff.Ended("Two tests fail on main too.")
				it.Handoff = &over
				SettleWait(it, &it, watchT0.Add(2*time.Minute))
				if it.Wait.Who != WaitYou || it.Wait.Action != c.what || !it.Wait.Deadline.IsZero() {
					t.Fatalf("handed back, the stop reads %s", it.Wait.key())
				}
				if slices.Contains(managerOffered(it), DecideHandToPerson) {
					t.Fatal("a stop already with the person is offered to be handed to them")
				}
				if err := WaitFault(it); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// WHAT IS THE PERSON'S STAYS THE PERSON'S, in both modes: an approval of
// anything public, the merge, the budget, a take-over, a decision put away,
// "bring a computer online", a question only a person can answer or one
// already sent on to them, a stall the watchdog handed over, and any stop on a
// job whose budget is spent.
func TestStopsThatAreNeverHandedOver(t *testing.T) {
	cases := []struct {
		name string
		make func(mode string) Item
		stop func(*Item)
		want string
	}{
		{"the publish approval", handRun, func(it *Item) {
			it.State = StateLanded
			it.Stream.Phases[1].State = PhaseDone
			it.Proof = []Claim{{Text: "works", OK: true}}
		}, "you:approve_publish"},
		{"the plan approval", handRun, func(it *Item) {
			it.Stages = []Stage{{Name: "plan", Kind: StageChat, On: true}, {Name: ApproveName, Kind: StageGate, On: true}}
			it.Stream = &Stream{Cur: 1, Phases: []Phase{{Name: "plan", Kind: StageChat, State: PhaseDone, Round: 1}, {Name: ApproveName, Kind: StageGate, State: PhaseWaiting}}}
			it.State, it.QKind, it.Question = StateNeedsYou, QKindApprove, ApproveQuestion("plan")
		}, "you:approve_plan"},
		{"a start held for approval", handRun, func(it *Item) {
			it.Stream = nil
			it.State, it.QKind, it.Question = StateNeedsYou, QKindAskFirst, "Approval needed: start"
		}, "you:approve_start"},
		{"marking the pull request ready", handOpen, func(it *Item) {
			it.State = StateLanded
			it.FollowUp.State, it.FollowUp.Reason = "ready_approval", "The final review passed."
		}, "you:approve_ready"},
		{"the merge", handOpen, func(it *Item) {
			it.Merging = &MergeRun{State: MergeFailed, Why: "GitHub refused the merge."}
		}, "you:merge"},
		{"the budget", handRun, func(it *Item) {
			it.State, it.QKind, it.Question = StateNeedsYou, "cap", "Budget reached · continue?"
			it.Asking = &Asked{Stage: "test", Question: it.Question, With: AskedYou}
		}, "you:budget"},
		{"Follow PR stopped on the budget", handOpen, func(it *Item) {
			it.FollowUp.State, it.FollowUp.BlockedBy, it.FollowUp.Reason = "needs_you", FollowBlockedBudget, "Job budget reached."
		}, "you:budget"},
		{"a take-over", handRun, func(it *Item) {
			stepFailed(it)
			it.Manual, it.State, it.QKind, it.Question = true, StateNeedsYou, "manual", "Automation paused."
		}, "you:retry"},
		{"a lost runner", handRun, func(it *Item) {
			it.State, it.QKind, it.Question = StateNeedsYou, NeedsRunnerOffline, "Runner went offline mid-step"
			StampFailure(it, NewFailure("test", CodeMachineLost, it.Question, "", FailureStopped, 1, watchT0))
		}, "you:computer"},
		{"a question only a person can answer", handRun, asksPerson(WhyOnlyAPerson, true), "you:answer"},
		{"a question the main agent sent on", handRun, asksPerson("it is a taste call", false), "you:answer"},
		{"a question a Manual job saved before the change", handRun, asksPerson(WhyManualAsksYou, false), "you:answer"},
		{"a stop the person asked for", handRun, func(it *Item) {
			it.State, it.QKind, it.Question = StateNeedsYou, QKindSpendStopped, "Stopped spending after write as you asked. Carry on?"
		}, "you:answer"},
		{"a step out of rounds with the budget spent", handRun, func(it *Item) {
			outOfRounds(it)
			it.Stream.Spent = it.Cap
		}, "you:answer"},
		{"a step the main agent already put back to work twice", handRun, func(it *Item) {
			outOfRounds(it)
			it.Handoff = &Handoff{Rounds: map[string]int{"test": ManagerRoundsMost}}
		}, "you:answer"},
	}
	for _, mode := range []string{AutonomyAuto, AutonomyManual} {
		for _, c := range cases {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				it := handStop(func() Item { return c.make(mode) }, c.stop)
				if it.Wait.key() != c.want {
					t.Fatalf("the stop reads %s, want %s", it.Wait.key(), c.want)
				}
				if h := it.Handoff; h != nil && h.Wait != "" {
					t.Fatalf("handed over: %+v", h)
				}
				if _, held := HandedOver(it); held {
					t.Fatal("the main agent holds a stop that is the person's")
				}
			})
		}
	}
}

// A STOP IS NEVER TAKEN BACK FROM A PERSON. A job saved before handoffs that
// already waits on its person for the same thing, one whose handoff ended,
// and one the store has never written a wait for all stay where they are,
// however often they are written again.
func TestOnlyANewStopIsHandedOver(t *testing.T) {
	saved := handRun(AutonomyAuto)
	outOfRounds(&saved)
	// A document from before waits were stored.
	legacy := saved
	SettleWait(Item{}, &legacy, watchT0)
	if legacy.Wait.key() != "you:answer" || legacy.Handoff != nil {
		t.Fatalf("a job with no stored wait was handed over: %s %+v", legacy.Wait.key(), legacy.Handoff)
	}
	// Written again and again, it stays the person's.
	for range 3 {
		SettleWait(legacy, &legacy, watchT0.Add(time.Hour))
	}
	if legacy.Wait.key() != "you:answer" || legacy.Handoff != nil {
		t.Fatalf("a stop its person held was taken back: %s %+v", legacy.Wait.key(), legacy.Handoff)
	}
	// A stall the watchdog handed to the person is theirs, with its words.
	stalled := handStop(func() Item { return handOpen(AutonomyAuto) }, followLimit)
	over := stalled.Handoff.Ended("")
	stalled.Handoff = &over
	stalled.Stall = &Stall{Kind: StallNoOffer, Words: "Nothing it offers moves it.", Wait: "you:retry", GaveUp: true}
	SettleWait(stalled, &stalled, watchT0.Add(time.Hour))
	if stalled.Wait.key() != "you:decide" || stalled.Stall == nil {
		t.Fatalf("a stall handed to the person reads %s", stalled.Wait.key())
	}
	stalled.Handoff = &Handoff{Wait: "you:retry", At: watchT0}
	if _, held := HandedOver(stalled); held || WaitOf(stalled).Who != WaitYou {
		t.Fatal("the main agent holds a stall the watchdog handed to the person")
	}
}

// A HANDOFF BELONGS TO THE STOP IT WAS MADE FOR. Once the job reads another
// wait underneath it is gone, and the next stop is a new one with a turn of
// its own.
func TestAHandoffGoesWithItsStop(t *testing.T) {
	it := handStop(func() Item { return handRun(AutonomyAuto) }, outOfRounds)
	first := *it.Handoff
	turned := first.Turned(watchT0.Add(time.Minute))
	it.Handoff = &turned
	// The main agent gave another round: the step is running again.
	before := it
	it.State, it.Question, it.QKind, it.Asking = StateRunning, "", "", nil
	it.Stream = &Stream{Cur: 1, Phases: []Phase{it.Stream.Phases[0], {Name: "test", Kind: StageChat, State: PhaseRunning, Round: 3}}}
	SettleWait(before, &it, watchT0.Add(2*time.Minute))
	if it.Handoff != nil || it.Wait.key() != "working:test" {
		t.Fatalf("the job moved on and kept its handoff: %s %+v", it.Wait.key(), it.Handoff)
	}
	// It stops again: a new handoff, with no turn given yet.
	before = it
	outOfRounds(&it)
	SettleWait(before, &it, watchT0.Add(3*time.Minute))
	if h := it.Handoff; h == nil || h.Turns != 0 || h.At.Equal(first.At) || it.Wait.Who != WaitMainAgent {
		t.Fatalf("the second stop: %s %+v", it.Wait.key(), it.Handoff)
	}
}

// AT MOST TWO EXTRA ROUNDS FROM THE MAIN AGENT, THEN THE PERSON. The count is
// kept by step across the stops, a step at the limit goes straight to its
// person, and their move wipes it.
func TestTwoRoundsFromTheMainAgentThenThePerson(t *testing.T) {
	it := handStored(handRun(AutonomyAuto))
	at := watchT0
	stop := func() {
		before := it
		outOfRounds(&it)
		at = at.Add(time.Minute)
		SettleWait(before, &it, at)
	}
	resume := func() {
		before := it
		it.State, it.Question, it.QKind, it.Asking = StateRunning, "", "", nil
		it.Stream = &Stream{Cur: 1, Phases: []Phase{it.Stream.Phases[0], {Name: "test", Kind: StageChat, State: PhaseRunning, Round: it.Stream.Phases[1].Round + 1}}}
		at = at.Add(time.Minute)
		SettleWait(before, &it, at)
	}
	for round := 1; round <= ManagerRoundsMost; round++ {
		stop()
		if it.Wait.Who != WaitMainAgent {
			t.Fatalf("stop %d reads %s", round, it.Wait.key())
		}
		if err := ManagerDecides(it, DecideAnotherRound); err != nil {
			t.Fatalf("round %d refused: %v", round, err)
		}
		kind, step := ManagerStop(it), ManagerStopStep(it)
		resume()
		before := it
		NoteManagerDecision(&it, kind, step, DecideAnotherRound, "Fix the import cycle first.", at)
		SettleWait(before, &it, at)
		if got := it.Handoff.Rounds["test"]; got != round {
			t.Fatalf("after round %d the count is %d", round, got)
		}
	}
	if last := it.Stream.Log[len(it.Stream.Log)-1].Text; last != "Main agent: another round · Fix the import cycle first." {
		t.Fatalf("the decision's log line: %q", last)
	}
	stop()
	if it.Wait.key() != "you:answer" {
		t.Fatalf("the third stop reads %s, want the person", it.Wait.key())
	}
	if err := ManagerDecides(it, DecideAnotherRound); !errors.Is(err, ErrManagerRoundsUsed) {
		t.Fatalf("a third round from the main agent: %v", err)
	}
	// Accepting the step is no round, and stays open to it.
	if err := ManagerDecides(it, DecideAccept); err != nil {
		t.Fatalf("accept at the limit: %v", err)
	}
	if got := managerOffered(it); !slices.Equal(got, []string{DecideAccept}) {
		t.Fatalf("offered at the limit: %v", got)
	}
	// The person answers: the count is wiped, and the next stop is the main
	// agent's again.
	resume()
	if it.Handoff != nil {
		t.Fatalf("the person's answer left the count: %+v", it.Handoff)
	}
	stop()
	if it.Wait.Who != WaitMainAgent {
		t.Fatalf("after the person's answer the next stop reads %s", it.Wait.key())
	}
}

// EACH DECISION FITS ITS OWN KIND OF STOP, and a job on no stop the main agent
// may decide takes none: the approvals, the merge and the budget are never
// pressed here.
func TestManagerDecidesOnlyWhatFitsTheStop(t *testing.T) {
	rounds := handStop(func() Item { return handRun(AutonomyManual) }, outOfRounds)
	if err := ManagerDecides(rounds, DecideRetryPublish); err == nil || !strings.Contains(err.Error(), "does not fit this stop") {
		t.Fatalf("retry_publish on a step out of rounds: %v", err)
	}
	if err := ManagerDecides(rounds, DecideWaitGitHub); err == nil {
		t.Fatal("wait_github was taken for a stop that is not Follow PR's")
	}
	// Handed back, the main agent may still decide it when its person asks.
	over := rounds.Handoff.Ended("")
	rounds.Handoff = &over
	if err := ManagerDecides(rounds, DecideSendBack); err != nil {
		t.Fatalf("send_back on a stop the person holds: %v", err)
	}
	if err := ManagerDecides(rounds, DecideHandToPerson); err == nil {
		t.Fatal("a stop the person holds was handed to them again")
	}
	working := handStored(handRun(AutonomyAuto))
	landed := handStop(func() Item { return handRun(AutonomyManual) }, func(it *Item) {
		it.State = StateLanded
		it.Stream.Phases[1].State = PhaseDone
	})
	for name, it := range map[string]Item{"a job at work": working, "a publish approval": landed} {
		for _, d := range ManagerDecisions {
			if err := ManagerDecides(it, d); !errors.Is(err, ErrNoManagerStop) {
				t.Fatalf("%s took %s: %v", name, d, err)
			}
		}
		for _, got := range managerOffered(it) {
			if slices.Contains(ManagerDecisions, got) {
				t.Fatalf("%s offers the main agent the decision %s", name, got)
			}
		}
	}
}

// GOING BACK TO WAITING ON GITHUB LIFTS FOLLOW PR'S STOP AND QUEUES NOTHING:
// what GitHub shows now is marked as seen, so the same failing check, the
// same review or the same conflict does not start a round, and something new
// does.
func TestWaitOnGitHubWatchesAgainAndQueuesNothing(t *testing.T) {
	now := watchT0.Add(time.Hour)
	for name, change := range map[string]func(*Item){
		"a failing check":   func(*Item) {},
		"changes requested": func(it *Item) { it.PullRequest.ReviewDecision = "changes_requested" },
		"a conflict":        func(it *Item) { it.PullRequest.Conflict = true },
		"a scope question":  followScope,
	} {
		t.Run(name, func(t *testing.T) {
			it := handOpen(AutonomyAuto)
			change(&it)
			if it.FollowUp.State != "needs_you" {
				followLimit(&it)
			}
			if err := WaitOnGitHub(&it, now); err != nil {
				t.Fatal(err)
			}
			if it.State != StateShipped || it.FollowUp.State != "watching" || it.Question != "" || it.FollowUp.BlockedBy != "" {
				t.Fatalf("after: state %s, follow %+v, question %q", it.State, it.FollowUp, it.Question)
			}
			if w := WaitOf(it); w.Who != WaitGitHub {
				t.Fatalf("the job reads %s", w.key())
			}
			it.FollowUp.Attempt = 0
			if QueueCIFollowUp(&it, now) && it.State != StateShipped {
				t.Fatalf("the same facts queued a round: state %s, follow %+v", it.State, it.FollowUp)
			}
			if it.State != StateShipped {
				t.Fatalf("the same facts moved the job to %s", it.State)
			}
			// Something new on GitHub starts the next round.
			it.PullRequest.HeadSHA = strings.Repeat("b", 40)
			it.PullRequest.Checks = PRChecks{Failed: 2}
			if !QueueCIFollowUp(&it, now) || it.State != StateQueued {
				t.Fatalf("a new head with failing checks queued nothing: state %s, follow %+v", it.State, it.FollowUp)
			}
		})
	}
	// It is refused for every stop that is not Follow PR's own.
	rounds := handStop(func() Item { return handRun(AutonomyAuto) }, outOfRounds)
	if err := WaitOnGitHub(&rounds, now); err == nil {
		t.Fatal("a step out of rounds went back to waiting on GitHub")
	}
	budget := handOpen(AutonomyAuto)
	budget.FollowUp.State, budget.FollowUp.BlockedBy = "needs_you", FollowBlockedBudget
	if err := WaitOnGitHub(&budget, now); err == nil {
		t.Fatal("a budget stop was lifted without a raise")
	}
}

// A STOP THE MAIN AGENT HOLDS IS NEVER A STALL, and the watchdog's clock for
// it is now until its turn is given and its deadline after.
func TestAHandoffIsWatchedByItsOwnClock(t *testing.T) {
	it := handStop(func() Item { return handRun(AutonomyAuto) }, outOfRounds)
	facts := WatchFacts{Held: true}
	now := it.Wait.Since.Add(time.Second)
	if at := WatchAt(it, facts, now); !at.Equal(now) {
		t.Fatalf("with no turn given the watchdog looks at %s, want now", at)
	}
	turned := it.Handoff.Turned(now)
	it.Handoff = &turned
	if at := WatchAt(it, facts, now); !at.Equal(it.Wait.Deadline) {
		t.Fatalf("with its turn given the watchdog looks at %s, want the deadline %s", at, it.Wait.Deadline)
	}
	if st := StallOf(it, facts, it.Wait.Deadline.Add(time.Hour)); st != nil {
		t.Fatalf("a handoff past its deadline is a stall: %+v", st)
	}
	// A turn that began after the deadline (the service was down when the
	// stop came) is given its minute.
	began := it.Wait.Deadline.Add(time.Hour)
	late := it.Handoff.Turned(began)
	it.Handoff = &late
	if due := HandoffDue(it); !due.Equal(began.Add(HandoffTurnFor)) {
		t.Fatalf("a turn begun late is due back at %s, want a minute after it began", due)
	}
	if at := WatchAt(it, facts, began); !at.Equal(began.Add(HandoffTurnFor)) {
		t.Fatalf("the watchdog looks at a late turn at %s", at)
	}
}

// ANOTHER ROUND CARRIES ITS INSTRUCTIONS. try_once_more: with words after it
// is one more round with those words, where it used to read as an answer
// nobody knew and stop the job.
func TestTryOnceMoreCarriesInstructions(t *testing.T) {
	if got := OutOfAttemptsChoice(true, "try_once_more: Fix the import cycle first."); got != ChoiceTryOnceMore {
		t.Fatalf("the choice = %q", got)
	}
	for words, want := range map[string]string{
		"try_once_more: Fix the import cycle first.": "Fix the import cycle first.",
		"try_once_more":          "",
		"Try_Once_More:":         "",
		"yes":                    "",
		"":                       "",
		"use the other file":     "use the other file",
		" try_once_more:  two  ": "two",
	} {
		if got := RoundNote(words); got != want {
			t.Errorf("RoundNote(%q) = %q, want %q", words, got, want)
		}
	}
}
