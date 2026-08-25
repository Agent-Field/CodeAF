package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
	confirm string
	// ahead is what the PRE-TURN screen answers about the request, and
	// aheadConfirm is the mastermind's word on its yes. EMPTY IS A NO for the
	// screen — which is what leaves every test about the post-turn read on this
	// page reading exactly as it did before the front of the turn had a question
	// in it — and empty is AGREEMENT for the confirm, as above.
	ahead        string
	aheadConfirm string
	// toolRounds is how many of the CONVERSATION's answers come back as a tool
	// call before it answers in words, so a turn that touches the belt can be
	// scripted without a second completer whose calls are counted by position.
	// Each round asks for a different path, so the loop detector has nothing to
	// say about a turn that grinds.
	toolRounds int
	// handoff is what the model answers when a converted turn asks it for the
	// dowry (checkpoint.go's [Agent.checkpointBrief]). That request carries no
	// belt, so without this it would fall through to the errand branch below and
	// the task would open on "(errand)".
	handoff string
	// sketch is what the MARK READER answers when the checkpoint's sidecar asks it
	// what is left (checkpoint.go's [Agent.readMark]).
	//
	// IT IS WHAT MAKES A RACED YES REACH A TASK AT ALL NOW. The race stopped
	// converting turns in the wave that measured it — a both-yes only pulls the
	// checkpoint's first mark down to the next boundary — so every test on this
	// page whose turn ends up on the rail ends up there through this answer.
	// Empty is a reader with nothing to say, which is a carry-on.
	sketch string
	// holdUntilRaced holds every CONVERSATION answer until this many PRE-TURN
	// calls have been entered — one for the screen, two for the screen and the
	// confirm.
	//
	// IT IS THE WHOLE OF WHAT MAKES A RACE TESTABLE. The read at the front of a
	// turn no longer stands in front of anything: it is asked on a goroutine and
	// the turn goes into the model on the same beat, so "the screen was asked
	// once" is a statement about two threads unless the turn is made to wait for
	// it somewhere. This is that somewhere, and it is in the FIXTURE rather than
	// in the code under test — the point of the wave is that the code never waits.
	holdUntilRaced int
	// onAnswer is called with the conversation's answer number before it comes
	// back, which is where a test that needs something to happen MID-TURN — an
	// interrupt, most usefully — puts it.
	onAnswer     func(int)
	judged       int
	confirmed    int
	preJudged    int
	preConfirmed int
	marks        int
	answers      int
	question     string
	confirmQ     string
	preQuestion  string
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
	if system == routeAheadBrief {
		c.mu.Lock()
		defer c.mu.Unlock()
		if request.Model == routeConfirmModel {
			c.preConfirmed++
			if c.aheadConfirm == "" {
				return textResponse(c.ahead), nil
			}
			return textResponse(c.aheadConfirm), nil
		}
		c.preJudged++
		c.preQuestion = asked
		if c.ahead == "" {
			return textResponse(routeConfirmNo), nil
		}
		return textResponse(c.ahead), nil
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
	// THE MARK'S SIDECAR, and it has to be caught above the errand branch below
	// for the dowry's reason: it carries no belt either.
	if askedForSketch(messages) {
		c.mu.Lock()
		c.marks++
		drawn := c.sketch
		c.mu.Unlock()
		return textResponse(drawn), nil
	}
	// THE DOWRY a converted turn is asked for on its way out. It carries no belt,
	// so it has to be caught above the errand branch below.
	if askedForHandoff(messages) {
		c.mu.Lock()
		brief := c.handoff
		c.mu.Unlock()
		return textResponse(brief), nil
	}
	// WHAT COUNTS AS THE CONVERSATION BEING ASKED is the request that carries the
	// BELT. Every errand this session runs on its own behalf — the namer a new
	// task sends after itself, a title, a memory pass — rides the same client with
	// no tools on it, and counting those as answers would make "the model was
	// never asked" an assertion about whichever errand happened to fire.
	if len(request.Tools) == 0 {
		return textResponse("(errand)"), nil
	}
	c.awaitRace()
	c.mu.Lock()
	c.answers++
	answer, round, hook := c.answer, c.answers, c.onAnswer
	call := round <= c.toolRounds
	c.mu.Unlock()
	if hook != nil {
		hook(round)
	}
	if call {
		return toolResponse(fmt.Sprintf("call-%d", round), "ls",
			fmt.Sprintf(`{"path":"./%d"}`, round)), nil
	}
	return textResponse(answer), nil
}

// awaitRace holds the conversation until the race at the front of the turn has
// got as far as this fixture needs it to. It spins rather than waiting on a
// channel because nothing outside the completer can see the race at all, and it
// always terminates: the only thing that ends a race early is the turn ending,
// and the turn cannot end while it is waiting here.
func (c *routeCompleter) awaitRace() {
	for {
		c.mu.Lock()
		reached := c.preJudged+c.preConfirmed >= c.holdUntilRaced
		c.mu.Unlock()
		if reached {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// stopHolding lets the conversation answer freely from here on, for the tests
// whose second turn has no race to wait for.
func (c *routeCompleter) stopHolding() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.holdUntilRaced = 0
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

// preAsked and preConfirms are the same two counts for the PRE-TURN read, and
// answered is how many times the CONVERSATION itself was asked for an answer —
// which is the assertion the whole front-of-turn wave turns on: a request that
// was handed over is a request the model was never asked to grind out.
func (c *routeCompleter) preAsked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.preJudged
}

func (c *routeCompleter) preConfirms() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.preConfirmed
}

func (c *routeCompleter) answered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answers
}

// marksRead is how many times the checkpoint's sidecar was asked what is left,
// which is the ONLY observable effect a raced yes has until that reading says
// something.
func (c *routeCompleter) marksRead() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.marks
}

// sawAheadQuestion is what the pre-turn judge was shown.
func (c *routeCompleter) sawAheadQuestion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.preQuestion
}

