package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// BUILDING A SUB-HARNESS, from the three sides a person meets it: the job that
// runs beside the conversation, the card it ends on, and the two answers to that
// card. WHO ASKS FOR ONE is tools_harness_test.go's half — a design is
// commissioned by the model's own hand now, and this file starts every design
// through exactly that hand.

// ── the card ────────────────────────────────────────────────────────────────

// THE DESIGN COMES BACK AS A PAGE, decoded, validated, and carried whole so
// that a surface draws the card everything else draws.
func TestADesignArrivesAsAValidHarness(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	if done.Harness == nil {
		t.Fatal("the design landed with no harness on it")
	}
	page := *done.Harness
	if page.Id.Name != "flake-triage" {
		t.Fatalf("the design is called %q", page.Id.Name)
	}
	if err := subharness.Validate(page); err != nil {
		t.Fatalf("a page that does not validate reached the card: %v", err)
	}
	if done.Text != page.Id.Name || done.Hint != page.Id.Desc {
		t.Fatalf("the card was named %q / %q", done.Text, done.Hint)
	}
	if done.ID == 0 {
		t.Fatal("the card carries no id, so nothing could answer it")
	}
	// NOTHING IS WRITTEN BEFORE THE ANSWER.
	if names, _ := agent.config.HarnessStore.Names(); len(names) != 0 {
		t.Fatalf("the registry already holds %v", names)
	}
	agent.ResolveHarness(done.ID, false, "")
}

