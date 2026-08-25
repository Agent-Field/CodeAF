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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/roles"
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
	// AN EMPTY SCRIPT IS A REVIEWER THAT CANNOT ANSWER, which is the fail-open
	// path and therefore the division exactly as the worker wrote it. That is
	// what every test in this file that says nothing about the review wants:
	// the road with its second opinion absent behaves as it did before there
	// was one (task_divide.go's header states the law).
	return newDivideNestOn(t, brief, limit, &scriptedCompleter{}, nil)
}

// newDivideNestOn is [newDivideNest] with the two things the review wave needs
// to vary: the model behind the WORKER — which is the model the division review
// is asked of — and the settings the roles ladder reads, which is what decides
// where a part graded `careful` is minted.
func newDivideNestOn(t *testing.T, brief string, limit int, worker Completer, source func(string) (string, bool)) *divideNest {
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
		Workspace:   t.TempDir(),
		Model:       "test/model",
		System:      "SYSTEM",
		InTask:      true,
		Divide:      true,
		RolesSource: source,
		tasker:      graph,
		taskID:      id,
		taskDepth:   1,
	}, worker)
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

// heldGovernor is a machine that is over its load ceiling and stays there. It
// reads a fixed sample rather than the host, so the answer does not depend on
// what else is running on the box the suite is on.
func heldGovernor() *admissionGovernor {
	return &admissionGovernor{
		maxLoad: 1.5,
		read:    func() (machineReading, bool) { return machineReading{loadPerCore: 4}, true },
		now:     time.Now,
	}
}

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

// divideGradedArgs is one well-formed call whose parts carry the grades named.
// An empty word writes NO grade field at all, which is the ordinary shape and
// the one that must read as mechanical.
func divideGradedArgs(evidence string, grades ...string) json.RawMessage {
	parts := make([]string, 0, len(grades))
	for i, grade := range grades {
		field := ""
		if grade != "" {
			field = fmt.Sprintf(`,"grade":%q`, grade)
		}
		parts = append(parts, fmt.Sprintf(
			`{"title":"part %d","summary":"s","brief":"b","acceptance":"a"%s}`, i+1, field))
	}
	return json.RawMessage(fmt.Sprintf(`{"evidence":%q,"parts":[%s]}`,
		evidence, strings.Join(parts, ",")))
}

// divideReviewer is the tier that thinks, standing behind one division. It
// dispatches on the review's own brief, so a worker turn that never made the
// call is visible as a count of zero rather than as an answer nobody asked for.
type divideReviewer struct {
	mu     sync.Mutex
	answer string
	fails  bool
	asked  int
	model  string
	shown  string
}

func (r *divideReviewer) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if len(messages) == 0 || messageText(messages[0]) != divideReviewBrief {
		return textResponse("(unscripted)"), nil
	}
	r.mu.Lock()
	r.asked++
	r.model = request.Model
	if len(messages) > 1 {
		r.shown = messageText(messages[1])
	}
	answer, fails := r.answer, r.fails
	r.mu.Unlock()
	if fails {
		return nil, errors.New("the reviewer could not be reached")
	}
	return textResponse(answer), nil
}

func (r *divideReviewer) reads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked
}

func (r *divideReviewer) rode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.model
}

func (r *divideReviewer) saw() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.shown
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

// ── THE ROAD, THROUGH THE CONSTRUCTOR THAT ACTUALLY BUILDS THE WORKERS ──────
//
// Every test above this line hands the worker a Config with `Divide: true`
// written into it by hand, which is exactly how a whole road stayed inert in
// production while eighteen tests passed over it: the person's setting was put
// on the CONVERSATION, and [Agent.newTaskAgent] — the one production constructor
// for every agent that can divide, called from the frontier, the repair rounds
// and the design door — copied about thirty fields from the parent and not that
// one. So `mayDivide` was false for every agent in the running program:
// divideTools returned nil, prompts/divide.md was left out, and the roster line,
// the schema and three manual pages promised a road nothing could take.
//
// THESE THREE GO THROUGH THE REAL DOOR AND NEVER AROUND IT. Nothing below
// writes a Config literal.

