package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// runBelt drives one whole turn against a scripted tool loop and returns the
// message it answered. The judgment that used to be a field on a router decision
// is now an argument to a tool, so the fixture is a tool call rather than a JSON
// envelope — and what is asserted underneath it did not move at all.
func runBelt(t *testing.T, graph *store.Store, session, body string, turns ...beltTurn) (store.Message, *beltClient) {
	t.Helper()
	user := postUser(t, graph, session, body)
	client := &beltClient{turns: turns}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatalf("answer %q: %v", body, err)
	}
	return user, client
}

func beltToolTurn(id, name string, args map[string]any) beltTurn {
	return beltTurn{calls: []ai.ToolCall{beltCall(id, name, args)}}
}

// Four faults read out in one breath are four jobs. The store still refuses a
// multi-root subtree and nothing about that rule moved: one splice is one piece
// of work, so each compiles on its own and lands as its own root, its own card
// and its own deliverable — which is the graph the person was picturing when
// they typed the sentence.
//
// What moved is HOW. It used to be a `commands` array on a terminal router
// decision, then an `orders` argument on spawn — a way for the chat to split
// work. Both are gone (chat-simplify §2.3): the chat never divides an ask, and
// genuinely unrelated asks are separate task calls in the same turn. The
// arithmetic the array guarded — one splice per piece, verbatim words, no
// target — is what this still pins, arrived at from the other side.
func TestOneMessageNamingIndependentWorkBecomesSeveralJobs(t *testing.T) {
	graph := openHeadStore(t)
	user, _ := runBelt(t, graph, "fan", "work issues 12, 41, 77 and 93 on my repo",
		beltTurn{calls: []ai.ToolCall{
			beltCall("s1", beltToolTask, map[string]any{"instruction": "work issue 12 on my repo"}),
			beltCall("s2", beltToolTask, map[string]any{"instruction": "work issue 41 on my repo"}),
			beltCall("s3", beltToolTask, map[string]any{"instruction": "work issue 77 on my repo"}),
			beltCall("s4", beltToolTask, map[string]any{"instruction": "work issue 93 on my repo"}),
		}},
		beltTurn{text: "On it — four of them, each landing here as it finishes."})

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 4 {
		t.Fatalf("one message journaled %d work orders, want four: %+v", len(commands), commands)
	}
	for index, command := range commands {
		if command.Kind != store.CommandSplice || strings.TrimSpace(command.Target) != "" {
			t.Fatalf("order %d = %s at %q, want an untargeted splice", index, command.Kind, command.Target)
		}
		if !strings.Contains(command.Instruction, "issue") {
			t.Fatalf("order %d lost its own words: %q", index, command.Instruction)
		}
	}

	// And each one is one root when the reconciler applies it, because a splice
	// is one subtree with one root and always has been.
	for index, command := range commands {
		spliceSurgeryJob(t, graph, fmt.Sprintf("issue-job-%d", index),
			fmt.Sprintf("Issue job %d", index), command.Instruction)
	}
	roots, err := graph.SearchSurgeryTargets("", false, store.Pending, store.Claimed, store.Running)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 4 {
		t.Fatalf("four work orders produced %d job roots: %+v", len(roots), roots)
	}

	// One sentence, one answer. A turn that spends several provider calls is
	// still one turn, and the person hears from it exactly once.
	if replies := agentRepliesAfter(t, graph, "fan", user.Seq); len(replies) != 1 {
		t.Fatalf("four jobs earned %d replies, want one: %+v", len(replies), replies)
	}
}

// The other direction is the one that costs money when it is wrong. A trip is
// flights and a hotel and somewhere to eat, and it is one plan with one thing to
// hand back — more commas than the four issues, and still one job.
func TestOnePlanWithManyPartsStaysOneJob(t *testing.T) {
	graph := openHeadStore(t)
	const ask = "plan a trip to Lisbon in October — flights, a hotel, and somewhere to eat"
	runBelt(t, graph, "trip", ask,
		beltToolTurn("s1", beltToolTask, map[string]any{"instruction": ask}),
		beltTurn{text: "On it — I'll come back with the whole plan."})

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("one plan became %d jobs: %+v", len(commands), commands)
	}
	if commands[0].Instruction != ask {
		t.Fatalf("the trip lost the user's own words: %q", commands[0].Instruction)
	}

	// And the law the model judges by is written down where the model reads it.
	// That used to be the router's system prompt; the judgment is the task tool's
	// now, so the test the model applies belongs in its own description — the one
	// string a tool-calling model is shown for it.
	description := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolTask {
			description = definition.Function.Description
		}
	}
	if !strings.Contains(description, "One ask is ONE task") {
		t.Errorf("the one-ask-one-task law is not stated to the model that applies it: %q", description)
	}
	if !strings.Contains(description, "the workforce decomposes it") {
		t.Error("the reason the chat does not split work is no longer stated")
	}
}

