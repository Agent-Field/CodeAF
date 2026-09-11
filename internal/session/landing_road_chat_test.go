package session

// THE LANDING ROAD ON THE CHAT'S SIDE: a task the conversation started lands
// while nobody is typing, and what the person gets must be a SENTENCE.
//
// These drive the same real settle path wake_test.go drives — a node admitted,
// finished and completed through the graph — and assert the three things the
// woken turn is judged on: that it happened at all, that the child's own answer
// and the branch outcome are in front of the model when it does, and HOW MANY
// turns a run of landings buys.

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// landing is one node's whole ending, as a test may shape it.
type landing struct {
	title   string
	report  string
	result  string
	changed []string
	branch  string
	merge   string
	state   TaskState
}

// landOnTheChat runs one node to a final state through the graph the
// conversation owns, so the note, the row and the wake all happen exactly as
// they do in a real session.
func landOnTheChat(t *testing.T, agent *Agent, end landing) *TaskNode {
	t.Helper()
	graph := agent.graph()
	state := end.state
	if state == "" {
		state = TaskDone
	}
	graph.run = func(node *TaskNode) {
		if end.result != "" {
			node.keepWorkerConclusion(end.result, io.Discard)
		}
		node.finish(end.report, end.changed, end.branch, end.merge)
		node.graph.complete(node, state)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: end.title, brief: "b", acceptance: "a"})
	node := graph.node(id)
	waitDoneNode(t, node)
	return node
}

func waitIdle(t *testing.T, agent *Agent) {
	t.Helper()
	waitFor(t, "the woken turn to end", func() bool {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return !agent.running
	})
}

// ── C: what the woken model is shown ────────────────────────────────────────

