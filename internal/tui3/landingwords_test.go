package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The words the ENGINE spells and this surface only draws (internal/session's
// task_status.go). They are written out HERE, in the tests, so a change to
// either side has to be made deliberately in both: the surface itself must never
// hold a second copy of a state word (docs/design/task-states/DESIGN.md).
const (
	taskYourCallWord    = "your call"
	taskDoneStateWord   = "done"
	taskStoppedState    = "stopped"
	taskIncompleteState = "incomplete"
	askCheckReason      = "nobody could check it"
	askHeldReason       = "the check did not pass it"
	askConflictReason   = "conflicts with your branch"
	askGroundReason     = "your folder already has files the task wrote"
)

// doneStatus is one landing card's reading built the way the surface builds it
// (taskdone.go's [doneNodeFacts]): a node this window watched, read once through
// [session.ProjectTask].
func doneStatus(facts session.TaskFacts) session.TaskStatus {
	facts.Liveness = session.TaskLivenessHeld
	return session.ProjectTask(facts)
}

// ── the landing, as a question ──────────────────────────────────────────────
//
// The landed `your call` is answered on the question block now (tasksettle.go),
// so a test that used to land a card and press a letter has to put the QUESTION
// in front of the surface as well as the row. These two do both, through the
// same shapes the engine sends: the task notice on the roster's lane, and the
// landing question on the questions lane.

// settleCall is one answer the fake engine was handed, in the engine's own
// vocabulary.
type settleCall struct {
	id     uint64
	answer session.TaskResolution
}

// settleFake is a tasker that also holds the questions lane and applies a
// landing answer the way the engine does (session's applyLanding): the key's
// meaning comes from the ONE mapping and never from a second table here.
//
// IT HAS NO MERGE ROUND, on purpose: a conflict's `[a]` spends
// [session.Agent.ResolveConflict], which is a capability of its own, and this
// fake is what a surface without it looks like.
type settleFake struct {
	*taskFake
	open     []session.Question
	lane     chan session.Event
	resolved []settleCall
	handed   []uint64
	back     []uint64
	merged   []uint64
	refuse   error
	// at is the app's clock, so a helper that raises a question can step past the
	// block's settle guard without sleeping (question.go's [questionSettle]).
	at *time.Time
}

func (f *settleFake) OpenQuestions() []session.Question { return f.open }

func (f *settleFake) WatchQuestions() (<-chan session.Event, func()) {
	if f.lane == nil {
		f.lane = make(chan session.Event, 8)
	}
	return f.lane, func() {}
}

func (f *settleFake) ResolveQuestion(answer session.Answer) error {
	switch answer.Kind {
	case session.QuestionLanding, session.QuestionConflict:
		if f.refuse != nil {
			return f.refuse
		}
		switch answer.FirstKey() {
		case session.LandingYesKey:
			if answer.Kind == session.QuestionConflict {
				f.merged = append(f.merged, answer.ID)
				return nil
			}
			f.resolved = append(f.resolved, settleCall{id: answer.ID, answer: session.TaskAccept})
		case session.LandingNoKey:
			f.resolved = append(f.resolved, settleCall{id: answer.ID, answer: session.TaskRefute})
		case session.LandingDecideKey:
			f.handed = append(f.handed, answer.ID)
		case session.LandingTakeBackKey:
			f.back = append(f.back, answer.ID)
		}
		return nil
	}
	return f.taskFake.ResolveQuestion(answer)
}

// settleApp is [taskApp] with the questions lane under it and a profile of its
// own, so that any write a test provokes lands in a temporary directory and
// never in the person running the suite.
func settleApp(t *testing.T) (*app, *settleFake) {
	t.Helper()
	agent := &settleFake{taskFake: &taskFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		updates:   make(chan session.Event, 8),
	}}
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	agent.at = &now
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	a.profileDir = t.TempDir()
	a.clock = func() time.Time { return *agent.at }
	// SOMEBODY IS AT THIS KEYBOARD. Delivery is presence-aware and the absence
	// boundary is measured from the last key (questiondelivery.go's [awayAfter]),
	// so a lab with a pinned clock and no keystroke behind it is a window nobody
	// is at — and its questions would go to the phone rather than to the block.
	a.lastQuestionKey = now
	return a, agent
}

// landUnverified puts one landed node that nobody could check in front of the
// person — the card AND the question — and answers with it.
func landUnverified(t *testing.T, a *app) *taskDone {
	t.Helper()
	return landAsk(t, a, session.TaskNotice{
		Elapsed: 400 * time.Second, Merge: mergeWordAborted, Branch: "task/parser",
	}, askCheckReason, "accept", "not right")
}

// landAsk lands one node as the person's call under whatever facts a test names,
// and raises the landing question the engine would raise beside it.
func landAsk(t *testing.T, a *app, notice session.TaskNotice, reason, yes, no string) *taskDone {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(7, "Port the parser", session.TaskUnverified, notice)})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("no card that is the person's call landed: %+v", card)
	}
	kind := session.QuestionLanding
	if reason == askConflictReason || reason == askGroundReason {
		kind = session.QuestionConflict
	}
	q := landingAsk(7, kind, "Port the parser", reason, yes, no)
	q.Asked = a.now()
	a.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	// DRAWN, THEN AGED PAST THE SETTLE GUARD. The guard is a claim about the
	// screen — a key that arrived before the question had been on it was aimed at
	// whatever was there before (question.go) — so a test buys its way past by
	// drawing a frame and moving the clock, never by sleeping.
	a.questionRows(a.width)
	if fake, ok := a.agent.(*settleFake); ok && fake.at != nil {
		*fake.at = fake.at.Add(2 * questionSettle)
	}
	return card
}
