package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// unreachedNote is what a gate that could not be reached leaves on the
// judgement, spelled the way revision spells it: the reason, how it was asked,
// and the provider's own sentence.
const unreachedNote = revision.GateUnreached +
	" · asked twice · API error (404): no endpoints found for openai/gpt-5-codex"

// ONE ROW, BUILT ONE WAY, AND AN UNREACHABLE GATE GETS ONE OF ITS OWN KIND.
//
// The fail-open pass ships the work and used to journal nothing at all — reef-145
// delivered twice with the node ✓, the door `ok` and not one delivery_gate row
// for the delivered leaf, so a rig comparing runs put an unchecked delivery in a
// column of judged passes (#514). What is recorded now says the one thing that
// is true about that delivery: nobody read it, and here is why.
func TestADeliveryTheGateCouldNotReachIsJournaledAsItsOwnKindOfRow(t *testing.T) {
	row := deliveryGateOf(revision.Judgment{Pass: true, Unjudged: unreachedNote})
	if !row.Unjudged || !row.Unclosed {
		t.Fatalf("the row does not say the gate was asked and never answered: %+v", row)
	}
	if row.Pass {
		t.Fatalf("the fail-open ship was written down as a verdict: %+v", row)
	}
	if row.Refused != unreachedNote {
		t.Fatalf("the row lost the reason: %q", row.Refused)
	}
	if row.Whole() {
		t.Fatal("a delivery nothing judged settled whole")
	}
	// AND NOTHING ELSE ABOUT THE GATE MOVED. A judgement that was actually held
	// builds exactly the row it always did.
	judged := deliveryGateOf(revision.Judgment{Pass: true, Checked: true, Subject: "claim"})
	if judged.Unjudged || !judged.Pass || !judged.Whole() {
		t.Fatalf("a gate that answered stopped being a pass: %+v", judged)
	}

	graph, _, _ := narrationFixture(t)
	if err := graph.RecordDeliveryGate("task-1", row); err != nil {
		t.Fatalf("the store would not keep the row: %v", err)
	}
	node, found, err := graph.Node("task-1")
	if err != nil || !found {
		t.Fatalf("node task-1: found %t, err %v", found, err)
	}
	if (&settlementWatch{graph: graph}).deliveredWhole(node) {
		t.Fatal("a delivery whose gate was never reached was reported as whole")
	}
}