// answerAheadWith changes what the pre-turn screen says from the next turn on.
func (c *routeCompleter) answerAheadWith(verdict string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ahead = verdict
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
		if len(messages) == 0 {
			ordinary++
			continue
		}
		switch messageText(messages[0]) {
		case routeJudgeBrief, routeAheadBrief:
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

// routeNotice is the told-after line, or "" if the turn never started anything.
//
// It matches the line's OPENING rather than the word "task", because the two
// lines that stand above it — the ceiling's and the race's — both say "a task"
// themselves, and a helper that answered with the reason instead of the start
// would make every "nothing was started" assertion on this page read backwards.
func routeNotice(collected []Event) string {
	for _, event := range collected {
		if event.Kind == EventNotice && strings.HasPrefix(event.Text, "this looked like work, so task ") {
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

// A TURN THAT TOUCHED THE BELT IS NOT JUDGED AFTERWARDS. It was already work of
// some size, and asking whether work should have been work is a question with no
// useful answer.
//
// THE PRE-TURN READ IS A DIFFERENT MOMENT and it happens anyway, which is the
// whole point of it: it is launched before the first request, when no tool has
// run and nothing about this turn is known except what the person typed. Here it
// says no, the turn calls its tool, and the post-turn judge stays out of it.
func TestATurnWithToolCallsIsNeverJudgedAfterwards(t *testing.T) {
	completer := &routeCompleter{answer: "done", toolRounds: 1, holdUntilRaced: 1}
	agent, _, nodes := routeAgent(t, completer)

	events, err := agent.Submit(context.Background(), routeAsk)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if completer.asked() != 0 {
		t.Fatalf("the post-turn judge was asked %d times about a turn that called tools", completer.asked())
	}
	if completer.preAsked() != 1 {
		t.Fatalf("the pre-turn judge was asked %d times, want once before the tool ran", completer.preAsked())
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
	if question := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, nil, ""); !strings.Contains(question, done) {
		t.Fatalf("the checker was asked %q, want the judge's done-condition in it", question)
	}
}

// AND A JUDGE THAT WROTE NO CONDITION IS ANSWERED WITH THE PERSON'S OWN WORDS.
//
// THIS IS THE ACCEPTANCE STAYING THEIRS, and it is the correction to a measured
// failure rather than a preference. What stood here before was a generic stand-in
// pointing at the task's TITLE — and on the checkpoint road the title is a
// sentence cut off the front of a handoff brief, so a ten-hour ask was measured
// being finished against "the work named at the top is done", where the work
// named at the top was whatever compile to-do the turn happened to be holding.
// The person's question had stopped being the question anybody was answering.
//
// So their sentence is the done-condition wherever no judge wrote a better one,
// and the generic line survives only for the door that has no request at all.
func TestAnAutoStartedTaskWithNoDoneConditionIsFinishedAgainstThePersonsWords(t *testing.T) {
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
	acceptance := node.acceptance()
	if !strings.Contains(acceptance, routeAsk) {
		t.Fatalf("the work is finished against %q, want the person's own words %q", acceptance, routeAsk)
	}
	// AND IT SAYS "ALL OF IT". The checker reads this and the title and nothing
	// else, so it has no way of knowing the paragraph it is holding is the whole
	// ask — and a half-finished piece of work reads as finished against a
	// done-condition that quotes only the easy part.
	if !strings.Contains(acceptance, "all of it") {
		t.Errorf("the frame around their words does not say the whole ask is meant: %q", acceptance)
	}
	// The stand-in underneath may not point at anything the checker is not shown.
	// The brief and the goal are both withheld from it on purpose, so a condition
	// naming either is a condition nobody can check.
	for _, absent := range []string{"the goal above", "the brief above"} {
		if strings.Contains(routeFallbackAcceptance, absent) {
			t.Fatalf("the stand-in says %q, which is not on the checker's page", absent)
		}
	}
	// AND IT REACHES THE CHECKER, which is the whole point of writing one.
	if question := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, nil, ""); !strings.Contains(question, routeAsk) {
		t.Fatalf("the checker was asked %q, want the person's own words in it", question)
	}
	// AND THE GENERIC LINE IS THE LAST RUNG AND NOT THE SECOND. It is what a door
	// carrying no request at all falls to, which is a graph built by hand or a node
	// restored from before requests were carried.
	if got := routeAcceptance(routeVerdict{}, "   "); got != routeFallbackAcceptance {
		t.Errorf("a verdict with neither a condition nor a request produced %q", got)
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

// ── THE RACE: the decision made beside the grinding, not in front of it ──────
//
// The measured failure this half of the file is written against: a chat message
// carrying four independent pieces of work was answered by ninety-odd rounds of
// inline tool calls, twice, with propose_task on the belt the whole time. That
// turn never reaches the post-turn judge — it called tools — so the question has
// to be put at the front of the turn, where the harness asks it rather than the
// model.
//
// AND THE SECOND MEASURED FAILURE, which is what these tests are now shaped by:
// asking it there BLOCKED the turn against a three-second deadline, and three
// seconds is under the floor latency of the model doing the asking (3.59s median
// at real prompt sizes, none of six calls inside three). Across ten benchmark
// cells the read completed zero times. So the question is now RACED — launched
// at the front, answered beside the turn, spent at a step boundary — and what
// these tests hold are the four edges of that: a yes converts, a late yes is
// dropped, a slow screen delays nothing, and an interrupt ends it.

// routeEnumerated is that message: four deliverables, none of which needs any of
// the others. Nothing in the code reads its shape — the judge does — which is
// why it is a fixture here and not a pattern anywhere.
const routeEnumerated = "fix the flaky auth test, upgrade the http client to v3, write the release notes for 2.4, and delete the dead billing code"

const routeAheadYes = `{"work": true, "wide": true, "goal": "fix the flaky auth test, upgrade the http client to v3, write the 2.4 release notes and delete the dead billing code", ` +
	`"acceptance": "the auth test passes ten runs in a row, the client is on v3 with the build green, RELEASE-2.4.md exists, and no file mentions the billing code", ` +
	`"why": "four separate pieces of work"}`

// routeDowry is what the model writes on its way out of a converted turn: the
// first line is the task's name and the rest is what the seconds of inline work
// already found out.
const routeDowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

// racingAgent is [routeAgent] with the whole cascade answering yes, the
// conversation grinding, and the mark reader drawing parts: the turn goes into
// the model, calls tools, has its first mark pulled down to the boundary where
// the race's verdict landed, and is handed over there because the reading of the
// WORK found independent parts in it.
//
// THE TWO READINGS ARE BOTH LOAD-BEARING NOW, which is the shape of the wave. The
// race decides only WHEN the work is looked at; the sidecar decides whether
// anything happens.
func racingAgent(t *testing.T, completer *routeCompleter) (*Agent, *routeRun, *ran) {
	t.Helper()
	completer.ahead = routeAheadYes
	completer.aheadConfirm = routeAheadYes
	completer.handoff = routeDowry
	if completer.sketch == "" {
		completer.sketch = checkpointSplitSketch
	}
	if completer.toolRounds == 0 {
		completer.toolRounds = 8
	}
	// Both judges have answered before the conversation says its first word, so
	// the boundary after that word is a boundary with a verdict waiting at it.
	completer.holdUntilRaced = 2
	return routeAgent(t, completer)
}

// A REQUEST WITH SEVERAL INDEPENDENT DELIVERABLES IN IT IS LOOKED AT EARLY, AND
// THE LOOK CONVERTS THE TURN.
//
// Two readings in sequence: the race says this one is worth looking at and pulls
// the checkpoint's first mark down to the boundary its verdict landed on; the
// sidecar at that mark reads the transcript, draws three independent parts, and
// THAT is what ends the turn. One task carries the work, the person's own words
// ride it verbatim, and what the conversation had already found out goes with it
// as the dowry — under the parts the sidecar named.
func TestARacedYesIsLookedAtEarlyAndTheLookConvertsTheTurn(t *testing.T) {
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, runs, nodes := racingAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != 1 || completer.preConfirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.preAsked(), completer.preConfirms())
	}
	// THE MODEL WAS ASKED, AND IT WAS ASKED FIRST. The race costs the turn no
	// latency because the turn never waits for it.
	if completer.answered() == 0 {
		t.Fatal("the race took the message away from the turn instead of racing it")
	}
	// AND THE TURN STOPPED WHERE THE VERDICT LANDED. Eight rounds were scripted;
	// what runs is the round or two before the boundary that had an answer waiting.
	if completer.answered() > 3 {
		t.Fatalf("the turn ground on for %d rounds before the race converted it", completer.answered())
	}
	// The judge read the REQUEST and nothing about an answer — it was written
	// before there was one.
	if question := completer.sawAheadQuestion(); !strings.Contains(question, routeEnumerated) ||
		strings.Contains(question, "HOW THE ASSISTANT ANSWERED") {
		t.Fatalf("the pre-turn judge was shown %q", question)
	}
	noCard(t, collected)
	waitFor(t, "the task the race converted the turn into", func() bool { return nodes.count() == 1 })

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if agent.graph().node(2) != nil {
		t.Fatal("one converted turn admitted two tasks")
	}
	// THE PERSON'S MESSAGE RIDES THE SPEC AS THEIR OWN WORDS, which is what makes
	// this a hand-off rather than a paraphrase: the worker opens on the sentence
	// they typed ([Agent.taskRequest], task_brief.go).
	if node.request() != routeEnumerated {
		t.Fatalf("the task carries %q as the person's request", node.request())
	}
	// AND THE FIRST SECONDS OF INLINE WORK ARE NOT WASTED: they are the dowry,
	// written by the model that did them, exactly as the ceiling's are — under the
	// parts the sidecar drew out of the same transcript.
	if !strings.Contains(node.spec.brief, "everything this turn already found out") {
		t.Fatalf("the converted task lost what the turn had already found: %q", node.spec.brief)
	}
	if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS:") {
		t.Fatalf("the brief does not open on the parts the mark reader named: %q", node.spec.brief)
	}
	// THE RACE'S OWN VERDICT STILL RIDES IT, which is the whole reason a demoted
	// race keeps writing one — the breadth two readers agreed on and the
	// done-condition the screen wrote are what this task is armed and finished
	// against.
	if !node.spec.wide {
		t.Error("the judge said the work was wide and the converted task is not armed to split")
	}
	if !strings.Contains(node.acceptance(), "RELEASE-2.4.md exists") {
		t.Errorf("the task is finished against %q, want the judge's own done-condition", node.acceptance())
	}
	// AND THEY ARE TOLD, in the split's own line with the told-after line under it.
	said := noticeTexts(collected)
	if !saidSomething(said, checkpointSplitNote) {
		t.Fatalf("the conversion never said its line; notices were %q", said)
	}
	notice := routeNotice(collected)
	if !strings.Contains(notice, "this looked like work") || !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the turn said %q about the work it started", notice)
	}
	// THE TURN IS SEALED, and the transcript is not left with a request nobody
	// replied to: the next turn would open on it and answer it all over again.
	if last := lastMessage(agent); last.Role != "assistant" ||
		messageText(last) != checkpointSplitNote+"\n"+notice {
		t.Fatalf("the transcript ends on %q by %q, want the two lines the person read",
			messageText(last), last.Role)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
	if started := runs.started(); len(started) != 0 {
		t.Fatalf("the race reached a planner: %v", started)
	}
}

// AND A RACED YES ON ITS OWN CONVERTS NOTHING AND SAYS NOTHING. IT ONLY MAKES
// AFORGE LOOK SOONER.
//
// This is the demotion, pinned. The benchmark measured the raced screen
// converting BOTH of its small-work traps — a message whose fastest correct
// answer was a few tool calls, taken out of the conversation that was answering
// it — because what the race reads is a REQUEST nobody has worked on yet. So the
// yes now buys one thing: the checkpoint's first mark lands at the next boundary
// instead of after ten rounds. Here the mark reader looks at the work and says it
// is one job, and the turn simply finishes.
//
// AND NOTHING IS SAID, because nothing the person can observe has happened — the
// emptiness law applied to an event rather than to a number.
func TestARacedYesOnlyMakesTheSidecarLookSooner(t *testing.T) {
	completer := &routeCompleter{
		answer: "Here is the fix, and the suite is green.",
		// The reading of the work says this is one job after all.
		sketch: checkpointChainSketch,
	}
	agent, _, nodes := racingAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != 1 || completer.preConfirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.preAsked(), completer.preConfirms())
	}
	// THE LOOK HAPPENED, AND IT HAPPENED EARLY. The ordinary first mark stands at
	// ten finished rounds; this turn answered two or three times in all.
	if read := completer.marksRead(); read != 1 {
		t.Fatalf("the sidecar was asked %d times, want the one look the race bought", read)
	}
	if completer.answered() >= checkpointMarkAt(1) {
		t.Fatalf("the turn ran %d rounds before the work was looked at; the raced yes is supposed to "+
			"pull that mark down from %d", completer.answered(), checkpointMarkAt(1))
	}
	// AND NOTHING ELSE HAPPENED AT ALL.
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a raced yes started work on its own")
	}
	if said := noticeTexts(collected); saidSomething(said, checkpointSplitNote) ||
		saidSomething(said, checkpointCeilingNote) {
		t.Fatalf("a raced yes said something to the person: %q", said)
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a raced yes announced %q", notice)
	}
	// AND THE TURN RAN TO ITS OWN END, on its own answer.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), "the suite is green") {
		t.Fatalf("the transcript ends on %q by %q, want the model's own answer",
			messageText(last), last.Role)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// AND THE ARMED TASK ACTUALLY CARRIES THE VERB, WHICH IS THE ONLY THING ARMING
// IS FOR.
//
// The measured failure this pins: a converted four-issue message ran as one
// worker to the end — parts 0, one pair of hands — and its whole transcript
// never mentions `divide_work`, which is the signature of a worker that was
// never given it rather than one that decided not to use it. There are two
// places that could have been true: the flag not surviving the road, or the
// judge never writing it. So this drives the WHOLE road — screen, confirm,
// conversion, admission — and then asks the production constructor for the agent
// that IS the node, which is the one that builds every worker in the running
// program (task_divide.go's constructor tests state why nothing here writes a
// Config literal).
func TestARacedWideYesPutsTheDivisionVerbOnTheWorkersBelt(t *testing.T) {
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, _, nodes := racingAgent(t, completer)
	agent.config.Divide = true

	collect(t, mustSubmit(t, agent, routeEnumerated))
	waitFor(t, "the task the race converted the turn into", func() bool { return nodes.count() == 1 })

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	// THE READER IS NAMED, so a failure says which link of the road broke rather
	// than that the end of it is wrong.
	if got := node.armedBy(); got != armedWide {
		t.Fatalf("the converted task was armed by %q, want the judge's own reading of breadth (%q)",
			got, armedWide)
	}
	worker, err := agent.newTaskAgent(context.Background(), t.TempDir(), node, "")
	if err != nil {
		t.Fatalf("the production constructor refused to build the worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if !beltHas(worker, "divide_work") {
		t.Fatal("the judge said the work was wide and the worker it started has no divide_work on its belt")
	}
	if !strings.Contains(renderSystem(worker.config), "divide_work") {
		t.Fatal("the worker was handed the verb and never told what it is for")
	}
}

// THE MASTERMIND'S OWN READING OF BREADTH IS NOT THROWN AWAY.
//
// The confirm answers the same contract as the screen and is the better reader
// of it; its `wide` was parsed and dropped, so the one field that decides whether
// a converted task may ever split was settled by the cheapest model in the
// cascade alone. Either yes now arms — see [routeWidth] for why breadth composes
// that way when work does not.
func TestTheConfirmsReadingOfBreadthArmsWhatTheScreenMissed(t *testing.T) {
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, _, nodes := racingAgent(t, completer)
	agent.config.Divide = true
	// The screen says work and says nothing about breadth; the mastermind reads
	// the same four deliverables and says it is broad.
	completer.ahead = `{"work": true, "goal": "` + routeEnumerated + `", "why": "four separate pieces of work"}`

	collect(t, mustSubmit(t, agent, routeEnumerated))
	waitFor(t, "the task the race converted the turn into", func() bool { return nodes.count() == 1 })

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if !node.spec.wide {
		t.Fatal("the mastermind said the work was broad and the task it confirmed did not carry it")
	}
	if !node.dividing() {
		t.Fatal("the mastermind's own reading of breadth armed nothing")
	}
}

// AND NEITHER RACE READER SAYING IT ARMS NOTHING OF THE RACE'S OWN. What arms
// this task is the reading of the WORK, and the word on the spec says so.
//
// Both race readers here call the request one self-contained sweep, so nothing
// they wrote carries breadth. The mark reader then opens the transcript and draws
// three independent parts out of it — and THAT is what arms the road, under
// [armedJudged] rather than [armedWide], because the two are told apart by which
// reader decided (task_divide.go's [TaskNode.armedByJudgement]).
func TestARacedVerdictWithoutBreadthArmsNothingOfItsOwn(t *testing.T) {
	const narrow = `{"work": true, "goal": "port the pricing tests to the new fixture", "why": "one self-contained sweep"}`
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, _, nodes := racingAgent(t, completer)
	agent.config.Divide = true
	completer.ahead, completer.aheadConfirm = narrow, narrow

	collect(t, mustSubmit(t, agent, routeEnumerated))
	waitFor(t, "the task the reading of the work handed over", func() bool { return nodes.count() == 1 })

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.spec.wide {
		t.Errorf("a race verdict that said nothing about breadth still wrote it onto the spec (%q)",
			node.armedBy())
	}
	if got := node.armedBy(); got != armedJudged {
		t.Errorf("the task was armed by %q, want the mark reader's own judgement (%q)", got, armedJudged)
	}
}

// AND A READING OF THE WORK THAT SAYS ONE JOB ARMS NOTHING EITHER — because
// nothing is handed over at all. Breadth is still a judgement somebody has to
// make, and when nobody makes it the turn simply finishes in the conversation.
func TestNobodySayingTheWorkIsWideStartsNothing(t *testing.T) {
	const narrow = `{"work": true, "goal": "port the pricing tests to the new fixture", "why": "one self-contained sweep"}`
	completer := &routeCompleter{answer: "I will start with the auth test.", sketch: checkpointChainSketch}
	agent, _, nodes := racingAgent(t, completer)
	agent.config.Divide = true
	completer.ahead, completer.aheadConfirm = narrow, narrow

	collect(t, mustSubmit(t, agent, routeEnumerated))

	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a turn nobody called wide was handed over anyway")
	}
}

// A VERDICT THAT LANDS AFTER THE TURN IS OVER IS DROPPED.
//
// Both judges say yes here and the turn answers in words alone, so there is no
// step boundary for the yes to be spent at. It is discarded rather than held
// over: that turn has already had a reading of its own (the post-turn judge,
// which refuses here so that anything started could only have come from the
// race), and a task appearing on top of an answer somebody has already read is
// the exact surprise this road is built to avoid.
func TestAVerdictThatLandsAfterTheTurnIsDropped(t *testing.T) {
	completer := &routeCompleter{
		answer:  "Here is what I would look at.",
		verdict: routeConfirmNo,
	}
	// No tool call, so the turn ends on its first answer and never reaches a
	// boundary — with the race already settled on a yes before it speaks.
	completer.toolRounds = -1
	agent, _, nodes := racingAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != 1 || completer.preConfirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.preAsked(), completer.preConfirms())
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a verdict that arrived after the answer started work anyway")
	}
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("a dropped verdict said its line out loud: %q", noticeTexts(collected))
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a dropped verdict said %q", notice)
	}
	// AND THE TURN ENDED AS ITS OWN ANSWER, which is the whole of what the person
	// asked for on a turn nothing was converted out of.
	if last := lastMessage(agent); last.Role != "assistant" ||
		messageText(last) != "Here is what I would look at." {
		t.Fatalf("the transcript ends on %q by %q, want the model's own answer",
			messageText(last), last.Role)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// AN INTERRUPT DISCARDS THE YES. A turn the person has just stopped is a turn
// they have said they do not want, and moving its remains onto the rail would be
// answering an interrupt with a task — the same law the ceiling keeps at the same
// boundary (checkpoint.go).
func TestAnInterruptDiscardsTheRacedYes(t *testing.T) {
	var agent *Agent
	completer := &routeCompleter{answer: "I will start with the auth test."}
	// The interrupt lands as the first answer comes back, so the turn reaches the
	// boundary with a settled yes AND a context that is already over.
	completer.onAnswer = func(round int) {
		if round == 1 {
			agent.Interrupt()
		}
	}
	agent, _, nodes := racingAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("an interrupted turn was converted into a task")
	}
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("an interrupted turn drew the conversion line: %q", noticeTexts(collected))
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("an interrupted turn said %q", notice)
	}
}

