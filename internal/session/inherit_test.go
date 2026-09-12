package session

// PROMOTION, THROUGH THE REAL RUNNER.
//
// The two ends of the one road this file's mechanism opens: a worker that IS
// the conversation that asked for it, and the refusal that stands where it
// cannot be. Both are driven the way task_quick_test.go drives every other
// quick task — a scripted model, the real belt, the real graph, the real
// worker — because the whole claim is about what arrives in the worker's FIRST
// REQUEST, and only the road can put it there.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The worker model of the refusal test, and the window an install says it has.
// They are named rather than written into the fixture twice because the whole
// assertion is that ONE conversation is small enough for one window and too big
// for the other.
const (
	inheritSmallModel  = "test/pocket"
	inheritSmallWindow = 32_000
)

// inheritOverSmall is a conversation that fits this build's own default window
// happily and does not fit a worker on the pocket model — the one figure the
// refusal test turns on.
var inheritOverSmall = compactThresholdOf(inheritSmallWindow) + 1

// holding puts a size on a scripted response: the provider's own count of what
// it was sent, which is where [Agent.ContextTokens] reads a conversation's size
// from.
//
// EVERY RESPONSE IN A SCRIPT THAT CARES ABOUT SIZE HAS TO CARRY ONE. A real
// provider's prompt count only grows; a fixture that puts a big number on one
// response and leaves the default on the next has the conversation SHRINK
// between the round that filled it and the call that reads it, which is a
// property of the script rather than of the road.
func holding(response *ai.Response, tokens int) *ai.Response {
	response.Usage = &ai.Usage{PromptTokens: tokens, CompletionTokens: 7, TotalTokens: tokens}
	return response
}

// quickCallInheriting is the model asking for a quick task and asking to be
// carried into it. It is quickCall with the one argument this wave adds, kept
// beside it rather than folded into it so a reader of either test can see at a
// glance which road is under test.
func quickCallInheriting(id, title, line string, tokens int) step {
	arguments, _ := json.Marshal(quickArguments{Title: title, Line: line, Inherit: true})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return holding(toolResponse(id, quickTaskToolName, string(arguments)), tokens), nil
	}
}

// A PROMOTED WORKER OPENS ON THE CONVERSATION AS IT STANDS, AND READS NOTHING
// TWICE.
//
// This is the measured failure inverted. On the owner's laptop a worker started
// out of a running turn was handed a brief and a list of POINTERS at the calls
// that turn had already made, and its first six calls ran them again: 551,000
// input tokens at 24% cached, 393 seconds, not one item ticked. So the thing to
// assert is not that a flag survived the road — it is that the bytes the caller
// paid for are in the worker's own request, and that the pointer section that
// used to stand in for them is not.
func TestAPromotedWorkerOpensOnTheConversationAsItStands(t *testing.T) {
	const evidence = "CARRIED-EVIDENCE-7"
	const line = "say what that command printed, and stop"

	completer := newQuickLanes([]step{
		// THE CALLER DOES A ROUND OF REAL WORK FIRST, because a transcript with
		// nothing in it proves nothing about carrying one.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "bash", `{"command":"echo `+evidence+`"}`), nil
		},
		quickCallInheriting("q1", "finish the reading", line, 4_000),
		finalText("it is out"),
	})
	completer.lane(line, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(evidence + " is what it printed."), nil
	})

	agent, graph, _ := quickAgent(t, completer)
	collect(t, mustSubmit(t, agent, "run that echo and then finish the reading"))

	node := quickNodeSaying(t, graph, line)
	// THE NODE KNOWS IT IS A PROMOTION, and it knows it by holding the transcript
	// rather than by carrying a flag beside it ([quickTaskSpec.inherits]).
	if !node.spec.quick.inherits() {
		t.Fatal("the worker was admitted without the conversation it asked to be started from")
	}
	waitDoneNode(t, node)

	requests := completer.asksOf(line)
	if len(requests) == 0 {
		t.Fatal("the promoted worker never made a request")
	}
	first := requests[0]

	// THE CALLER'S OWN RESULT IS IN THE WORKER'S FIRST REQUEST, verbatim. This is
	// the whole of the saving: what it holds, it does not fetch.
	carried := false
	for _, message := range first {
		if message.Role == "tool" && strings.Contains(messageText(message), evidence) {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("the promoted worker's first request carries no result of its caller's; it holds %d messages",
			len(first))
	}
	// AND THE POINTERS ARE NOT ALSO THERE. A worker holding the results and
	// handed a list of places to go and find them is the failure with one extra
	// step in it (inherit.go's [admissionFor]).
	if completer.sawIn(line, admissionEvidenceHeading) {
		t.Error("a promoted worker was handed pointers at calls it is already holding")
	}
	// AND IT WAS TOLD WHOSE CONVERSATION IT IS READING, which is the one thing a
	// reader of an inherited transcript gets wrong on its own.
	if first[0].Role != "system" || !strings.Contains(messageText(first[0]), "not what you are answering") {
		t.Errorf("the promoted worker opened on %q, want the caller's page with the one paragraph that is "+
			"true of a promotion", messageText(first[0]))
	}
	// AND IT ANSWERED OUT OF WHAT IT WAS HOLDING.
	if notice := node.notice(); notice.State != TaskDone || !strings.Contains(notice.Report, evidence) {
		t.Errorf("the promoted worker landed %q saying %q", notice.State, notice.Report)
	}
}

