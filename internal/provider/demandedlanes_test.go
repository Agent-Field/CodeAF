package provider

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
	if !hasName(row.Lanes, row.Lane) {
		t.Fatalf("lane = %q is not a member of the demanded set %v", row.Lane, row.Lanes)
	}
	if row.Served == "" || !hasName(row.Lanes, row.Served) {
		t.Fatalf("served = %q is not inside the admitted set %v", row.Served, row.Lanes)
	}
}

// TestANarrowedRetryRecordsTheMachineItReallyDemanded is the reviewer's second
// case (#1796 review): after `cinnabar` refuses, the retry sends `only: [tinder]`
// and `tinder` serves. The retry's row must NOT keep the original set in `lanes`
// (that would read a serve by `cinnabar` on some later call as inside-the-set
// agreement), and it must attribute to the machine it actually demanded, not the
// ranked head of the choice. The demand that travelled is read off the wire, not
// off the choice the call was decided on.
func TestANarrowedRetryRecordsTheMachineItReallyDemanded(t *testing.T) {
	read := loggingTo(t)
	client, server, _ := pacedPair(t, "cinnabar")

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("the healthy machine should have answered: %v", err)
	}
	calllog.Close()

	if served := server.Served(); len(served) == 0 || served[len(served)-1] != "tinder" {
		t.Fatalf("%v answered, want tinder", served)
	}
	finishes := ended(read())
	// The attempt that tinder served: it demanded tinder alone, so it carries no
	// set, and both its lane and served are tinder.
	var servedRow *calllog.Record
	for i := range finishes {
		if finishes[i].Served == "tinder" {
			servedRow = &finishes[i]
		}
	}
	if servedRow == nil {
		t.Fatalf("no finished row was served by tinder: %+v", finishes)
	}
	if len(servedRow.Lanes) != 0 {
		t.Fatalf("the narrowed retry kept a set it no longer demanded: lanes = %v", servedRow.Lanes)
	}
	if servedRow.Lane != "" && servedRow.Lane != "tinder" {
		t.Fatalf("the narrowed retry attributed to %q, want the machine it demanded (tinder) or nothing", servedRow.Lane)
	}
	// No row from this call may claim the original two-machine set, since no
	// attempt that could read a stray cinnabar serve as agreement went out
	// demanding both after cinnabar refused.
	for _, r := range finishes {
		if r.Served == "tinder" && len(r.Lanes) == 2 {
			t.Fatalf("a tinder-served row still claimed the demanded set %v", r.Lanes)
		}
	}
}

// TestASingleMachineDemandIsNotWrittenAsASet is the emptiness side: a composed
// demand of one machine is the case `lane` alone already answers, so no `lanes`
// field is written, and a call that demanded nothing writes none either.
func TestASingleMachineDemandIsNotWrittenAsASet(t *testing.T) {
	if got := demandedLanes([]string{"cinnabar"}); got != nil {
		t.Fatalf("a one-machine demand wrote lanes = %v, want nothing", got)
	}
	if got := demandedLanes(nil); got != nil {
		t.Fatalf("a call that demanded nothing wrote lanes = %v, want nothing", got)
	}
	if got := demandedLanes([]string{"cinnabar", "tinder"}); len(got) != 2 {
		t.Fatalf("a two-machine demand wrote lanes = %v, want both", got)
	}
	// Blank members are trimmed, and a set that trims to one is not a set.
	if got := demandedLanes([]string{"cinnabar", "  "}); got != nil {
		t.Fatalf("a demand that trims to one machine wrote lanes = %v, want nothing", got)
	}
}

