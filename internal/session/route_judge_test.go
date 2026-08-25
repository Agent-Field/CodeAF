package session

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
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

// THE TWO MODELS OF THE CASCADE, and the reason these tests configure tiers at
// all. The screen and the confirm are asked the SAME BRIEF in the same words
// (route_judge.go: a second reader handed the first reader's verdict is a
// reader agreeing with it), so the system prompt cannot tell them apart and the
// only honest thing that can is the model each one actually rode. Setting the
// two tiers here is therefore both the fixture and one of the assertions: a
// confirm that resolved anywhere but the mastermind's model never reaches
// routeConfirmModel and every both-yes test below fails.
const (
	routeScreenModel  = "test/cheap-router"
	routeConfirmModel = "test/thinking-router"
)

type routeCompleter struct {
	mu      sync.Mutex
	answer  string // what the assistant says on an ordinary turn
	verdict string // what the SCREEN answers when it is asked
	// confirm is what the mastermind answers on the screen's yes. EMPTY MEANS
	// IT AGREES: most of these tests are about the first judge, and one that
	// says nothing about the second gets a cascade that behaves as the screen
	// alone used to.
	confirm   string
	judged    int
	confirmed int
	question  string
	confirmQ  string
}

func (c *routeCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	system, asked := "", ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	if len(messages) > 1 {
		asked = messageText(messages[1])
	}
	if system == routeJudgeBrief {
		c.mu.Lock()
		defer c.mu.Unlock()
		if request.Model == routeConfirmModel {
			c.confirmed++
			c.confirmQ = asked
			if c.confirm == "" {
				return textResponse(c.verdict), nil
			}
			return textResponse(c.confirm), nil
		}
		c.judged++
		c.question = asked
		return textResponse(c.verdict), nil
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

// answerConfirmWith changes what the mastermind says from the next turn on.
func (c *routeCompleter) answerConfirmWith(verdict string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.confirm = verdict
}

// confirms is how many times the mastermind was asked to stand behind a yes.
func (c *routeCompleter) confirms() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.confirmed
}

// sawConfirmQuestion is what the confirm was shown, which must be the screen's
// own question and nothing about the screen's answer.
func (c *routeCompleter) sawConfirmQuestion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.confirmQ
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
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierLow):        routeScreenModel,
			roles.TierKey(roles.TierMastermind): routeConfirmModel,
		})
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

// ── what the auto-started task is finished against ──────────────────────────

// THE JUDGE WRITES THE DONE-CONDITION, AND IT IS THE ONE THE CHECKER IS HANDED.
//
// This is the only door into the graph nobody typed at, so it is the only one
// where a weak done-condition is invisible: propose_task's schema demands one,
// `/task` has the shaper write one, a divided part carries its own. The judge is
// already reading the turn and already writing the goal, so the condition costs
// nothing extra — and what it buys is a check with something to look at.
func TestTheJudgesDoneConditionIsWhatTheWorkIsFinishedAgainst(t *testing.T) {
	const done = "every package under internal/ has been read and the report names each pricing bug with its file and line"
	verdict := `{"work": true, "goal": "audit every package's pricing code", "acceptance": "` + done + `", "why": "research across every package"}`
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: verdict}
	agent, _, _ := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.acceptance() != done {
		t.Fatalf("the work is finished against %q, want the judge's own done-condition", node.acceptance())
	}
	// AND IT REACHES THE CHECKER. The acceptance is the whole of what the checker
	// is judged against and the whole of what it is shown about the goal
	// (task_audit.go), so the question it is actually asked is where this is
	// worth asserting.
	if question := auditQuestion(node, taskTree{}, nil, ""); !strings.Contains(question, done) {
		t.Fatalf("the checker was asked %q, want the judge's done-condition in it", question)
	}
}

