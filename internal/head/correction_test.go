package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const deliveredNumbers = "Q3 revenue was $4.21M against a $3.90M plan, and headcount closed at 214."

// The correction path moved house without changing shape. It used to be a cue
// recognizer that journaled terminally; it is now a reading in the prompt
// (hints.go) plus the correct tool, whose body is the same correction block with
// the same dispute line and the same anchor. So each test below asserts both
// halves: that the reading which used to decide silently is in front of the
// model, and that the tool it points at still does exactly what the recognizer
// did.

// correctionAnchorLine is how renderHints names the delivered work a correction
// is about. Asserting the sentence rather than the id is deliberate: the whole
// point of the demotion is that the model reads this as prose.
func correctionAnchorLine(id, label string) string {
	return "the delivered work it most likely means is " + id + " (" + label + ")"
}

// The whole shape, end to end: a delivered job, "that's wrong, the numbers are
// off", and one journaled revision of THAT deliverable carrying the previous
// attempt and the critique. Before this the sentence was recognized by
// redirectCue, discarded because nothing was live, and then read by the router
// as an instruction to delete a notebook belief.
func TestCorrectionOfDeliveredWorkJournalsARevisionCarryingCritiqueAndAnchor(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "correct", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)

	const critique = "that's wrong, the numbers are off"
	// The model says nothing of its own, so the reply is the receipt the tool
	// itself reported — which is what makes the assertion about the receipt an
	// assertion about the engine rather than about the fixture.
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolTask, map[string]any{
			"amends": "q3-numbers", "instruction": critique})}},
		{text: ""},
	}}
	user := postUser(t, graph, "correct", critique)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	// The reading the recognizer used to act on is now evidence in the prompt,
	// naming the one referent no board read resolves: a job that is over.
	if opening := client.openingPrompt(); !strings.Contains(opening,
		correctionAnchorLine("q3-numbers", "Q3 numbers")) {
		t.Fatalf("the loop was not told which delivered work this is about:\n%s", opening)
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
	if !strings.HasPrefix(command.Instruction, critique) {
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
//
// The question is prose now rather than a recognizer's askback, so what this
// pins is the two things prose cannot supply itself: the anchor is computed and
// handed to the model before it writes, and a turn that only speaks journals
// nothing at all.
func TestBareThatsWrongAsksOneQuestionNamingTheDeliverable(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "bare", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)

	head := New(nil, graph)
	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	bare := postUser(t, graph, "bare", "that's wrong")
	reading := head.renderHints(bare, active)
	if !strings.Contains(reading, "reads as a correction") {
		t.Fatalf("a bare rejection is no longer read as a correction:\n%s", reading)
	}
	if !strings.Contains(reading, correctionAnchorLine("q3-numbers", "Q3 numbers")) {
		t.Fatalf("the reading does not name what was delivered:\n%s", reading)
	}

	client := &beltClient{turns: []beltTurn{
		{text: "The Q3 numbers, from the close I sent you. " + correctionAskTail},
	}}
	if err := New(client, graph).answer(context.Background(), bare); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a bare rejection commissioned work: %+v", commands)
	}
	if opening := client.openingPrompt(); !strings.Contains(opening,
		correctionAnchorLine("q3-numbers", "Q3 numbers")) {
		t.Fatalf("the anchor never reached the model that had to name it:\n%s", opening)
	}
	reply := waitForAgentReply(t, graph, "bare", bare.Seq)
	if !strings.Contains(reply.Body, "Q3 numbers") || !strings.HasSuffix(reply.Body, correctionAskTail) {
		t.Fatalf("the question does not name the deliverable and ask what is wrong: %q", reply.Body)
	}
	if reply.CommandSeq != 0 {
		t.Fatalf("a question was tied to durable work: %+v", reply)
	}
}

// And the answer to that question — ordinary words, no cue in them anywhere —
// becomes the revision. The rail used to be a durable anchored question; it is
// the thread itself now, so what has to hold is that the head's own question is
// still in front of the loop on the next message and that the answer's words
// travel verbatim into the correction.
func TestAnswerToTheClarifyingQuestionBecomesTheRevision(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "answered", "q3-numbers", "Q3 numbers",
		"pull the Q3 revenue and headcount numbers", deliveredNumbers)

	const question = "The Q3 numbers. " + correctionAskTail
	asking := &beltClient{turns: []beltTurn{{text: question}}}
	first := postUser(t, graph, "answered", "that's wrong")
	if err := New(asking, graph).answer(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("the question journaled work before it was answered: %+v", commands)
	}

	const answer = "the revenue figure is from 2023, not 2024"
	answering := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolTask, map[string]any{
			"amends": "q3-numbers", "instruction": answer})}},
		{text: ""},
	}}
	second := postUser(t, graph, "answered", answer)
	if err := New(answering, graph).answer(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	// The rail: the head's own question is what makes the next sentence a
	// critique rather than a fresh ask, and it is only a rail if the loop can
	// still see it.
	if opening := answering.openingPrompt(); !strings.Contains(opening, question) {
		t.Fatalf("the question this answers is not in front of the loop:\n%s", opening)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("the answer journaled %d commands, want one: %+v", len(commands), commands)
	}
	if commands[0].Target != "q3-numbers" || !IsCorrection(commands[0].Instruction) {
		t.Fatalf("the answer did not become a revision of the deliverable: %+v", commands[0])
	}
	if !strings.HasPrefix(commands[0].Instruction, answer) {
		t.Fatalf("the critique lost the user's words:\n%s", commands[0].Instruction)
	}
}