// The chat cannot split work at all any more, and that is a property of the
// SCHEMA rather than of any arithmetic: there is no argument on task that takes
// more than one ask, so an over-long list cannot be journaled as one call's
// worth of jobs. What replaced the fan-out cap is the plainer rule — the
// workforce decomposes, and the whole sentence travels to it.
func TestTaskCannotCarryMoreThanOneAsk(t *testing.T) {
	properties := map[string]any{}
	for _, definition := range beltDefinitions() {
		if definition.Function.Name != beltToolTask {
			continue
		}
		properties, _ = definition.Function.Parameters["properties"].(map[string]any)
	}
	if len(properties) == 0 {
		t.Fatal("task has no arguments at all")
	}
	for name, property := range properties {
		shape, _ := property.(map[string]any)
		if shape["type"] == "array" {
			t.Fatalf("task grew an array argument %q — the chat is splitting work again", name)
		}
	}
	if _, present := properties["orders"]; present {
		t.Fatal("the orders argument is back")
	}

	// And an over-long enumeration is journaled whole, as one job, because that
	// is the only thing one call can do with it.
	graph := openHeadStore(t)
	const ask = "work every open item on the board: 12, 41, 77, 93, 104, 118, 122, 130"
	runBelt(t, graph, "many", ask,
		beltToolTurn("s1", beltToolTask, map[string]any{"instruction": ask}),
		beltTurn{text: "On it."})
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("an enumeration journaled %d jobs, want one: %+v", len(commands), commands)
	}
	if commands[0].Instruction != ask {
		t.Fatalf("the job dropped the user's words: %q", commands[0].Instruction)
	}
}

// "Make it warmer, less legal" matches no conflict cue and never will — the cue
// list is frozen and growing it is the wrong repair. The model reads the
// sentence as an adjustment of what was handed over, and everything downstream
// is the correction path unchanged: same job, previous version in hand, and the
// marker the workspace inheritance keys off.
//
// The door it comes through is the correct tool now rather than an `adjust` flag
// on a router decision. A blunt rejection and a polite adjustment are one tool
// because they are one event said in two registers.
func TestPoliteAdjustmentLandsAsACorrectionOfTheDeliveredWork(t *testing.T) {
	graph := openHeadStore(t)
	const delivered = "Dear Mr Okafor, I am writing to formally notify you of persistent mould in the bathroom."
	deliverJob(t, graph, "polite", "landlord-letter", "Landlord mould letter",
		"write my landlord about the mould", delivered)

	const ask = "make it warmer and less legal"
	user, _ := runBelt(t, graph, "polite", ask,
		beltToolTurn("c1", beltToolTask, map[string]any{
			"amends": "landlord-letter", "instruction": ask}),
		beltTurn{text: "Warming it up and taking the legal edge off."})

	// The cue list is untouched: this sentence still matches nothing in it.
	if cue, cued := redirectCue(ask); cued {
		t.Fatalf("the frozen cue list grew to cover %q (as %q)", ask, cue)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("the adjustment journaled %d commands: %+v", len(commands), commands)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice || command.Target != "landlord-letter" {
		t.Fatalf("the adjustment did not go back to the letter: %s at %q", command.Kind, command.Target)
	}
	if !strings.HasPrefix(command.Instruction, ask) {
		t.Fatalf("the user's words are not verbatim at the front:\n%s", command.Instruction)
	}
	if !strings.Contains(command.Instruction, CorrectionPrefix+" landlord-letter") {
		t.Fatalf("the revision carries no anchor for the workspace to inherit:\n%s", command.Instruction)
	}
	if !strings.Contains(command.Instruction, delivered) {
		t.Fatalf("the previous version never reached the revision:\n%s", command.Instruction)
	}
	if !strings.Contains(command.Instruction, correctionDisputeLine) {
		t.Fatalf("the revision lost the line that says who outranks whom:\n%s", command.Instruction)
	}
	reply := waitForAgentReply(t, graph, "polite", user.Seq)
	if strings.TrimSpace(reply.Body) == "" || reply.CommandSeq != command.Seq {
		t.Fatalf("the receipt is not tied to the work it changed: %+v", reply)
	}
}

// An adjustment with nothing delivered to adjust is not an adjustment. The tool
// refuses it in a sentence the loop must speak to, and journals nothing —
// where the router's `adjust` flag used to silently fall through to ordinary
// work, the refusal is now visible and the loop has to choose the honest route.
func TestAdjustmentWithNothingDeliveredFallsBackToOrdinaryWork(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "unstarted", "Warm letter", "write the letter")
	user := postUser(t, graph, "nothing", "make it warmer")
	run := &beltRun{head: New(nil, graph), user: user}

	refusal, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"amends": "unstarted", "instruction": "make it warmer"}))
	if !failed {
		t.Fatalf("correcting work that delivered nothing was accepted: %s", refusal)
	}
	if !strings.Contains(refusal, "amends is for work that already delivered") {
		t.Fatalf("the refusal does not say why: %q", refusal)
	}
	if run.acted || len(pendingCommandsOf(t, graph)) != 0 {
		t.Fatalf("a refused correction journaled something: acted=%t commands=%+v",
			run.acted, pendingCommandsOf(t, graph))
	}

	// The honest route the refusal points at: ordinary, untargeted, uncorrected.
	if _, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
		"instruction": "make it warmer instead"})); failed {
		t.Fatal("task refused the work the refusal handed back")
	}
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Target != "" || IsCorrection(commands[0].Instruction) {
		t.Fatalf("the fall-through did not land as ordinary work: %+v", commands)
	}
}

