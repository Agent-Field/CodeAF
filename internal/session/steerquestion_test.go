package session

// The laws of a question the person talked past (steerquestion.go):
//
//	(a) the next request is on the wire within [lane.SpokenWithin] of the steer,
//	    and it carries the words they typed
//	(b) the model reads that nothing was chosen, and no decision is recorded
//	(c) the question comes off every surface, with their own act as the reason
//	(d) an ANSWERED question is untouched by any of it — the three endings of a
//	    parked ask stay three

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// askedGenres is a well-formed `ask` call: the one the measured conversation
// made before it wedged (steerquestion.go).
const askedGenres = `{"head":"Which painting genres do you like?","kind":"choice",` +
	`"reason":"the record does not say which they like and the work forks on it",` +
	`"options":[{"key":"1","label":"Landscapes"},{"key":"2","label":"Portraits"}],` +
	`"stakes":"reversible"}`

// questioningTurn is an interactive agent with the questions lane watched, which
// is the one rig every test here needs: the act under test can only happen while
// a question is open, and only a watcher can see it come back off.
type questioningTurn struct {
	agent *Agent
	asks  <-chan Event
	stop  func()
}

// askResult is what the `ask` call itself answered — the LAST tool message in
// the request rather than the first, because the load that put `ask` on the belt
// is a tool result too.
func askResult(messages []ai.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			return messageText(messages[i])
		}
	}
	return ""
}

func newQuestioningTurn(t *testing.T, completer *scriptedCompleter) *questioningTurn {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Interactive = true })
	asks, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	return &questioningTurn{agent: agent, asks: asks, stop: stop}
}

// waitForTheQuestion blocks until the `ask` tool is parked on the person, which
// is the only moment the act under test can happen.
func waitForTheQuestion(t *testing.T, agent *Agent) Question {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if open := agent.OpenQuestions(); len(open) > 0 {
			return open[0]
		}
		select {
		case <-deadline:
			t.Fatal("the `ask` tool never parked on a question")
		case <-time.After(time.Millisecond):
		}
	}
}

// askingSteps is the two steps every test here opens with: load the group the
// real belt shelves `ask` in, then ask.
func askingSteps() []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("", loadCapabilityToolName, `{"group":"`+questionsGroup+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("", "ask", askedGenres), nil
		},
	}
}

// ── (a) and (b) the wedge, and what the model reads instead ─────────────────

// THE MEASURED WEDGE, AS A TEST. Before steerquestion.go this did not merely
// take a minute: the eighth-second `select` in tools_ask.go had no arm for it
// and NO REQUEST WAS EVER MADE. The turn waited for the question, the question
// waited for the person, and the person had already spoken.
func TestASteerAtAnOpenQuestionPutsTheNextRequestOnTheWireAtOnce(t *testing.T) {
	next := make(chan time.Time, 1)
	var carried []string
	var toolSaid string
	steps := append(askingSteps(), func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		select {
		case next <- time.Now():
		default:
		}
		carried = userLines(messages)
		toolSaid = askResult(messages)
		return textResponse("portraits it is"), nil
	})
	rig := newQuestioningTurn(t, &scriptedCompleter{steps: steps})

	turn := mustSubmit(t, rig.agent, "ask me about painting")
	waitForTheQuestion(t, rig.agent)

	steeredAt := time.Now()
	steer := mustSteer(t, rig.agent, "portrait")

	select {
	case at := <-next:
		// THE WHOLE OF WHAT THIS LANE PROMISES. A person's own sentence reaches
		// the model inside the allowance every wait in the request path is held
		// to, rather than behind a tool that is waiting for that same sentence.
		if waited := at.Sub(steeredAt); waited > lanes.SpokenWithin {
			t.Fatalf("the request after the steer went out %s later, want within %s", waited, lanes.SpokenWithin)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("no request followed the steer: the turn is still parked on a question the person spoke past")
	}

	fromSteer := collect(t, steer)
	collect(t, turn)

	// It rides the SAME turn, as every other splice does: the question, the
	// work, then the correction.
	if want := []string{"ask me about painting", "portrait"}; !equalStrings(carried, want) {
		t.Fatalf("the request carried %v, want %v", carried, want)
	}
	// AND THE MODEL IS TOLD NOTHING WAS CHOSEN. A tool result that looked like
	// an answer would put a pick in front of every later turn that the person
	// never made.
	if !strings.Contains(toolSaid, "did not answer this question") {
		t.Fatalf("the `ask` result reads %q, want the sentence saying nobody answered", toolSaid)
	}
	if decisions := rig.agent.Decisions(); len(decisions) != 0 {
		t.Fatalf("a question nobody answered was recorded as a decision: %+v", decisions)
	}
	// The person watching their own correction is told it landed and where.
	if got, want := steerEvents(fromSteer), []string{"accepted:portrait", "consumed:portrait"}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	for _, event := range fromSteer {
		if event.Kind == EventSteerAccepted && event.Steer.Landing != steerTookTheQuestion {
			t.Fatalf("the landing reads %q, want %q", event.Steer.Landing, steerTookTheQuestion)
		}
	}
}