// AND A JUDGE THAT WROTE NO CONDITION STILL LEAVES ONE THE CHECKER CAN READ.
// The stand-in points at the TITLE, which is on the checker's page, rather than
// at a goal it is never shown — which is what the sentence that stood here
// before did, and it made the check against an auto-started task a check
// against a blank.
func TestAnAutoStartedTaskWithNoDoneConditionStillHasOneToCheck(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes}
	agent, _, _ := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.acceptance() != routeFallbackAcceptance {
		t.Fatalf("the work is finished against %q, want the stand-in", node.acceptance())
	}
	// The stand-in may not point at anything the checker is not shown. The brief
	// and the goal are both withheld from it on purpose, so a condition naming
	// either is a condition nobody can check.
	for _, absent := range []string{"the goal above", "the brief above"} {
		if strings.Contains(routeFallbackAcceptance, absent) {
			t.Fatalf("the stand-in says %q, which is not on the checker's page", absent)
		}
	}
	if question := auditQuestion(node, taskTree{}, nil, ""); !strings.Contains(question, routeFallbackAcceptance) {
		t.Fatalf("the checker was asked %q, want the stand-in in it", question)
	}
}

// THE BRIEF ASKS FOR THE FIELD THE CODE READS, and it says the one thing that
// makes the answer worth having: the condition is read on its own.
func TestTheJudgesBriefAsksForADoneConditionSomebodyElseCanCheck(t *testing.T) {
	if !strings.Contains(routeJudgeBrief, `"acceptance"`) {
		t.Fatal("the judge is never shown the field the task is finished against")
	}
	if !strings.Contains(routeJudgeBrief, "DONE WHEN") {
		t.Fatal("the judge is not told what the field is")
	}
	if !strings.Contains(routeJudgeBrief, "ON ITS OWN") {
		t.Fatal("the judge is not told the condition is read without the goal beside it")
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

// ── THE CONFIRM: a cheap yes is not a start ─────────────────────────────────
//
// Auto-start is what made this necessary. While a yes raised a card, a wrong one
// cost a row somebody dismissed; a yes now spends a task's money in a worktree,
// and the model that answers it is on the cheap tier because it is asked after
// every substantial wordy turn. So the cascade: the cheap model SCREENS, and its
// yes is put once more to the tier that thinks before anything is admitted.

const routeConfirmNo = `{"work": false}`

// THE ROLE IS THE DECISION, so the tier is asserted rather than assumed. A
// confirm registered anywhere but the mastermind tier is the whole feature
// quietly not happening — it would resolve to the same cheap model, agree with
// itself, and every test below would still pass.
func TestTheConfirmIsAskedOfTheTierThatThinks(t *testing.T) {
	tier, ok := roles.TierOf(roles.RoleRouterConfirm)
	if !ok {
		t.Fatal("the confirm is not a registered role at all, so it resolves to nothing")
	}
	if tier != roles.TierMastermind {
		t.Fatalf("the confirm resolves on %q, want the mastermind tier", tier)
	}
	// AND THE SCREEN STAYS CHEAP. It reads every substantial wordy turn, and
	// paying for thinking on all of them to correct the rare yes is the bill
	// this shape exists to avoid.
	if tier, ok := roles.TierOf(roles.RoleRouter); !ok || tier != roles.TierLow {
		t.Fatalf("the screen resolves on %q, want the low tier", tier)
	}
}

// BOTH-YES STARTS EXACTLY ONE TASK, and the confirm is asked the SCREEN'S OWN
// QUESTION — the same turn, not the screen's answer about it. A second reader
// handed the first reader's verdict is a reader agreeing with it.
func TestBothJudgesMustAgreeBeforeWorkStarts(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes, confirm: routeYes}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeAsk))

	if completer.asked() != 1 || completer.confirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.asked(), completer.confirms())
	}
	if question := completer.sawConfirmQuestion(); question != completer.sawQuestion() {
		t.Fatalf("the confirm was shown %q, want the same turn the screen was shown", question)
	}
	if strings.Contains(completer.sawConfirmQuestion(), `"work"`) {
		t.Fatal("the confirm was shown the screen's own verdict, so it is agreeing rather than judging")
	}
	noCard(t, collected)
	waitFor(t, "the task both judges agreed to", func() bool { return nodes.count() == 1 })
	if agent.graph().node(2) != nil {
		t.Fatal("one turn admitted two tasks")
	}
	if notice := routeNotice(collected); !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the turn said %q about the work it started", notice)
	}
}

