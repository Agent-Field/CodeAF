package session

// SAYING WHAT IS WRONG WITH A PAGE, AND GETTING A BETTER ONE.
//
// A design used to have two endings and the person had to pick one of them
// looking at a draft they had read once: keep it, or throw away a minute of
// model work and start over. The room underneath the card was a place they could
// stand and ask questions in, and the one thing they most wanted to do there —
// "make it also run the linter" — was refused in so many words.
//
// These tests hold the third ending. The design's thread has one verb
// (revise_design), the verb reaches the loop parked on the card
// ([TaskNode.reviseDoor]), the card is WITHDRAWN before the page under it stops
// existing, the same gauntlet writes the page again, and a new card goes up. The
// two old endings are still exactly the two old endings, which the last tests
// here are about: a loop that broke either of them would be a worse program than
// the straight line it replaced.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fixtures ────────────────────────────────────────────────────────────

const reviseAsk = "make it also run the linter before it reports"

// revisedReply is the SECOND page: the same harness with a third step in it, so
// a test can tell the two apart by shape rather than by counting calls.
const revisedReply = `{
  "cues": ["flaky test", "triage the flake", "chase a flake"],
  "justification": "Three jobs now: lint, read the failure, then check the report names it.",
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "lint", "kind": "agent.loop", "fields": {"brief": "run the linter and say what it found", "tools": "read", "max_turns": "2"}},
        {"id": "look", "kind": "agent.loop", "fields": {"brief": "read the failing test and say what it does", "tools": "read", "max_turns": "3"}},
        {"id": "check", "kind": "verify", "fields": {"ladder": "accept", "check": "the report names the failing test"}}
      ],
      "edges": [["lint", "look"], ["look", "check"]]
    },
    "whitelist": ["read"],
    "verify": {"ladder": "accept"},
    "dyn": {"ladder": "fixed"}
  }
}`

// revisingCompleter answers a whole first design and then a whole second one.
// The design turns are told apart by what is in the history rather than by
// counting, because the thread agent's own turns are on the same client and a
// script that counted would drift the moment a test said one more thing.
func revisingCompleter() *scriptedCompleter {
	return &scriptedCompleter{steps: repeatedly(func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		last := ""
		if len(messages) > 0 {
			last = messageContentText(messages[len(messages)-1])
		}
		switch {
		case strings.Contains(last, "Review it and reply with your findings"):
			return textResponse(reviewReply), nil
		case strings.Contains(last, reviseAsk):
			return textResponse(revisedReply), nil
		case strings.Contains(last, "Design the sub-harness for it"):
			return textResponse(designReply), nil
		}
		return textResponse("nothing to add"), nil
	})}
}

// repeatedly makes a script that answers every call the same way. The design
// ladder's call count is not a fact any of these tests is about.
func repeatedly(answer step) []step {
	steps := make([]step, 64)
	for i := range steps {
		steps[i] = answer
	}
	return steps
}

// askForAChange calls the design thread's own tool, which is what the model does
// when it decides the person asked for something different. It is called on the
// CHILD agent because that is the only belt the tool is on.
func askForAChange(t *testing.T, node *TaskNode, change string) (string, bool) {
	t.Helper()
	child := node.openRoom().speaker()
	if child == nil {
		t.Fatal("nobody is in the design's room")
	}
	return runTool(t, child, "revise_design", mustJSON(t, map[string]string{"change": change}))
}

// savedSteps is how many steps the registry's newest version of one harness
// has. It is the cheapest way to tell the two fixture pages apart from outside.
func savedSteps(t *testing.T, agent *Agent, name string) int {
	t.Helper()
	page, err := agent.config.HarnessStore.Load(name, 0)
	if err != nil {
		t.Fatalf("nothing reached the registry as %q: %v", name, err)
	}
	return len(page.Program.Nodes)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	written, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("could not write the arguments: %v", err)
	}
	return string(written)
}

// ── the verb exists exactly where it can work ───────────────────────────────

