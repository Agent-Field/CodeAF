package serve_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// THE MAIN AGENT'S THIRD TASK ON ONE STOP IS REFUSED AS A REFUSAL. A task it
// sends to a stop counts as a round, and after two the next move is the
// person's. The door answers 409 with the sentence, as it answers every job
// that takes no task now, and never 500: the app reads a 500 as "this may
// have happened". The person's own task there is still taken.
func TestTaskVerbRefusesTheMainAgentsTaskPastItsRounds(t *testing.T) {
	f := newV2Jobs(t)
	stopped := factory.Item{Kind: factory.KindIssue, Assignee: "sam", Cap: 10, State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts,
		Question: "test is not green after 4 rounds · try again, send it back, or stop the job?",
		Asking:   &factory.Asked{Stage: "test", Question: "test is not green after 4 rounds", With: factory.AskedYou},
		Stages:   []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, {Name: "test", Kind: factory.StageChat, On: true, Until: factory.UntilGreen}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 4},
		}},
		Handoff: &factory.Handoff{Rounds: map[string]int{"test": factory.ManagerRoundsMost}}}
	it := f.create(t, stopped)
	if saved, _ := f.st.Get(it.ID); factory.ManagerStop(saved) != factory.StopRounds || saved.Handoff == nil {
		t.Fatalf("the fixture is not a step out of rounds with its count kept: %+v", saved.Handoff)
	}
	before, tasked := f.document(t, it.ID), f.receipts(record.Tasked)
	w := f.post(t, it, "task", `{"words":"Fix the two tests.","by":"main_agent"}`, 409)
	words, reason := refusal(t, w)
	if reason != "conflict" || !strings.Contains(words, "already put this step back to work 2 times") {
		t.Fatalf("refusal %s", w.Body)
	}
	if f.document(t, it.ID) != before || f.receipts(record.Tasked) != tasked {
		t.Fatal("the refused task changed the job or the record")
	}
	f.post(t, it, "task", `{"words":"Fix the two tests.","by":"you"}`, 200)
}