// AND WHERE THE CONVERSATION WILL NOT FIT, THE MODEL IS TOLD SO AND TOLD WHAT
// TO DO INSTEAD.
//
// The bound is a property rather than a number: a worker can be handed this
// conversation when this conversation still fits inside what that model can
// grow to and keep working ([Agent.inheritFits], `compactThresholdOf`). Past
// it there is no promotion to be had, and the interesting part is what happens
// instead — NOT a silent downgrade to a cold worker, which would be a verb
// doing something cheaper than it was asked for, but a result the model reads
// and can act on.
func TestInheritIsRefusedWhenTheConversationWillNotFitAndSaysWhatToDoInstead(t *testing.T) {
	const line = "carry the rest of this on"

	completer := newQuickLanes([]step{
		// ONE ROUND THAT FILLS MORE THAN THE WORKER'S WINDOW. The figure is the
		// provider's own count of what it was sent, which is where
		// [Agent.ContextTokens] reads a conversation's size from, and it sits
		// between the two windows on purpose: this conversation is still working
		// happily in its own, and no worker on the small model could hold it.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return holding(toolResponse("c1", "bash", `{"command":"echo one"}`), inheritOverSmall), nil
		},
		quickCallInheriting("q1", "carry it on", line, inheritOverSmall),
		finalText("I will finish it here instead."),
	})

	agent, graph, _ := quickAgent(t, completer, func(config *Config) {
		// AND THE WORK LEAVES ON A SMALLER MODEL THAN THE PERSON IS TALKING TO,
		// which is the ordinary case rather than a contrived one: a `task.model`
		// pin or the crew's worker seat hands tasks to a cheaper model every day
		// ([Agent.defaultTaskModel]). The bound is the WORKER'S window, so this is
		// the conversation that does not fit one.
		config.TaskModel = inheritSmallModel
		config.ContextWindowFor = func(model string) int {
			if model == inheritSmallModel {
				return inheritSmallWindow
			}
			return defaultContextWindow
		}
	})
	collect(t, mustSubmit(t, agent, "read the whole tree and then carry the rest on"))

	// NOTHING WAS STARTED.
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d workers were started on a conversation that does not fit one", count)
	}
	// AND THE MODEL WAS TOLD WHY, in figures it can check, with the road that is
	// still open named in the same breath.
	if !completer.sawIn("", "`inherit` is refused") {
		t.Fatal("the refusal never reached the model that asked")
	}
	if !completer.sawIn("", "Start it without `inherit`") {
		t.Error("the refusal named no way on")
	}
}

