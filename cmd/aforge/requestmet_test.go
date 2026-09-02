package main

// The door: at the one moment a round would otherwise be bought, the gate asks
// whether the request the person made is already satisfied.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// errandGraph is one job with the errand's own request on it: run a command,
// report a line, change nothing.
func errandGraph(t *testing.T) (*store.Store, store.Node) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	const request = "Run the command 'go test ./internal/subharness/ -count=1' in this " +
		"workspace and report the final line it prints. Change no files."
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Title: "run it", Brief: "run it and report the line", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: request}); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("job")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	return graph, node
}

// A GATE THAT WOULD HAVE BOUGHT A ROUND ASKS FIRST, AND A YES ENDS IT. Every
// reader downstream of this seam turns on gate.Pass, so the receipt is the whole
// of the change: no repair, no remainder, no continuation, no reservation.
func TestARequestAlreadySatisfiedPassesTheGateWithItsReceipt(t *testing.T) {
	graph, node := errandGraph(t)
	settings := config.Config{Model: "worker/model"}
	answer := &gateCaptureClient{model: "worker/model", response: `{"met":true,"missing":""}`}

	gate := revision.Judgment{Pass: false, Checked: true,
		Gaps:      "The deliverable is a report about the output, not the output itself.",
		Citations: []string{"report the final line it prints"},
		Grounds:   revision.Grounds{Intent: node.Provenance.Intent}}
	evidence := store.DeliveryGate{Pass: false, Gap: gate.Gaps}

	requestSettled(context.Background(), settings, adoptLiveClient(settings, answer.model, answer),
		graph, node, "The command completed. The final line printed was:\n\n```\nok\t0.4s\n```",
		revision.Evidence{Observed: true}, &gate, &evidence)

	if !gate.Pass || gate.Gaps != "" {
		t.Fatalf("a satisfied request still failed its gate: %+v", gate)
	}
	if gate.Receipt != revision.RequestMetWords || evidence.Receipt != revision.RequestMetWords {
		t.Fatalf("the receipt did not reach the verdict and the journal: %q / %q",
			gate.Receipt, evidence.Receipt)
	}
	if !gate.RequestAsked {
		t.Fatal("the answer did not travel, so the extension door would pay for it again")
	}
	// And the person's own record says so, where a reservation would otherwise
	// have been written.
	messages, err := graph.NodeMessages("job", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, message := range messages {
		said += message.Body + "\n"
	}
	if !strings.Contains(said, revision.RequestMetWords) {
		t.Fatalf("nothing on the node's record says why the run stopped:\n%s", said)
	}
}

// AND A NO LEAVES THE RUN EXACTLY WHERE IT WAS. What the question found absent
// is journaled BESIDE the gap, never inside it: the gap is what the repair round
// is briefed with verbatim.
func TestARequestStillShortLeavesTheGateAndItsGapAlone(t *testing.T) {
	graph, node := errandGraph(t)
	settings := config.Config{Model: "worker/model"}
	answer := &gateCaptureClient{model: "worker/model",
		response: `{"met":false,"missing":"report the final line it prints"}`}

	const gap = "The deliverable is a report about the output, not the output itself."
	gate := revision.Judgment{Pass: false, Checked: true, Gaps: gap,
		Grounds: revision.Grounds{Intent: node.Provenance.Intent}}
	evidence := store.DeliveryGate{Pass: false, Gap: gap}

	requestSettled(context.Background(), settings, adoptLiveClient(settings, answer.model, answer),
		graph, node, "I ran the command.", revision.Evidence{Observed: true}, &gate, &evidence)

	if gate.Pass || gate.Gaps != gap || evidence.Gap != gap {
		t.Fatalf("a short request moved the gate: %+v / %+v", gate, evidence)
	}
	if evidence.Missing != "report the final line it prints" {
		t.Fatalf("what the request still wanted was not journaled: %q", evidence.Missing)
	}
	if strings.Contains(evidence.Gap, "report the final line") {
		t.Fatalf("a second reader's sentence was folded into the repair brief: %q", evidence.Gap)
	}
	if evidence.Receipt != "" {
		t.Fatalf("a short request earned a receipt: %q", evidence.Receipt)
	}
}

// AND A QUESTION NOBODY COULD ANSWER BUYS THE ROUND IT WAS GOING TO BUY. The
// fail-open direction is the existing path: the alternative is a delivery ended
// as satisfied on the strength of a provider failure.
func TestAnUnansweredQuestionLeavesTheGateFailing(t *testing.T) {
	graph, node := errandGraph(t)
	settings := config.Config{Model: "worker/model"}
	answer := &gateCaptureClient{model: "worker/model", response: "I think it is fine."}

	gate := revision.Judgment{Pass: false, Checked: true, Gaps: "the line is missing",
		Grounds: revision.Grounds{Intent: node.Provenance.Intent}}
	evidence := store.DeliveryGate{Pass: false, Gap: gate.Gaps}

	requestSettled(context.Background(), settings, adoptLiveClient(settings, answer.model, answer),
		graph, node, "I ran the command.", revision.Evidence{Observed: true}, &gate, &evidence)

	if gate.Pass || gate.RequestAsked || evidence.Receipt != "" || evidence.Missing != "" {
		t.Fatalf("prose was read as an answer: %+v / %+v", gate, evidence)
	}
}

// THE RECEIPT REACHES THE STREAM, under the mark this stream already uses for
// work that finished. A run that had the answer at two minutes and then spent
// eleven more ended on the word `partial`, and nothing anywhere said the thing
// the person asked for had been done (FAILSAFE clause 3).
func TestTheReceiptReachesTheStreamAndTheRunIsNotPartial(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Pass: true, Receipt: revision.RequestMetWords}); err != nil {
		t.Fatal(err)
	}
	line := narrated(t, watcher, said)
	if !strings.Contains(line, "gate: pass") {
		t.Fatalf("the verdict was not said:\n%s", line)
	}
	if !strings.Contains(line, "✓") || !strings.Contains(line, revision.RequestMetWords) {
		t.Fatalf("the receipt never reached the person:\n%s", line)
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if !watcher.deliveredWhole(node) {
		t.Fatal("a delivery that did what was asked did not settle whole")
	}
	said.Reset()
	watcher.sayStanding(node)
	if standing := said.String(); strings.Contains(standing, "partial") {
		t.Fatalf("a run that met its request closed with a reservation:\n%s", standing)
	}
}
