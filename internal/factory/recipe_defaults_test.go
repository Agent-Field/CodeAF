package factory_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE DEFAULT ROUNDS LEAVE ROOM TO FIX WHAT A ROUND FOUND: a step that does the
// work gets two, and one judged on what it leaves gets three, so one honest
// shortfall is another try and not a stopped job. Approve and proof are fixed,
// and each kind runs its own list of the one set of shipped stages.
func TestTheDefaultRecipeLeavesRoomToFix(t *testing.T) {
	r := factory.DefaultRecipe()
	rounds := map[string]int{"plan": 2, "write": 2, "test": 3, "review": 3, "proof": 3}
	for name, want := range rounds {
		i := factory.StageIndex(r.Stages, name)
		if i < 0 || r.Stages[i].Max != want {
			t.Errorf("%s rounds = %+v, want %d", name, r.Stages, want)
		}
	}
	lists := map[factory.Kind]string{
		factory.KindIssue: "plan approve write test review proof",
		factory.KindPR:    "read checks review proof approve",
		factory.KindCI:    "bisect fix test proof approve",
	}
	for kind, want := range lists {
		var names []string
		for _, st := range r.For(kind) {
			names = append(names, st.Name)
			if st.Fixed != (st.Name == factory.ApproveName || st.Name == "proof") {
				t.Errorf("%s %s fixed = %v", kind, st.Name, st.Fixed)
			}
			if !st.On {
				t.Errorf("%s %s is off: a default list carries no off stage", kind, st.Name)
			}
			// ONE NAME IS ONE STAGE: every kind's review is the shipped review.
			if shipped, ok := factory.ShippedStage(st.Name); !ok || shipped.Ask != st.Ask || shipped.Max != st.Max || shipped.Until != st.Until {
				t.Errorf("%s %s = %+v, not the shipped stage %+v", kind, st.Name, st, shipped)
			}
		}
		if got := strings.Join(names, " "); got != want {
			t.Errorf("%s runs %s, want %s", kind, got, want)
		}
	}
	// Every call answers fresh slices.
	r.Stages[0].Max = 9
	if factory.DefaultRecipe().Stages[0].Max != 2 {
		t.Fatal("a caller's copy reached the default recipe")
	}
}

// SECURITY IS SHIPPED BUT IN NO LIST. A job typed with the word gains the
// shipped stage after review, on.
func TestTheSecurityWordAddsTheShippedStageAfterReview(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	id, err := seam.New("agentfield/codeaf", "fix the ledger double count, security")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := seam.Load()
	var it factory.Item
	for _, x := range snap.Items {
		if x.ID == id {
			it = x
		}
	}
	sec, rv := factory.StageIndex(it.Stages, "security"), factory.StageIndex(it.Stages, "review")
	shipped, _ := factory.ShippedStage("security")
	if sec < 0 || sec != rv+1 || !it.Stages[sec].On || it.Stages[sec].Ask != shipped.Ask {
		t.Fatalf("stages = %s", strings.Join(factory.StageLines(it.Stages), " | "))
	}
}