// THE RATE LIMIT SPENDS ONCE ON A CONVERSION, exactly as the ceiling spends it.
// The person has just been interrupted by a task and does not care which moment
// noticed, so the next three turns are quiet whatever any judge says.
func TestAConversionSpendsTheGapOnce(t *testing.T) {
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, _, nodes := racingAgent(t, completer)

	first := collect(t, mustSubmit(t, agent, routeEnumerated))
	if !saidSomething(noticeTexts(first), checkpointSplitNote) {
		t.Fatalf("the first turn was not converted; notices were %q", noticeTexts(first))
	}
	waitFor(t, "the task the race converted the turn into", func() bool { return nodes.count() == 1 })

	// THE VERY NEXT TURN is inside the gap the conversion opened, so the race is
	// never even launched — no screen call, no confirm, no second task.
	before, beforeConfirms := completer.preAsked(), completer.preConfirms()
	completer.stopHolding()
	second := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != before || completer.preConfirms() != beforeConfirms {
		t.Fatalf("the race ran again inside the gap (%d screens then %d)", before, completer.preAsked())
	}
	if saidSomething(noticeTexts(second), checkpointSplitNote) {
		t.Fatalf("a second conversion happened inside the gap: %q", noticeTexts(second))
	}
	if agent.graph().node(2) != nil {
		t.Fatal("two tasks started within three turns of each other")
	}
}