// TestARescueArmRecordsOnlyTheMachineItWentOutTo is the reviewer's first case
// (#1796 review): a rescue sends `only: [the one arm]`, but its row was recording
// the whole original admitted set, so a serve by the other machine on that row
// read as inside-the-set agreement. The demanded set is captured from the
// composed wire preference at encode time, so a rescue arm's row carries its one
// machine (and therefore no set) and attributes to it, never the original pair.
func TestARescueArmRecordsOnlyTheMachineItWentOutTo(t *testing.T) {
	client, _, model := pacedPair(t)

	// A rescue arm: the call was decided on the admitted set {cinnabar, tinder},
	// but THIS attempt is the hedge's own arm, which demands tinder alone.
	knobs := callKnobs{
		intent: IntentInteractive,
		laneChoice: &lanes.Choice{
			Only:  []string{"cinnabar", "tinder"},
			Order: []string{"cinnabar", "tinder"},
		},
		hedgeLane: "tinder",
		trace:     newCallTrace(),
	}
	request := &ai.Request{Model: model, Messages: userMessages("hello")}
	if _, err := client.encodeRequest(request, knobs); err != nil {
		t.Fatal(err)
	}

	// What the wire carried, captured at encode: the rescue's one machine.
	if got := knobs.trace.demandedOnly; len(got) != 1 || got[0] != "tinder" {
		t.Fatalf("the rescue arm captured demandedOnly = %v, want only the arm it went out to", got)
	}
	// So the row carries no set (one machine is Lane's job) and attributes to the
	// machine the arm actually demanded, never the original pair's head.
	if got := demandedLanes(knobs.trace.demandedOnly); got != nil {
		t.Fatalf("the rescue arm wrote a set it did not demand: lanes = %v", got)
	}
	if got := recordedLane(knobs, "cinnabar", knobs.trace.demandedOnly); got != "tinder" {
		t.Fatalf("the rescue arm attributed to %q, want the machine it demanded (tinder)", got)
	}
}

// TestAHoppedAttemptKeepsItsOwnDemandedSetThroughTheClose covers the fallback
// closing mechanism (#1796 review, second pass): the unconditional close
// ([Client.closeOpenAttempt]) clears `trace.open` before it records, so a finish
// row built from it cannot reach the closed attempt's demand off the trace - the
// trace's last-encoded set is a LATER attempt's. The closing row must read the
// snapshot it was handed instead. This tests [bodyDemandedOnly] at that seam
// directly, because the lane fields are only written for a watched call and the
// close path's bug is in which set reaches them, not in the watch.
func TestAHoppedAttemptKeepsItsOwnDemandedSetThroughTheClose(t *testing.T) {
	trace := newCallTrace()
	// Attempt 1 went out demanding both; its snapshot is what the close carries.
	openOne := &openAttempt{id: "one", demandedOnly: []string{"cinnabar", "tinder"}}
	// Attempt 2 has since re-encoded to one machine, so the trace's own set is
	// tinder alone - the stand-in that would stand in if the snapshot were lost.
	trace.demandedOnly = []string{"tinder"}
	trace.open = nil // closeOpenAttempt cleared it before recording.

	// The carried closing row, as closeOpenAttempt builds it: ended set, the
	// closed attempt's own snapshot carried, the trace holding a later set.
	closing := recordFacts{
		knobs:        callKnobs{trace: trace},
		ended:        endedHopped,
		demandedOnly: openOne.demandedOnly,
	}
	if got := bodyDemandedOnly(closing); len(got) != 2 || !hasName(got, "cinnabar") || !hasName(got, "tinder") {
		t.Fatalf("the hopped ending took a later attempt's demand: %v, want the set it carried", got)
	}
	if got := demandedLanes(bodyDemandedOnly(closing)); len(got) != 2 {
		t.Fatalf("the hopped ending wrote the wrong set: lanes = %v", got)
	}

	// And an attempt closed carrying NO demand is not handed the trace's later
	// one: the snapshot, not the non-nil-ness of the field, is the signal.
	singleClose := recordFacts{knobs: callKnobs{trace: trace}, ended: endedHopped, demandedOnly: nil}
	if got := bodyDemandedOnly(singleClose); got != nil {
		t.Fatalf("an attempt closed with no demand was handed %v", got)
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
