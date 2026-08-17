package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ROUTE JUDGE, on the two questions it exists to get right: does it fire at
// all, and does the card it raises start the thing it named.
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

// routeRun is the adaptive runner as these tests hold it: what it was asked for,
// and how much of somebody's money it was handed.
type routeRun struct {
	mu    sync.Mutex
	goals []string
	caps  []float64
}

func (r *routeRun) start(_ context.Context, goal, _ string, capDollars float64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.goals = append(r.goals, goal)
	r.caps = append(r.caps, capDollars)
	return "1", nil
}

func (r *routeRun) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.goals...)
}

// routeAgent is a watched conversation with an adaptive runner behind it.
func routeAgent(t *testing.T, completer Completer) (*Agent, *routeRun) {
	t.Helper()
	runs := &routeRun{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.OrchestrateRunner = runs.start
	})
	return agent, runs
}

// drainAnsweringRoute drains one turn, answering every offer as it arrives.
func drainAnsweringRoute(t *testing.T, agent *Agent, events <-chan Event, run bool) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventHarnessOffer {
				agent.ResolveHarness(event.ID, run, "")
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

const routeYes = `{"work": true, "shape": "adaptive", "goal": "audit every package's pricing code and report what is wrong", "why": "research across every package"}`

// A WORDY TURN ABOUT REAL WORK: the judge is asked, one card is raised, and a
// yes reaches the same runner run_adaptive reaches — on the default tank,
// because nobody named one.
func TestTheJudgeOffersAfterAToolLessTurn(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, runs := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, true)

	if completer.asked() != 1 {
		t.Fatalf("the judge was asked %d times", completer.asked())
	}
	// It is asked about the PERSON'S words and about the shape of the answer, and
	// about nothing else.
	if question := completer.sawQuestion(); !strings.Contains(question, routeAsk) ||
		!strings.Contains(question, "Here is what I would look at.") {
		t.Fatalf("the judge was shown %q", question)
	}
	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("no card was raised; events: %v", kinds(collected))
	}
	if offer.Text != "adaptive run" || offer.Hint != "research across every package" {
		t.Fatalf("the card says %q / %q", offer.Text, offer.Hint)
	}
	started := runs.started()
	if len(started) != 1 || !strings.HasPrefix(started[0], "audit every package's pricing code") {
		t.Fatalf("the runner was handed %v", started)
	}
	runs.mu.Lock()
	cap := runs.caps[0]
	runs.mu.Unlock()
	if cap != orchestrateDefaultCap {
		t.Fatalf("the run opened on $%v, want the default tank", cap)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// A NO IS FREE: the card came down, nothing started, and the turn is the turn
// the person already had.
func TestANoOnTheCardStartsNothing(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, runs := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, false)
	if _, ok := firstOfKind(collected, EventHarnessOffer); !ok {
		t.Fatalf("no card was raised; events: %v", kinds(collected))
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("a no started %v", started)
	}
}

// A TRIVIAL TURN IS NOT WORTH A MODEL CALL. The judge is never asked, so the
// feature costs a conversation of short questions exactly nothing.
func TestTheJudgeIgnoresATrivialTurn(t *testing.T) {
	completer := &routeCompleter{answer: "Any time.", verdict: routeYes}
	agent, runs := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), "thanks, that helps")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, true)
	if completer.asked() != 0 {
		t.Fatalf("the judge was asked about a three-word turn")
	}
	if _, ok := firstOfKind(collected, EventHarnessOffer); ok {
		t.Fatalf("a card was raised on a trivial turn: %v", kinds(collected))
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("a trivial turn started %v", started)
	}
}