// "taking too long" is in the frozen phrase list and "taking forever" is not,
// and the next phrasing is always the one nobody wrote down. The list stays
// frozen; what it misses is now reached because there is no list standing
// between the sentence and the tools at all. The expedite lane it reaches is the
// one that was already there — no question, and the receipt names the job that
// took the pressure.
func TestUrgencyIsReachedWithoutACuePhrase(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "old-job", "Market research", "research the market")
	spliceSurgeryJob(t, graph, "new-job", "Podcast edit", "edit the podcast")
	claim, ok, err := graph.Claim("old-job", "tester")
	if err != nil || !ok {
		t.Fatalf("claim: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}

	const ask = "this is taking forever"
	if _, cued := redirectCue(ask); cued {
		t.Fatalf("the frozen cue list grew to cover %q", ask)
	}
	if urgencyCued(ask) {
		t.Fatal("the fixture no longer exercises the gap in the phrase list")
	}

	user, client := runBelt(t, graph, "urgent", ask,
		beltToolTurn("b1", beltToolBoard, map[string]any{}),
		beltToolTurn("e1", beltToolChange, map[string]any{"target": "old-job", "words": "hurry up"}),
		beltTurn{text: "Pushing the market research up the queue."})

	// A message no phrase list covers still arrives with the board in hand, so
	// the id the verb needs is there to be read rather than guessed.
	if opening := client.openingPrompt(); !strings.Contains(opening, "- old-job | ") {
		t.Fatalf("the uncued message reached the loop without the board:\n%s", opening)
	}

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("urgency journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Kind != store.CommandExpedite {
		t.Fatalf("urgency did not reach the expedite lane: %s", commands[0].Kind)
	}
	if commands[0].Target != "old-job" {
		t.Fatalf("the pressure landed on %q, want the thing that is running", commands[0].Target)
	}
	if replies := agentRepliesAfter(t, graph, "urgent", user.Seq); len(replies) != 1 {
		t.Fatalf("urgency produced %d replies, want one: %+v", len(replies), replies)
	}
}

// And impatience no longer switches itself off at the load that produces it: a
// bare pronoun under a cue counts however many jobs are live, and the question
// that used to be impossible is now one plain question.
func TestBarePronounUnderACueCountsWithSeveralJobsLive(t *testing.T) {
	if !refersToLiveWork("skip it", true, 4) {
		t.Fatal("a cued pronoun stopped referring to live work once four jobs were running")
	}
	if refersToLiveWork("thanks, that helps", false, 4) {
		t.Fatal("an uncued pronoun became a reference to live work")
	}
	if !refersToLiveWork("thanks, that helps", false, 1) {
		t.Fatal("one job running is no longer licence enough for a bare pronoun")
	}
}