// A YES CANNOT CONVERT A TURN WHOSE WORK IS ALREADY DONE.
//
// The measured defect: a model wrote all eight files it was asked for inline in
// under a minute, the confirm landed at the tail of that, and the conversion
// started a task on the leftovers of a finished answer — which then stopped,
// having duplicated a turn nobody needed duplicating. The race reads the REQUEST
// and cannot know any of that; the model holding the findings can, so the dowry
// ask asks it, and [checkpointNothingLeft] drops the whole handover.
//
// AND THE MARK'S OWN READER SAYS THE SAME THING, which is what a drop now needs:
// the running model declaring itself finished is that model grading its own work,
// so the declaration is corroborated against the sketch drawn at the same mark
// ([checkpointSketch.saysDone]). That is not a harder bar on this turn — it is
// the honest one. The reader here is shown an account of the work whose written
// section holds all eight files, which is exactly the evidence that answers
// "(done)"; a reader still drawing three parts over the top of that would be
// disagreeing about the facts, and the file sides with the second mind.
func TestARacedYesIsDroppedWhenTheModelSaysNothingIsLeft(t *testing.T) {
	completer := &routeCompleter{answer: "All eight files are written and the smoke check passed."}
	agent, _, nodes := racingAgent(t, completer)
	// The turn calls one tool and then answers, so there IS a boundary for the
	// verdict to land at — the conversion is declined there rather than missed.
	completer.toolRounds = 1
	completer.handoff = checkpointNothingLeft
	completer.sketch = checkpointDoneSketch

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	// The race ran and answered yes: this is a declined conversion, not a turn
	// that was never judged.
	if completer.preAsked() != 1 || completer.preConfirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.preAsked(), completer.preConfirms())
	}
	// NO TASK, AND NO LINE ABOUT ONE.
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("the person was told their answer was being moved and then watched it finish where "+
			"it was; notices were %q", noticeTexts(collected))
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a task was announced over a finished turn: %q", notice)
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatalf("%d tasks were admitted out of a turn with nothing left to hand over", nodes.count())
	}
	// AND THE ANSWER STANDS. The turn ends on the model's own words rather than on
	// two dim lines written over the top of them.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), "All eight files are written") {
		t.Fatalf("the transcript ends on %q by %q, want the answer the turn was giving",
			messageText(last), last.Role)
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// AND THE CONVERTED TASK IS NAMED FROM THE PERSON'S WORDS, NEVER FROM THE DOWRY.
//
// The other road into [Agent.launchRouteTask] cuts a title off a goal a judge
// wrote to a contract. A converted turn's goal is a CONTINUATION written on a
// transcript full of tool calls, and it has been measured arriving as a
// provider's tool-call sentinel and as a markdown heading. The person's own
// sentence is the one thing on this road nobody writes.
func TestAConvertedTaskIsNamedFromTheAskAndNotFromTheDowry(t *testing.T) {
	completer := &routeCompleter{answer: "I will start with the auth test."}
	agent, _, nodes := racingAgent(t, completer)
	completer.handoff = dsmlSentinel + "\nwhatever this turn found out"

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))
	waitFor(t, "the task the race converted the turn into", func() bool { return nodes.count() == 1 })

	notice := routeNotice(collected)
	if notice == "" {
		t.Fatalf("no task was announced; notices were %q", noticeTexts(collected))
	}
	if strings.Contains(notice, "DSML") {
		t.Fatalf("the sentinel became the task's name: %q", notice)
	}
	if !strings.Contains(notice, "fix the flaky auth test") {
		t.Errorf("the task was announced as %q, want the person's own words", notice)
	}
}