// A TURN THAT TOUCHED THE BELT IS NOT JUDGED. It was already work of some size,
// and asking whether work should have been work is a question with no useful
// answer.
func TestATurnWithToolCallsIsNeverJudged(t *testing.T) {
	completer := &scriptedCompleter{steps: oneCallThenAnswer("call-1", "read")}
	agent, runs := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, true)
	for index := 0; index < completer.requests(); index++ {
		if messages := completer.request(index); len(messages) > 0 && messageText(messages[0]) == routeJudgeBrief {
			t.Fatalf("the judge was asked about a turn that called tools")
		}
	}
	if _, ok := firstOfKind(collected, EventHarnessOffer); ok {
		t.Fatalf("a card was raised after a turn with tools: %v", kinds(collected))
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("a turn with tools started %v", started)
	}
}

// A REPLY THAT IS NOT JSON IS SILENCE. No card, no note, no error: the judge
// answered badly, and the person never asked it anything.
func TestAJudgeThatCannotAnswerIsSilent(t *testing.T) {
	completer := &routeCompleter{
		answer:  "Here is what I would look at.",
		verdict: "I think that probably should have been an adaptive run, yes.",
	}
	agent, runs := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, true)
	if completer.asked() != 1 {
		t.Fatalf("the judge was asked %d times", completer.asked())
	}
	for _, event := range collected {
		switch event.Kind {
		case EventHarnessOffer:
			t.Fatalf("prose raised a card")
		case EventError, EventNotice:
			t.Fatalf("a salvage failure said %q out loud", event.Text)
		}
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("prose started %v", started)
	}
}

// THE RATE LIMIT: one card, then three turns of quiet, whatever the judge says.
// It is also the whole of the "never twice in a row" rule — two consecutive
// turns can never both offer.
func TestTheOfferRateLimitHolds(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, _ := routeAgent(t, completer)

	offers := 0
	for turn := 1; turn <= 4; turn++ {
		events, err := agent.Submit(context.Background(), routeAsk)
		if err != nil {
			t.Fatalf("submit %d: %v", turn, err)
		}
		collected := drainAnsweringRoute(t, agent, events, false)
		if _, raised := firstOfKind(collected, EventHarnessOffer); raised {
			offers++
			if turn != 1 && turn != routeJudgeGap+1 {
				t.Fatalf("a card was raised on turn %d, inside the gap", turn)
			}
		}
	}
	if offers != 2 {
		t.Fatalf("%d cards over four turns, want one on turn 1 and one on turn %d", offers, routeJudgeGap+1)
	}
}

// A TASK-SHAPED YES ADMITS A NODE, through the graph's own admission rather
// than through a second proposal nobody answered.
func TestATaskShapedYesAdmitsANode(t *testing.T) {
	const verdict = `{"work": true, "shape": "task", "goal": "port the pricing tests to the new fixture", "why": "one self-contained sweep"}`
	completer := &routeCompleter{answer: "Here is what I would do.", verdict: verdict}
	agent, _ := routeAgent(t, completer)
	var ran []uint64
	var mu sync.Mutex
	stubbedGraph(agent, func(node *TaskNode) {
		mu.Lock()
		ran = append(ran, node.id)
		mu.Unlock()
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringRoute(t, agent, events, true)

	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("no card was raised; events: %v", kinds(collected))
	}
	if offer.Text != "task" {
		t.Fatalf("the card offered %q", offer.Text)
	}
	mu.Lock()
	started := len(ran)
	mu.Unlock()
	if started != 1 {
		t.Fatalf("%d nodes ran, want the one the person said yes to", started)
	}
	// The brief is the judge's goal, whole: whoever runs it cannot see this
	// conversation.
	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.spec.brief != "port the pricing tests to the new fixture" {
		t.Fatalf("the node's brief is %q", node.spec.brief)
	}
	if strings.TrimSpace(node.spec.acceptance) == "" {
		t.Fatal("a node was admitted with no acceptance at all")
	}
}

// AND THE GATES: no surface to answer a card is no judge at all, whatever the
// turn said. It is the same posture the harness offer keeps — a headless run
// must never pay for a question nobody will be shown.
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
}
