package head

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Four faults read out in one breath are four jobs. The store still refuses a
// multi-root subtree and nothing about that rule moved: the head journals one
// splice per piece of work, so each compiles on its own and lands as its own
// root, its own card and its own deliverable — which is the graph the person was
// picturing when they typed the sentence.
func TestOneMessageNamingIndependentWorkBecomesSeveralJobs(t *testing.T) {
	graph := openHeadStore(t)
	user := answerWith(t, graph, "fan", "work issues 12, 41, 77 and 93 on my repo",
		`{"reply":"On it — four of them, I'll report back as each lands.","command":null,"commands":[
			{"kind":"splice","instruction":"work issue 12 on my repo"},
			{"kind":"splice","instruction":"work issue 41 on my repo"},
			{"kind":"splice","instruction":"work issue 77 on my repo"},
			{"kind":"splice","instruction":"work issue 93 on my repo"}]}`)

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

	// One sentence, one answer.
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
	answerWith(t, graph, "trip", ask,
		`{"reply":"On it — I'll come back with the whole plan.","command":{"kind":"splice","target":"","instruction":"`+ask+`"}}`)

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("one plan became %d jobs: %+v", len(commands), commands)
	}
	if commands[0].Instruction != ask {
		t.Fatalf("the trip lost the user's own words: %q", commands[0].Instruction)
	}

	// A list of one is not a fan-out, it is a job; and the law the model judges
	// by is written down where the model reads it.
	graphTwo := openHeadStore(t)
	answerWith(t, graphTwo, "trip", ask,
		`{"reply":"On it.","command":null,"commands":[{"kind":"splice","instruction":"`+ask+`"}]}`)
	if commands := pendingCommandsOf(t, graphTwo); len(commands) != 1 ||
		commands[0].Instruction != ask {
		t.Fatalf("a one-entry list did not collapse to one job: %+v", commands)
	}
	if !strings.Contains(orchestratorPrompt, "genuinely independent") {
		t.Error("the independence test is not stated to the model that applies it")
	}
}

// A message that names more things than a person addresses in one breath is a
// list, and a list is one job that enumerates — which is what the compiler
// already does well. The fallback keeps the user's whole sentence.
func TestTooManyOrdersFallBackToOneJobCarryingTheWholeAsk(t *testing.T) {
	graph := openHeadStore(t)
	orders := make([]string, 0, fanOutLimit+2)
	for index := 0; index < fanOutLimit+2; index++ {
		orders = append(orders, fmt.Sprintf(`{"kind":"splice","instruction":"work item %d"}`, index))
	}
	const ask = "work every open item on the board"
	answerWith(t, graph, "many", ask,
		`{"reply":"On it.","command":null,"commands":[`+strings.Join(orders, ",")+`]}`)

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
func TestPoliteAdjustmentLandsAsACorrectionOfTheDeliveredWork(t *testing.T) {
	graph := openHeadStore(t)
	const delivered = "Dear Mr Okafor, I am writing to formally notify you of persistent mould in the bathroom."
	deliverJob(t, graph, "polite", "landlord-letter", "Landlord mould letter",
		"write my landlord about the mould", delivered)

	const ask = "make it warmer and less legal"
	user := answerWith(t, graph, "polite", ask,
		`{"reply":"Warming it up and taking the legal edge off.","command":null,"adjust":true}`)

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
	if !strings.Contains(reply.Body, "Landlord mould letter") {
		t.Fatalf("the receipt does not name what is being changed: %q", reply.Body)
	}
}

// An adjustment with nothing delivered to adjust is not an adjustment. The
// reading hands the message back rather than inventing a target for it.
func TestAdjustmentWithNothingDeliveredFallsBackToOrdinaryWork(t *testing.T) {
	graph := openHeadStore(t)
	answerWith(t, graph, "nothing", "make it warmer",
		`{"reply":"On it.","command":{"kind":"splice","target":"","instruction":"make it warmer"},"adjust":true}`)
	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 || commands[0].Target != "" || IsCorrection(commands[0].Instruction) {
		t.Fatalf("an unanchored adjustment did not fall through to ordinary work: %+v", commands)
	}
}

// "taking too long" is in the frozen phrase list and "taking forever" is not,
// and the next phrasing is always the one nobody wrote down. The list stays
// frozen; what it misses is read by the model, and the expedite lane it reaches
// is the one that was already there — no question, and the receipt names the job
// that took the pressure.
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

	user := answerWith(t, graph, "urgent", ask,
		`{"reply":"Pushing the market research up the queue.","command":null,"urgent":true}`)

	commands := pendingCommandsOf(t, graph)
	if len(commands) != 1 {
		t.Fatalf("urgency journaled %d commands: %+v", len(commands), commands)
	}
	if commands[0].Kind != store.CommandExpedite {
		t.Fatalf("urgency did not reach the expedite lane: %s", commands[0].Kind)
	}
	if commands[0].Target != "old-job" {
		t.Fatalf("the pressure landed on %q, want the oldest thing running", commands[0].Target)
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