// A DECLINED CEILING DOES NOT RE-ARM THE NET AT THE VERY NEXT BOUNDARY.
//
// The net is a CONDITION over a quantity that only grows, and three of the
// endings it leads to leave the turn running — no brief to give, work this
// conversation is holding, a reader saying nothing is left. So the shape to be
// afraid of is not the firing, it is the SECOND one: without a latch, every
// remaining step of the turn buys one mastermind call and one unconditional
// `cutGeneration`, and the person watches their answer be chopped once a round
// until it ends. The old ladder could not do this — the ceiling was a rung and a
// rung happens once — and nothing about the new road makes it impossible except
// [checkpointMeter.netRested].
//
// THE FIXTURE IS THE HELD-WORK ENDING, driven by the net rather than by the
// write seam: two pieces genuinely out, a drawing that is half coordination, and
// a turn that touches no file, so the reduction blanks every rung, the ceiling
// declines, and the turn carries on with the condition still true.
func TestADeclinedCeilingDoesNotReadTheWorkAgainEveryRound(t *testing.T) {
	const asked = "land everything once the other two report"
	const draft = "Wait for tasks 1 and 2 to report, then land everything together."

	// LONG PAST THE FIRING, so the count below is a count of what the rest of the
	// turn cost rather than of one boundary: two full prices of rounds after the
	// net's own rung, which is two chances for a latch to be missing.
	rounds := checkpointMarkAt(checkpointMarks) + 2*checkpointPrice + checkpointSlack
	completer := &scriptedCompleter{steps: grindingSteps(rounds, custodyMixedSketch, draft)}
	agent := custodyAgent(t, completer)
	graph := stubbedGraph(agent, custodyRunner(t))
	handOutPiece(t, graph, 1, "the folder picker rail")
	handOutPiece(t, graph, 2, "the settings pane copy")

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// NOTHING WAS ADMITTED, which is what makes this the ending under test: the
	// ceiling declined and the turn was left to carry on.
	if count := admitted(graph); count != 2 {
		t.Fatalf("%d nodes are in the graph, want the two pieces that were already out", count)
	}
	// AND THE WORK WAS READ A HANDFUL OF TIMES AT MOST, not once per round. The
	// bound is the one the meter already owns: a firing buys [checkpointPrice]
	// rounds of quiet, so a turn this long can afford two more readings and no
	// more.
	most := 1 + (rounds-checkpointMarkAt(checkpointMarks))/checkpointPrice
	if read := marksRead(completer); read > most {
		t.Fatalf("the work was read %d times over %d rounds past the net's rung, want at most %d — "+
			"a declined ceiling re-armed the net at the next boundary", read, rounds, most)
	}
	if read := marksRead(completer); read == 0 {
		t.Fatal("the net never fired at all, so this test proves nothing about the latch")
	}
}