// THE HAND IS THE DESIGN THREAD'S AND NOBODY ELSE'S. tools.go's law is that a
// capability with nothing behind it is absent rather than broken, and the whole
// gate here is the door being nil: a conversation has no design parked on a
// card, so it is never given the verb and can never be refused for using it.
func TestOnlyADesignsOwnThreadCarriesTheReviseVerb(t *testing.T) {
	agent, _ := buildAgent(t, revisingCompleter(), t.TempDir())
	if hasTool(agent, "revise_design") {
		t.Fatal("the conversation was given a verb for changing a page it is not holding")
	}
	// THE LANE IS SUBSCRIBED BEFORE THE BUILD IS ASKED FOR. A design outlives
	// its turn and runs on its own goroutine, so with a scripted model its
	// "designing" and card events can both be emitted before a subscription
	// taken AFTER the build is registered — and [Agent.emitHarness] fans out
	// only to the watchers present at emit time, so a late subscriber meets an
	// empty lane and the read hangs. Every other test in this file subscribes
	// first for exactly this reason; this one did not, which is the flake.
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)

	child := node.openRoom().speaker()
	if child == nil {
		t.Fatal("nobody is in the design's room")
	}
	if !hasTool(child, "revise_design") {
		t.Fatal("the design's own thread cannot ask for the page to be changed")
	}

	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// ── the loop ────────────────────────────────────────────────────────────────

// THE WHOLE ROUND TRIP, and it is the test this file exists for: the change goes
// in, the card comes down, the page is written again with the change in it, a
// new card goes up, and the person's approval saves THAT page.
func TestAChangeWithdrawsTheCardRewritesThePageAndAsksAgain(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, revisingCompleter(), dir)
	lane := agent.HarnessDesigns()
	// THE PHASES ARE WATCHED ON THE WIRE AND NEVER POLLED FOR. Every phase change
	// is announced (harness_task.go's doingNow), and a rewrite in a test is over
	// in microseconds — so a poll asking "what is it doing now" would miss the
	// designing phase entirely on a fast machine and see it on a slow one, which
	// is a test that reports the machine rather than the program.
	phases := agent.TaskUpdates()
	submitBuild(t, agent)

	first := designDone(t, lane)
	node := designNode(t, agent)
	// Read the lane up to the FIRST card going up, so that the designing phase
	// waited for below can only be the rewrite's — the first round's is behind
	// this line, and a wait that matched it would pass before anything happened.
	waitForPhaseOnTheWire(t, phases, node.id, HarnessPhaseAsking)
	if first.Harness == nil || len(first.Harness.Program.Nodes) != 2 {
		t.Fatalf("the first card is not the first draft: %+v", first.Harness)
	}

	if text, isError := askForAChange(t, node, reviseAsk); isError {
		t.Fatalf("the design's own thread could not ask for a change: %s", text)
	}

	// THE CARD COMES DOWN BEFORE THE PAGE UNDER IT STOPS EXISTING. Left standing
	// it would be a save key over a draft that has been replaced.
	withdrawn := nextOfKind(t, lane, EventHarnessDesignRevising)
	if withdrawn.ID != first.ID {
		t.Fatalf("the withdrawal is about design %d, not %d", withdrawn.ID, first.ID)
	}
	if !strings.Contains(withdrawn.Text, reviseAsk) {
		t.Fatalf("the withdrawal does not carry what was asked for: %q", withdrawn.Text)
	}

	// AND THE ROW GOES BACK TO "designing", which is what takes the approval row
	// out of the design's room while there is nothing to approve.
	waitForPhaseOnTheWire(t, phases, node.id, harnessPhaseDesigning)

	second := nextOfKind(t, lane, EventHarnessDesignDone)
	if second.Harness == nil || len(second.Harness.Program.Nodes) != 3 {
		t.Fatalf("the second card is not the rewritten page: %+v", second.Harness)
	}
	if second.ID != first.ID {
		t.Fatalf("the rewrite took a new id (%d, was %d) — one design is one number", second.ID, first.ID)
	}
	waitForPhase(t, node, HarnessPhaseAsking)

	// AND APPROVING NOW SAVES THE REWRITTEN PAGE, not the one that was replaced.
	agent.ResolveHarness(second.ID, true, "")
	if report := designOutcome(t, agent); !strings.Contains(report, "saved") {
		t.Fatalf("the approved rewrite did not save: %q", report)
	}
	if steps := savedSteps(t, agent, "flake-triage"); steps != 3 {
		t.Fatalf("the registry holds the page that was thrown away: %d steps", steps)
	}
}

