package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const deliveredNumbers = "Q3 revenue was $4.21M against a $3.90M plan, and headcount closed at 214."

// The whole shape, end to end: a delivered job, "that's wrong, the numbers are
// off", and one journaled revision of THAT deliverable carrying the previous
// attempt and the critique. Before this the sentence was recognized by
// redirectCue, discarded because nothing was live, and then read by the router
// as an instruction to delete a notebook belief.
func TestCorrectionOfDeliveredWorkJournalsARevisionCarryingCritiqueAndAnchor(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "correct", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)

	user := postUser(t, graph, "correct", "that's wrong, the numbers are off")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("correction journaled %d commands, want one: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice || command.Target != "q3-numbers" {
		t.Fatalf("correction command = %s at %q, want a splice anchored to q3-numbers", command.Kind, command.Target)
	}
	if !IsCorrection(command.Instruction) {
		t.Fatalf("the command does not read as a correction:\n%s", command.Instruction)
	}
	// The user's words lead, verbatim. The previous attempt rides with them,
	// which is the gate-revision input shape.
	if !strings.HasPrefix(command.Instruction, "that's wrong, the numbers are off") {
		t.Fatalf("the critique is not verbatim at the front:\n%s", command.Instruction)
	}
	if !strings.Contains(command.Instruction, deliveredNumbers) {
		t.Fatalf("the previous attempt never reached the revision:\n%s", command.Instruction)
	}
	reply := waitForAgentReply(t, graph, "correct", user.Seq)
	if !strings.Contains(reply.Body, "Q3 numbers") || reply.CommandSeq != command.Seq {
		t.Fatalf("the receipt does not name the deliverable it revises: %+v", reply)
	}
}

// A contentless rejection earns exactly one question, and the question names
// what was delivered. Never a shrug, and never a fresh unrelated job.
func TestBareThatsWrongAsksOneQuestionNamingTheDeliverable(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "bare", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)

	user := postUser(t, graph, "bare", "that's wrong")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a bare rejection commissioned work: %+v", commands)
	}
	reply := waitForAgentReply(t, graph, "bare", user.Seq)
	if !strings.Contains(reply.Body, "Q3 numbers") {
		t.Fatalf("the question does not name what was delivered: %q", reply.Body)
	}
	if !strings.HasSuffix(reply.Body, correctionAskTail) {
		t.Fatalf("the question does not ask what is wrong: %q", reply.Body)
	}
	if reply.NodeID != "q3-numbers" {
		t.Fatalf("the question is not anchored to the deliverable: %+v", reply)
	}
}

// And the answer to that question — ordinary words, no cue in them anywhere —
// becomes the revision. Without this rail the clarifying question would be a
// dead end that turns the user's own answer into an unrelated new job.
func TestAnswerToTheClarifyingQuestionBecomesTheRevision(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "answered", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)
	head := New(&fakeClient{}, graph)

	first := postUser(t, graph, "answered", "that's wrong")
	if err := head.answer(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := postUser(t, graph, "answered", "the revenue figure is from 2023, not 2024")
	if err := head.answer(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("the answer journaled %d commands, want one: %+v", len(commands), commands)
	}
	if commands[0].Target != "q3-numbers" || !IsCorrection(commands[0].Instruction) {
		t.Fatalf("the answer did not become a revision of the deliverable: %+v", commands[0])
	}
	if !strings.HasPrefix(commands[0].Instruction, "the revenue figure is from 2023, not 2024") {
		t.Fatalf("the critique lost the user's words:\n%s", commands[0].Instruction)
	}
}

// Live work is redirection's, and it stays redirection's. A correction while a
// job is running edits the plan; it must never become a re-delivery.
func TestCorrectionLeavesLiveWorkToRedirection(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "live-audit", "Live audit", "audit the ledger")
	startNode(t, graph, "live-audit")

	user := postUser(t, graph, "live", "actually, that's wrong — use the audited ledger")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect {
		t.Fatalf("a correction over live work should redirect it: %+v", commands)
	}
}

// A correction cue about nothing on the graph is ordinary conversation and must
// reach the router untouched.
func TestCorrectionWithNoDeliverableFallsThrough(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{`{"reply":"Say more?","command":null}`}}
	user := postUser(t, graph, "quiet", "that's wrong")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if client.callCount() == 0 {
		t.Fatal("a correction about nothing never reached the router")
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a correction about nothing commissioned work: %+v", commands)
	}
}

// A question about a deliverable is a question. It carries a correction cue,
// rejects nothing, and must never buy a re-run of work the answer is already
// sitting in.
func TestQuestionShapedCueIsNotACorrection(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "asking", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)
	client := &fakeClient{responses: []string{`{"reply":"The headcount line.","command":null}`}}
	user := postUser(t, graph, "asking", "sorry, what was that last number?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a question re-commissioned the work: %+v", commands)
	}
}

// The prompt line that taught the router the wrong meaning. It may still route
// a rejected BELIEF to retract, and it must say out loud that a correction of
// work is not that.
func TestRoutingLawNoLongerTeachesThatsWrongAsARetraction(t *testing.T) {
	if strings.Contains(orchestratorPrompt, `("forget that", "that's wrong")`) {
		t.Fatal("the router still reads a correction of work as a notebook retraction")
	}
	if !strings.Contains(orchestratorPrompt, "is not a notebook retraction") {
		t.Fatal("the router is not told what a correction of work is instead")
	}
}

// The leaf said "Everything is verified. The browser builds cleanly, launches a
// Fyne window" while the person watching it was typing "I keep getting could not
// load, no page is loading". The delivery gate believed the leaf; the revision
// spawned from the user's words inherited that belief as trusted context and
// could re-verify nothing while still saying "verified" a second time. The
// instruction now says which of the two accounts is evidence — with no phrase
// list and no new classification, because what counts as a claim and what would
// settle it is a judgment about the words, not a lookup.
func TestCorrectionCarriesTheDisputeAgainstTheDeliverablesOwnVerification(t *testing.T) {
	const verified = "Everything is verified. The browser builds cleanly and launches a Fyne window."
	graph := openHeadStore(t)
	deliverJob(t, graph, "dispute", "ui-browser", "UI browser",
		"build a UI browser and launch it", verified)

	user := postUser(t, graph, "dispute",
		"that's wrong — the browser is not working properly, no page ever loads")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("correction journaled %d commands, want one: %+v", len(commands), commands)
	}
	instruction := commands[0].Instruction
	if !strings.Contains(instruction, verified) {
		t.Fatalf("the disputed claim never reached the revision:\n%s", instruction)
	}
	if !strings.Contains(instruction, correctionDisputeLine) {
		t.Fatalf("the revision was handed the predecessor's verification as settled ground:\n%s", instruction)
	}
}
