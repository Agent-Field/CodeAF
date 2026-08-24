package session

// DIVISION, AS TESTS: a worker that opens the material, finds the work is wider
// than one pair of hands, and hands the parts out.
//
// Everything here drives the real doors — the belt the worker is actually
// handed, the tool call it actually makes, the graph the conversation actually
// owns — because the whole of division is that there is NO second spawning
// road: a part is a node in the conversation's own graph, admitted the way
// task.go's nesting admits one (task_divide.go). What is scripted is only the
// part a test may not have, which is a worktree and a provider.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/splitgate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// divideNest is [nest] with the division road on: a conversation, a task the
// road was armed for, and the agent that IS that task.
type divideNest struct {
	session *Agent
	graph   *TaskGraph
	parent  *TaskNode
	node    *Agent
}

// newDivideNest builds that shape. brief is what the task was admitted with —
// which is also what arms it, since [Agent.armDivision] weighs the work's own
// text — and limit is the person's task.parallel cap, 0 for none.
func newDivideNest(t *testing.T, brief string, limit int) *divideNest {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
		config.TaskParallel = limit
	})
	graph := session.graph()
	graph.run = func(*TaskNode) {}

	id := graph.reserve()
	// THE PERSON'S OWN SENTENCE rides on the node, which is where a worker
	// inside it reads its own from ([Agent.taskRequest]) — a conversation's
	// personAsk is not what a node inherits.
	graph.admit(id, taskSpec{title: "the whole job", request: personSentence,
		brief: brief, acceptance: "a", depth: 1})
	parent := graph.node(id)

	node, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "test/model",
		System:    "SYSTEM",
		InTask:    true,
		Divide:    true,
		tasker:    graph,
		taskID:    id,
		taskDepth: 1,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
	parent.openRoom().speaking(node)
	return &divideNest{session: session, graph: graph, parent: parent, node: node}
}

// personSentence is what somebody typed to start all of this. A part three
// levels down still opens on it.
const personSentence = "please modernise the adapters"

// wideBrief is a piece of work whose own text names enough separate items to
// arm the road. It is written out rather than generated so the number a reader
// sees is the number the floor is compared against.
const wideBrief = "bring the adapters up to the new interface: 11 files, one each"

// wideEvidence is what a worker says it saw. Same shape, said from inside.
const wideEvidence = "the adapters directory holds 11 files, one interface each: alpha, beta, gamma, delta, epsilon, zeta, eta, theta, iota, kappa, lambda"

// narrowEvidence names too few items to pay for a division.
const narrowEvidence = "there are 3 bugs in the reconciler"

// divideArgs is one well-formed divide_work call with n parts.
func divideArgs(evidence string, n int) json.RawMessage {
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, fmt.Sprintf(
			`{"title":"part %d","summary":"s","brief":"b","acceptance":"a"}`, i+1))
	}
	return json.RawMessage(fmt.Sprintf(`{"evidence":%q,"parts":[%s]}`,
		evidence, strings.Join(parts, ",")))
}

// divide makes one division from inside the worker and returns what the model
// was told.
func (n *divideNest) divide(t *testing.T, args json.RawMessage) string {
	t.Helper()
	answer, _, err := n.node.divideWork(context.Background(), args)
	if err != nil {
		t.Fatalf("divide_work: %v", err)
	}
	return answer
}

// ── the road is armed, and only where something said the work might be wide ──

func TestNarrowWorkNeverCarriesTheDivisionVerb(t *testing.T) {
	// A brief nobody judged and whose own words name nothing worth dividing.
	// EVERY BYTE OF THIS WORKER'S RUN MUST BE WHAT IT WAS BEFORE THE ROAD
	// EXISTED, which is what "the gate refuses small work for free" means when
	// it is written down as a fact rather than as a promise.
	nest := newDivideNest(t, "fix the failing reconciler test", 0)
	if nest.parent.dividing() {
		t.Fatal("narrow work was armed to divide: the road is not free after all")
	}
	if beltHas(nest.node, "divide_work") {
		t.Fatal("a narrow task's worker carries divide_work, so its schema and its prompt are not what they were")
	}
	if strings.Contains(renderSystem(nest.node.config), "divide_work") {
		t.Fatal("a narrow task's worker is told about a verb it does not have")
	}
}

func TestWorkWhoseOwnWordsCountTheItemsIsArmedToDivide(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	if !nest.parent.dividing() {
		t.Fatalf("a brief naming %d items was not armed; the floor is %d",
			splitgate.Items(wideBrief), splitgate.Floor)
	}
	if !beltHas(nest.node, "divide_work") {
		t.Fatal("an armed task's worker has no divide_work on its belt")
	}
	if !strings.Contains(renderSystem(nest.node.config), "divide_work") {
		t.Fatal("an armed worker is given the verb and never told what it is for")
	}
}