// THE DESIGNER IS SHOWN THE PAGE IT IS CHANGING AND THE WORDS THAT ASKED, which
// is the difference between a rewrite and a second guess. The history is the
// story of what happened: the guide, the goal, the envelope it wrote, and then
// the person.
func TestARewriteIsGivenTheStandingPageAndThePersonsOwnWords(t *testing.T) {
	seen := make(chan []ai.Message, 8)
	watching := &scriptedCompleter{steps: repeatedly(func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		last := ""
		if len(messages) > 0 {
			last = messageContentText(messages[len(messages)-1])
		}
		switch {
		case strings.Contains(last, "Review it and reply with your findings"):
			return textResponse(reviewReply), nil
		case strings.Contains(last, reviseAsk):
			select {
			case seen <- messages:
			default:
			}
			return textResponse(revisedReply), nil
		case strings.Contains(last, "Design the sub-harness for it"):
			return textResponse(designReply), nil
		}
		return textResponse("nothing to add"), nil
	})}

	agent, _ := buildAgent(t, watching, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	first := designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)
	if text, isError := askForAChange(t, node, reviseAsk); isError {
		t.Fatalf("the change was refused: %s", text)
	}

	var history []ai.Message
	select {
	case history = <-seen:
	case <-time.After(harnessTestPatience):
		t.Fatal("the rewrite call never happened")
	}
	if len(history) != 4 {
		t.Fatalf("a rewrite's history is %d messages, want the guide, the goal, the page and the ask", len(history))
	}
	if history[0].Role != "system" || !strings.Contains(messageContentText(history[0]), "THE GOAL") && history[1].Role != "user" {
		t.Fatalf("a rewrite does not open with the guide and the goal: %s / %s", history[0].Role, history[1].Role)
	}
	if history[2].Role != "assistant" || !strings.Contains(messageContentText(history[2]), `"flake-triage"`) {
		t.Fatalf("the designer was not shown the page it is changing: %q", messageContentText(history[2]))
	}
	if !strings.Contains(messageContentText(history[3]), reviseAsk) {
		t.Fatalf("the person's own words did not reach the designer: %q", messageContentText(history[3]))
	}
	if !strings.Contains(messageContentText(history[3]), "WHOLE ENVELOPE") {
		t.Fatalf("the rewrite was not told it replaces the page rather than patching it: %q", messageContentText(history[3]))
	}

	second := nextOfKind(t, lane, EventHarnessDesignDone)
	if second.ID != first.ID {
		t.Fatalf("the rewrite took a new id (%d, was %d)", second.ID, first.ID)
	}
	agent.ResolveHarness(second.ID, false, "")
	designOutcome(t, agent)
}

// ── the clock ───────────────────────────────────────────────────────────────

