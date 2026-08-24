package session

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/splitgate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ROUTE JUDGE, on the two questions it exists to get right: does it fire at
// all, and does a yes actually START the work rather than offering it.
//
// The completer here dispatches on the SYSTEM PROMPT rather than on request
// order, because a turn is not one call any more — the answer, the judge, and
// the session's own title call all arrive on the same client, and a script
// indexed by position would be asserting about whichever of them happened to be
// second.

const routeAsk = "audit the pricing code across every package and tell me what is wrong"

type routeCompleter struct {
	mu       sync.Mutex
	answer   string // what the assistant says on an ordinary turn
	verdict  string // what the judge answers when it is asked
	judged   int
	question string
}

func (c *routeCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system, asked := "", ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	if len(messages) > 1 {
		asked = messageText(messages[1])
	}
	if system == routeJudgeBrief {
		c.mu.Lock()
		c.judged++
		c.question = asked
		verdict := c.verdict
		c.mu.Unlock()
		return textResponse(verdict), nil
	}
	c.mu.Lock()
	answer := c.answer
	c.mu.Unlock()
	return textResponse(answer), nil
}

func (c *routeCompleter) asked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.judged
}

func (c *routeCompleter) sawQuestion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.question
}

// ordinaryRequests is how many of a scripted completer's calls were the
// CONVERSATION'S. The route judge rides the same client (route_judge.go), so a
// watched session that answers a substantial message in words alone makes one
// more call than its script has steps — and a test asserting "the turn was one
// request" means the turn, not the session's own bookkeeping about it.
func ordinaryRequests(completer *scriptedCompleter) int {
	ordinary := 0
	for index := 0; index < completer.requests(); index++ {
		messages := completer.request(index)
		if len(messages) > 0 && messageText(messages[0]) == routeJudgeBrief {
			continue
		}
		ordinary++
	}
	return ordinary
}

// routeRun is the adaptive runner as these tests hold it. IT EXISTS TO STAY
// EMPTY: there is one road out of the judge now, and a runner wired behind every
// one of these sessions is what makes "nothing reached the planner" an assertion
// rather than an assumption about how the agent happened to be built.
type routeRun struct {
	mu    sync.Mutex
	goals []string
}

func (r *routeRun) start(_ context.Context, goal, _ string, _ float64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.goals = append(r.goals, goal)
	return "1", nil
}

func (r *routeRun) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.goals...)
}

// routeAgent is a watched conversation with an adaptive runner behind it and a
// graph that runs its nodes instantly, which is every yes-shaped test's setup:
// the judge admits straight to the graph now, so an un-stubbed one would spin up
// a real worker on a scripted completer.
func routeAgent(t *testing.T, completer Completer) (*Agent, *routeRun, *ran) {
	t.Helper()
	runs := &routeRun{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.OrchestrateRunner = runs.start
	})
	nodes := &ran{}
	stubbedGraph(agent, func(node *TaskNode) {
		nodes.add(node.id)
	})
	return agent, runs, nodes
}

// ran is the set of nodes a stubbed graph actually started, read under a lock
// because the frontier turns on a goroutine of its own.
type ran struct {
	mu  sync.Mutex
	ids []uint64
}

func (r *ran) add(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *ran) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ids)
}

// noCard fails if anything on the stream asked the person a question. THE CARD
// IS GONE and its absence is the point of the wave: a surface that still drew
// one would mean somebody has to press a key before work that has already
// started.
func noCard(t *testing.T, collected []Event) {
	t.Helper()
	if _, ok := firstOfKind(collected, EventHarnessOffer); ok {
		t.Fatalf("a card was raised: %v", kinds(collected))
	}
	if _, ok := firstOfKind(collected, EventTaskProposal); ok {
		t.Fatalf("a proposal was raised: %v", kinds(collected))
	}
}

// routeNotice is the told-after line, or "" if the turn never said anything.
func routeNotice(collected []Event) string {
	for _, event := range collected {
		if event.Kind == EventNotice && strings.Contains(event.Text, "task ") {
			return event.Text
		}
	}
	return ""
}

const routeYes = `{"work": true, "goal": "audit every package's pricing code and report what is wrong", "why": "research across every package"}`

