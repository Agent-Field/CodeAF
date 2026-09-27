package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// countdownTaskAgent gives the task fixture a question subscription too, so a
// bare engine replay reaches the same replacement seam as a live connection.
type countdownTaskAgent struct {
	*taskFake
	questions chan session.Event
}

func (a *countdownTaskAgent) OpenQuestions() []session.Question { return nil }

func (a *countdownTaskAgent) WatchQuestions() (<-chan session.Event, func()) {
	return a.questions, func() {}
}

// A dropped digit must not look like an accepted pause, and it must never be
// saved up to answer the proposal after the settle guard expires.
func TestAnEarlyProposalDigitLeavesTheCountdownRunning(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	ev := proposal(a, 7, 15*time.Second)
	ev.Task.Program = "senior-dev"
	drive(t, a, streamEventMsg{gen: a.gen, ev: ev})
	taskAsk(a)
	advance(questionSettle / 2)
	before := taskAsk(a)
	drive(t, a, key("2"))
	if len(agent.held) != 0 || !a.task.deadline.Equal(ev.Task.Deadline) {
		t.Fatalf("the dropped digit held the proposal: holds=%v deadline=%v", agent.held, a.task.deadline)
	}
	if got := taskAsk(a); got != before {
		t.Fatalf("the dropped digit changed the card:\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if len(agent.answered) != 0 || len(a.questionRecords) != 0 {
		t.Fatalf("the dropped digit recorded an answer: %+v", agent.answered)
	}
	advance(time.Second)
	drive(t, a, frameMsg{})
	if got := taskAsk(a); !strings.Contains(got, "start it in 14s") {
		t.Fatalf("the clock stopped advancing after the dropped digit:\n%s", got)
	}
	if len(agent.answered) != 0 || len(a.questionRecords) != 0 {
		t.Fatal("the dropped digit was deferred until the guard expired")
	}
	drive(t, a, key("2"), key("enter"))
	if len(agent.answered) != 1 || agent.answered[0].id != 7 || agent.answered[0].answer.Approved || agent.answered[0].answer.Redirect != "" {
		t.Fatalf("2 then enter did not decline exactly once: %+v", agent.answered)
	}
	if len(agent.held) != 1 || agent.held[0] != 7 || a.awaitingTask() {
		t.Fatalf("the accepted digit did not hold and settle the proposal: holds=%v awaiting=%v", agent.held, a.awaitingTask())
	}
	if got := taskAsk(a); !strings.Contains(got, "→ "+taskAnswerWord(false)+" · you") {
		t.Fatalf("the declined proposal left no matching receipt:\n%s", got)
	}
}

// Both proposal events and bare question replays arrive while a card remains
// visible. Neither may reopen the guard on the same question token.
func TestAReplayedCountdownProposalKeepsItsSettleStamp(t *testing.T) {
	a, agent, advance := taskApp(t)
	a.agent = &countdownTaskAgent{taskFake: agent, questions: make(chan session.Event)}
	agent.pending = []uint64{7}
	ev := proposal(a, 7, 15*time.Second)
	drive(t, a, streamEventMsg{gen: a.gen, ev: ev})
	taskAsk(a)
	shown := a.questions[0].shown
	if shown.IsZero() {
		t.Fatal("the first draw did not stamp the question")
	}
	advance(questionSettle + time.Millisecond)
	q := a.taskQuestion(ev.Task)
	for _, replay := range []session.Event{ev, {Kind: session.EventQuestion, Question: &q}} {
		drive(t, a, streamEventMsg{gen: a.gen, ev: replay})
		taskAsk(a)
		if len(a.questions) != 1 || !a.questions[0].shown.Equal(shown) || !a.questionSettled(a.questions[0]) {
			t.Fatal("a replay or redraw reopened the settle guard")
		}
	}
	drive(t, a, key("2"), key("enter"))
	if len(agent.answered) != 1 || agent.answered[0].answer.Approved {
		t.Fatalf("the replayed proposal dropped the decline: %+v", agent.answered)
	}
}

// Sets have their own navigation and grouped-answer routes, but a dropped key
// carries no evidence of a person reading those questions either.
func TestAnEarlyQuestionSetKeyLeavesReadingClocksRunning(t *testing.T) {
	for _, mode := range []string{"tabs", "review", "group"} {
		t.Run(mode, func(t *testing.T) {
			lab := newQuestionLab(t)
			holds := 0
			for id := uint64(1); id <= 2; id++ {
				q := setQuestion(id, "step:4", "Which storage?")
				if mode == "group" {
					q = setPermission(lab, id, "step:4", "read", "go.mod", session.StakesReversible)
				}
				lab.a.raiseQuestion(questionShown{
					question: q, clockAt: lab.at, clockFor: time.Minute,
					held: func() { holds++ },
				})
			}
			set := lab.a.questionSet()
			if mode == "review" {
				lab.a.questionSetNow(set).focus = questionReviewTab
			}
			if (mode == "group") != lab.a.questionGrouped(set) {
				t.Fatal("the fixture did not select the intended set route")
			}
			before := lab.plain()
			key := "down"
			if mode == "tabs" {
				key = "right"
			}
			if !lab.press(key) {
				t.Fatal("the early key escaped the guard")
			}
			if holds != 0 || len(lab.answer) != 0 {
				t.Fatalf("the early key reached a clock or resolver: holds=%d answers=%v", holds, lab.answer)
			}
			for _, q := range lab.a.questions {
				if q.clockHeld {
					t.Fatal("the early key held a reading clock")
				}
			}
			if got := lab.plain(); got != before {
				t.Fatalf("the early key changed the set:\nbefore:\n%s\nafter:\n%s", before, got)
			}
			lab.tick(questionSettle)
			lab.press(key)
			if holds != 2 {
				t.Fatalf("the settled key held %d clocks, want 2", holds)
			}
		})
	}
}