// workerFor builds one worker the way the running program builds it: admit a
// spec through the graph's own arming door, then ask the production constructor
// for the agent that IS the node.
func workerFor(t *testing.T, session *Agent, spec taskSpec) (*Agent, *TaskNode) {
	t.Helper()
	graph := session.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, spec)
	node := graph.node(id)
	worker, err := session.newTaskAgent(context.Background(), t.TempDir(), node, "")
	if err != nil {
		t.Fatalf("the production constructor refused to build a worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker, node
}

func TestTheProductionConstructorCarriesTheRoadOntoTheWorker(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
	})
	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", request: personSentence, brief: wideBrief, acceptance: "a", depth: 1,
	})

	if !node.dividing() {
		t.Fatalf("a brief naming %d items was not armed at admission; the floor is %d",
			splitgate.Items(wideBrief), splitgate.Floor)
	}
	if !worker.config.Divide {
		t.Fatal("the person's own yes did not travel from the conversation to the worker it built")
	}
	if !worker.mayDivide() {
		t.Fatal("the worker was armed and built by the real constructor and still may not divide")
	}
	if !beltHas(worker, "divide_work") {
		t.Fatal("a worker built by newTaskAgent for armed work has no divide_work on its belt")
	}
	// AND THE PAGE THAT SAYS WHAT THE VERB IS FOR. The belt and the prompt are
	// built from one predicate on purpose, and a verb with no page is a model
	// improvising a tool it was never taught.
	if !strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("a worker built armed is handed the verb and never told what it is for")
	}
}

// A HARNESS DESIGN IS A PAGE WRITER AND IS NEVER HANDED THE VERB. Its goal is
// free text, so a goal saying "a harness that checks the 8 endpoint files"
// enumerates enough items for the evidence signal — and a design thread has a
// tasker and a depth, so it satisfies every other condition. Without the kind
// guard, switching the road on would hand divide_work to the one node kind that
// has no worktree, no parts and nothing to hand out.
func TestAHarnessDesignIsNeverArmedToDivide(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = true
	})
	goal := "a harness that checks the 8 endpoint files and reports what each returned"
	if !splitgate.WorthIt(goal) {
		t.Fatal("this goal arms nothing by itself, so it cannot show that the kind guard is what refused it")
	}
	worker, node := workerFor(t, session, taskSpec{
		title: "a checker", brief: goal, acceptance: "a", depth: 1,
		design: &harnessDesignSpec{goal: goal},
	})

	if node.dividing() {
		t.Fatal("a harness design was armed for division by its own goal text")
	}
	if worker.mayDivide() {
		t.Fatal("a page writer may divide")
	}
	if beltHas(worker, "divide_work") {
		t.Fatal("a harness design's thread carries divide_work, which spawns workers in worktrees")
	}
	if strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("a harness design's thread is told about a verb it does not have")
	}
}

// THE WAY OUT STILL WORKS, THROUGH THE SAME DOOR. With the road off, the worker
// the constructor builds is byte-identical to the pre-division one however wide
// its brief reads.
func TestTheRoadOffProducesWorkersWithoutTheVerb(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Divide = false
	})
	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", request: personSentence, brief: wideBrief, acceptance: "a", depth: 1,
	})

	if node.dividing() {
		t.Fatal("the road is off and the node was armed anyway")
	}
	if worker.config.Divide || worker.mayDivide() {
		t.Fatal("the road is off and the worker it built may still divide")
	}
	if beltHas(worker, "divide_work") {
		t.Fatal("the road is off and the worker carries divide_work")
	}
	if strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("the road is off and the worker's prompt still describes the verb")
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
	if !strings.Contains(answer, "one task at a time") {
		t.Fatalf("the worker was told %q, want the person's own cap named", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts were born with nobody free to run them", len(kids))
	}

	// AND IT DOES NOT INVITE THE WORKER BACK TO A DOOR THAT NEVER OPENS. With
	// the cap at one there is no second pair of hands in this session and there
	// never will be — the asker's own lane is deliberately not counted — so
	// "ask again later" would be a sentence that is simply untrue.
	if strings.Contains(answer, "ask again") && !strings.Contains(answer, "asking again will not change this") {
		t.Fatalf("a permanent refusal invites the worker back: %q", answer)
	}
	nest.graph.mu.Lock()
	nest.graph.running = 0
	nest.graph.mu.Unlock()
	if answer := nest.divide(t, divideArgs(wideEvidence, 3)); !strings.Contains(answer, "split into 3 parts") {
		t.Fatalf("with a hand free the worker was told %q, want the division taken", answer)
	}
}

