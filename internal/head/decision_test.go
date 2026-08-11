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
// multi-root subtree and nothing about that rule moved: the head journals one
// splice per piece of work, so each compiles on its own and lands as its own
// root, its own card and its own deliverable — which is the graph the person was
// picturing when they typed the sentence.
//
// What moved is where the judgment sits. It used to be a `commands` array on a
// terminal router decision made before anything had been read; it is now the
// spawn tool's `orders` argument, callable after a board read. The arithmetic
// the terminal position guarded — one splice per order, verbatim words, no
// target — is inside the tool and is what this pins.
func TestOneMessageNamingIndependentWorkBecomesSeveralJobs(t *testing.T) {
	graph := openHeadStore(t)
	user, _ := runBelt(t, graph, "fan", "work issues 12, 41, 77 and 93 on my repo",
		beltToolTurn("s1", beltToolSpawn, map[string]any{"orders": []string{
			"work issue 12 on my repo", "work issue 41 on my repo",
			"work issue 77 on my repo", "work issue 93 on my repo"}}),
		beltTurn{text: "On it — four of them, I'll report back as each lands."})

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

	// And each order is one root when the reconciler applies it, because a splice
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
		beltToolTurn("s1", beltToolSpawn, map[string]any{"instruction": ask}),
		beltTurn{text: "On it — I'll come back with the whole plan."})

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("one plan became %d jobs: %+v", len(commands), commands)
	}
	if commands[0].Instruction != ask {
		t.Fatalf("the trip lost the user's own words: %q", commands[0].Instruction)
	}

	// A list of one is not a fan-out, it is a job.
	graphTwo := openHeadStore(t)
	runBelt(t, graphTwo, "trip", ask,
		beltToolTurn("s1", beltToolSpawn, map[string]any{"orders": []string{ask}}),
		beltTurn{text: "On it."})
	if commands := pendingCommandsOf(t, graphTwo); len(commands) != 1 ||
		commands[0].Instruction != ask {
		t.Fatalf("a one-entry list did not collapse to one job: %+v", commands)
	}

	// And the law the model judges by is written down where the model reads it.
	// That used to be the router's system prompt; the judgment is an argument to
	// spawn now, so the test the model applies belongs in spawn's own
	// description — the one string a tool-calling model is shown for it.
	spawnDescription := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolSpawn {
			spawnDescription = definition.Function.Description
		}
	}
	if !strings.Contains(spawnDescription, "genuinely independent") {
		t.Errorf("the independence test is not stated to the model that applies it: %q", spawnDescription)
	}
	if !strings.Contains(spawnDescription, "When it could be read either way it is one") {
		t.Error("the tie-break that keeps a trip one job is no longer stated")
	}
}

// A message that names more things than a person addresses in one breath is a
// list, and a list is one job that enumerates — which is what the compiler
// already does well. The fallback keeps the user's whole sentence.
func TestTooManyOrdersFallBackToOneJobCarryingTheWholeAsk(t *testing.T) {
	graph := openHeadStore(t)
	orders := make([]string, 0, fanOutLimit+2)
	for index := 0; index < fanOutLimit+2; index++ {
		orders = append(orders, fmt.Sprintf("work item %d", index))
	}
	const ask = "work every open item on the board"
	runBelt(t, graph, "many", ask,
		beltToolTurn("s1", beltToolSpawn, map[string]any{"orders": orders}),
		beltTurn{text: "On it."})

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("an over-long list journaled %d jobs, want one: %+v", len(commands), commands)
	}
	if commands[0].Instruction != ask {
		t.Fatalf("the fallback dropped the user's words: %q", commands[0].Instruction)
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
		beltToolTurn("c1", beltToolCorrect, map[string]any{
			"job": "landlord-letter", "words": ask}),
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

	refusal, failed := run.execute(beltToolCorrect, beltArguments(t, map[string]any{
		"job": "unstarted", "words": "make it warmer"}))
	if !failed {
		t.Fatalf("correcting work that delivered nothing was accepted: %s", refusal)
	}
	if !strings.Contains(refusal, "correction is for work that already delivered") {
		t.Fatalf("the refusal does not say why: %q", refusal)
	}
	if run.acted || len(pendingCommandsOf(t, graph)) != 0 {
		t.Fatalf("a refused correction journaled something: acted=%t commands=%+v",
			run.acted, pendingCommandsOf(t, graph))
	}

	// The honest route the refusal points at: ordinary, untargeted, uncorrected.
	if _, failed := run.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "make it warmer"})); failed {
		t.Fatal("spawn refused the work the correction handed back")
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
		beltToolTurn("e1", beltToolExpedite, map[string]any{"job": "old-job"}),
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