// ── (c) the question comes off the surfaces, in the person's own words ──────

func TestAQuestionThePersonSpokePastIsWithdrawnWithTheirOwnAct(t *testing.T) {
	steps := append(askingSteps(), func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("portraits it is"), nil
	})
	rig := newQuestioningTurn(t, &scriptedCompleter{steps: steps})

	turn := mustSubmit(t, rig.agent, "ask me about painting")
	waitForTheQuestion(t, rig.agent)
	mustSteer(t, rig.agent, "portrait")

	gone := waitForAsk(t, rig.asks, EventQuestionWithdrawn)
	if gone.Question.Kind != QuestionAsk {
		t.Fatalf("the withdrawal names a %v, want the model's own question", gone.Question.Kind)
	}
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason != askTalkedPastReason {
		t.Fatalf("the withdrawal reads %+v, want %q", gone.Question.Withdrawn, askTalkedPastReason)
	}
	collect(t, turn)
	// AND IT IS SAID ONCE, WHICH IS WHAT MAKES THE SENTENCE A CLAIM. Two maps
	// hold one question, and the `ask` lane's own deferred withdrawal is racing
	// this one with [questionGoneReason]'s `the turn moved on without it` — the
	// consent line's fact, about the person's own act. Both are claimed under one
	// lock (steerquestion.go), so the loser finds nothing and there is no second
	// event to read. Without that this assertion was a coin flip that happened to
	// land: `waitForAsk` returns the FIRST withdrawal, whichever road won.
	select {
	case event := <-rig.asks:
		if event.Kind == EventQuestionWithdrawn {
			t.Fatalf("the question was withdrawn twice; the second reads %+v", event.Question.Withdrawn)
		}
	case <-time.After(200 * time.Millisecond):
	}
	// AND THE SESSION IS NOT PARKED ON ANYBODY ANY MORE, which is the fact the
	// presence file publishes to every other window (taskpresence.go).
	if open := rig.agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("the question is still open after the turn ended: %+v", open)
	}
}

// ── (d) the three endings stay three ────────────────────────────────────────

// AN ANSWER IS STILL AN ANSWER. The talked-past ending is a third road beside
// the two that were already there and takes nothing from either: a question the
// person answers resolves exactly as it did, with the whole answer in the tool
// result and the decision on the record.
func TestAnAnsweredQuestionIsUntouchedByTheTalkedPastRoad(t *testing.T) {
	var toolSaid string
	steps := append(askingSteps(), func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		toolSaid = askResult(messages)
		return textResponse("landscapes then"), nil
	})
	rig := newQuestioningTurn(t, &scriptedCompleter{steps: steps})

	turn := mustSubmit(t, rig.agent, "ask me about painting")
	question := waitForTheQuestion(t, rig.agent)
	if err := rig.agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: question.ID, Key: "1", Picked: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	collect(t, turn)

	if strings.Contains(toolSaid, "did not answer") {
		t.Fatalf("an answered question was reported as spoken past: %q", toolSaid)
	}
	if !strings.Contains(toolSaid, `"key":"1"`) {
		t.Fatalf("the answer did not reach the model: %q", toolSaid)
	}
}