// A CAP ABOVE ONE IS A WAIT AND SAYS SO. The lanes are all busy now and one
// frees the moment something finishes, so the honest thing to tell the worker is
// to come back — which is the opposite of what the one-lane case says, and the
// two must not be one sentence.
func TestADivisionHeldByBusyLanesInvitesTheWorkerBack(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 2)
	nest.graph.mu.Lock()
	nest.graph.running = 2
	nest.graph.mu.Unlock()

	answer := nest.divide(t, divideArgs(wideEvidence, 3))
	if !strings.Contains(answer, "every lane is busy") {
		t.Fatalf("the worker was told %q, want the busy lanes named", answer)
	}
	if !strings.Contains(answer, "ask again once something finishes") {
		t.Fatalf("a refusal that can lift does not invite the worker back: %q", answer)
	}
	if strings.Contains(answer, "one task at a time") {
		t.Fatalf("a session with two lanes was told it runs one task at a time: %q", answer)
	}
}

// THE MACHINE'S OWN HOLD IS NOT A REFUSAL, and this is the asymmetry the audit
// found: a loaded box made the DEFAULT road for wide work fail closed while the
// exception — work admitted through propose_task — merely queued, announced
// `machine busy`, and lifted itself five seconds later. The division takes the
// same road now: the parts are admitted, the frontier holds them, and the
// receipt says so instead of sending the worker away to remember to re-ask.
func TestABusyMachineHoldsTheDivisionsPartsRatherThanRefusingIt(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	// A governor that is holding, with no lane cap at all: every lane in this
	// session is empty, which is exactly the state the old refusal described as
	// "no free hand".
	nest.graph.governor = heldGovernor()

	if free := nest.graph.freeHands(); free <= 0 {
		t.Fatalf("a busy machine took the lanes away: %d free hands with no cap set", free)
	}
	answer := nest.divide(t, divideArgs(wideEvidence, 3))
	if !strings.Contains(answer, "split into 3 parts") {
		t.Fatalf("a busy machine refused the division: %q", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 3 {
		t.Fatalf("%d parts were born, want 3 waiting on the machine", len(kids))
	}
	// AND THE RECEIPT SAYS THEY ARE WAITING, in a person's words, without naming
	// the machinery that decided or asking the worker to come back.
	if !strings.Contains(answer, "machine is busy") {
		t.Fatalf("the receipt does not say the parts are waiting: %q", answer)
	}
	if !strings.Contains(answer, "start themselves") {
		t.Fatalf("the receipt does not say the wait lifts by itself: %q", answer)
	}
	for _, banned := range []string{"governor", "gate", "quorum", "armed", "ask again"} {
		if strings.Contains(strings.ToLower(answer), banned) {
			t.Fatalf("the receipt says %q, which is machinery or a false invitation: %q", banned, answer)
		}
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

// AND IT STILL REACHES THE SESSION WHEN THE PARENT WAS STOPPED FIRST. On a stop,
// threshold or deadline ending the parent's own agent is closed and folded
// before [TaskGraph.stopChildren] cuts the parts, so every part folding
// afterwards was folding into books nobody was ever going to read again: the
// money sat on the part's row, visible and uncounted, and the session ledger was
// short by a whole part on exactly the path somebody takes when they are worried
// about spending. A closed hop is skipped, not paid into.
func TestAStoppedPartsSpendStillReachesTheSessionLedger(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	nest.divide(t, divideArgs(wideEvidence, 2))
	kids := nest.graph.children(nest.parent.id)

	worker, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "S"}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the part's worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	cost := 0.25
	worker.addAuxiliaryUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens: 10, CompletionTokens: 20, Cost: &cost,
	}}, "test/model", 1)

	// The parent has finished and closed its books, which is the whole of the
	// ordering: the part is only cut down after this has happened.
	_ = nest.node.Close()

	before := nest.session.Usage()
	nest.node.foldTaskUsage(kids[0], worker)

	if got := kids[0].spend(); got != 0.25 {
		t.Fatalf("the part shows %v spent, want what its worker cost", got)
	}
	if got := nest.session.Usage(); got.CostUSD-before.CostUSD != 0.25 {
		t.Fatalf("the session ledger moved by %v over a stopped part that cost $0.25: one ledger means every road folds into it",
			got.CostUSD-before.CostUSD)
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

	// AND THERE IS NOTHING ELSE ON THE BELT TO REACH FOR. `run_adaptive` used to
	// sit beside propose_task carrying a paragraph about being the exception, and
	// a paragraph is a weaker instrument than an absence: the verb is off the belt
	// now (tools_harness.go), so width has nowhere else to go.
	if _, found := onBelt(agent, "run_adaptive"); found {
		t.Error("run_adaptive is back on the belt, so wide work has a planner to reach for again")
	}

	// ONE SOURCE OF TRUTH: the prompt may not advertise the planner as the way
	// to parallelize while the belt says otherwise.
	if !strings.Contains(systemPrompt, "WIDE WORK") || !strings.Contains(systemPrompt, "with `wide`") {
		t.Error("prompts/system.md does not route wide work to propose_task")
	}
	// AND THE PROMPT SAYS THE ABSENCE OUTRIGHT. The page used to argue that the
	// planner was the exception, which is a sentence that only makes sense while
	// the verb is there to be excepted; the verb is gone, so what the prompt owes
	// the model is the plain fact plus the road that replaced it.
	if !strings.Contains(systemPrompt, "THERE IS NO PLANNER ON YOUR BELT") {
		t.Error("prompts/system.md does not tell the model it has no planner")
	}
	if !strings.Contains(systemPrompt, "Wide\nwork is one task that hands its own parts out once the material shows the width\nis real") {
		t.Error("prompts/system.md does not name the road that replaced the planner")
	}
	// And the sentences that produced the live reflex are gone rather than merely
	// argued with somewhere else on the page — the verb itself included, because a
	// prompt that still spells it is a prompt promising a hand the belt withheld.
	for _, gone := range []string{
		"Independent parts that share one goal and one synthesis: ONE adaptive run",
		"the parallelism is already built",
		"run_adaptive",
	} {
		if strings.Contains(systemPrompt, gone) {
			t.Errorf("prompts/system.md still says %q", gone)
		}
	}
}

// ── THE PLAN IS READ ONCE BY THE TIER THAT THINKS ───────────────────────────
//
// The two gates measure whether a division is WORTH it, and neither of them
// reads the parts. But a part's brief is that worker's whole world, and it was
// written by whatever model the parent task runs on — on a cheap crew, the cheap
// one. So a division that has passed both gates is put once to the mastermind
// tier, which reads the evidence, the parent's brief and every part together and
// answers with them approved, amended, merged, or refused.

// the model the high tier is set to in these tests, which is where a careful
// part is minted and nowhere else.
const carefulModelID = "test/careful-model"

// THE TWO ROLES ARE THE DECISION, so the tiers are asserted rather than assumed.
// A review that resolved low would be a cheap model reviewing a cheap model's
// plan, and a careful part minted on the mastermind tier would spend a thinking
// model's price on many turns of ordinary work.
func TestTheDivisionsTwoRolesSitWhereTheyWereReasonedTo(t *testing.T) {
	for role, want := range map[roles.Role]roles.Tier{
		roles.RoleDivision: roles.TierMastermind,
		roles.RoleCareful:  roles.TierHigh,
	} {
		tier, ok := roles.TierOf(role)
		if !ok {
			t.Fatalf("%q is not a registered role, so it resolves to nothing", role)
		}
		if tier != want {
			t.Errorf("%q resolves on %q, want %q", role, tier, want)
		}
	}
}

// WHAT THE REVIEWER WRITES IS WHAT THE CHILDREN ACTUALLY GET. Three parts go in,
// the reviewer merges them into two and sharpens both briefs, and the two
// workers that exist afterwards are working from the reviewer's words — not from
// the ones the dividing worker wrote.
func TestTheReviewedBriefsAreWhatThePartsAreActuallyGiven(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"parts":[` +
		`{"title":"the eleven adapters","summary":"s","brief":"SHARPENED ONE — and do not touch the fixtures","acceptance":"the adapters compile"},` +
		`{"title":"the fixtures","summary":"s","brief":"SHARPENED TWO","acceptance":"the fixtures compile"}]}`}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer, nil)

	answer := nest.divide(t, divideArgs(wideEvidence, 3))

	if reviewer.reads() != 1 {
		t.Fatalf("the plan was read %d times, want once and no repair turn", reviewer.reads())
	}
	// IT SEES THE WHOLE DIVISION: what the worker saw, the work it came out of,
	// and every part.
	for _, want := range []string{wideEvidence, personSentence, "part 1", "part 3", "grade: " + gradeMechanical} {
		if !strings.Contains(reviewer.saw(), want) {
			t.Errorf("the reviewer was never shown %q", want)
		}
	}
	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division the reviewer settled", answer)
	}
	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 2 {
		t.Fatalf("the merged division bore %d parts, want 2", len(kids))
	}
	for i, want := range []string{"SHARPENED ONE", "SHARPENED TWO"} {
		if !strings.Contains(kids[i].instruction(), want) {
			t.Fatalf("part %d works from %q, want the reviewer's brief", kids[i].id, kids[i].instruction())
		}
	}
	if got := kids[0].title(); got != "the eleven adapters" {
		t.Fatalf("part 1 is called %q, want the reviewer's name for it", got)
	}
}

// A REFUSAL ADMITS NOTHING AND THE WORKER CARRIES ON SOLO — the gates' own
// ending, which is the one thing about it that must not be new. What is its own
// is the SENTENCE: the other two refusals explain themselves in terms of a count
// and a free lane, and either of those said over a plan refused for overlapping
// scopes would send the worker back to fix a number that was never the problem.
func TestAReviewThatRefusesLeavesTheWorkerWhereItWas(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"refuse": true, "why": "parts 2 and 3 are the same file"}`}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer, nil)

	answer := nest.divide(t, divideArgs(wideEvidence, 3))

	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("a refused plan still bore %d parts", len(kids))
	}
	if !strings.HasPrefix(answer, "not split:") {
		t.Fatalf("the worker was told %q, want the same refusal shape the gates use", answer)
	}
	if !strings.Contains(answer, "parts 2 and 3 are the same file") {
		t.Fatalf("the refusal says %q and never what the worker could act on", answer)
	}
	if !strings.Contains(answer, "Carry on with the work in your own hands") {
		t.Fatalf("the refusal says %q, want the gates' own ending", answer)
	}
	// THE VOCABULARY LAW: the worker reads this and so does a person, over its
	// shoulder, in the journal.
	for _, banned := range []string{"gate", "review", "mastermind", "verdict", "refused by"} {
		if strings.Contains(strings.ToLower(answer), banned) {
			t.Fatalf("the refusal says %q, which is machinery: %q", banned, answer)
		}
	}
}

