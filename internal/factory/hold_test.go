package factory_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// holdDoors is directDoors with the Hold door, recording what it was asked.
type holdDoors struct {
	directDoors
	asked *[]bool
}

func (h holdDoors) Hold(_ int, on bool) error {
	*h.asked = append(*h.asked, on)
	return nil
}

// THE HOLD DOOR CARRIES ITS INTENT: a runner that has it hangs it on the seam
// directly, the mailbox carries `hold` with the intent as Yes, and a runner
// without it leaves the door absent on the seam and refuses the verb in
// words rather than toggling.
func TestTheHoldDoorCarriesItsIntent(t *testing.T) {
	_, st := openLocal(t)
	var asked []bool
	seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(holdDoors{asked: &asked}))
	if !seam.Has("hold") {
		t.Fatal("a runner with Hold drew no hold door")
	}
	if err := seam.Hold(1, true); err != nil {
		t.Fatal(err)
	}
	if reply := factory.Carry(holdDoors{asked: &asked}, factory.Ask{ID: 1, Verb: factory.VerbHold, Yes: false, Seq: 3}); reply.Err != "" {
		t.Fatalf("carried hold = %+v", reply)
	}
	if len(asked) != 2 || !asked[0] || asked[1] {
		t.Fatalf("the runner was asked %v, want [true false]", asked)
	}
	if plain := factory.LocalSeam(st, time.Now(), factory.WithRunner(directDoors{})); plain.Has("hold") {
		t.Fatal("a runner with no Hold drew a hold door")
	}
	if reply := factory.Carry(directDoors{}, factory.Ask{Verb: factory.VerbHold, Seq: 4}); reply.Err != `the floor's runner does not know "hold"` {
		t.Fatalf("an older runner's hold = %+v", reply)
	}
	if mailed := factory.LocalSeam(st, time.Now(), factory.WithMailbox(st.Mailbox())); !mailed.Has("hold") {
		t.Fatal("a window over the mailbox drew no hold door")
	}
}

// AN ISSUE STILL ON THE OLD SHIP DEFAULT IS READ WITH ITS APPROVE STEP AFTER
// PLAN (the owner's #1550 on 2026-10-09 read plan write test review security
// proof approve): the floor used to write every issue `ask me at ship`, as the
// default and nobody's choice.
func TestAnIssueOnTheOldShipDefaultHoldsAfterPlan(t *testing.T) {
	stages := func(names ...string) []factory.Stage {
		var out []factory.Stage
		for _, n := range names {
			s := factory.Stage{Name: n, On: n != "security"}
			if n == factory.ApproveName {
				s.Kind = factory.StageGate
			}
			out = append(out, s)
		}
		return out
	}
	names := func(it factory.Item) string {
		var out []string
		for _, s := range it.Stages {
			out = append(out, s.Name)
		}
		return strings.Join(out, " ")
	}
	read := func(it factory.Item) factory.Item {
		raw, err := json.Marshal(it)
		if err != nil {
			t.Fatal(err)
		}
		var back factory.Item
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		return back
	}
	old := factory.Item{ID: 84, Kind: factory.KindIssue, Stages: stages("plan", "write", "test", "review", "security", "proof", "approve")}
	old.Stages[4].On = true // a step the person switched on is kept as it is
	got := read(old)
	if names(got) != "plan approve write test review security proof" || !got.Stages[5].On {
		t.Fatalf("the old default read as %q (security on %v)", names(got), got.Stages[5].On)
	}

	// EVERY OTHER ITEM IS LEFT EXACTLY AS IT IS: one that ran, one whose steps
	// somebody set, a pull request, and an issue whose steps are not the
	// recipe's.
	ran := old
	ran.Stream = &factory.Stream{}
	edited := old
	edited.Stages = stages("plan", "write", "test", "review", "security", "proof", "approve")
	edited.Stages[1].By = factory.ByManager
	pr := old
	pr.Kind = factory.KindPR
	other := old
	other.Stages = stages("plan", "write", "proof", "approve")
	for name, it := range map[string]factory.Item{"ran": ran, "edited": edited, "pr": pr, "other": other} {
		before := names(it)
		if after := names(read(it)); after != before {
			t.Errorf("%s: %q read as %q", name, before, after)
		}
	}
	if factory.RepairShipDefault(nil) {
		t.Fatal("a nil item was repaired")
	}
}