// THE WRITING WINDOW IS TAKEN PER ROUND, and this is the test that tells that
// apart from one window over the whole job.
//
// The window here is a fifth of a second, and the person spends twice that
// reading the card before they say what they want changed. Under a job-wide
// clock the rewrite would open with a context that expired while they were
// reading and land as "the design ran out of time"; under a per-round one it
// gets the window whole, which is what a collaboration needs of it — somebody
// who reads a page, thinks about it, and comes back with two changes has done
// exactly what this room is for.
func TestEachRoundOfADesignGetsTheWritingWindowWhole(t *testing.T) {
	const window = 200 * time.Millisecond
	// THE COMPLETER HONOURS ITS CONTEXT, which a scripted one otherwise does not
	// and which is the whole of what this test measures: a design call handed a
	// context that has already expired is a call a real provider refuses, and a
	// fixture that answered it anyway would make any window look like it worked.
	clocked := &scriptedCompleter{steps: repeatedly(func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		last := ""
		if len(messages) > 0 {
			last = messageContentText(messages[len(messages)-1])
		}
		switch {
		case strings.Contains(last, "Review it and reply with your findings"):
			return textResponse(reviewReply), nil
		case strings.Contains(last, reviseAsk):
			return textResponse(revisedReply), nil
		case strings.Contains(last, "Design the sub-harness for it"):
			return textResponse(designReply), nil
		}
		return textResponse("nothing to add"), nil
	})}
	agent, _ := newTestAgent(t, clocked, func(config *Config) {
		buildConfig(config, t.TempDir())
		config.HarnessDesignWindow = window
	})
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)

	// Reading the card takes longer than the whole window, which is allowed: a
	// card is a question on a person's screen and carries no clock at all.
	time.Sleep(2 * window)

	if text, isError := askForAChange(t, node, reviseAsk); isError {
		t.Fatalf("the change was refused: %s", text)
	}
	second := nextOfKind(t, lane, EventHarnessDesignDone)
	if second.Harness == nil || len(second.Harness.Program.Nodes) != 3 {
		t.Fatalf("the rewrite did not get a window of its own: %+v", second.Harness)
	}
	agent.ResolveHarness(second.ID, true, "")
	if report := designOutcome(t, agent); !strings.Contains(report, "saved") {
		t.Fatalf("the rewrite did not land saved: %q", report)
	}
}

// THE THREAD REASONS FROM THE PAGE IN FRONT OF IT AND NEVER FROM THE ONE BEFORE.
// A transcript is append-only, so the replaced page is still in the context; it
// is retired in words, because that is the only way it can be retired
// ([harnessPageSuperseded]).
func TestARewrittenPageSupersedesTheOneItReplaced(t *testing.T) {
	agent, _ := buildAgent(t, revisingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)
	if text, isError := askForAChange(t, node, reviseAsk); isError {
		t.Fatalf("the change was refused: %s", text)
	}
	second := nextOfKind(t, lane, EventHarnessDesignDone)
	waitForPhase(t, node, HarnessPhaseAsking)

	child := node.openRoom().speaker()
	child.mu.Lock()
	var told []string
	for _, message := range child.messages {
		if message.Role == "system" {
			told = append(told, messageContentText(message))
		}
	}
	child.mu.Unlock()

	last := told[len(told)-1]
	if !strings.Contains(last, "REWRITTEN") || !strings.Contains(last, "superseded") {
		t.Fatalf("the replaced page was left standing beside the new one: %q", last)
	}
	if !strings.Contains(last, `"lint"`) {
		t.Fatalf("the page the thread now reasons from is not the rewritten one: %q", last)
	}

	// AND THE PERSON IS TOLD, IN THE ROOM, that the card in front of them is the
	// answer to what they asked for.
	if shown := threadText(t, node); !strings.Contains(shown, "written again, with your change in it") {
		t.Fatalf("the room does not say the page was rewritten: %q", shown)
	}

	agent.ResolveHarness(second.ID, false, "")
	designOutcome(t, agent)
}

// A REWRITE THE LAW WILL NOT ACCEPT LANDS THE NODE HONESTLY, and it says which
// of the two designs failed: there is a page the person has already read, so
// "the design failed" would be a sentence about the wrong thing. What both
// spellings agree on is that nothing was saved.
func TestARewriteThatCannotBeWrittenLandsSayingSo(t *testing.T) {
	dir := t.TempDir()
	failing := &scriptedCompleter{steps: repeatedly(func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		last := ""
		if len(messages) > 0 {
			last = messageContentText(messages[len(messages)-1])
		}
		switch {
		case strings.Contains(last, "Review it and reply with your findings"):
			return textResponse(reviewReply), nil
		case strings.Contains(last, reviseAsk) || strings.Contains(last, "REFUSED"):
			// Not an envelope, every attempt, all the way down the retry ladder.
			return textResponse("I would rather explain it in prose."), nil
		case strings.Contains(last, "Design the sub-harness for it"):
			return textResponse(designReply), nil
		}
		return textResponse("nothing to add"), nil
	})}
	agent, _ := buildAgent(t, failing, dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)
	if text, isError := askForAChange(t, node, reviseAsk); isError {
		t.Fatalf("the change was refused before it was tried: %s", text)
	}

	report := designOutcome(t, agent)
	if !strings.Contains(report, "rewrite failed") {
		t.Fatalf("a failed rewrite reported as something else: %q", report)
	}
	if !strings.Contains(report, "nothing was saved") {
		t.Fatalf("a failed rewrite did not say the registry is untouched: %q", report)
	}
	if _, err := agent.config.HarnessStore.Head("flake-triage"); err == nil {
		t.Fatal("a design nobody approved reached the registry")
	}
}