// FAIL OPEN. A reviewer that cannot be reached, or that answers something the
// contract does not allow, admits the ORIGINAL parts unchanged: the division has
// already earned its way past two gates that were measured, and a flaky second
// opinion must not be able to turn the road off.
func TestAReviewerThatCannotAnswerAdmitsTheOriginalParts(t *testing.T) {
	for _, test := range []struct {
		name     string
		reviewer *divideReviewer
	}{
		{"the call fails outright", &divideReviewer{fails: true}},
		{"the answer is prose", &divideReviewer{answer: "This division looks sensible to me."}},
		{"the answer is one part, which is the refusal it was told to spell out",
			&divideReviewer{answer: `{"parts":[{"title":"all of it","summary":"s","brief":"b","acceptance":"a"}]}`}},
		{"a part comes back with a field missing",
			&divideReviewer{answer: `{"parts":[{"title":"one","summary":"s","brief":"b","acceptance":"a"},{"title":"two","summary":"s","brief":"","acceptance":"a"}]}`}},
		{"more parts come back than one piece of work may be split into",
			&divideReviewer{answer: `{"parts":[` + strings.TrimSuffix(strings.Repeat(
				`{"title":"x","summary":"s","brief":"b","acceptance":"a"},`, taskFanLimit+1), ",") + `]}`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			nest := newDivideNestOn(t, wideBrief, 0, test.reviewer, nil)
			answer := nest.divide(t, divideArgs(wideEvidence, 3))
			if !strings.HasPrefix(answer, "split into 3 parts:") {
				t.Fatalf("the worker was told %q, want the division it wrote", answer)
			}
			if kids := nest.graph.children(nest.parent.id); len(kids) != 3 {
				t.Fatalf("the division bore %d parts, want the 3 the worker wrote", len(kids))
			}
		})
	}
}