func TestTheSizingJudgesYesArmsTheSingleWorkerItStarts(t *testing.T) {
	// THE POINT OF THE WAVE, and now the road a wide `/task <brief>` takes by
	// default: the judge's yes no longer offers anybody a planner, it starts one
	// worker and arms it, so the work is cut up by whoever has actually opened
	// the material.
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
	})
	ask := "go through the regional reports and bring each one up to date"
	if splitgate.WorthIt(ask) {
		t.Fatal("this ask arms itself, so it cannot show that the judge's answer is what armed it")
	}
	session.rememberDivisible(ask)

	graph := session.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the reports", request: ask, brief: "shaped words a model wrote", acceptance: "a", depth: 1})
	if !graph.node(id).dividing() {
		t.Fatal("the judge said this parallelizes and one worker was started on it: that worker must be able to divide")
	}
}

func TestTheRoadOffLeavesTheWorkerExactlyAsItWas(t *testing.T) {
	// AFORGE_SWARM=0 is the whole of the way out, and this is what it buys.
	nest := newDivideNest(t, wideBrief, 0)
	off := nest.node.config
	off.Divide = false
	if off.mayDivide() {
		t.Fatal("the road is off and the worker may still divide")
	}
	if strings.Contains(renderSystem(off), "divide_work") {
		t.Fatal("the road is off and the prompt still describes the verb")
	}
}

// ── the two gates ───────────────────────────────────────────────────────────

func TestADivisionTheEvidenceDoesNotSupportChangesNothingAtAll(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	before := len(nest.graph.order)
	// AND NOT ONE CALL IS MADE TO DECIDE. The whole bargain that lets this road
	// be on by default is that saying no is free, so the money is counted on
	// both sides of the question.
	spentBefore := nest.node.Usage()

	answer := nest.divide(t, divideArgs(narrowEvidence, 3))

	if spent := nest.node.Usage(); spent.Calls != spentBefore.Calls || spent.CostUSD != spentBefore.CostUSD {
		t.Fatalf("a refused division cost %d calls and $%v: deciding must be free",
			spent.Calls-spentBefore.Calls, spent.CostUSD-spentBefore.CostUSD)
	}

	if !strings.Contains(answer, "not split") {
		t.Fatalf("the worker was told %q, want a refusal", answer)
	}
	// THE REFUSAL IS PERSON-READABLE. It says the number back so the worker can
	// tell an under-count from genuinely narrow work, and it says what to do.
	if !strings.Contains(answer, "Carry on with the work in your own hands") {
		t.Fatalf("the refusal does not say what to do next: %q", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born from a refused division", len(kids))
	}
	if len(nest.graph.order) != before {
		t.Fatalf("the graph grew from %d to %d over a division nobody took", before, len(nest.graph.order))
	}
	// AND NO SLOT IS STILL HELD. A claim taken for a division that came to
	// nothing and never handed back would silently lower the fan cap for every
	// later division of this work.
	nest.graph.mu.Lock()
	held := nest.graph.claims[nest.parent.id]
	nest.graph.mu.Unlock()
	if held != 0 {
		t.Fatalf("%d slots are still held after a refused division", held)
	}
}

func TestADivisionNobodyIsFreeToPickUpIsDeferredRatherThanTaken(t *testing.T) {
	// THE STARVATION LAW: do not divide what nobody is free to pick up. One
	// lane, and the parent is standing in it.
	nest := newDivideNest(t, wideBrief, 1)
	nest.graph.mu.Lock()
	nest.graph.running = 1
	nest.graph.mu.Unlock()

	if free := nest.graph.freeHands(); free != 0 {
		t.Fatalf("with the only lane held there are %d free hands, want none", free)
	}
	answer := nest.divide(t, divideArgs(wideEvidence, 3))
	if !strings.Contains(answer, "no free hand") {
		t.Fatalf("the worker was told %q, want the wait named as a free hand", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born with nobody free to run them", len(kids))
	}

	// AND IT IS A DEFERRAL AND NOT A VERDICT. The same request goes through the
	// moment a hand comes free, which is why the refusal says to ask again.
	if !strings.Contains(answer, "ask again later") {
		t.Fatalf("the refusal does not invite the worker back: %q", answer)
	}
	nest.graph.mu.Lock()
	nest.graph.running = 0
	nest.graph.mu.Unlock()
	if answer := nest.divide(t, divideArgs(wideEvidence, 3)); !strings.Contains(answer, "split into 3 parts") {
		t.Fatalf("with a hand free the worker was told %q, want the division taken", answer)
	}
}

// ── the parts, once they exist ──────────────────────────────────────────────

func TestAForcedWideDivisionBearsPartsOnTheNestingRoad(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, divideArgs(wideEvidence, 3))

	// THE VOCABULARY LAW: what the worker reads is what a person reads over its
	// shoulder in the journal.
	if !strings.HasPrefix(answer, "split into 3 parts:") {
		t.Fatalf("the worker was told %q, want it said in parts", answer)
	}
	for _, banned := range []string{"gate", "quorum", "starvation", "armed", "starved"} {
		if strings.Contains(strings.ToLower(answer), banned) {
			t.Fatalf("the receipt says %q, which is machinery: %q", banned, answer)
		}
	}

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the division bore %d parts, want 3", len(kids))
	}
	for _, kid := range kids {
		if kid.parent != nest.parent.id {
			t.Fatalf("part %d hangs off %d, want the work it came out of (%d)", kid.id, kid.parent, nest.parent.id)
		}
		if kid.depth != 2 {
			t.Fatalf("part %d sits at depth %d, want one below its parent", kid.id, kid.depth)
		}
		if kid.owner != nest.node {
			t.Fatal("a part is run by the conversation, so its worktree would branch off the person's tree instead of its parent's")
		}
		// A part is a task like any other: it can be opened, watched and read.
		if kid.openRoom() == nil {
			t.Fatalf("part %d has no room to open", kid.id)
		}
	}
}

