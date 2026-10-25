package session

import (
	"testing"
)

// A phone host has to open one irreversible ask without calling a model. The
// words are the ones the live phone test already waits for, and the answer
// still goes through ResolveQuestion, the same door a window uses.
func TestParkedFixtureConsentIsARealAskUntilItIsAnswered(t *testing.T) {
	agent, _ := questionSession(t, "phonefixture", nil)
	if err := agent.ParkFixtureConsent(); err != nil {
		t.Fatal(err)
	}
	open := agent.OpenQuestions()
	if len(open) != 1 || open[0].Head != "Apply the selected change?" || open[0].Kind != QuestionConsent || open[0].Stakes != StakesIrreversible {
		t.Fatalf("open asks: %+v", open)
	}
	allow := false
	for _, option := range open[0].Options {
		if option.Key == "1" && option.Label == "Allow once" {
			allow = true
		}
	}
	if !allow {
		t.Fatalf("options: %+v", open[0].Options)
	}
	if err := agent.ResolveQuestion(Answer{Kind: QuestionConsent, ID: open[0].ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if left := agent.OpenQuestions(); len(left) != 0 {
		t.Fatalf("answered ask still open: %+v", left)
	}
}