// AND THE NAME IS CLEANED THE WAY EVERY OTHER NAME ON THIS SURFACE IS.
//
// The title of an auto-started task is the one that goes straight into a line a
// person reads and into a rail row twenty-four columns wide, and it is cut off
// the front of a paragraph nobody wrote to be a name. A row rendering
// `**refactor beta.py — 15+ single-rename steps, verify each**` was measured, so
// the markdown comes off with the quotes and the announcements that hand already
// takes ([cleanTitle], title.go).
func TestAnAutoStartedTasksNameCarriesNoMarkdown(t *testing.T) {
	for _, raw := range []string{
		"**refactor beta.py — 15+ single-rename steps, verify each**",
		"## refactor the parser",
		"`refactor the parser`",
		"> refactor the parser",
	} {
		title := routeTaskTitle(raw)
		if strings.ContainsAny(title, "*`") {
			t.Errorf("%q was named %q, which draws its own markup on the rail", raw, title)
		}
		if strings.HasPrefix(title, "#") || strings.HasPrefix(title, ">") {
			t.Errorf("%q was named %q, which opens on a heading marker", raw, title)
		}
		if title == "" {
			t.Errorf("%q was named nothing at all", raw)
		}
	}
	// AND A NAME ALWAYS SURVIVES. Unlike the namer's own answer, this is the only
	// title the task has, so a cleaner that refused everything would announce a
	// task with no name in it.
	if title := routeTaskTitle("Title: here is the name"); title == "" {
		t.Error("a goal the cleaner refuses outright left the task with no name to be announced by")
	}
}