func TestThePersonsOwnWordsAndTheStandingOrdersReachEveryPart(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))

	for _, kid := range nest.graph.children(nest.parent.id) {
		if got := kid.request(); got != personSentence {
			t.Fatalf("part %d opens on %q, want the person's own sentence", kid.id, got)
		}
		if !strings.Contains(kid.instruction(), briefAskHeading) {
			t.Fatalf("part %d's brief has no heading saying whose words those are", kid.id)
		}
	}

	// THE STANDING ORDERS ARE THE FRONTIER'S, and a part is started by the same
	// frontier every other node is, so the same world reaches it: the assembled
	// brief is [TaskGraph.briefLocked]'s, orders last.
	orders := "The house rules: never touch vendor/."
	kids := nest.graph.children(nest.parent.id)
	nest.graph.mu.Lock()
	for _, kid := range kids {
		kid.brief = nest.graph.briefLocked(kid, orders)
	}
	nest.graph.mu.Unlock()
	for _, kid := range kids {
		if !strings.Contains(kid.assembledBrief(), orders) {
			t.Fatalf("part %d was started without the standing orders over this place", kid.id)
		}
	}
}

func TestStoppingTheWorkStopsEveryPartItSplitInto(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 3))
	kids := nest.graph.children(nest.parent.id)

	nest.graph.stopChildren(nest.parent.id)

	for _, kid := range kids {
		if !kid.stateNow().settled() {
			t.Fatalf("part %d is still %s after its parent was stopped", kid.id, kid.stateNow())
		}
	}
}

func TestWhatAPartSpendsIsFoldedIntoTheWorkItCameOutOf(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))
	kids := nest.graph.children(nest.parent.id)

	// THE FOLD GOES UP, and it goes up because a part is OWNED by the agent that
	// divided the work: [Agent.foldTaskUsage] charges the owner, so a part's
	// money lands on the parent's books and travels home with the parent's own
	// (task_run.go's one door for every fold there is).
	worker, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "S"}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the part's worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	cost := 0.25
	worker.addAuxiliaryUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens: 10, CompletionTokens: 20, Cost: &cost,
	}}, "test/model", 1)

	beforeParent := nest.node.Usage()
	nest.node.foldTaskUsage(kids[0], worker)

	if got := kids[0].spend(); got != 0.25 {
		t.Fatalf("the part shows %v spent, want what its worker cost", got)
	}
	if got := kids[1].spend(); got != 0 {
		t.Fatalf("a part that spent nothing shows %v; unknown must render as nothing", got)
	}
	if got := nest.node.Usage(); got.CostUSD <= beforeParent.CostUSD {
		t.Fatalf("the work that divided shows %v spent, unchanged from %v: a part's money never reached it",
			got.CostUSD, beforeParent.CostUSD)
	}
}

// ── the fan cap, met before anything is born ────────────────────────────────