// THE CHILD'S OWN LAST MESSAGE IS IN THE REQUEST, under its own lead, because
// the report is the card's three lines and the answer is what the person asked
// for ([resultBlock]).
func TestAnIdleChatsWakeCarriesTheChildsOwnAnswer(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("the evidence page is up"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)

	landOnTheChat(t, agent, landing{
		title:   "build the evidence page",
		report:  "wrote the page and checked the links",
		result:  "EVIDENCE-BODY: the page lists all eleven receipts with their dates.",
		changed: []string{"evidence.html"},
	})

	select {
	case messages := <-asked:
		text := userTextIn(messages)
		for _, want := range []string{
			"A note from the session, not from the person",
			"task 1 done: build the evidence page",
			"wrote the page and checked the links",
			resultWholeLead,
			"EVIDENCE-BODY: the page lists all eleven receipts",
			"changed: evidence.html",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("the woken turn does not carry %q:\n%s", want, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a landed quick task never started a turn: the person gets a card and silence")
	}
}

// AND A LONG ANSWER IS CUT AT [taskResultCarry], WITH THE ADDRESS OF THE WHOLE
// OF IT. Saying "in full" over an excerpt is the defect the pair of leads exists
// to prevent.
func TestALongChildAnswerReachesTheChatCutAndSaysSo(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("read it"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)

	long := "HEAD-OF-THE-ANSWER " + strings.Repeat("evidence line. ", 600) + " TAIL-OF-THE-ANSWER"
	if len(long) <= taskResultCarry {
		t.Fatalf("the fixture is shorter than the carry (%d bytes)", len(long))
	}
	landOnTheChat(t, agent, landing{
		title:  "read the ledger",
		report: "read it and summarised",
		result: long,
	})

	select {
	case messages := <-asked:
		text := userTextIn(messages)
		if !strings.Contains(text, resultPartLead) {
			t.Fatalf("a cut answer did not say it was cut:\n%s", text)
		}
		if strings.Contains(text, resultWholeLead+":") {
			t.Fatal("a cut answer claimed to be whole")
		}
		if !strings.Contains(text, "HEAD-OF-THE-ANSWER") {
			t.Fatal("the beginning of the answer did not travel")
		}
		if strings.Contains(text, "TAIL-OF-THE-ANSWER") {
			t.Fatalf("the whole answer travelled: the %d-byte carry did not hold", taskResultCarry)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a landed task never started a turn")
	}
}

// ── C: how many turns a run of landings buys ────────────────────────────────

// FOUR LANDINGS ON AN IDLE CHAT ARE FOUR TURNS. There is no debounce and no
// batch: the rule is [Agent.wakeLocked]'s — a landing that finds no turn running
// starts one — so a chat that said "I will verify when they land" answers each
// one as it arrives.
func TestFourLandingsOnAnIdleChatAreFourSeparateAnswers(t *testing.T) {
	answered := make(chan string, 8)
	answer := func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		answered <- userTextIn(messages)
		return textResponse("that one is up"), nil
	}
	completer := &scriptedCompleter{steps: []step{answer, answer, answer, answer}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)

	for _, title := range []string{"the evidence page", "the contact page", "the pricing page", "the about page"} {
		landOnTheChat(t, agent, landing{title: title, report: "put up " + title})
		select {
		case text := <-answered:
			if !strings.Contains(text, "put up "+title) {
				t.Fatalf("the wake for %q carried the wrong landing:\n%s", title, text)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%q landed on an idle chat and nothing was said", title)
		}
		waitIdle(t, agent)
	}

	if count := conversationRequests(completer); count != 4 {
		t.Fatalf("four landings produced %d turns, want one answer per landing", count)
	}
}

// AND THE ONLY BATCHING THERE IS IS TURN OCCUPANCY. Three landings that arrive
// while the turn the FIRST one woke is still running are read by that turn at
// its next step boundary — one wake, one answer, all four outcomes in front of
// the model.
func TestLandingsInsideAWokenTurnAreAnsweredByThatSameTurn(t *testing.T) {
	var (
		inFlight = make(chan struct{})
		release  = make(chan struct{})
		woken    = make(chan []ai.Message, 4)
	)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(inFlight)
			<-release
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			woken <- messages
			return textResponse("all four are up"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)

	go landOnTheChat(t, agent, landing{title: "the evidence page", report: "put up the evidence page"})
	<-inFlight
	for _, title := range []string{"the contact page", "the pricing page", "the about page"} {
		landOnTheChat(t, agent, landing{title: title, report: "put up " + title})
	}
	close(release)

	select {
	case messages := <-woken:
		text := userTextIn(messages)
		for _, want := range []string{"the evidence page", "the contact page", "the pricing page", "the about page"} {
			if !strings.Contains(text, "put up "+want) {
				t.Fatalf("the one turn did not carry %q:\n%s", want, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("four landings in one window never produced an answer")
	}

	waitIdle(t, agent)
	if count := conversationRequests(completer); count != 2 {
		t.Fatalf("four landings in one window produced %d requests, want ONE wake's two steps", count)
	}
}

// ── D: the branch outcome is in the wake ────────────────────────────────────

// A WORKTREE TASK THAT CAME HOME SAYS SO, so the model can answer "it landed,
// and here is what changed" without going looking ([taskMergeNote]).
func TestAChatsWakeForALandedTaskCarriesTheMergeOutcome(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("it is on your branch"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, nil)

	landOnTheChat(t, agent, landing{
		title:   "port the parser",
		report:  "ported it and the tests pass",
		changed: []string{"parser.go", "parser_test.go"},
		branch:  "aforge/task-1-port-the-parser",
		merge:   mergeMerged,
	})

	select {
	case messages := <-asked:
		text := userTextIn(messages)
		for _, want := range []string{
			"task 1 done: port the parser",
			"changed: parser.go, parser_test.go",
			"its branch aforge/task-1-port-the-parser merged into yours",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("the woken turn cannot say what landed — it does not carry %q:\n%s", want, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a landed worktree task never started a turn")
	}
}

// AND A LANDING NOBODY COULD CHECK ASKS FOR THE DECISION rather than announcing
// one: the branch was kept, and the note names the verb that settles it.
func TestAChatsWakeForAYourCallLandingAsksForTheDecision(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("I would take it"), nil
		},
	}}
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskSettle = string(TaskSettleAsk)
	})

	landOnTheChat(t, agent, landing{
		title:   "port the parser",
		report:  "ported it; the check could not be run",
		changed: []string{"parser.go"},
		branch:  "aforge/task-1-port-the-parser",
		merge:   mergeKept,
		state:   TaskUnverified,
	})

	select {
	case messages := <-asked:
		text := userTextIn(messages)
		if !strings.Contains(text, "tasks id 1 resolve accept|reaudit|refute") {
			t.Fatalf("the your-call landing did not tell the model how to settle it:\n%s", text)
		}
		if !strings.Contains(text, "its branch is kept") {
			t.Fatalf("the your-call landing did not say the branch was kept:\n%s", text)
		}
		for _, banned := range []string{"verdict", "auditor", "refuted", "needs your look"} {
			if strings.Contains(text, banned) {
				t.Fatalf("the note carries machinery vocabulary %q:\n%s", banned, text)
			}
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a your-call landing never started a turn")
	}
}
