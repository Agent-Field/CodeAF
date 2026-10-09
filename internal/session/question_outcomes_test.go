package session

import (
	"testing"
	"time"
)

// A decided question and a withdrawn one both leave a receipt, newest first, in
// words a person reads; the decision comes from the durable record.
func TestRecentQuestionOutcomesHoldDecisionsAndWithdrawals(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place.Dir = t.TempDir()
	})
	earlier := time.Now().Add(-time.Minute)
	agent.recordDecision(DecisionRecord{
		ID: 7, Kind: QuestionConsent, Head: "needs your ok to run bash",
		Picked: []string{"1"}, Labels: []string{"allow once"}, By: DecidedByPerson, At: earlier,
	})
	agent.sayWithdrawn(Question{
		ID: 9, Kind: QuestionTask, Head: "wants to start a task: X",
		Asker: Asker{Kind: AskerTask},
	}, "the work is no longer waiting on it")

	got := agent.RecentQuestionOutcomes(10)
	if len(got) != 2 {
		t.Fatalf("outcomes = %+v, want two", got)
	}
	if got[0].Outcome != QuestionWithdrawn || got[0].Token != "9" ||
		got[0].Words != "No longer needed — the work is no longer waiting on it" {
		t.Fatalf("newest = %+v", got[0])
	}
	if got[1].Outcome != QuestionDecided || got[1].Token != "7" || got[1].Words != "Allow once" || got[1].By != "person" {
		t.Fatalf("older = %+v", got[1])
	}
	if one := agent.RecentQuestionOutcomes(1); len(one) != 1 || one[0].Token != "9" {
		t.Fatalf("limit 1 = %+v", one)
	}
}

// A session that has ended nothing has no receipts, and the default limit holds.
func TestRecentQuestionOutcomesAreEmptyBeforeAnythingEnds(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := agent.RecentQuestionOutcomes(0); len(got) != 0 {
		t.Fatalf("outcomes = %+v", got)
	}
}