// Live work is redirection's, and it stays redirection's. A correction while a
// job is running edits the plan; it must never become a re-delivery — and the
// membrane that guarantees it is in the tool itself, which refuses any job that
// has not delivered.
func TestCorrectionLeavesLiveWorkToRedirection(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "live-audit", "Live audit", "audit the ledger")
	startNode(t, graph, "live-audit")

	const words = "actually, that's wrong — use the audited ledger"
	refusing := &beltRun{head: New(nil, graph), user: postUser(t, graph, "refuse", words)}
	message, failed := refusing.task(map[string]any{"amends": "live-audit", "instruction": words})
	if !failed || !strings.Contains(message, "amends is for work that already delivered") {
		t.Fatalf("amends reached live work: failed=%t %q", failed, message)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a refused correction still journaled: %+v", commands)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolChange, map[string]any{
			"target": "live-audit", "words": words})}},
		{text: ""},
	}}
	user := postUser(t, graph, "live", words)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "live-audit" || commands[0].Instruction != words {
		t.Fatalf("a correction over live work should redirect it: %+v", commands)
	}
}

// A correction cue about nothing on the graph is ordinary conversation and must
// still reach the model, which is the only party that can answer it.
func TestCorrectionWithNoDeliverableFallsThrough(t *testing.T) {
	graph := openHeadStore(t)
	client := &fakeClient{responses: []string{"Say more?"}}
	user := postUser(t, graph, "quiet", "that's wrong")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if client.callCount() == 0 {
		t.Fatal("a correction about nothing never reached the model")
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
	client := &fakeClient{responses: []string{"The headcount line."}}
	user := postUser(t, graph, "asking", "sorry, what was that last number?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands := pendingCommandsOf(t, graph); len(commands) != 0 {
		t.Fatalf("a question re-commissioned the work: %+v", commands)
	}
}

// The prompt line that taught the router the wrong meaning is gone with the
// router, and so is the retraction verb it pointed at. What replaces the old
// assertion is the same guarantee stated against what exists: the one prompt
// never equates a rejection of delivered work with a memory operation, the belt
// has no retraction tool for it to reach, and the tool it does have says in its
// own description that a rejection is a redo of the deliverable.
func TestRoutingLawNoLongerTeachesThatsWrongAsARetraction(t *testing.T) {
	if strings.Contains(orchestratorPrompt, `("forget that", "that's wrong")`) {
		t.Fatal("the prompt still reads a correction of work as a notebook retraction")
	}
	for _, definition := range beltDefinitions() {
		switch definition.Function.Name {
		case beltToolForget:
			// The retraction door exists — a person who says a numbered belief is
			// untrue must be able to let it go. What it may never be is the answer
			// to a rejected deliverable, so its own description has to say which
			// of the two it is for, out loud, where the model reads it.
			low := strings.ToLower(definition.Function.Description)
			if !strings.Contains(low, "not for a correction aimed at work") {
				t.Fatalf("the retraction verb does not refuse a correction of work:\n%s",
					definition.Function.Description)
			}
			if !strings.Contains(low, "loses the correction entirely") {
				t.Fatalf("the retraction verb does not say what quietly deleting a belief costs:\n%s",
					definition.Function.Description)
			}
		case beltToolNote:
			if strings.Contains(strings.ToLower(definition.Function.Description), "that's wrong") {
				t.Fatalf("the notebook write still claims a rejection of work:\n%s",
					definition.Function.Description)
			}
		}
	}
	correction := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolTask {
			properties, _ := definition.Function.Parameters["properties"].(map[string]any)
			amends, _ := properties["amends"].(map[string]any)
			correction, _ = amends["description"].(string)
		}
	}
	if correction == "" {
		t.Fatal("there is no correction route for a rejection of delivered work to reach")
	}
	if !strings.Contains(correction, "reject, dispute or want changed something already delivered") {
		t.Fatalf("the amends argument does not claim the sentence the retraction verb used to take:\n%s", correction)
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

	const critique = "that's wrong — the browser is not working properly, no page ever loads"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolTask, map[string]any{
			"amends": "ui-browser", "instruction": critique})}},
		{text: ""},
	}}
	user := postUser(t, graph, "dispute", critique)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
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
