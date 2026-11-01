package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// RUN AGAIN FROM HERE, WITH ANOTHER MODEL: the step and every step after it
// go back to pending (an approval after it asks again), and the step runs on
// the model the person named. "" reruns it on the model it had. A check or
// an approval runs no model and a fixed step keeps its own, so a model there
// is refused and nothing moves.
func TestARerunFromAStepCanNameTheModelItRunsOn(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	item := func() *factory.Item {
		return &factory.Item{ID: 3, State: factory.StateNeedsYou,
			Stages: []factory.Stage{
				{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
				{Name: "test", Kind: factory.StageCheck, Ask: "go test", On: true},
				{Name: "review", Kind: factory.StageChat, Ask: "read it", On: true, Model: "vendor/old"},
				{Name: "ok", Kind: factory.StageGate, On: true},
				{Name: "proof", Kind: factory.StageChat, Ask: "show it", On: true, Fixed: true},
			},
			Stream: &factory.Stream{Cur: 3, Phases: []factory.Phase{
				{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
				{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
				{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
				{Name: "ok", Kind: factory.StageGate, State: factory.PhaseWaiting},
				{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
			}}}
	}
	it := item()
	if err := StepRerun(it, "review", "", " vendor/new ", at); err != nil {
		t.Fatal(err)
	}
	if it.Stages[2].Model != "vendor/new" || it.Stages[2].By != factory.ByYou {
		t.Fatalf("review runs on %+v", it.Stages[2])
	}
	for _, i := range []int{2, 3, 4} {
		if it.Stream.Phases[i].State != factory.PhasePending {
			t.Fatalf("%s is %s after a rerun from review", it.Stream.Phases[i].Name, it.Stream.Phases[i].State)
		}
	}
	if it.Stream.Phases[0].State != factory.PhaseDone || it.Stream.Phases[1].State != factory.PhaseDone {
		t.Fatalf("a step before review moved: %+v", it.Stream.Phases[:2])
	}
	same := item()
	if err := StepRerun(same, "review", "", "", at); err != nil || same.Stages[2].Model != "vendor/old" {
		t.Fatalf("a rerun without a model = %v, %+v", err, same.Stages[2])
	}
	for _, c := range []struct{ step, want string }{
		{"test", "test is a check or an approval and runs no model"},
		{"proof", factory.FixedRefusal("proof").Error()},
		{"review", "a model is a model id of at most 200 characters, with no spaces"},
	} {
		it := item()
		model := "vendor/new"
		if c.step == "review" {
			model = "two words"
		}
		if err := StepRerun(it, c.step, "", model, at); err == nil || err.Error() != c.want {
			t.Fatalf("%s = %v, want %q", c.step, err, c.want)
		}
		if it.Stream.Phases[2].State != factory.PhaseDone {
			t.Fatalf("a refused rerun moved review: %+v", it.Stream.Phases[2])
		}
	}
}