// AND IT IS NEVER PAID FOR BY A DIVISION THE GATES REFUSE. The review is the one
// step on this road that costs money, so it stands AFTER both free gates: a
// worker whose work is not wide, or whose session has no free hand, never
// reaches it.
func TestTheGatesRefuseADivisionBeforeAnybodyPaysToReadIt(t *testing.T) {
	for _, test := range []struct {
		name     string
		limit    int
		evidence string
	}{
		{"the evidence names too few items", 0, narrowEvidence},
		{"nobody is free to pick the parts up", 1, wideEvidence},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewer := &divideReviewer{answer: `{"refuse": true}`}
			nest := newDivideNestOn(t, wideBrief, test.limit, reviewer, nil)
			if answer := nest.divide(t, divideArgs(test.evidence, 3)); !strings.HasPrefix(answer, "not split:") {
				t.Fatalf("the worker was told %q", answer)
			}
			if reviewer.reads() != 0 {
				t.Fatalf("a division the gates refused was read %d times", reviewer.reads())
			}
		})
	}
}

// ── A PART'S GRADE IS WHICH MODEL IT RUNS ON ────────────────────────────────

// THE GRADE DECIDES THE TIER AND NOTHING ELSE ABOUT THE PART. A careful part is
// minted on the high tier, a mechanical one keeps the parent task's model, and a
// part that said nothing is mechanical — which is what makes the field free.
func TestACarefulPartIsMintedOnTheHighTierAndAMechanicalOneIsNot(t *testing.T) {
	nest := newDivideNestOn(t, wideBrief, 0, &scriptedCompleter{},
		tierSettings(map[string]string{roles.TierKey(roles.TierHigh): carefulModelID}))

	nest.divide(t, divideGradedArgs(wideEvidence, gradeCareful, "", gradeMechanical))

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the division bore %d parts, want 3", len(kids))
	}
	if got := kids[0].spec.model; got != carefulModelID {
		t.Fatalf("the careful part runs on %q, want the high tier's model", got)
	}
	for _, kid := range kids[1:] {
		if got := kid.spec.model; got != "test/model" {
			t.Fatalf("part %d runs on %q, want the model its parent task runs on", kid.id, got)
		}
	}
}

