package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func deliveryQuestion(id uint64, ask session.AskKind, blocking bool) session.Question {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	return session.Question{
		ID: id, Kind: session.QuestionTask, Ask: ask, Form: session.FormCard,
		Head: "Choose the index", Reason: "the build needs one", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}, {Key: "2", Label: "memory"}},
		Pick:    &session.Pick{Key: "1", Reason: "it survives a restart"}, Asked: now,
		Blocking: session.Blocking{Turn: blocking},
	}
}

func TestAQuietQuestionWaitsForItsStepAndABlockingOneDoesNot(t *testing.T) {
	d := newQuestionDeliveryRule()
	now := time.Date(2026, 9, 9, 12, 1, 0, 0, time.UTC)
	if got := d.deliver(deliveryQuestion(1, session.AskChoice, false), "step:7", questionOnPage, now); got.Pin != nil {
		t.Fatalf("a quiet question escaped its step: %#v", got)
	}
	if got := d.deliver(deliveryQuestion(2, session.AskPermission, true), "step:7", questionOnPage, now); got.Pin == nil {
		t.Fatal("a blocking question was held for the boundary")
	}
	held := d.boundary("step:7")
	if len(held) != 1 || held[0].ID != 1 {
		t.Fatalf("boundary = %#v, want only the quiet question", held)
	}
	if again := d.boundary("step:7"); len(again) != 0 {
		t.Fatalf("the boundary handed its batch over twice: %#v", again)
	}
}

func TestAQuestionOnAnotherPageIsPinnedAndSaysWhereItWent(t *testing.T) {
	d := newQuestionDeliveryRule()
	got := d.deliver(deliveryQuestion(1, session.AskChoice, true), "", questionOtherPage, time.Now())
	if got.Pin == nil {
		t.Fatal("a question on another page never reached the block")
	}
	want := "Choose the index · waiting in this conversation · " + questionChipKey
	if got.Note != want {
		t.Fatalf("note = %q, want %q", got.Note, want)
	}
}

func TestAwayTakesOnlyAMatureSafePickAndRingsOnce(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 20, 0, 0, time.UTC)
	q := deliveryQuestion(1, session.AskChoice, true)
	q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: time.Minute}
	d := newQuestionDeliveryRule()
	got := d.deliver(q, "", questionAway, now)
	if got.Answer == nil || got.Answer.DecidedBy != session.DecidedByDial || got.Answer.Key != "1" {
		t.Fatalf("the rule did not take a mature reversible pick: %#v", got)
	}
	// A clarification is never taken by a clock, however mature the rule is.
	q.ID, q.Ask = 2, session.AskClarification
	got = d.deliver(q, "", questionAway, now)
	if got.Answer != nil || !got.Phone || !got.Bell {
		t.Fatalf("clarification away = %#v, want the phone and one bell", got)
	}
	if got = d.deliver(q, "", questionAway, now.Add(time.Second)); got.Bell {
		t.Fatal("the same blocking question rang twice")
	}
}

func TestAnIrreversibleQuestionIsNeverTakenWhileNobodyIsThere(t *testing.T) {
	q := deliveryQuestion(1, session.AskChoice, true)
	q.Stakes = session.StakesIrreversible
	q.Policy = session.Policy{Kind: session.PolicyDecide}
	d := newQuestionDeliveryRule()
	if got := d.deliver(q, "", questionAway, time.Now()); got.Answer != nil {
		t.Fatalf("irreversible stakes were decided by the rule: %#v", got.Answer)
	}
}

func TestAWithdrawnQuestionLeavesTheStepItWasGatheringIn(t *testing.T) {
	d := newQuestionDeliveryRule()
	q := deliveryQuestion(1, session.AskChoice, false)
	_ = d.deliver(q, "step:1", questionOnPage, time.Now())
	q.Withdrawn = &session.Withdrawal{Reason: "the plan changed"}
	d.forget(q)
	if held := d.boundary("step:1"); len(held) != 0 {
		t.Fatalf("the boundary released a withdrawn question: %#v", held)
	}
}

func TestTheWithdrawnLineSaysWhatStoppedNeedingAnAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	q := deliveryQuestion(1, session.AskChoice, false)
	a.raiseQuestion(questionShown{question: q})
	q.Withdrawn = &session.Withdrawal{Reason: "the plan changed"}
	a.withdrawQuestion(q, q.Withdrawn.Reason)
	text := ansi.Strip(strings.Join(a.questionRows(80), "\n"))
	if !strings.Contains(text, "Choose the index — no longer needed · the plan changed") {
		t.Fatalf("withdrawn block:\n%s", text)
	}
	if a.questionCount() != 0 {
		t.Fatalf("the chip still counts %d", a.questionCount())
	}
}

// deliveryApp is a window with the question doors under it and a keyboard that
// was last touched `since` ago.
func deliveryApp(t *testing.T, since time.Duration) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{})
	a.lastQuestionKey = a.now().Add(-since)
	return a
}

func TestAQuestionOnThePagePinsItselfAndSaysNothingElsewhere(t *testing.T) {
	a := deliveryApp(t, time.Second)
	a.deliverQuestion(deliveryQuestion(1, session.AskChoice, true))
	if a.questionCount() != 1 {
		t.Fatalf("the block holds %d questions", a.questionCount())
	}
	if a.pageMsg != "" {
		t.Fatalf("a question on the page also wrote to a place's line: %q", a.pageMsg)
	}
}