// ── what the paragraph predicts ─────────────────────────────────────────────

// EVERY QUESTION STANDING AT ONCE COMES DOWN. A sentence is the person's answer
// to what is in front of them, which is the sheet and not one row of it — so a
// turn parked on two questions is released by one sentence, and neither is left
// waiting for a person who has moved on.
func TestOneSentenceRetiresEveryQuestionStandingAtOnce(t *testing.T) {
	steps := []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("", loadCapabilityToolName, `{"group":"`+questionsGroup+`"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("", "ask", askedGenres)
			second := toolResponse("", "ask", strings.Replace(askedGenres, "painting genres", "picture sizes", 1))
			response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls,
				second.Choices[0].Message.ToolCalls[0])
			response.Choices[0].Message.ToolCalls[1].ID = "second-ask"
			return response, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("both taken"), nil
		},
	}
	rig := newQuestioningTurn(t, &scriptedCompleter{steps: steps})

	turn := mustSubmit(t, rig.agent, "ask me two things")
	deadline := time.After(5 * time.Second)
	for len(rig.agent.OpenQuestions()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("only %d question(s) ever opened", len(rig.agent.OpenQuestions()))
		case <-time.After(time.Millisecond):
		}
	}

	mustSteer(t, rig.agent, "portrait, and make them small")
	collect(t, turn)

	if open := rig.agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("a sentence left %d question(s) standing: %+v", len(open), open)
	}
}

// ── the sentence is a claim, not a race ─────────────────────────────────────

// TWO MAPS HOLD ONE QUESTION and this is the law that they are taken together.
//
// The assertion above that the withdrawal is said once and says the right thing
// is true but PROBABILISTIC: closing the channel makes the `ask` lane runnable,
// and its own deferred withdrawal ([Agent.rememberQuestion]) carries
// [questionGoneReason]'s `the turn moved on without it` — the consent line's
// fact, about the person's own act. `WithdrawQuestion` is claim-or-nothing, so
// whichever road reached the word-book first decided what the person read, and a
// test that waits for the first event asserts a coin flip.
//
// So this one holds the claim directly and with no goroutine in it: after the
// talked-past ending has run under a.mu, the words are ALREADY off the book, and
// the road that arrives second provably finds nothing and says nothing.
func TestTalkingPastAQuestionClaimsItsWordsWithItsWait(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Interactive = true })
	asks, stop := agent.WatchQuestions()
	t.Cleanup(stop)

	question := Question{ID: 1, Kind: QuestionAsk, Asker: Asker{Kind: AskerModel}, Head: "Which painting genres do you like?"}
	agent.mu.Lock()
	agent.asked.parkLocked(question.ID)
	agent.mu.Unlock()
	forget := agent.rememberQuestion(question)

	agent.mu.Lock()
	taken := agent.talkedPastQuestionsLocked()
	// THE BOOK IS ALREADY EMPTY WHILE THE LOCK IS STILL HELD, which is the whole
	// law: there is no window in which the other road could win.
	if _, said := agent.claimQuestionLocked(QuestionAsk, question.Token(), true); said {
		agent.mu.Unlock()
		t.Fatal("the words are still on the book after the wait was ended: the `ask` lane's own withdrawal can still win")
	}
	agent.mu.Unlock()

	if len(taken) != 1 || taken[0].ID != question.ID {
		t.Fatalf("the ending answered %+v, want the one question it retired", taken)
	}
	agent.sayTheQuestionsCameDown(taken)
	gone := waitForAsk(t, asks, EventQuestionWithdrawn)
	if gone.Question.Withdrawn == nil || gone.Question.Withdrawn.Reason != askTalkedPastReason {
		t.Fatalf("the withdrawal reads %+v, want %q", gone.Question.Withdrawn, askTalkedPastReason)
	}

	// And the losing road — the `ask` lane returning and running its own defer —
	// says nothing at all.
	forget()
	select {
	case event := <-asks:
		if event.Kind == EventQuestionWithdrawn {
			t.Fatalf("the lane's own withdrawal still spoke, and it reads %+v", event.Question.Withdrawn)
		}
	case <-time.After(200 * time.Millisecond):
	}
}
