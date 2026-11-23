package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

const appAcceptRed = "'go' 'test' './internal/home' '-count=1' (in /work) · failed"

func stuckTestJob(assignee string) factory.Item {
	return factory.Item{
		Kind: factory.KindIssue, State: factory.StateNeedsYou, Assignee: assignee, QKind: factory.QKindOutOfAttempts,
		Question: "test is not green after 2 rounds: verification blocked: " + appAcceptRed + " · try again, send it back, or stop the job?",
		Stages: []factory.Stage{
			{Name: "write", Until: factory.UntilDone, On: true},
			{Name: "test", Until: factory.UntilGreen, Max: 2, On: true},
			{Name: "proof", Until: factory.UntilProven, On: true},
		},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", State: factory.PhaseDone, Result: &factory.StageResult{Done: true}},
			{Name: "test", State: factory.PhaseWaiting, Round: 2, RoundLimit: 2, Result: &factory.StageResult{Exit: 1, Notes: []string{verification.FailurePrefix + " " + appAcceptRed}}},
			{Name: "proof", State: factory.PhasePending},
		}},
	}
}

func TestPersonAcceptsAStuckStepThroughJobAction(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	it := f.create(t, stuckTestJob("alice"))
	job := alice.job(t, it.ID)
	if !job.Can.AcceptStep {
		t.Fatal("a stuck Test step has no accept action")
	}
	if job.NeedsYou == nil || job.NeedsYou.Question == nil || !strings.Contains(strings.Join(job.NeedsYou.Question.Options, " "), factory.ChoiceAccept) {
		t.Fatalf("accept is not among the question's options: %+v", job.NeedsYou)
	}
	if hello := string(alice.w.lines[0]); !strings.Contains(hello, appwire.FeatureFactoryAcceptStep) {
		t.Fatal("hello missing feature")
	}
	stale := alice.params(t, it.ID, nil)
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "revised"; return nil }); err != nil {
		t.Fatal(err)
	}
	alice.refused(t, "factory.job_accept_step", stale, appwire.CodeStale)

	out := alice.act(t, "factory.job_accept_step", it.ID, nil)
	if out.Action != "accept_step" || out.Current == nil || out.Current.Can.AcceptStep {
		t.Fatalf("acted %+v", out)
	}
	after, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	ph := after.Stream.Phases[1]
	if after.State != factory.StateQueued || after.Question != "" || ph.State != factory.PhaseDone || ph.Note != "accepted with 1 failing check" {
		t.Fatalf("job %s %q, test %+v", after.State, after.Question, ph)
	}
	if got := strings.Join(factory.AcceptedFailures(after), "|"); got != appAcceptRed {
		t.Fatalf("accepted %q", got)
	}
	if f.count(record.Answered) != 1 {
		t.Fatalf("the acceptance left %d answered receipts", f.count(record.Answered))
	}
}

func TestAcceptStepRefusesOtherPersonAndAStepThatMakesTheWork(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	it := f.create(t, stuckTestJob("alice"))
	if bob.job(t, it.ID).Can.AcceptStep {
		t.Fatal("another person is offered accept")
	}
	bob.refused(t, "factory.job_accept_step", bob.params(t, it.ID, nil), "forbidden")

	write := stuckTestJob("alice")
	write.Stream.Cur = 0
	write.Stream.Phases[0] = factory.Phase{Name: "write", State: factory.PhaseWaiting, Round: 1, Result: &factory.StageResult{Output: "could not"}}
	write.Stream.Phases[1] = factory.Phase{Name: "test", State: factory.PhasePending}
	write.Question = "write did not finish: could not · try again, or stop the job?"
	write = f.create(t, write)
	if alice.job(t, write.ID).Can.AcceptStep {
		t.Fatal("a write step offers accept")
	}
	words, _ := alice.refused(t, "factory.job_accept_step", alice.params(t, write.ID, nil), appwire.CodeConflict)
	if !strings.Contains(words, "cannot be accepted as it stands") {
		t.Fatal(words)
	}
	after, err := f.st.Get(write.ID)
	if err != nil || after.State != factory.StateNeedsYou || after.Stream.Phases[0].State != factory.PhaseWaiting {
		t.Fatalf("a refused accept moved the job: %s %v", after.State, err)
	}
}