func TestAWindowNobodyHasTouchedIsAway(t *testing.T) {
	a := deliveryApp(t, awayAfter+time.Minute)
	if got := a.questionPresenceNow(); got != questionAway {
		t.Fatalf("presence = %v, want away", got)
	}
	a.lastQuestionKey = a.now()
	if got := a.questionPresenceNow(); got != questionOnPage {
		t.Fatalf("presence after a key = %v, want on the page", got)
	}
}

func TestAnotherWindowsAnswerIsNotDrawnAsYours(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	q := deliveryQuestion(1, session.AskChoice, true)
	a.raiseQuestion(questionShown{question: q})
	a.foldOthersAnswer(q, session.Answer{
		At: a.now(), Kind: q.Kind, ID: q.ID, Ask: q.Ask,
		Key: "2", Picked: []string{"2"}, DecidedBy: session.DecidedByPerson,
	})
	text := ansi.Strip(strings.Join(a.questionRows(120), "\n"))
	if !strings.Contains(text, "another window") {
		t.Fatalf("the receipt did not say who answered:\n%s", text)
	}
	if strings.Contains(text, "· you ·") {
		t.Fatalf("the receipt claimed a key this window never pressed:\n%s", text)
	}
	if a.questionCount() != 0 {
		t.Fatalf("the question stayed open after somebody else answered it")
	}
}

func TestTwoWindowsAnsweringDifferentlyAreShownAndNotMerged(t *testing.T) {
	a := newTestApp(&questionScript{fakeAgent: &fakeAgent{}})
	q := deliveryQuestion(1, session.AskChoice, true)
	a.raiseQuestion(questionShown{question: q})
	head, _ := a.questionHead()
	a.answerQuestion(head, session.Answer{Key: "1", Picked: []string{"1"}})
	a.foldOthersAnswer(q, session.Answer{
		At: a.now(), Kind: q.Kind, ID: q.ID, Ask: q.Ask,
		Key: "2", Picked: []string{"2"}, DecidedBy: session.DecidedByPerson,
	})
	text := ansi.Strip(strings.Join(a.questionRows(120), "\n"))
	if !strings.Contains(text, "sqlite") || !strings.Contains(text, "memory") {
		t.Fatalf("only one of the two answers is on screen:\n%s", text)
	}
	if !strings.Contains(plain(lastNote(t, a)), questionRaceWord) {
		t.Fatalf("nothing said which answer counted")
	}
}

func TestAnAnswerThisWindowAlreadyGaveIsNotDrawnTwice(t *testing.T) {
	a := newTestApp(&questionScript{fakeAgent: &fakeAgent{}})
	q := deliveryQuestion(1, session.AskChoice, true)
	a.raiseQuestion(questionShown{question: q})
	head, _ := a.questionHead()
	a.answerQuestion(head, session.Answer{Key: "1", Picked: []string{"1"}})
	before := len(a.questionRecords)
	a.foldOthersAnswer(q, session.Answer{
		At: a.now(), Kind: q.Kind, ID: q.ID, Ask: q.Ask,
		Key: "1", Picked: []string{"1"}, DecidedBy: session.DecidedByPerson,
	})
	if len(a.questionRecords) != before {
		t.Fatalf("the lane's echo of this window's own answer wrote a second row")
	}
}

// TestAQuestionHoldingItsOwnStepOpenIsStillReleased is the deadlock the gather
// clock exists to prevent: the `ask` tool blocks its turn until it is answered,
// so a question held for "the model speaking again" would be held until the
// model speaks, which cannot happen until the question is answered.
func TestAQuestionHoldingItsOwnStepOpenIsStillReleased(t *testing.T) {
	d := newQuestionDeliveryRule()
	got := d.deliver(deliveryQuestion(1, session.AskPermission, false), "step:1", questionOnPage, time.Now())
	if !got.Gather {
		t.Fatal("a held question did not ask for the boundary's clock")
	}
	if got.Pin != nil {
		t.Fatal("a held question was drawn anyway")
	}
	// The clock's own release names the step it was armed for, and hands the
	// batch over exactly once.
	if held := d.boundary("step:1"); len(held) != 1 {
		t.Fatalf("the clock released %d questions, want the one that was held", len(held))
	}
}

// TestHomeAnswersALaneItHasNoOlderPathFor is the reach law from home's side:
// the whole question rides in the presence file, so a lane home never learnt
// about is still answerable there.
func TestHomeAnswersALaneItHasNoOlderPathFor(t *testing.T) {
	script := &questionScript{fakeAgent: &fakeAgent{}}
	var gave []session.Answer
	script.answer = func(answer session.Answer) error {
		gave = append(gave, answer)
		return nil
	}
	a := newTestApp(script)
	whole := deliveryQuestion(9, session.AskChoice, true)
	whole.Kind = session.QuestionAsk
	presence := session.PresenceQuestion{
		Kind: whole.Kind, ID: whole.ID, Text: whole.Head,
		Options: whole.Options, Full: &whole,
	}
	if _, took := a.answerWholeQuestion(presence, "2"); !took {
		t.Fatal("home would not answer a lane it has no older path for")
	}
	if len(gave) != 1 || gave[0].Key != "2" || gave[0].Kind != session.QuestionAsk {
		t.Fatalf("the one door was handed %#v", gave)
	}
	// A KEY THAT QUESTION NEVER OFFERED IS NOT AN ANSWER TO IT.
	if _, took := a.answerWholeQuestion(presence, "7"); took {
		t.Fatal("home answered with a key the question never offered")
	}
}