// AND THE NOTE'S THREE FIGURES ARE ABOUT THIS ANSWER, NOT ABOUT THE SESSION.
//
// The rounds come from the turn's own meter and the other two are read off the
// transcript, so a walk that started at message zero would put yesterday's files
// and yesterday's bytes beside today's round count — a note telling a model it is
// holding 400 KB after two rounds, which is the kind of figure that makes a model
// do the wrong thing confidently.
func TestTheNotesFiguresCountThisTurnAndNotTheOnesBeforeIt(t *testing.T) {
	transcript := []ai.Message{
		textMessage("system", "SYSTEM"),
		textMessage("user", "what is in the pricing module"),
		{Role: "assistant", ToolCalls: []ai.ToolCall{
			{Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"pricing/old.go"}`}}}},
		textMessage("tool", strings.Repeat("y", 4_096)),
		textMessage("assistant", "it holds the tariff table"),
		// THE SECOND TURN, and a steer inside it: a steer is part of the turn it
		// lands in, so the count starts at the ask above it and not at the steer.
		textMessage("user", "now check the currency module"),
		textMessage("user", "and the rounding while you are in there"),
		{Role: "assistant", ToolCalls: []ai.ToolCall{
			{Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"currency/rates.go"}`}}}},
		textMessage("tool", strings.Repeat("z", 1_024)),
	}

	opened := turnOpenedAt(transcript)
	if want := 5; opened != want {
		t.Fatalf("the turn is read as opening at message %d, want the ask at %d", opened, want)
	}
	facts := readTurnFacts(transcript, opened, 3)
	if facts.rounds != 3 {
		t.Errorf("the rounds are %d, want the meter's own count", facts.rounds)
	}
	if facts.files != 1 {
		t.Errorf("%d files are counted, want the one THIS turn opened", facts.files)
	}
	// THE BYTES ARE THE RESULT PLUS WHAT THE MESSAGE WEIGHS AROUND IT
	// ([messageBytes]), so this asks the question the note asks: is it this turn's
	// one kilobyte, or the session's five.
	if facts.bytes < 1_024 || facts.bytes > 2_048 {
		t.Errorf("%d bytes are counted, want the one result this turn is holding", facts.bytes)
	}
	// AND THE WHOLE-SESSION WALK IS WHAT IT WOULD HAVE SAID, which is the figure
	// the note used to carry.
	if whole := readTurnFacts(transcript, 0, 3); whole.files != 2 || whole.bytes < 5_120 {
		t.Fatalf("the fixture does not distinguish the two walks: the session holds %d files and %d bytes",
			whole.files, whole.bytes)
	}
}

// AND A DRAWING OF WHAT THIS CONVERSATION IS WAITING FOR IS NOT A CHECKLIST FOR
// A WORKER.
//
// "wait for the second | wait for the third | wait for the fourth" is three
// parts by the separator and no pairs of hands at all: every one of them is a
// verb this conversation owns and a worker in its own copy cannot do
// ([checkpointSketch.handsBack]). The gate used to ride on
// [checkpointSketch.split] — the promotion road asked for a split and got the
// hand-back refusal with it — and when the split went, this went with it. It is
// asked in its own right now.
//
// THE TURN HERE IS NOT A WATCHING TURN, which is what makes the case real: a
// turn that only looked at work already out never climbs a rung at all
// ([checkpointMeter.round]), so the coordination test on the other page passes
// whatever this road does. This turn WORKS, runs out of room, and is holding a
// drawing about somebody else's work when the net takes it.
func TestATurnHoldingCoordinationIsNotPromotedWithItAsAChecklist(t *testing.T) {
	const asked = "keep the four pieces moving and land them when they report"
	const coordination = "wait for the second | wait for the third | wait for the fourth\n" +
		"The second, third and fourth pieces are the ones still out."
	const dowry = "Land the pieces\nwhat is left, and everything this turn already found out"

	// THE LOOP ROAD, because it is the one the net takes that CAN promote: a turn
	// whose context ran out cannot hand that context on and is refused a rung
	// earlier ([checkpointMeter.outOfRoom]), so a fixture built on it would pass
	// whatever this gate did ([loopingGrindSteps]).
	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: loopingGrindSteps(rounds, coordination, dowry)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// NOTHING WAS PROMOTED. Whatever else this turn's ending did, it did not hand
	// a worker a list of things only this conversation can do.
	graph.mu.Lock()
	var promoted []string
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node != nil && node.spec.quick != nil {
			promoted = append(promoted, strings.Join(node.spec.quick.items, " | "))
		}
	}
	graph.mu.Unlock()
	if len(promoted) > 0 {
		t.Fatalf("a turn holding coordination was carried on as a quick task with %q as its list", promoted)
	}
	// AND THE CARRY-ON LINE WAS NOT SAID, because no carry-on happened.
	if said := noticeTexts(collected); saidSomething(said, checkpointQuickNote) {
		t.Fatalf("the carry-on said its line over a drawing of what this conversation is waiting for: %q", said)
	}
}
