package api

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// handedJob is a job whose step failed and whose main agent holds the stop,
// as the store wrote it.
func handedJob(t *testing.T) factory.Item {
	t.Helper()
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 7, Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: factory.StateRunning, Cap: 10, Autonomy: factory.AutonomyAuto,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1}}}}
	factory.SettleWait(factory.Item{Wait: &factory.Wait{Who: factory.WaitBacklog}}, &it, at)
	before := it
	it.State = factory.StateNew
	factory.StampFailure(&it, factory.NewFailure("write", factory.CodeToolError, "the delegate crashed", "", factory.FailureStopped, 1, at))
	it.Stream = &factory.Stream{Ended: at, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseFailed, Round: 1, Failure: it.Failure}}}
	factory.SettleWait(before, &it, at.Add(time.Minute))
	if it.Wait.Who != factory.WaitMainAgent {
		t.Fatalf("the stop was not handed to the main agent: %+v", it.Wait)
	}
	return it
}

// A READER IS SHOWN A JOB AT WORK WHILE ITS MAIN AGENT HOLDS A STOP, with the
// one line that says so and nothing its cards are drawn from; the wait and
// the offers still say the whole truth. The job as the codec makes it, which
// a worker is sent, is the job as it is stored.
func TestPresentMainAgentHoldShowsAJobAtWork(t *testing.T) {
	it := handedJob(t)
	stored := FromItem(it)
	if stored.State != StateNeedsYou || stored.Failure == nil || stored.NeedsYou == nil || stored.StatePresentation.Kind != "failed" {
		t.Fatalf("the codec's job: state %s, failure %+v, needs %+v, presentation %+v", stored.State, stored.Failure, stored.NeedsYou, stored.StatePresentation)
	}
	shown := stored
	if !PresentMainAgentHold(&shown) {
		t.Fatal("a stop its main agent holds was not presented as one")
	}
	if shown.State != StateRunning || shown.NeedsYou != nil || shown.Failure != nil || shown.PublishError != "" || shown.AutonomyWhy != "" || shown.Ready || shown.Gate != "" {
		t.Fatalf("shown: state %s, needs %+v, failure %+v, autonomy_why %q, gate %q", shown.State, shown.NeedsYou, shown.Failure, shown.AutonomyWhy, shown.Gate)
	}
	if p := shown.StatePresentation; p.Kind != StateRunning || p.Title != "Running" || p.Detail != factory.MainAgentDeciding {
		t.Fatalf("the presentation: %+v", p)
	}
	if shown.Wait == nil || shown.Wait.Who != factory.WaitMainAgent || shown.Wait.What != factory.ActRetry || len(shown.Offers) == 0 {
		t.Fatalf("the wait and offers a reader is sent: %+v %v", shown.Wait, shown.Offers)
	}
	// The stored job and the codec's own were not touched.
	if stored.State != StateNeedsYou || stored.Failure == nil || it.Failure == nil {
		t.Fatal("presenting the hold changed the job it was made from")
	}

	// Handed to the person, the job is shown as it is stored.
	over := it.Handoff.Ended("It crashes the same way every time.")
	it.Handoff = &over
	factory.SettleWait(it, &it, time.Date(2026, 11, 17, 12, 5, 0, 0, time.UTC))
	back := FromItem(it)
	if PresentMainAgentHold(&back) || back.State != StateNeedsYou || back.Failure == nil {
		t.Fatalf("a stop its person holds: state %s, failure %+v", back.State, back.Failure)
	}

	// A step's question the manager holds is a job at work already, and is
	// left as it is.
	asked := factory.Item{ID: 8, Repo: "acme/api", Kind: factory.KindIssue, Num: 8, State: factory.StateRunning, Cap: 10,
		Asking: &factory.Asked{Stage: "write", Question: "Which table?", With: factory.AskedManager},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1}}}}
	factory.SettleWait(factory.Item{}, &asked, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
	held := FromItem(asked)
	if PresentMainAgentHold(&held) || held.State != StateRunning {
		t.Fatalf("a question the manager holds: state %s", held.State)
	}
}

// A QUEUE'S NEEDS-YOU GROUPS FOLLOW ITS JOBS: a job presented as held by its
// main agent leaves them.
func TestRegroupNeedsYouDropsAJobItsMainAgentHolds(t *testing.T) {
	it := handedJob(t)
	q := FromSnapshot(factory.Snapshot{Items: []factory.Item{it}}, "sam", time.Date(2026, 11, 17, 12, 2, 0, 0, time.UTC))
	if len(q.NeedsYou) != 1 || len(q.Jobs) != 1 {
		t.Fatalf("the queue as stored: groups %+v, %d jobs", q.NeedsYou, len(q.Jobs))
	}
	PresentMainAgentHold(&q.Jobs[0])
	RegroupNeedsYou(&q)
	if len(q.NeedsYou) != 0 {
		t.Fatalf("the queue's groups after the hold was presented: %+v", q.NeedsYou)
	}
}