// A WORDY TURN ABOUT REAL WORK: the judge is asked, the task STARTS, and the
// person is told it started. Nobody was offered anything and nobody pressed a
// key.
func TestTheJudgeStartsWorkAfterAToolLessTurn(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, runs, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	if completer.asked() != 1 {
		t.Fatalf("the judge was asked %d times", completer.asked())
	}
	// It is asked about the PERSON'S words and about the shape of the answer, and
	// about nothing else.
	if question := completer.sawQuestion(); !strings.Contains(question, routeAsk) ||
		!strings.Contains(question, "Here is what I would look at.") {
		t.Fatalf("the judge was shown %q", question)
	}
	noCard(t, collected)
	// The admission STARTS the node on its own goroutine (TaskGraph.runFrontier),
	// so this is something another goroutine will do shortly — polled to a
	// deadline rather than read on the beat the stream closed, which is a race
	// the test loses whenever the machine is busy enough to schedule it late.
	waitFor(t, "the task the judge started to run", func() bool { return nodes.count() == 1 })

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	// The brief is the judge's goal, whole: whoever runs it cannot see this
	// conversation.
	if node.spec.brief != "audit every package's pricing code and report what is wrong" {
		t.Fatalf("the node's brief is %q", node.spec.brief)
	}
	if strings.TrimSpace(node.spec.acceptance) == "" {
		t.Fatal("a node was admitted with no acceptance at all")
	}
	// AND THEY ARE TOLD, in one line that says why work began that they did not
	// ask for.
	notice := routeNotice(collected)
	if !strings.Contains(notice, "this looked like work") || !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the turn said %q about the work it started", notice)
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("the judge reached a planner: %v", started)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// AN "ADAPTIVE" ANSWER IS NOT A SHAPE ANY MORE. The word came off the wire with
// the road it named, so a judge that still writes one is answering a question
// this brief does not ask: the field is dropped, the yes is still a yes, and the
// task it starts is the same task any other yes starts. Nothing reaches a
// planner, because from here nothing can.
func TestAnAdaptiveShapedVerdictIsNoLongerAShape(t *testing.T) {
	const verdict = `{"work": true, "shape": "adaptive", "goal": "audit every package's pricing code and report what is wrong", "why": "research across every package"}`
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: verdict}
	agent, runs, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	noCard(t, collected)
	waitFor(t, "the task an adaptive-shaped yes started", func() bool { return nodes.count() == 1 })
	if node := agent.graph().node(1); node == nil {
		t.Fatal("an adaptive-shaped yes admitted nothing")
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("the word \"adaptive\" still opened a planner: %v", started)
	}
	// And the brief the judge was given never taught it the word in the first
	// place — a field the code ignores is a field the prompt must not ask for.
	if strings.Contains(routeJudgeBrief, "adaptive") || strings.Contains(routeJudgeBrief, "shape") {
		t.Fatal("the judge's brief still teaches a shape nothing reads")
	}
}

// A TRIVIAL TURN IS NOT WORTH A MODEL CALL. The judge is never asked, so the
// feature costs a conversation of short questions exactly nothing.
func TestTheJudgeIgnoresATrivialTurn(t *testing.T) {
	completer := &routeCompleter{answer: "Any time.", verdict: routeYes}
	agent, _, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), "thanks, that helps")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if completer.asked() != 0 {
		t.Fatalf("the judge was asked about a three-word turn")
	}
	noCard(t, collected)
	if nodes.count() != 0 {
		t.Fatal("a trivial turn started work")
	}
	if agent.graph().node(1) != nil {
		t.Fatal("a trivial turn admitted a node")
	}
}

// A TURN THAT TOUCHED THE BELT IS NOT JUDGED. It was already work of some size,
// and asking whether work should have been work is a question with no useful
// answer.
func TestATurnWithToolCallsIsNeverJudged(t *testing.T) {
	completer := &scriptedCompleter{steps: oneCallThenAnswer("call-1", "read")}
	agent, _, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	for index := 0; index < completer.requests(); index++ {
		if messages := completer.request(index); len(messages) > 0 && messageText(messages[0]) == routeJudgeBrief {
			t.Fatalf("the judge was asked about a turn that called tools")
		}
	}
	noCard(t, collected)
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a turn with tools started work of its own")
	}
}

