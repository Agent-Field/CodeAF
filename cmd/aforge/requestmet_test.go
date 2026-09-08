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

// A MODEL'S READING MAY NOT OVERTURN A MEASUREMENT, and the door at the gate is
// where that could most easily have happened: it ran for EVERY failed gate, so
// a regression, a file the plan promised and the disk does not hold, or a
// behaviour nothing exercises could have been talked away by one sentence about
// the deliverable. The two world-doors above it are forbidden to acquit on
// evidence; this must not acquit on prose.
func TestAMeasuredFailureIsNeverOverturnedAtTheGatesDoor(t *testing.T) {
	graph, node := errandGraph(t)
	settings := config.Config{Model: "worker/model"}
	for name, gate := range map[string]revision.Judgment{
		"a file the plan promised": {Pass: false, Checked: true, Mechanical: true,
			Gaps: "the plan promised report.md and the disk does not hold it"},
		"a check this work broke": {Pass: false, Checked: true, Sourced: true,
			Gaps: "this work broke checks that were passing before it"},
		"a behaviour nothing exercises": {Pass: false, Checked: true,
			Gaps:        "1 behaviour the request states has no check that exercises it",
			Unexercised: []string{"the final line is reported"}},
		"the checks this work wrote, red": {Pass: false, Checked: true,
			Gaps: "the checks this work wrote fail", OwnFailing: []string{"test_probe"}},
		"a definition its callers no longer fit": {Pass: false, Checked: true,
			Gaps: "callers expect the old shape", Consumers: []string{"configs"}},
	} {
		gate.Grounds = revision.Grounds{Intent: node.Provenance.Intent}
		answer := &gateCaptureClient{model: "worker/model", response: `{"met":true,"missing":""}`}
		evidence := store.DeliveryGate{Pass: false, Gap: gate.Gaps}

		requestSettled(context.Background(), settings,
			adoptLiveClient(settings, answer.model, answer), graph, node,
			"here is the answer", revision.Evidence{Observed: true}, &gate, &evidence)

		if answer.messages != nil {
			t.Errorf("%s: a measurement was sent to a model to be overturned", name)
		}
		if gate.Pass || evidence.Pass || evidence.Receipt != "" {
			t.Errorf("%s: a measured failure was talked into a pass: %+v", name, evidence)
		}
		if gate.RequestAsked {
			t.Errorf("%s: the record says a question was put that never was", name)
		}
	}
}

// THE REPAIR'S OWN JUDGEMENT IS A DIFFERENT VERDICT OVER A DIFFERENT TEXT, and
// it gets its own question. A repair round rewrites the deliverable and is
// judged again, so the answer to "is the request satisfied" may have changed
// with it — and without asking, the run buys a whole remainder over a request
// the repair had just satisfied. It is still asked only once per verdict.
func TestTheRepairsOwnJudgementIsAskedAndTheFirstIsNotAskedTwice(t *testing.T) {
	graph, node := errandGraph(t)
	settings := config.Config{Model: "worker/model"}
	grounds := revision.Grounds{Intent: node.Provenance.Intent}

	// The first gate: the question is put and the answer is no, so the repair
	// round is bought exactly as it was.
	first := &gateCaptureClient{model: "worker/model",
		response: `{"met":false,"missing":"report the final line it prints"}`}
	gate := revision.Judgment{Pass: false, Checked: true, Grounds: grounds,
		Gaps: "the deliverable does not carry the line"}
	evidence := store.DeliveryGate{Pass: false, Gap: gate.Gaps}
	requestSettled(context.Background(), settings,
		adoptLiveClient(settings, first.model, first), graph, node,
		"I ran the command.", revision.Evidence{Observed: true}, &gate, &evidence)
	if gate.Pass || !gate.RequestAsked || evidence.Missing == "" {
		t.Fatalf("the first door did not put the question: %+v / %+v", gate, evidence)
	}

	// The same verdict, carried into the extension seam, is not paid for again.
	unmet := gate
	twice := &gateCaptureClient{model: "worker/model", response: `{"met":true,"missing":""}`}
	requestSettled(context.Background(), settings,
		adoptLiveClient(settings, twice.model, twice), graph, node,
		"I ran the command.", revision.Evidence{Observed: true}, &unmet, &evidence)
	if twice.messages != nil {
		t.Fatal("one verdict was asked about twice")
	}
	if unmet.Pass {
		t.Fatal("a verdict already told no came back met without being asked")
	}

	// And the judgement the REPAIR produced is a new verdict over new text, so
	// it is asked, and a yes ends the run before any remainder is bought.
	repaired := revision.Judgment{Pass: false, Checked: true, Grounds: grounds,
		Gaps: "that is a report about the output, not the output itself"}
	second := &gateCaptureClient{model: "worker/model", response: `{"met":true,"missing":""}`}
	requestSettled(context.Background(), settings,
		adoptLiveClient(settings, second.model, second), graph, node,
		"The command completed. The final line printed was:\n\n```\nok\t0.4s\n```",
		revision.Evidence{Observed: true}, &repaired, &evidence)

	if second.messages == nil {
		t.Fatal("the repair's own judgement was never asked about")
	}
	if !repaired.Pass || repaired.Gaps != "" {
		t.Fatalf("a request the repair satisfied still bought a remainder: %+v", repaired)
	}
	if evidence.Receipt != revision.RequestMetWords || evidence.Gap != "" {
		t.Fatalf("the receipt did not replace the gap on the record: %+v", evidence)
	}
}
