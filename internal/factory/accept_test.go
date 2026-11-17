package factory

import (
	"strings"
	"testing"
)

const stuckCheck = "'go' 'test' './internal/home' '-count=1' (in /work/codeaf-1758) · failed"

// stuckTest is a job waiting on its Test step with one red check, the way
// codeaf#1758 stood on 2026-11-09.
func stuckTest() Item {
	return Item{
		ID: 31, Kind: KindIssue, State: StateNeedsYou, QKind: QKindOutOfAttempts,
		Question: "test is not green after 2 rounds: verification blocked: " + stuckCheck + " · try again, send it back, or stop the job?",
		Stages: []Stage{
			{Name: "write", Until: UntilDone, On: true},
			{Name: "test", Until: UntilGreen, Max: 2, On: true},
			{Name: "proof", Until: UntilProven, On: true},
		},
		Stream: &Stream{Cur: 1, Phases: []Phase{
			{Name: "write", State: PhaseDone, Result: &StageResult{Done: true}},
			{Name: "test", State: PhaseWaiting, Round: 2, Result: &StageResult{Exit: 1, Notes: []string{
				"Command receipt: {}", unresolvedCheckPrefix + " " + stuckCheck,
			}}},
			{Name: "proof", State: PhasePending},
		}},
	}
}

func TestAcceptIsOfferedOnlyOnAJudgingStepThatReported(t *testing.T) {
	if it := stuckTest(); !OffersAccept(it) || !strings.Contains(strings.Join(OutOfAttemptChoices(it), " "), ChoiceAccept) {
		t.Fatal("a Test step stuck on a red check does not offer accept")
	}
	for name, change := range map[string]func(*Item){
		"not waiting on the person": func(it *Item) { it.State = StateRunning },
		"another question":          func(it *Item) { it.QKind = QKindApprove },
		"a step that makes work":    func(it *Item) { it.Stream.Cur = 0; it.Stream.Phases[0].State = PhaseWaiting },
		"a step that never reported": func(it *Item) {
			it.Stream.Phases[1].Result = &StageResult{Output: "stopped without reporting"}
		},
		"no result at all":         func(it *Item) { it.Stream.Phases[1].Result = nil },
		"a check the machine cut":  func(it *Item) { it.Stream.Phases[1].Verification = &StepVerification{State: "incomplete"} },
		"a command step":           func(it *Item) { it.Stages[1].Kind = StageCheck },
		"after a pull request":     func(it *Item) { it.PullRequest = &PullRequest{Number: 4} },
		"while following the PR":   func(it *Item) { it.FollowUp = &PRFollowUp{} },
		"a job its person stopped": func(it *Item) { stopped := true; it.PersonStopped = &stopped },
	} {
		it := stuckTest()
		change(&it)
		if StoppedByPerson(it) != (name == "a job its person stopped") {
			t.Fatalf("%s: fixture does not set what it names", name)
		}
		if OffersAccept(it) {
			t.Errorf("%s: accept is offered", name)
		}
		if strings.Contains(strings.Join(OutOfAttemptChoices(it), " "), ChoiceAccept) {
			t.Errorf("%s: accept is among the choices", name)
		}
	}
	// Review with open findings and Proof with a claim not shown are judging steps too.
	review := stuckTest()
	review.Stages[1] = Stage{Name: "test", Until: UntilClean, On: true}
	review.Stream.Phases[1].Result = &StageResult{Done: true, Findings: 2}
	if !OffersAccept(review) {
		t.Error("a step with open findings does not offer accept")
	}
}

func TestOnlyTheWordAcceptIsAccept(t *testing.T) {
	for words, want := range map[string]string{
		"accept": ChoiceAccept, " Accept ": ChoiceAccept, "accept it": ChoiceCancel, "": ChoiceCancel,
		ChoiceTryOnceMore: ChoiceTryOnceMore, ChoiceSendBack + ": redo": ChoiceSendBack,
	} {
		if got := OutOfAttemptsChoice(false, words); got != want {
			t.Errorf("%q reads as %s, want %s", words, got, want)
		}
	}
	if OutOfAttemptsChoice(true, "") != ChoiceTryOnceMore {
		t.Error("a bare yes is no longer try once more")
	}
}

func TestAcceptingAStepKeepsWhatWasAcceptedInItsNotes(t *testing.T) {
	it := stuckTest()
	ph := &it.Stream.Phases[1]
	what := AcceptStep(ph, StuckReason(it.Question))
	if len(what) != 1 || what[0] != stuckCheck {
		t.Fatalf("accepted %q", what)
	}
	if got := strings.Join(ph.Result.Notes, "\n"); strings.Contains(got, unresolvedCheckPrefix) || !strings.Contains(got, AcceptedPrefix+" "+stuckCheck) || !strings.Contains(got, "Command receipt: {}") {
		t.Fatalf("notes:\n%s", got)
	}
	if ph.Result.Exit != 1 || ph.Result.Done {
		t.Fatal("accepting rewrote what the step reported")
	}
	// Nothing is accepted until the step is done.
	if len(AcceptedSteps(it)) != 0 {
		t.Fatal("a waiting step reads as accepted")
	}
	ph.State = PhaseDone
	steps := AcceptedSteps(it)
	if len(steps) != 1 || steps[0].Step != "test" || strings.Join(AcceptedFailures(it), "|") != stuckCheck {
		t.Fatalf("accepted steps %+v", steps)
	}
	// A shortfall with no failing check keeps the reason it was asked with.
	review := Phase{Name: "review", Result: &StageResult{Done: true, Findings: 2}}
	if what := AcceptStep(&review, StuckReason("review is not clean: 2 findings · try again, or stop the job?")); len(what) != 1 || what[0] != "review is not clean: 2 findings" {
		t.Fatalf("accepted %q", what)
	}
}

func TestThePullRequestNamesWhatTheOwnerAccepted(t *testing.T) {
	it := stuckTest()
	if strings.Contains(PublishBody(it, ""), "accepted") {
		t.Fatal("a job with nothing accepted says accepted")
	}
	AcceptStep(&it.Stream.Phases[1], "")
	it.Stream.Phases[1].State = PhaseDone
	body := PublishBody(it, "")
	if !strings.Contains(body, "- test: accepted by the owner as it stands, with these unresolved:\n  - `go test ./internal/home -count=1 · failed`\n") {
		t.Fatalf("body:\n%s", body)
	}
	if strings.Contains(body, "/work/codeaf-1758") {
		t.Fatalf("the body names the machine's checkout:\n%s", body)
	}
}