// AN UNJUDGED DELIVERY IS NEVER `ok`. It is its own ending, said in words on the
// door, in the record and in `--json`.
//
// This is the whole of #514 through the real settlement: a run whose gate could
// not be reached ends `unchecked` at exit 2 with the reason beside it, and the
// last line a person reads says the answer above them is theirs to keep and that
// nothing has vouched for it. Two graded runs left with `ok` and exit 0 over
// exactly this shape (2026-09-02, aforge-v2-14 anchor 1).
func TestARunNothingJudgedEndsUncheckedAndSaysSoOnTheDoorAndInTheJson(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	session := "headless-unjudged"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "compare the two parsers",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveCommand(command.Seq, store.CommandApplied, "spliced 1 node"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "compare the two parsers", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
		Intent: "compare the two parsers"}); err != nil {
		t.Fatal(err)
	}
	claim, claimed, err := graph.Claim("task-1", "test")
	if err != nil || !claimed {
		t.Fatalf("claim: %v (claimed=%v)", err, claimed)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "parser A wins on every corpus"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("task-1",
		deliveryGateOf(revision.Judgment{Pass: true, Unjudged: unreachedNote})); err != nil {
		t.Fatal(err)
	}

	said := &strings.Builder{}
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: said,
		started: time.Now(), quiet: time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, err := watcher.wait(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if outcome.stop == stopDone {
		t.Fatalf("a delivery nothing judged ended done:\n%s", said)
	}
	if outcome.stop != stopUnchecked {
		t.Fatalf("the run ended %q, want %q", outcome.stop, stopUnchecked)
	}
	if got := outcome.status(); got != exitIncomplete {
		t.Fatalf("an unchecked run left with %d, want %d", got, exitIncomplete)
	}
	if outcome.Unjudged != unreachedNote {
		t.Fatalf("the reason nothing checked this run is %q, want %q", outcome.Unjudged, unreachedNote)
	}
	// The work still ships. Fail-open stands; what changed is that nobody can
	// read the ending as a check that held.
	if !strings.Contains(outcome.Deliverable, "parser A wins") {
		t.Fatalf("the deliverable was held hostage to the weather: %q", outcome.Deliverable)
	}

	// THE DOOR SAYS IT, IN WORDS, ONCE, AND LAST.
	//
	// Once, because the event's own narration and the closing reservation are
	// two places one sentence could be said from, and a person reading the
	// identical line twice learns nothing the second time. Last, because the
	// exit code is the one thing a person at a terminal cannot see: five
	// headless runs ended on a ✓ with what the run believed it had not done
	// sitting in the journal (FAILSAFE clause 3, and sayStanding's own comment).
	sentence := "delivered without a check: " + revision.GateUnreached
	if got := strings.Count(said.String(), sentence); got != 1 {
		t.Fatalf("the door said the delivery went out unchecked %d times, want once:\n%s", got, said)
	}
	if last := lastDoorLine(said.String()); !strings.HasPrefix(last, sentence) {
		t.Fatalf("the last thing the door said is %q, want the unchecked sentence:\n%s", last, said)
	}
	if strings.Contains(said.String(), "gate: refused") {
		t.Fatalf("a gate nobody reached was called a refusal:\n%s", said)
	}

	// AND THE TWO PLACES IT COULD BE SAID FROM SAY IT BETWEEN THEM ONCE.
	//
	// The poll above reached the sentence through the closing reservation,
	// which is the fallback; a watched run reaches the event first and says it
	// there. Both arms are driven here, in that order, because the duplication
	// they can produce is invisible to any test that only ever exercises one of
	// them — the run above prints the line exactly once whichever arm is broken.
	watched, watcher, door := narrationFixture(t)
	if err := watched.RecordDeliveryGate("task-1",
		deliveryGateOf(revision.Judgment{Pass: true, Unjudged: unreachedNote})); err != nil {
		t.Fatal(err)
	}
	watchedNodes, err := watcher.sessionNodes()
	if err != nil {
		t.Fatal(err)
	}
	watcher.narrate(watchedNodes)
	watcher.sayStanding(watchedNodes[0])
	if got := strings.Count(door.String(), sentence); got != 1 {
		t.Fatalf("the event and the closing line said it %d times between them, want once:\n%s", got, door)
	}

	// AND `--json` CARRIES IT, because the caller this is for is a machine.
	envelope, err := json.Marshal(errandEnvelope(outcome))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(envelope, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["stop"] != string(stopUnchecked) {
		t.Fatalf("--json says stop=%v, want %q", fields["stop"], stopUnchecked)
	}
	if fields["ok"] != false {
		t.Fatalf("--json reported an unchecked delivery as ok: %s", envelope)
	}
	if fields["unjudged"] != unreachedNote {
		t.Fatalf("--json says unjudged=%v, want %q", fields["unjudged"], unreachedNote)
	}
	// And it is absent on every run whose gate answered, so a caller may read
	// the key's presence as the answer.
	judged, err := json.Marshal(errandEnvelope(headlessOutcome{stop: stopDone}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(judged), "unjudged") {
		t.Fatalf("a judged run carried the key anyway: %s", judged)
	}
}

// lastDoorLine is the final thing a person watching actually read, with the
// stream's own two-space indent and its elapsed clock taken off the ends. The
// door writes one line per fact and pads none of them, so the last non-empty
// line is the last fact.
func lastDoorLine(door string) string {
	lines := strings.Split(strings.TrimRight(door, "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}
