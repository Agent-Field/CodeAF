package provider

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/calllog"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// TestADemandedSetLeavesEveryAdmittedMachineOnTheRow is #969's first acceptance:
// a call that demands a SET (every machine the chooser admitted, `provider.only`
// with fallbacks off) writes a row that carries the whole set in `lanes` and the
// ranked head in `lane`. Without the set, `served != lane` cannot tell "the
// router chose another machine we admitted" from "the router went somewhere we
// never named".
func TestADemandedSetLeavesEveryAdmittedMachineOnTheRow(t *testing.T) {
	read := loggingTo(t)
	client, _, _ := pacedPair(t)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	calllog.Close()

	finishes := ended(read())
	if len(finishes) != 1 {
		t.Fatalf("one call should leave one finished row; got %d: %+v", len(finishes), finishes)
	}
	row := finishes[0]
	if len(row.Lanes) != 2 || !hasName(row.Lanes, "cinnabar") || !hasName(row.Lanes, "tinder") {
		t.Fatalf("lanes = %v, want both admitted machines on the row: %+v", row.Lanes, row)
	}
	if row.Lane != row.Lanes[0] {
		t.Fatalf("lane = %q, want the ranked head of the demanded set %v", row.Lane, row.Lanes)
	}
	if row.Served == "" || !hasName(row.Lanes, row.Served) {
		t.Fatalf("served = %q is not inside the admitted set %v", row.Served, row.Lanes)
	}
}

// TestASingleMachineDemandLeavesNoSetOnTheRow is the emptiness side: a demand of
// exactly one machine is the case `lane` alone already answers, so no `lanes`
// field is written - which is also the state of every row from before the
// set-demanding chooser existed.
func TestASingleMachineDemandLeavesNoSetOnTheRow(t *testing.T) {
	if got := demandedLanes(callKnobs{laneChoice: &lanes.Choice{Only: []string{"cinnabar"}}}); got != nil {
		t.Fatalf("a one-machine demand wrote lanes = %v, want nothing", got)
	}
	if got := demandedLanes(callKnobs{}); got != nil {
		t.Fatalf("a call that demanded nothing wrote lanes = %v, want nothing", got)
	}
}

func hasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
