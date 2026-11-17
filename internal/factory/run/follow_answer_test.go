package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// followAsking is a job Follow PR parked on its own question: a round reported
// needs_you, the job needs you with the reason as its question, and no runner
// holds it.
func followAsking() factory.Item {
	const head = "1111111111111111111111111111111111111111"
	const asks = "The failing audit predates this change; it needs a scope decision."
	return factory.Item{
		ID: 1, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 10, Publish: "pr", HeadCommit: head,
		Question: asks, QKind: "scope",
		Asking:      &factory.Asked{Stage: factory.FollowPRStep, Question: asks, With: factory.AskedYou},
		PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, HeadSHA: head, ChecksRead: true, Checks: factory.PRChecks{Failed: 1}},
		FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "needs_you", HeadSHA: head, Attempt: 3, Limit: 3, Reason: asks, EventKey: "seen"},
		Stages:      []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone},
			{Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1},
		}},
	}
}

// The answer to Follow PR's question starts the round it is for. Kept only
// as a note, it left the job asking the same question with nobody asking it.
func TestAnAnswerToFollowPRsQuestionQueuesItsNextRound(t *testing.T) {
	it := followAsking()
	if err := AnswerUnheld(&it, true, "Leave the audit alone.", time.Now()); err != nil {
		t.Fatal(err)
	}
	f := it.FollowUp
	if it.State != factory.StateQueued || f.State != "queued" || it.Question != "" || it.QKind != "" || it.Asking != nil {
		t.Fatalf("the answer queued no round: state %s, follow %s, question %q", it.State, f.State, it.Question)
	}
	if !strings.Contains(f.Request, "Leave the audit alone.") || !strings.Contains(f.Request, "scope decision") {
		t.Fatalf("the round does not carry the question and its answer: %q", f.Request)
	}
	// The repair count was at its limit; the answer is the person's word for
	// this one round, and no more.
	if f.Attempt != f.Limit {
		t.Fatalf("attempt %d of %d after the answer", f.Attempt, f.Limit)
	}
	if ph := it.Stream.Phases[it.Stream.Cur]; ph.Name != factory.FollowPRStep || ph.State != factory.PhasePending || ph.Round != 2 {
		t.Fatalf("Follow PR's step is not queued for its next round: %+v", ph)
	}
}

// A job its person took over keeps its own door: the answer is kept, and
// Resume pipeline hands the job back.
func TestAnAnswerDuringATakeOverQueuesNothing(t *testing.T) {
	it := followAsking()
	it.Manual = true
	if err := AnswerUnheld(&it, true, "Leave the audit alone.", time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.State == factory.StateQueued || it.FollowUp.State == "queued" {
		t.Fatalf("an answer queued a round on a job its person took over: state %s, follow %s", it.State, it.FollowUp.State)
	}
}