// AND WITH NO TIERS CONFIGURED THE FIELD COSTS NOTHING. The ladder floors on the
// model the work is already on (internal/roles), so an install nobody has opened
// the settings sheet in mints a careful part exactly where a mechanical one goes
// — the grade is a word on the wire and no model id is written anywhere.
func TestACarefulPartOnAnInstallWithNoTiersRunsWhereItsParentDoes(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)

	nest.divide(t, divideGradedArgs(wideEvidence, gradeCareful, gradeCareful))

	for _, kid := range nest.graph.children(nest.parent.id) {
		if got := kid.spec.model; got != "test/model" {
			t.Fatalf("part %d runs on %q, want the model its parent task runs on", kid.id, got)
		}
	}
}

// A WORD NOBODY TAUGHT THE MODEL IS MECHANICAL, and a whole division is never
// refused over one. The cheap answer is the safe one to be wrong with.
func TestAnUnknownGradeIsOrdinaryWork(t *testing.T) {
	nest := newDivideNestOn(t, wideBrief, 0, &scriptedCompleter{},
		tierSettings(map[string]string{roles.TierKey(roles.TierHigh): carefulModelID}))

	answer := nest.divide(t, divideGradedArgs(wideEvidence, "URGENT", "Careful"))

	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want a division a strange word did not refuse", answer)
	}
	kids := nest.graph.children(nest.parent.id)
	if got := kids[0].spec.model; got != "test/model" {
		t.Fatalf("a part graded with a word nobody taught runs on %q, want its parent's model", got)
	}
	// AND THE WORD IS READ THE WAY A MODEL WOULD WRITE IT. "Careful" is the
	// grade, capitalised by a model that was writing a sentence.
	if got := kids[1].spec.model; got != carefulModelID {
		t.Fatalf("a part graded \"Careful\" runs on %q, want the high tier's model", got)
	}
}