// THE SECOND PASS IS A PATCH, and it lands on the page the card carries: the
// critic never re-emits the design, so what it changed and what it says it
// changed are the same object.
func TestTheReviewPassPatchesTheDraft(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"findings": [{"pass": "cost", "text": "three turns buys nothing here"}],
				"ops": [{"op": "set_field", "node": "look", "field": "max_turns", "text": "1"}],
				"calls": {"draft": 4, "revised": 2},
				"cues": ["flaky test", "triage the flake", "chase a flake", "flake"]}`), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	node, found := done.Harness.Program.Node("look")
	if !found {
		t.Fatal("the patched page lost the node the op named")
	}
	if turns := node.Fields.Get("max_turns"); turns != "1" {
		t.Fatalf("the op did not land: max_turns is %q", turns)
	}
	// THE CRITIC SAW THE DRAFT AND THE LAW IT WAS JUDGING AGAINST. A review turn
	// shown neither would be reviewing its recollection of both.
	review := completer.request(1)
	if len(review) != 2 {
		t.Fatalf("the review turn was %d messages", len(review))
	}
	if !strings.Contains(messageText(review[0]), "PART FOUR") {
		t.Fatal("the critic was not handed the reviewer's addendum")
	}
	if !strings.Contains(messageText(review[1]), "flake-triage") {
		t.Fatal("the critic was not shown the draft it is patching")
	}
	// And the cues it rewrote are the ones the entry is registered with.
	agent.ResolveHarness(done.ID, true, "")
	designOutcome(t, agent)
	entry, _ := entryNamed(agent.harnessRegistry(), "flake-triage")
	if len(entry.Cues) != 4 {
		t.Fatalf("the entry kept the draft's cues: %v", entry.Cues)
	}
}

// A YES WRITES THE PAGE AND MAKES IT REACHABLE FROM THE NEXT SENTENCE.
func TestAnApprovedDesignIsSavedAndDetectableAtOnce(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, designingCompleter(), dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, true, "")

	report := designOutcome(t, agent)
	if !strings.Contains(report, `"flake-triage" v1 saved`) {
		t.Fatalf("the design landed saying %q", report)
	}
	saved, err := subharness.At(dir).Load("flake-triage", 0)
	if err != nil {
		t.Fatalf("the registry has no page: %v", err)
	}
	if saved.Id.Version != 1 {
		t.Fatalf("the page landed as v%d", saved.Id.Version)
	}
	// THE CUES CAME OFF THE ENVELOPE, because the page has nowhere to put them —
	// and without them the harness would answer to its own name and nothing else.
	registry := agent.harnessRegistry()
	entry, found := entryNamed(registry, "flake-triage")
	if !found {
		t.Fatalf("the harness is not in what detection reads: %v", registry)
	}
	if len(entry.Cues) < 2 || entry.Revision != 1 || entry.Description != saved.Id.Desc {
		t.Fatalf("the entry is %+v", entry)
	}
	// And the model is told, so the next thing said in this conversation happens
	// after a harness was saved rather than before it.
	if queued := steeringText(agent); !strings.Contains(queued, "saved") {
		t.Fatalf("the transcript was never told; the queue says %q", queued)
	}
}

// AND THE NEXT SENTENCE CAN REACH IT. This is the whole point of building one
// in conversation: the harness a person just approved is offered by the turn
// after it, not by the next process.
func TestAHarnessBuiltHereIsOfferedByTheVeryNextTurn(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, true, "")
	designOutcome(t, agent)

	events, err := agent.Submit(context.Background(), "chase a flake in the render tests")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, false)
	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("the harness this session built was not offered: %v", kinds(collected))
	}
	if offer.Text != "flake-triage" {
		t.Fatalf("the offer named %q", offer.Text)
	}
}

// A NO CHANGES NOTHING, which is what makes the card free to answer.
func TestADeclinedDesignIsNotSaved(t *testing.T) {
	dir := t.TempDir()
	agent, _ := buildAgent(t, designingCompleter(), dir)
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, false, "")

	if report := designOutcome(t, agent); !strings.Contains(report, "not saved") {
		t.Fatalf("the decline landed saying %q", report)
	}
	if names, _ := subharness.At(dir).Names(); len(names) != 0 {
		t.Fatalf("a declined design was written: %v", names)
	}
	if entry, found := entryNamed(agent.harnessRegistry(), "flake-triage"); found {
		t.Fatalf("a declined design is detectable: %+v", entry)
	}
}

// A DESIGN THAT NEVER PARSED SAYS SO, AND ITS TASK FAILS. The person asked for a
// harness and silence is the one answer that leaves them wondering; what they
// get instead is a settle card with the reason on it.
func TestADesignThatWillNotParseFailsSayingWhy(t *testing.T) {
	// Every reply is prose, so salvage fails, the repair turn fails, and the two
	// retries after it fail the same way.
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	if started := nextDesign(t, lane); started.Kind != EventHarnessDesign {
		t.Fatalf("the lane opened with %v", started.Kind)
	}
	node := designNode(t, agent)
	report := designOutcome(t, agent)
	if !strings.HasPrefix(report, "the design failed:") || !strings.Contains(report, "attempts") {
		t.Fatalf("the failure said %q", report)
	}
	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("a design that never parsed settled as %q", state)
	}
}

// ── the design as a task ────────────────────────────────────────────────────

// A SESSION SAYS WHICH TASK IS WRITING A HARNESS, and it says it the way it says
// everything else about work in flight: a node on the roster, with a phase on it
// in the words a person would use.
func TestADesignRunsAsATaskWithAPhaseOnIt(t *testing.T) {
	// The design turn is held open, so the whole test happens while the page is
	// being written.
	held := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			<-held
			return textResponse(designReply), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	node := designNode(t, agent)
	if !strings.Contains(node.title(), buildGoal) {
		t.Fatalf("the design's row is called %q", node.title())
	}
	waitForPhase(t, node, harnessPhaseDesigning)
	if state := node.stateNow(); state != TaskRunning {
		t.Fatalf("a design being written is %q", state)
	}
	// AND IT IS ENTERABLE WHILE IT WRITES. The room is what a person walks into,
	// and the journal is where the thread is kept; both exist from before the
	// first model call so that arriving early finds a place rather than nothing.
	if journal := agent.TaskJournal(node.id); journal == "" {
		t.Fatal("the design has no journal, so its thread is nowhere")
	}
	if _, err := agent.WatchTask(node.id); err != nil {
		t.Fatalf("the design's room could not be entered: %v", err)
	}

	// THE CARD IS THE SECOND PHASE. The page exists, the question about it is on
	// screen, and a row still saying "designing" would be claiming work that is
	// over.
	close(held)
	done := designDone(t, lane)
	waitForPhase(t, node, harnessPhaseAsking)
	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// AND ITS NODE TAKES NO SLOT. The concurrency ceiling is about workers with
// checkouts and builds; a design is two calls and a card on somebody's screen,
// and one held behind a busy machine would be a page nobody can write because
// tasks are running.
func TestADesignDoesNotSpendAConcurrencySlot(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	agent.graph().limit = 1
	agent.graph().running = 1

	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// A PERSON CAN TALK TO THE DESIGN AND IS ANSWERED, which is what makes the node a
// thread rather than a receipt. The line goes in the room's own door, a turn runs
// for it, and what that turn says comes back out of the room — with the page in
// the context that wrote it.
//
// THE CARD IS UP WHILE THIS HAPPENS, which is the moment that matters: the design
// spends most of its life there, waiting on a person, and a thread that could only
// be talked to while the page was being written would be a thread nobody could
// reach.
func TestASteeredLineIsAnsweredInTheDesignRoom(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	done := designDone(t, lane)
	node := designNode(t, agent)
	waitForPhase(t, node, harnessPhaseAsking)

	// Subscribed BEFORE the line is sent: the room replays nothing, so a watcher
	// that joined afterwards would be watching for an answer already given.
	watching, err := agent.WatchTask(node.id)
	if err != nil {
		t.Fatalf("the design's room could not be entered: %v", err)
	}
	if err := agent.SteerTask(node.id, "would this work for the nightly build?"); err != nil {
		t.Fatalf("the design's room refused a line: %v", err)
	}
	waitForTurn(t, watching)

	// THE PAGE IS THE THREAD'S WORKING CONTEXT. It was recorded before anybody
	// could say anything, so whatever answered is answering with the harness in
	// front of it — and the person's own line is in the thread beside it.
	thread := threadText(t, node)
	if !strings.Contains(thread, "flake-triage") {
		t.Fatalf("the design thread does not hold the page: %q", thread)
	}
	if !strings.Contains(thread, "nightly build") {
		t.Fatalf("the person's line never reached the thread: %q", thread)
	}

	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// waitForTurn drains a room until the turn it is watching ends.
func waitForTurn(t *testing.T, watching <-chan Event) {
	t.Helper()
	for {
		select {
		case event, open := <-watching:
			if !open {
				t.Fatal("the design's room closed before the line was answered")
			}
			if event.Kind == EventTurnDone {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("nothing came back out of the design's room")
		}
	}
}

// AND STOPPING ONE IS STOPPING A TASK. `design:` names nothing now; the node's
// own id does, and what it settles saying is that the registry is untouched.
func TestStoppingADesignIsStoppingItsTask(t *testing.T) {
	held := make(chan struct{})
	defer close(held)
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-held:
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	submitBuild(t, agent)

	node := designNode(t, agent)
	waitForPhase(t, node, harnessPhaseDesigning)
	line, err := agent.Cancel("task:" + itoa64(node.id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "nothing was saved") {
		t.Fatalf("the stop promised %q, which is a branch a design never had", line)
	}
	if report := designOutcome(t, agent); report != designStoppedWord {
		t.Fatalf("the stopped design settled saying %q", report)
	}
	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("a stopped design settled as %q", state)
	}
}

// THE ANNOUNCE IS ALWAYS IN FRONT OF THE CARD, and it names the node the card
// belongs to. Admitting a design starts it at once — no dependencies, no slot to
// wait for — so a design this fast would put its page on the lane before the
// sentence explaining where it came from, and a person would be asked to save a
// harness nothing had said was being written.
func TestTheDesignAnnounceLeadsItsCard(t *testing.T) {
	agent, _ := buildAgent(t, designingCompleter(), t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)

	started := nextDesign(t, lane)
	if started.Kind != EventHarnessDesign {
		t.Fatalf("the lane opened with %v, before anything said a design had started", started.Kind)
	}
	if started.Task == nil || started.Task.ID == 0 {
		t.Fatal("the announce names no task, so there is nowhere for a person to go")
	}
	if node := designNode(t, agent); node.id != started.Task.ID {
		t.Fatalf("the announce named task %d and the design is task %d", started.Task.ID, node.id)
	}

	done := designDone(t, lane)
	agent.ResolveHarness(done.ID, false, "")
	designOutcome(t, agent)
}

// designNode is the one design node in this session's graph, waited for: the
// tool admits it and the frontier starts it on a goroutine, so a test reading
// the graph on the next line would be reading a race.
func designNode(t *testing.T, agent *Agent) *TaskNode {
	t.Helper()
	graph := agent.graph()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		graph.mu.Lock()
		var found *TaskNode
		for _, id := range graph.order {
			if node := graph.nodes[id]; node != nil && node.spec.design != nil {
				found = node
			}
		}
		graph.mu.Unlock()
		if found != nil {
			return found
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("this session admitted no design node")
	return nil
}

// waitForPhase waits for one node to publish a named phase.
func waitForPhase(t *testing.T, node *TaskNode, phase string) {
	t.Helper()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		node.graph.mu.Lock()
		doing := node.doing
		node.graph.mu.Unlock()
		if doing == phase {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	node.graph.mu.Lock()
	doing := node.doing
	node.graph.mu.Unlock()
	t.Fatalf("the design is %q, want %q", doing, phase)
}

// designOutcome waits for the design node to land and answers with the report
// its settle card carries.
func designOutcome(t *testing.T, agent *Agent) string {
	t.Helper()
	node := designNode(t, agent)
	select {
	case <-node.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the design never landed")
	}
	report, _, _, _ := node.leavings()
	return report
}

// threadText is everything written into the design's own thread so far, read
// off the agent standing in its room.
func threadText(t *testing.T, node *TaskNode) string {
	t.Helper()
	child := node.openRoom().speaker()
	if child == nil {
		t.Fatal("nobody is in the design's room")
	}
	var said []string
	for _, entry := range child.Transcript() {
		said = append(said, entry.Text)
	}
	return strings.Join(said, "\n")
}

// ── the fixtures ────────────────────────────────────────────────────────────

const buildGoal = "triaging flaky tests"

// designReply is one designer turn: the envelope the guide asks for, around a
// page this package will accept.
const designReply = `{
  "cues": ["flaky test", "triage the flake", "chase a flake"],
  "justification": "Two jobs: read the failure, then check the report names it.",
  "derivation": [
    {"a": "look", "b": "check", "rel": "depends", "why": "the check reads the report the loop wrote"}
  ],
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "look", "kind": "agent.loop", "fields": {"brief": "read the failing test and say what it does", "tools": "read", "max_turns": "3"}},
        {"id": "check", "kind": "verify", "fields": {"ladder": "accept", "check": "the report names the failing test"}}
      ],
      "edges": [["look", "check"]]
    },
    "whitelist": ["read"],
    "verify": {"ladder": "accept"},
    "dyn": {"ladder": "fixed"}
  }
}`

// reviewReply is a critic that found nothing worth patching, which is a real
// review outcome and the cheapest one.
const reviewReply = `{"findings": [], "ops": [], "calls": {"draft": 2, "revised": 2}}`

// designingCompleter answers the two turns of a whole design.
func designingCompleter() *scriptedCompleter {
	return &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
}

// buildConfig is the three seams a build needs, on a registry of its own.
func buildConfig(config *Config, dir string) {
	config.AskConsent = true
	config.HarnessStore = subharness.At(dir)
	config.TaskModels = func() []string { return testModels }
	config.RunHarness = func(context.Context, string, string, string) (string, error) { return "", nil }
}

func buildAgent(t *testing.T, completer Completer, dir string) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, completer, func(config *Config) { buildConfig(config, dir) })
}

// submitBuild starts a design THE WAY THE MODEL STARTS ONE: one call to the
// belt's build_harness tool, which returns the moment the job is in flight.
func submitBuild(t *testing.T, agent *Agent) {
	t.Helper()
	if text, isError := runTool(t, agent, "build_harness", `{"goal":"`+buildGoal+`"}`); isError {
		t.Fatalf("build_harness refused the call: %s", text)
	}
}

// nextDesign takes the next event off the standing lane, failing rather than
// hanging.
func nextDesign(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	select {
	case event, open := <-lane:
		if !open {
			t.Fatal("the design lane closed")
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("nothing arrived on the design lane")
		return Event{}
	}
}

// designDone reads past the "designing" line to the card.
func designDone(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	for {
		event := nextDesign(t, lane)
		switch event.Kind {
		case EventHarnessDesignDone:
			return event
		case EventNotice:
			t.Fatalf("the design ended in a note instead of a card: %q", event.Text)
		}
	}
}

func entryNamed(entries []subharness.Entry, name string) (subharness.Entry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return subharness.Entry{}, false
}

// steeringText is what the session has queued for the model to read at the top
// of the next turn.
func steeringText(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	var said []string
	for _, note := range agent.steering {
		said = append(said, note.text())
	}
	return strings.Join(said, "\n")
}