// ── the door is shut when there is nothing behind it ────────────────────────

// A CHANGE ASKED FOR WHEN NO CARD IS UP IS TOLD SO. The phase is the test: while
// the page is being written there is nothing in front of the person to change,
// and after the design lands there is nothing this door could act on.
func TestAskingForAChangeWithNoCardUpSaysSo(t *testing.T) {
	agent, _ := buildAgent(t, revisingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, HarnessPhaseAsking)
	child := node.openRoom().speaker()

	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)

	text, isError := runTool(t, child, "revise_design", mustJSON(t, map[string]string{"change": reviseAsk}))
	if !isError {
		t.Fatalf("a change to a design that has landed was accepted: %s", text)
	}
	if !strings.Contains(text, "not waiting on an answer") {
		t.Fatalf("the refusal does not say why: %q", text)
	}
}

// ── and the two old endings are still the two old endings ───────────────────

// APPROVING ON THE FIRST CARD STILL SAVES, unchanged by any of the above: a
// design nobody asked to change behaves exactly as it did before there was a
// loop around it.
func TestApprovingTheFirstCardStillSaves(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, revisingCompleter(), dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, true, "")
	if report := designOutcome(t, agent); !strings.Contains(report, "saved") {
		t.Fatalf("an approved first draft did not save: %q", report)
	}
	if steps := savedSteps(t, agent, "flake-triage"); steps != 2 {
		t.Fatalf("the registry holds something other than the approved draft: %d steps", steps)
	}
}

// AND DROPPING STILL DROPS, and still lands DONE rather than failed: the person
// was asked and they answered, which is the node's whole job.
func TestDroppingTheFirstCardStillDrops(t *testing.T) {
	agent, _ := buildAgent(t, revisingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, false, "")
	if report := designOutcome(t, agent); !strings.Contains(report, "not saved") {
		t.Fatalf("a declined design reported as something else: %q", report)
	}
	if _, err := agent.config.HarnessStore.Head("flake-triage"); err == nil {
		t.Fatal("a design nobody approved reached the registry")
	}
}

// waitForPhaseOnTheWire reads the task lane until one node ANNOUNCES a phase.
//
// It is the honest version of polling the graph: a phase a node passed through
// in microseconds is still a phase it published, and this sees it. What a
// surface reads is this lane, so what this asserts is what a surface would draw.
func waitForPhaseOnTheWire(t *testing.T, lane <-chan Event, id uint64, phase string) {
	t.Helper()
	deadline := time.After(harnessTestPatience)
	for {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatalf("the task lane closed before task %d said %q", id, phase)
			}
			if event.Task != nil && event.Task.ID == id && event.Task.Doing == phase {
				return
			}
		case <-deadline:
			t.Fatalf("task %d never announced %q", id, phase)
		}
	}
}

// nextOfKind reads past whatever else is on the standing lane to the next event
// of one kind, failing rather than hanging.
func nextOfKind(t *testing.T, lane <-chan Event, kind EventKind) Event {
	t.Helper()
	for {
		event := nextDesign(t, lane)
		if event.Kind == kind {
			return event
		}
		if event.Kind == EventNotice {
			t.Fatalf("the design ended in a note while waiting for %v: %q", kind, event.Text)
		}
	}
}