// A REPLY THAT IS NOT JSON IS SILENCE. No task, no note, no error: the judge
// answered badly, and the person never asked it anything.
func TestAJudgeThatCannotAnswerIsSilent(t *testing.T) {
	completer := &routeCompleter{
		answer:  "Here is what I would look at.",
		verdict: "I think that probably should have been a task, yes.",
	}
	agent, _, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if completer.asked() != 1 {
		t.Fatalf("the judge was asked %d times", completer.asked())
	}
	for _, event := range collected {
		switch event.Kind {
		case EventError, EventNotice:
			t.Fatalf("a salvage failure said %q out loud", event.Text)
		}
	}
	noCard(t, collected)
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("prose started work")
	}
}

// THE RATE LIMIT: one task, then three turns of quiet, whatever the judge says.
// It is also the whole of the "never twice in a row" rule — and auto-start is
// what makes it load-bearing rather than a courtesy, because there is no longer
// a keypress between a judge that likes every turn and a rail full of work.
func TestTheRateLimitHolds(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, _, _ := routeAgent(t, completer)

	starts := 0
	for turn := 1; turn <= 4; turn++ {
		events, err := agent.Submit(context.Background(), routeAsk)
		if err != nil {
			t.Fatalf("submit %d: %v", turn, err)
		}
		collected := collect(t, events)
		noCard(t, collected)
		if routeNotice(collected) != "" {
			starts++
			if turn != 1 && turn != routeJudgeGap+1 {
				t.Fatalf("work started on turn %d, inside the gap", turn)
			}
		}
	}
	if starts != 2 {
		t.Fatalf("%d tasks over four turns, want one on turn 1 and one on turn %d", starts, routeJudgeGap+1)
	}
}

// THE JUDGE'S OWN WIDE VERDICT ARMS THE TASK IT STARTS. This was the one
// model-decided door for wide work that admitted UNARMED: the judge is asked for
// a self-contained goal and never for a count, so the only signal reaching
// [Agent.armDivision] here was the text gate — which reads a number only beside
// one of eighteen item-nouns and therefore counts zero on almost every goal a
// judge writes. The verdict now carries the judgement it was already making, and
// `wide` is the only place breadth is said at all.
func TestTheJudgesWideVerdictArmsTheTaskItStarts(t *testing.T) {
	const verdict = `{"work": true, "wide": true, "goal": "research the pricing tiers of every major cloud provider and say where they differ", "why": "research across many sources"}`
	// THE GOAL ARMS NOTHING BY ITSELF, deliberately: if the node comes out armed,
	// the judge's own word is the only thing that could have armed it.
	const goal = "research the pricing tiers of every major cloud provider and say where they differ"
	if splitgate.WorthIt(goal) {
		t.Fatal("this goal arms itself, so it cannot show that the judge's verdict armed it")
	}

	completer := &routeCompleter{answer: "Here is what I would do.", verdict: verdict}
	agent, _, _ := routeAgent(t, completer)
	agent.config.Divide = true

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if !node.spec.wide {
		t.Fatal("the judge said the work was wide and the spec did not carry it")
	}
	if !node.dividing() {
		t.Fatal("the judge's wide yes did not arm the task it started")
	}
}

// AND A YES THAT SAID NOTHING ABOUT BREADTH ARMS NOTHING. The field is the
// judge's own reading and never a default: work that is one job however long it
// takes starts one worker with the belt it has always had.
func TestARouteYesWithoutWidthArmsNothing(t *testing.T) {
	const verdict = `{"work": true, "goal": "port the pricing tests to the new fixture", "why": "one self-contained sweep"}`
	completer := &routeCompleter{answer: "Here is what I would do.", verdict: verdict}
	agent, _, _ := routeAgent(t, completer)
	agent.config.Divide = true

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.spec.wide || node.dividing() {
		t.Fatal("a verdict that said nothing about breadth armed the task anyway")
	}
}

// AND THE GATES: nobody watching is no judge at all, whatever the turn said. It
// is the same posture the harness offer keeps — a headless run must never pay a
// model to start work nobody will see appear.
func TestAnUnwatchedSessionNeverJudges(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.OrchestrateRunner = (&routeRun{}).start
	})
	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)
	if completer.asked() != 0 {
		t.Fatalf("an unwatched session paid for %d judge calls", completer.asked())
	}
	if agent.graph().node(1) != nil {
		t.Fatal("an unwatched session started work anyway")
	}
}