// AND THE RACE HAS NO LINE OF ITS OWN AT ALL.
//
// It used to have one — "this reads like work · moving it to a task that is
// watched and can split" — and it went away with the conversion that earned it.
// A raced yes now changes nothing a person can observe at the moment it lands, so
// there is nothing honest to say and NOTHING IS SAID (THE EMPTINESS LAW). What
// they eventually read, if the reading of the work agrees, is the checkpoint's
// own line about parts.
//
// This is a test rather than a deletion because the retired string is the kind of
// thing that comes back: it is quoted in the manual, it reads well, and the next
// lane to touch this file will want a line here.
func TestTheRaceHasNoLineOfItsOwn(t *testing.T) {
	const retired = "this reads like work · moving it to a task that is watched and can split"
	for _, line := range []string{checkpointSplitNote, checkpointCeilingNote, taskEscalationNote} {
		if line == retired {
			t.Errorf("the race's retired line came back as %q", line)
		}
	}
	// AND THE LINE THAT REPLACED IT SAYS WHAT ITS OWN MOMENT SAW. The race read a
	// request; the split read the work and drew parts out of it, which is a
	// different and stronger claim.
	if !strings.Contains(checkpointSplitNote, "parts") {
		t.Errorf("the split's line does not say what it saw: %q", checkpointSplitNote)
	}
}