// THE REVIEWER HOLDS THE GRADE TOO, because it is the one reader that can tell
// which of these parts actually needs thinking — it has the whole division in
// front of it and the worker had only the material.
func TestTheReviewerMayPromoteAPartToCareful(t *testing.T) {
	reviewer := &divideReviewer{answer: `{"parts":[` +
		`{"title":"the tricky one","summary":"s","brief":"b","acceptance":"a","grade":"` + gradeCareful + `"},` +
		`{"title":"the rest","summary":"s","brief":"b","acceptance":"a"}]}`}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer,
		tierSettings(map[string]string{roles.TierKey(roles.TierHigh): carefulModelID}))

	nest.divide(t, divideArgs(wideEvidence, 2))

	kids := nest.graph.children(nest.parent.id)
	if got := kids[0].spec.model; got != carefulModelID {
		t.Fatalf("the part the reviewer graded careful runs on %q, want the high tier's model", got)
	}
	if got := kids[1].spec.model; got != "test/model" {
		t.Fatalf("the part the reviewer left alone runs on %q, want its parent's model", got)
	}
}

// AND THE FIELD IS ON THE WIRE. A grade the schema does not carry is a grade no
// model can write, and everything above it would be machinery nothing reaches.
func TestTheDivisionSchemaCarriesTheGrade(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	tool, found := onBelt(nest.node, "divide_work")
	if !found {
		t.Fatal("this worker was built armed and has no divide_work")
	}
	schema := string(tool.Schema)
	for _, want := range []string{`"grade"`, gradeMechanical, gradeCareful} {
		if !strings.Contains(schema, want) {
			t.Errorf("divide_work's schema never says %q: %s", want, schema)
		}
	}
	// IT IS THE ONE FIELD A PART MAY LEAVE OUT, and the required list is where
	// that is actually said.
	if strings.Contains(schema, `"required":["title","summary","brief","acceptance","grade"]`) {
		t.Error("grade is required, so a part cannot simply be ordinary work")
	}
	// AND THE WORKER IS TOLD WHAT IT IS FOR where it reads about the verb at all.
	if !strings.Contains(tool.Description, "grade") || !strings.Contains(tool.Description, gradeCareful) {
		t.Errorf("divide_work never tells the worker about the grade: %s", tool.Description)
	}
	// ONE SOURCE OF TRUTH: the page the worker is given teaches the same field.
	if !strings.Contains(renderSystem(nest.node.config), gradeCareful) {
		t.Error("prompts/divide.md does not teach the grade the schema asks for")
	}
}