func TestADivisionWiderThanTheCapIsRefusedWholeRatherThanHalfTaken(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	// Four parts already handed out leaves room for one, and a division of
	// three is not a division of one.
	for i := 0; i < taskFanLimit-1; i++ {
		if answer := nest.divide(t, divideArgs(wideEvidence, 2)); i == 0 && !strings.Contains(answer, "split into") {
			t.Fatalf("the first division was refused: %q", answer)
		}
	}
	before := nest.graph.children(nest.parent.id)
	answer := nest.divide(t, divideArgs(wideEvidence, 3))
	if !strings.Contains(answer, "as many as one task may") {
		t.Fatalf("the worker was told %q, want the cap said in the words it is said in everywhere else", answer)
	}
	if after := nest.graph.children(nest.parent.id); len(after) != len(before) {
		t.Fatalf("a refused division still bore %d parts: it was taken halfway", len(after)-len(before))
	}
}

func TestADivisionOfOneIsNotADivision(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, json.RawMessage(
		`{"evidence":"`+wideEvidence+`","parts":[{"title":"t","summary":"s","brief":"b","acceptance":"a"}]}`))
	if !strings.Contains(answer, "at least 2 parts") {
		t.Fatalf("the worker was told %q, want one part refused as no division", answer)
	}
}

func TestEveryPartNeedsSomethingToBeFinishedAgainst(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, json.RawMessage(
		`{"evidence":"`+wideEvidence+`","parts":[{"title":"t","summary":"s","brief":"b","acceptance":"a"},{"title":"u","summary":"s","brief":"b"}]}`))
	if !strings.Contains(answer, "part 2 has no acceptance") {
		t.Fatalf("the worker was told %q, want the part with no done-condition named", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born from a malformed division", len(kids))
	}
}

// ── the free-hand count itself ──────────────────────────────────────────────

func TestFreeHandsCountsTheLanesTheAskerIsNotStandingIn(t *testing.T) {
	graph := newTaskGraph()
	graph.limit = 3
	graph.running = 1
	if got := graph.freeHands(); got != 2 {
		t.Fatalf("with 1 of 3 lanes held there are %d free hands, want 2", got)
	}
	graph.running = 3
	if got := graph.freeHands(); got != 0 {
		t.Fatalf("with every lane held there are %d free hands, want none", got)
	}
	// NO CAP MEANS HANDS ENOUGH: there is no count to subtract from, so the
	// answer is the most parts one piece of work could ever ask for.
	graph.limit = 0
	if got := graph.freeHands(); got != taskFanLimit {
		t.Fatalf("with no cap there are %d free hands, want %d", got, taskFanLimit)
	}
	// A graph nobody built answers none rather than panicking: the tool asks
	// this of a session that may never have groomed a task.
	var none *TaskGraph
	if got := none.freeHands(); got != 0 {
		t.Fatalf("a session with no work answers %d free hands, want none", got)
	}
}

// ── the manual law, checked where the verb actually lives ───────────────────

func TestTheManualMentionsTheDivisionVerb(t *testing.T) {
	// The belt gate above this one builds a CONVERSATION's belt, which never
	// carries divide_work — so it would let this verb ship with no page: green,
	// and wrong in exactly the way that gate exists to catch.
	nest := newDivideNest(t, wideBrief, 0)
	found := false
	for _, tool := range nest.node.belt() {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
		if tool.Name == "divide_work" {
			found = true
		}
	}
	if !found {
		t.Fatal("this worker was built armed and has no divide_work: the test is checking the wrong belt")
	}
}

// ── THE MODEL'S OWN DOOR TAKES THE DIVISION ROAD ────────────────────────────
//
// The typed `/task` front door flipped first: a sizing yes there starts one
// armed worker and never a planner. These pin the other half of it. A chat
// model that has decided the work in front of it is broad says so on
// propose_task, and what that starts is ONE task with the road open — not a
// planner graph, and not three proposals cut up from the request.