// A SMALL QUESTION IS ANSWERED, and nothing is asked about it. The floor under
// "worth a model call" is the same one the post-turn read uses, so a
// conversation of short questions pays for this feature exactly nothing.
func TestASmallQuestionIsAnsweredWithNoPreTurnAsk(t *testing.T) {
	completer := &routeCompleter{answer: "Any time.", ahead: routeAheadYes, aheadConfirm: routeAheadYes}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, "thanks, that helps"))

	if completer.preAsked() != 0 {
		t.Fatalf("a three-word turn paid for %d pre-turn judge calls", completer.preAsked())
	}
	if completer.answered() != 1 {
		t.Fatalf("the conversation was asked %d times, want the one ordinary answer", completer.answered())
	}
	noCard(t, collected)
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a small question started work")
	}
}

// ONE RATE LIMIT SPANS BOTH MOMENTS. The person is being interrupted by TASKS
// and does not care which of the two reads noticed, so a start from either one
// buys the same three turns of quiet from the other — one counter, one gap.
func TestTheRateLimitIsSharedByBothAsks(t *testing.T) {
	completer := &routeCompleter{
		answer: "Here is what I would look at.", verdict: routeYes, confirm: routeYes,
		// The first turn's race is held in front of the answer so that "the screen
		// was asked once" is a fact about this turn rather than about two threads.
		holdUntilRaced: 1,
	}
	agent, _, nodes := routeAgent(t, completer)

	// TURN ONE starts work the POST-turn way: the pre-turn screen says no (its
	// default), the answer comes back in words, and the judge behind it starts a
	// task.
	first := collect(t, mustSubmit(t, agent, routeAsk))
	if notice := routeNotice(first); !strings.Contains(notice, "task 1 started") {
		t.Fatalf("the first turn said %q, want the work the post-turn read started", notice)
	}
	waitFor(t, "the task the post-turn read started", func() bool { return nodes.count() == 1 })

	// TURN TWO would be a yes at the FRONT of the turn — and it is never asked,
	// because the gap the first start opened is the same gap.
	completer.answerAheadWith(routeAheadYes)
	before := completer.preAsked()
	second := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != before {
		t.Fatalf("the pre-turn screen was asked again inside the gap (%d then %d)", before, completer.preAsked())
	}
	if notice := routeNotice(second); notice != "" {
		t.Fatalf("a second task started inside the gap: %q", notice)
	}
	if agent.graph().node(2) != nil {
		t.Fatal("two tasks started within three turns of each other")
	}
	// And the turn ran as an ordinary one: the message the front of the turn did
	// not take was answered by the model.
	if completer.answered() != 2 {
		t.Fatalf("the conversation answered %d turns, want both of them", completer.answered())
	}
}

// A RACED YES THE MASTERMIND REFUSES IS SILENCE, AND THE TURN RUNS TO ITS END.
// The cheap model screens and cannot start anything on its own, and a refused
// yes leaves no trace at all: no task, no line, and a turn the model answers
// exactly as it would have. The post-turn read still gets its own look
// afterwards.
func TestARacedYesTheConfirmRefusesLetsTheTurnRunToItsEnd(t *testing.T) {
	completer := &routeCompleter{
		answer:       "Here is what I would look at.",
		ahead:        routeAheadYes,
		aheadConfirm: routeConfirmNo,
		verdict:      `{"work": false}`,
		// Both judges have been asked before the turn says a word, so the silence
		// below is a refusal rather than a question still in flight.
		holdUntilRaced: 2,
	}
	agent, _, nodes := routeAgent(t, completer)

	collected := collect(t, mustSubmit(t, agent, routeEnumerated))

	if completer.preAsked() != 1 || completer.preConfirms() != 1 {
		t.Fatalf("the screen was asked %d times and the confirm %d, want one each",
			completer.preAsked(), completer.preConfirms())
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a refused pre-turn yes started work anyway")
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a refused pre-turn yes said %q out loud", notice)
	}
	for _, event := range collected {
		if event.Kind == EventError {
			t.Fatalf("a refused pre-turn yes said %q out loud", event.Text)
		}
	}
	// THE TURN RAN. The model was asked, it answered, and the post-turn read had
	// its own look at what came back — which is the fall-through this whole seam
	// promises when it decides not to act.
	if completer.answered() != 1 {
		t.Fatalf("the conversation was asked %d times, want the ordinary turn", completer.answered())
	}
	if completer.asked() != 1 {
		t.Fatalf("the post-turn judge was asked %d times, want its usual one look", completer.asked())
	}
	noCard(t, collected)
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// ONLY WHAT A PERSON TYPED, AND ONLY WHAT A TASK COULD BE HANDED, IS READ AT
// THE FRONT OF A TURN. A woken turn's note is the session talking to itself, a
// task's own worker has no surface to put a question on, the woken turn's empty
// opening has nothing in it at all — and a message with pictures attached can
// only be looked at here, because a spec is words.
func TestOnlyATypedMessageIsPreJudged(t *testing.T) {
	completer := &routeCompleter{answer: "done", ahead: routeAheadYes, aheadConfirm: routeAheadYes}
	agent, _, nodes := routeAgent(t, completer)

	cases := []struct {
		name string
		user userMessage
	}{
		{"a wake note", wakeNote(routeEnumerated)},
		{"a note the session wrote", userMessage{
			message: textMessage("user", routeEnumerated), authored: true,
		}},
		{"the woken turn's empty opening", userMessage{}},
		{"a message with pictures in it", userMessage{
			message: textMessage("user", routeEnumerated), refs: []journalPart{{}},
		}},
	}
	for _, c := range cases {
		if race := agent.routeAhead(context.Background(), c.user); race != nil {
			race.end()
			t.Errorf("%s was raced", c.name)
		}
	}

	// AND A TASK'S OWN WORKER IS NEVER ASKED EITHER. It is a node with no person
	// in front of it: work started there would appear on nobody's screen.
	inTask, _, _ := routeAgent(t, completer)
	inTask.config.InTask = true
	if race := inTask.routeAhead(context.Background(), userText(routeEnumerated)); race != nil {
		race.end()
		t.Error("a node's own turn was raced")
	}

	// AND NEITHER IS A SESSION NOBODY IS WATCHING. `--once` and anything else
	// headless has no screen for a conversion line to land on.
	screenless, _, _ := routeAgent(t, completer)
	screenless.config.AskConsent = false
	if race := screenless.routeAhead(context.Background(), userText(routeEnumerated)); race != nil {
		race.end()
		t.Error("a session with nobody watching was raced")
	}

	if completer.preAsked() != 0 {
		t.Fatalf("%d judge calls were paid for on turns nobody typed", completer.preAsked())
	}
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a turn nobody typed started work")
	}
}