// A CONFIRM THAT REFUSES STARTS NOTHING AND SAYS NOTHING. There is no note, no
// card and no line about a judgement nobody asked for: the turn ends exactly as
// it would have if neither model had ever been called.
func TestAYesTheConfirmRefusesStartsNothingAndSaysNothing(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes, confirm: routeConfirmNo}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeAsk))

	if completer.asked() != 1 || completer.confirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.asked(), completer.confirms())
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a refused yes started work anyway")
	}
	noCard(t, collected)
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a refused yes said %q out loud", notice)
	}
	for _, event := range collected {
		if event.Kind == EventError {
			t.Fatalf("a refused yes said %q out loud", event.Text)
		}
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// A CONFIRM THAT CANNOT ANSWER IS A NO, and this is the one place the cascade
// fails CLOSED — the opposite of the division review, which admits its parts
// when it cannot be reached (task_divide.go). The difference is what each stands
// in front of: a division has already passed two measured gates, and this yes
// has nothing behind it but a cheap model's opinion. There is no repair turn
// either: the confirm is asked once and prose is silence.
func TestAConfirmThatAnswersProseStartsNothing(t *testing.T) {
	completer := &routeCompleter{
		answer:  "Here is what I would look at.",
		verdict: routeYes,
		confirm: "Yes, I think that really should have been a task.",
	}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeAsk))

	if completer.confirms() != 1 {
		t.Fatalf("the confirm was asked %d times, want once and never repaired", completer.confirms())
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a yes nobody could confirm started work")
	}
	noCard(t, collected)
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a yes nobody could confirm said %q", notice)
	}
}

// THE CONFIRM IS NEVER ASKED ABOUT A NO, and that is the whole economy of the
// cascade: the mastermind is billed once per yes, and a yes is the rare half of
// a rare case.
func TestTheConfirmIsNeverAskedAboutANo(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: `{"work": false}`}
	agent, _, nodes := routeAgent(t, completer)

	collect(t, mustSubmit(t, agent, routeAsk))

	if completer.asked() != 1 {
		t.Fatalf("the screen was asked %d times", completer.asked())
	}
	if completer.confirms() != 0 {
		t.Fatalf("a no paid for %d thinking calls", completer.confirms())
	}
	if nodes.count() != 0 {
		t.Fatal("a no started work")
	}
}

// A CONFIRMED NO DOES NOT SPEND THE GAP. The rate limit is a person's patience
// about work that STARTED over the top of them ([routeJudgeGap]) — three turns
// of quiet after an interruption. A yes the confirm refused is not an
// interruption: nothing began and nothing was said, so silencing the screen for
// the next three turns would charge the person twice for one cheap model's
// mistake. The cost of that decision is one thinking call per screened yes
// rather than one every three turns, which is bounded by the screen saying yes
// at all.
func TestAConfirmedNoDoesNotSpendTheGap(t *testing.T) {
	completer := &routeCompleter{answer: "Here is what I would look at.", verdict: routeYes, confirm: routeConfirmNo}
	agent, _, nodes := routeAgent(t, completer)

	if notice := routeNotice(collect(t, mustSubmit(t, agent, routeAsk))); notice != "" {
		t.Fatalf("the refused turn said %q", notice)
	}
	completer.answerConfirmWith(routeYes)
	// THE VERY NEXT TURN, which is inside the gap a start would have opened.
	collected := collect(t, mustSubmit(t, agent, routeAsk))
	if notice := routeNotice(collected); !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the turn after a refused yes said %q, want the work it agreed to", notice)
	}
	waitFor(t, "the task the second turn started", func() bool { return nodes.count() == 1 })
}