func TestTheModelsOwnWideJudgementStartsOneArmedWorker(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
	})
	graph := stubbedGraph(session, func(*TaskNode) {})

	// A BRIEF THAT ARMS NOTHING BY ITSELF, deliberately: no count, no listing,
	// nothing the enumeration signal could read. If this node ends up armed, the
	// model's own word is the only thing that could have armed it.
	brief := "look into how the pricing pages read across the site and report what is inconsistent"
	if splitgate.WorthIt(brief) {
		t.Fatal("this brief arms itself, so it cannot show that the model's own judgement armed it")
	}
	answer, isError, err := session.proposeTask(context.Background(), json.RawMessage(fmt.Sprintf(
		`{"title":"the pricing pages","summary":"s","brief":%q,"deliverable":"d","acceptance":"a","wide":true}`,
		brief)))
	if err != nil {
		t.Fatalf("propose_task: %v", err)
	}
	if isError {
		t.Fatalf("the proposal was refused: %q", answer)
	}

	graph.mu.Lock()
	admitted := len(graph.nodes)
	graph.mu.Unlock()
	if admitted != 1 {
		t.Fatalf("%d nodes were admitted, want exactly one task", admitted)
	}
	node := graph.node(1)
	if node == nil {
		t.Fatalf("no node came out of the proposal: %q", answer)
	}
	if !node.dividing() {
		t.Fatal("the model said this work was wide and the worker it started cannot divide: the model's door is not on the division road")
	}
	// AND THE VERB IS ACTUALLY THERE. `dividing` is the decision; the belt is
	// the consequence, and a decision the worker's hands never hear about is
	// the road open on paper only.
	worker, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
		InTask: true, Divide: true, tasker: graph, taskID: node.id, taskDepth: 1,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if !beltHas(worker, "divide_work") {
		t.Fatal("the worker started for wide work has no divide_work on its belt")
	}
}

func TestAProposalThatNeverSaidItWasWideIsTheTaskItAlwaysWas(t *testing.T) {
	// The other side of the same law, and it is what keeps the road free: a
	// model that said nothing about width gets byte-identically the task it got
	// before `wide` existed.
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
	})
	graph := stubbedGraph(session, func(*TaskNode) {})
	answer, isError, err := session.proposeTask(context.Background(), json.RawMessage(
		`{"title":"the nil-map crash","summary":"s","brief":"fix the failing reconciler test","deliverable":"d","acceptance":"a"}`))
	if err != nil || isError {
		t.Fatalf("propose_task: %v %q", err, answer)
	}
	if node := graph.node(1); node == nil || node.dividing() {
		t.Fatal("a proposal that claimed no width was armed to divide: the road is not free after all")
	}
}

// WHAT THE MODEL IS TOLD, WHICH IS THE WHOLE OF WHICH ROAD IT TAKES. The
// deciding here is the model's — no regular expression watches the person's
// phrasing (tools_harness.go) — so the descriptions and the prompt ARE the
// default. A live session announced "a broad multi-source sweep, so I'm
// launching an adaptive research run" while every one of these read the other
// way round.
func TestTheBeltRoutesWideWorkToOneWorkerAndNotToAPlanner(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.OrchestrateRunner = neverRuns
		config.AskConsent = true
	})

	task, found := onBelt(agent, "propose_task")
	if !found {
		t.Fatal("the belt has no propose_task")
	}
	for _, want := range []string{"WIDE WORK", "`wide`", "do not reach for a planner"} {
		if !strings.Contains(task.Description, want) {
			t.Errorf("propose_task never says %q, so nothing tells the model wide work belongs here", want)
		}
	}
	if !strings.Contains(string(task.Schema), `"wide"`) {
		t.Errorf("propose_task's schema has no wide argument: %s", task.Schema)
	}

	run, found := onBelt(agent, "run_adaptive")
	if !found {
		t.Fatal("the belt has no run_adaptive")
	}
	// IT STAYS, AND IT STAYS EXPLICIT. The exception has to be reachable — a
	// person who asks for a planned graph gets one — and it has to say that it
	// is the exception, or width reaches for it again.
	for _, want := range []string{"THIS IS THE EXCEPTION", "merely WIDE", "propose_task"} {
		if !strings.Contains(run.Description, want) {
			t.Errorf("run_adaptive never says %q, so it still reads as the way to parallelize", want)
		}
	}

	// ONE SOURCE OF TRUTH: the prompt may not advertise the planner as the way
	// to parallelize while the belt says otherwise.
	if !strings.Contains(systemPrompt, "WIDE WORK") || !strings.Contains(systemPrompt, "with `wide`") {
		t.Error("prompts/system.md does not route wide work to propose_task")
	}
	if !strings.Contains(systemPrompt, "deliberate exception, not the way to") {
		t.Error("prompts/system.md does not name run_adaptive as the exception")
	}
	// And the sentence that produced the live reflex is gone rather than merely
	// argued with somewhere else on the page.
	for _, gone := range []string{
		"Independent parts that share one goal and one synthesis: ONE adaptive run",
		"the parallelism is already built",
	} {
		if strings.Contains(systemPrompt, gone) {
			t.Errorf("prompts/system.md still says %q", gone)
		}
	}
}