// routeSlowCompleter is a judge that cannot keep up: it takes the pre-turn
// question and never answers it. It records whether the call carried a deadline
// at all, because a bound nobody set is a wedged judge nothing ever ends — and
// whether the SCREEN HAD RESOLVED at the moment the conversation was answered,
// which is the whole assertion of the wave: the first request of the turn goes
// out with the question still in flight.
type routeSlowCompleter struct {
	mu       sync.Mutex
	asked    int
	answers  int
	bounded  bool
	resolved bool
	// racedFirst records that the conversation was answered while the screen was
	// still unresolved.
	racedFirst bool
}

func (c *routeSlowCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	switch system {
	case routeAheadBrief:
		c.mu.Lock()
		c.asked++
		deadline, ok := ctx.Deadline()
		c.bounded = ok && time.Until(deadline) <= routeRaceWindow
		c.mu.Unlock()
		// This call is never coming back, and NOTHING IS WAITING ON IT.
		<-ctx.Done()
		c.mu.Lock()
		c.resolved = true
		c.mu.Unlock()
		return nil, ctx.Err()
	case routeJudgeBrief:
		return textResponse(routeConfirmNo), nil
	}
	// The conversation waits for the screen to have been ENTERED — and for nothing
	// else. Without it the turn would be over before the goroutine was scheduled,
	// and "the request went out while the question was in flight" would be a
	// statement about neither. The screen never resolves, so this is the strongest
	// form of the assertion available: entered, unresolved, answered anyway.
	for {
		c.mu.Lock()
		entered := c.asked > 0
		c.mu.Unlock()
		if entered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	c.mu.Lock()
	c.answers++
	if c.answers == 1 {
		c.racedFirst = !c.resolved
	}
	c.mu.Unlock()
	return textResponse("here is what I would look at."), nil
}

// A SLOW SCREEN DELAYS NOTHING, AND A JUDGE THAT NEVER ANSWERS IS A NO.
//
// This is the measured defect, pinned: the screen used to stand between somebody
// pressing enter and the first request of their turn, under a deadline shorter
// than the model's own floor latency, so every turn paid the wait and every
// verdict was a timeout. Now the request goes out with the question still in
// flight — nothing is said, nothing is started, and the model answers the message
// it would have answered.
func TestASlowScreenNeverDelaysTheTurnsFirstRequest(t *testing.T) {
	completer := &routeSlowCompleter{}
	agent, _, nodes := routeAgent(t, completer)

	started := time.Now()
	collected := collect(t, mustSubmit(t, agent, routeEnumerated))
	waited := time.Since(started)

	completer.mu.Lock()
	asked, answers, bounded, racedFirst := completer.asked, completer.answers, completer.bounded, completer.racedFirst
	completer.mu.Unlock()

	if asked != 1 {
		t.Fatalf("the screen was asked %d times, want once and never repaired", asked)
	}
	if !bounded {
		t.Fatal("the screen carried no deadline of its own, so a wedged judge would never end")
	}
	// THE REQUEST WENT OUT FIRST. The conversation was answered while the screen
	// was still thinking, which is the difference between racing a turn and
	// standing in front of one.
	if !racedFirst {
		t.Fatal("the turn's first request waited for the screen to resolve")
	}
	// AND THE TURN PAID NONE OF THE WINDOW. A quarter of it is already far more
	// than a scripted turn can take, so anything near the window is the old shape
	// come back.
	if waited > routeRaceWindow/4 {
		t.Fatalf("the turn waited %s on a judge that never answered", waited)
	}
	if answers != 1 {
		t.Fatalf("the conversation was asked %d times, want the ordinary turn", answers)
	}
	if notice := routeNotice(collected); notice != "" {
		t.Fatalf("a judge that never answered said %q", notice)
	}
	for _, event := range collected {
		if event.Kind == EventError {
			t.Fatalf("a judge that never answered put %q on the screen", event.Text)
		}
	}
	noCard(t, collected)
	if nodes.count() != 0 || agent.graph().node(1) != nil {
		t.Fatal("a judge that never answered started work")
	}
}

// THE TWO BRIEFS ASK THEIR OWN QUESTION AND WRITE THE SAME OBJECT. The evidence
// differs — an unanswered request, a finished turn — and the wire does not,
// because both land in one [routeVerdict] and start one kind of task.
func TestBothBriefsAskForOneObjectAndOnlyOneOfThemMentionsAnAnswer(t *testing.T) {
	if !strings.Contains(routeAheadBrief, routeVerdictContract) ||
		!strings.Contains(routeJudgeBrief, routeVerdictContract) {
		t.Fatal("the two briefs spell the wire twice, so they will drift")
	}
	if strings.Contains(routeAheadBrief, "answered the person in WORDS ALONE") {
		t.Fatal("the pre-turn brief judges an answer that has not been written yet")
	}
	if !strings.Contains(routeAheadBrief, "CRITICAL PATH") {
		t.Fatal("the pre-turn brief drops the law that keeps small work out of the task graph")
	}
	if !strings.Contains(routeAheadBrief, "SEVERAL INDEPENDENT DELIVERABLES") ||
		!strings.Contains(routeAheadBrief, "SWEEP") ||
		!strings.Contains(routeAheadBrief, "MINUTES OF TOOL CALLS") {
		t.Fatal("the pre-turn brief does not teach the three shapes only a request can show")
	}
}
