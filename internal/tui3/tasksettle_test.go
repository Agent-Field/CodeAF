package tui3

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DECISION, ANSWERED FROM THE CARD.
//
// Everything here is about one landing: a task that finished with nobody able
// to say whether it holds. It used to be a card with a question printed on it
// and no way to answer, and every assertion below is about the two halves of
// fixing that — the answers are on the card, and pressing one spends the
// person's answer through the engine's own door.

// settleFake is a tasker that can also be answered (tasksettle.go's
// [settleAgent]).
type settleFake struct {
	*taskFake
	resolved []settleCall
	handed   []uint64
	refuse   error
}

type settleCall struct {
	id     uint64
	answer session.TaskResolution
}

func (f *settleFake) ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.resolved = append(f.resolved, settleCall{id: id, answer: resolution})
	return nil
}

func (f *settleFake) HandUnverifiedToModel(id uint64) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.handed = append(f.handed, id)
	return nil
}

// settleApp is [taskApp] with a resolver under it and a profile of its own, so
// that the fourth choice's write lands in a temporary directory and never in
// the person running the suite.
func settleApp(t *testing.T) (*app, *settleFake) {
	t.Helper()
	agent := &settleFake{taskFake: &taskFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		updates:   make(chan session.Event, 8),
	}}
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	a.profileDir = t.TempDir()
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, agent
}

// landUnverified puts one landed card that needs a look in front of the person,
// and answers with it.
func landUnverified(t *testing.T, a *app) *taskDone {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{
			Elapsed: 400 * time.Second, Merge: mergeWordAborted, Branch: "task/parser",
			Report: "finished, but needs your look — the checker answered neither way",
		})})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || !card.unverified {
		t.Fatalf("no card that needs a look landed: %+v", card)
	}
	return card
}

// THE CARD ASKS, AND IT SAYS WHAT IT IS ASKING. The head and the outcome line
// say what happened; the two rows under them say that finishing the looking is
// somebody's job and which four presses do it.
func TestALandingThatNeedsALookOffersItsAnswers(t *testing.T) {
	a, _ := settleApp(t)
	landUnverified(t, a)

	text := taskText(a)
	for _, want := range []string{
		settleAskWord,
		settleTakeKey + settleTakeWord,
		settleAgainKey + settleAgainWord,
		settleNotRightKey + settleNotRightWord,
		settleAlwaysKey + settleAlwaysWord,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card is missing %q:\n%s", want, text)
		}
	}
	// THE VOCABULARY LAW HOLDS ON THE ROW THAT SPENDS THE ENGINE'S VERBS. A
	// person is choosing between taking work, having it looked at again, and
	// saying it is not finished — never between a verdict and a refutation.
	for _, never := range []string{"reaudit", "refute", "verdict", "auditor", "unverified"} {
		if strings.Contains(text, never) {
			t.Fatalf("the answers row says %q, which is machinery:\n%s", never, text)
		}
	}
}

// AND AN ORDINARY LANDING OFFERS NOTHING. There is nothing to decide about work
// that came home, so a card that drew four choices under it would be asking a
// question nobody has.
func TestADoneCardHasNoAnswersRow(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})})
	if text := taskText(a); strings.Contains(text, settleAskWord) {
		t.Fatalf("a finished card asks to be decided about:\n%s", text)
	}
}

// AND SO DOES A SURFACE WITH NOTHING BEHIND THE CHOICES. The absence law: a
// capability that cannot work is absent rather than drawn and broken, so a
// surface whose agent has no resolver draws the card exactly as it always did.
func TestWithNoResolverTheCardDoesNotOfferChoices(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Elapsed: time.Second})})
	if text := taskText(a); strings.Contains(text, settleAskWord) {
		t.Fatalf("a surface that cannot answer offered to:\n%s", text)
	}
}

// PRESSING ACCEPT SPENDS THE ANSWER, and the card stops asking.
func TestAcceptingFromTheCardResolvesTheTask(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleTake)

	if len(agent.resolved) != 1 || agent.resolved[0].id != 7 ||
		agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the accept reached the engine as %+v", agent.resolved)
	}
	// THE LANDING ABOVE THE CHOICES IS NOT REWRITTEN. The head is a record of how
	// this work came home — the branch it kept included — and a surface that
	// re-lettered it "done" would be reporting a merge nobody told it about. The
	// engine's own second card says what became of the work.
	if !card.unverified || card.failed {
		t.Fatalf("the card's landing was rewritten: %+v", card)
	}
	if card.decided != settleTookLine {
		t.Fatalf("the card's receipt reads %q, want %q", card.decided, settleTookLine)
	}
	text := taskText(a)
	if !strings.Contains(text, settleTookLine) {
		t.Fatalf("the card does not say what was decided:\n%s", text)
	}
	// DECIDED MEANS THE CONTROLS ARE GONE, not greyed: the question and every
	// answer to it leave the card.
	for _, gone := range []string{settleAskWord, settleTakeKey + settleTakeWord, settleAlwaysWord} {
		if strings.Contains(text, gone) {
			t.Fatalf("an answered card still offers %q:\n%s", gone, text)
		}
	}
	// And a second press cannot double-spend it.
	a.settleCard(card, settleNotRight)
	if len(agent.resolved) != 1 {
		t.Fatalf("an answered card was answered again: %+v", agent.resolved)
	}
}

// "NOT RIGHT" IS THE REFUSAL, and the card wears the failure it just became.
func TestSayingItIsNotRightFailsTheTask(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleNotRight)

	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskRefute {
		t.Fatalf("the refusal reached the engine as %+v", agent.resolved)
	}
	if !card.unverified || card.failed {
		t.Fatalf("the card's landing was rewritten: %+v", card)
	}
	if card.decided != settleNotRightLine {
		t.Fatalf("the receipt reads %q", card.decided)
	}
}

// "LOOK AGAIN" IS NOT A SETTLE. A fresh check runs on its own and answers
// minutes later, so the node genuinely still needs a look — what goes away is
// the choices, because it has been answered.
func TestLookingAgainLeavesTheCardWaiting(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleAgain)

	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskReaudit {
		t.Fatalf("the re-check reached the engine as %+v", agent.resolved)
	}
	if !card.unverified || card.failed {
		t.Fatalf("the card stopped needing a look while a check was still running: %+v", card)
	}
	if text := taskText(a); !strings.Contains(text, settleAgainLine) ||
		strings.Contains(text, settleAskWord) {
		t.Fatalf("the card does not read as sent back:\n%s", text)
	}
}

// THE FOURTH CHOICE IS THE ESCAPE HATCH: the preference is written, and this
// card is handed over on the way past.
func TestDecidingTheseForMeFlipsTheSettingAndHandsThisOneOver(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)

	a.settleCard(card, settleAlways)

	if got := config.TaskSettleAt(a.profileDir); got != config.TaskSettleAuto {
		t.Fatalf("task.settle reads %q, want %q", got, config.TaskSettleAuto)
	}
	if len(agent.handed) != 1 || agent.handed[0] != 7 {
		t.Fatalf("this card was not handed over: %v", agent.handed)
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("handing a decision over settled the node: %+v", agent.resolved)
	}
	if !strings.HasPrefix(card.decided, settleHandedLine) {
		t.Fatalf("the receipt reads %q, want it to lead with %q", card.decided, settleHandedLine)
	}
	if !strings.HasSuffix(card.decided, settleSavedLine) {
		t.Fatalf("the receipt does not say the preference was saved: %q", card.decided)
	}
}

// A DECISION SOMEBODY ELSE ALREADY MADE IS A REFRESH, NOT AN ALERT. The model's
// own `tasks … resolve`, a re-check that finally answered, another window — any
// of them can settle this node while the card is still on screen, and the honest
// thing for the card to do is stop asking.
func TestAnAlreadyAnsweredCardRefreshesQuietly(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)
	agent.refuse = fmt.Errorf("task 7 is done, and only a task that needs a look is waiting on somebody to decide: %w",
		session.ErrTaskDecided)
	entries := len(a.entries)

	a.settleCard(card, settleTake)

	if len(a.entries) != entries {
		t.Fatalf("the refusal wrote %d blocks into the conversation", len(a.entries)-entries)
	}
	if card.decided != settleGoneLine {
		t.Fatalf("the card reads %q, want %q", card.decided, settleGoneLine)
	}
	if text := taskText(a); strings.Contains(text, "waiting on somebody to decide") {
		t.Fatalf("the engine's refusal was put on screen:\n%s", text)
	}
}

// AND A REFUSAL THAT IS NOT A DECISION LEAVES THE QUESTION STANDING. "There is
// no checker to ask" and "its working copy is gone" both mean this ONE answer
// could not be spent — the other two may still work — so the choices stay and
// the card says so in one line rather than reporting a decision nobody made.
func TestAnAnswerThatCouldNotBeTakenKeepsTheChoices(t *testing.T) {
	a, agent := settleApp(t)
	card := landUnverified(t, a)
	agent.refuse = errors.New("task.audit is off, so there is no auditor to ask")

	a.settleCard(card, settleAgain)

	if card.decided != "" {
		t.Fatalf("the card was marked answered by a refusal: %q", card.decided)
	}
	text := taskText(a)
	if !strings.Contains(text, settleTroubleLine) {
		t.Fatalf("the card does not say the answer could not be taken:\n%s", text)
	}
	if !strings.Contains(text, settleTakeKey+settleTakeWord) {
		t.Fatalf("the remaining answers went away with the one that failed:\n%s", text)
	}
	// AND THE ENGINE'S OWN SENTENCE STAYS OFF THE SCREEN: it names the apparatus,
	// which is the one vocabulary this surface may never spend on a person.
	for _, never := range []string{"task.audit", "auditor"} {
		if strings.Contains(text, never) {
			t.Fatalf("the engine's refusal reached the person (%q):\n%s", never, text)
		}
	}
	// A second answer that works clears it.
	agent.refuse = nil
	a.settleCard(card, settleTake)
	if card.trouble != "" || card.decided != settleTookLine {
		t.Fatalf("the trouble line survived a decision: %+v", card)
	}
}

// THE KEYS AND THE POINTER ANSWER THE SAME ROW, and the keys are letters — so
// every guard `x` has is on them.
func TestTheAnswersAnswerToKeysAndToTheColumn(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)

	// A letter with nothing selected is a letter, and goes in the box.
	drive(t, a, key("a"))
	if len(agent.resolved) != 0 {
		t.Fatalf("a letter typed with no card selected answered one: %+v", agent.resolved)
	}
	a.input.reset()

	// Selected, over an empty box: the same letter is the answer.
	a.sel = len(a.entries) - 1
	drive(t, a, key("a"))
	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the key did not answer the selected card: %+v", agent.resolved)
	}

	// And the pointer answers the same columns the row was drawn with.
	b, other := settleApp(t)
	second := landUnverified(t, b)
	_ = taskText(b)
	if len(second.chips) != 4 {
		t.Fatalf("the row recorded %d pressable answers, want 4", len(second.chips))
	}
	if !b.settlePress(len(b.entries)-1, second.chips[2].span.from+1) {
		t.Fatal("a press on the answers row was not taken")
	}
	if len(other.resolved) != 1 || other.resolved[0].answer != session.TaskRefute {
		t.Fatalf("the third chip answered %+v", other.resolved)
	}
}

// A LETTER WITH A SENTENCE IN THE BOX IS A LETTER. This is the rule every key on
// this surface that is also a letter is held to, and it matters more here than
// anywhere: the thing on the other side of it is somebody's work being called
// finished.
func TestALetterTypedIntoASentenceNeverAnswersACard(t *testing.T) {
	a, agent := settleApp(t)
	landUnverified(t, a)
	a.sel = len(a.entries) - 1
	drive(t, a, key("h"), key("a"))
	if len(agent.resolved) != 0 {
		t.Fatalf("typing answered a card: %+v", agent.resolved)
	}
	if got := a.input.String(); got != "ha" {
		t.Fatalf("the box holds %q, want the letters that were typed", got)
	}
}

// ── the hierarchy ───────────────────────────────────────────────────────────

// child is one node the engine says was handed out by another.
func child(id, parent uint64, state session.TaskState) session.Event {
	return update(id, "Port the parser", state, session.TaskNotice{Parent: parent, Elapsed: time.Second})
}

// ATTENTION SURFACES AT THE ROOT ONLY.
//
// A sub-task's landing note goes to its parent's own agent, which has the tool
// and the diff and is already the decider. So while the parent is working, the
// column must not put "needs you" over the family — and the moment the parent
// lands, the same child is the person's.
func TestAChildThatNeedsALookWaitsForItsParentAndThenForYou(t *testing.T) {
	a, _ := settleApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: child(2, 1, session.TaskUnverified)},
	)
	kid := a.tasks[2]
	if group := a.railGroupOf(kid); group == railAttention {
		t.Fatalf("a child under a working parent is filed under %q", railGroupWords[group])
	}
	if rank := a.railGlyphRank(kid); rank == 0 {
		t.Fatal("a folded family wore a demand its own head is already holding")
	}

	// The parent lands. Nobody is reading that child's news any more, so it is
	// the person's.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Rebuild the index", session.TaskDone,
		session.TaskNotice{Elapsed: time.Second, Merge: mergeWordMerged})})
	if group := a.railGroupOf(kid); group != railAttention {
		t.Fatalf("an orphaned child is filed under %q, want %q",
			railGroupWords[group], railGroupWords[railAttention])
	}
	if rank := a.railGlyphRank(kid); rank != 0 {
		t.Fatalf("the orphaned child ranks %d, want the loudest", rank)
	}
}

// AND A ROOT IS ALWAYS THE PERSON'S. Nothing is above it that could be asked.
func TestARootThatNeedsALookIsAlwaysAttention(t *testing.T) {
	a, _ := settleApp(t)
	landUnverified(t, a)
	if group := a.railGroupOf(a.tasks[7]); group != railAttention {
		t.Fatalf("a root that needs a look is filed under %q", railGroupWords[group])
	}
}

// UNDER `auto` THE CARD IS QUIET. aforge has been handed the decision by the
// landing note itself, so a card offering four choices would be asking a question
// somebody else is already answering.
func TestUnderAutoTheCardOffersNothing(t *testing.T) {
	a, _ := settleApp(t)
	row, ok := a.registry().Row(config.KeyTaskSettle)
	if !ok {
		t.Fatal("the settings registry has no task.settle row")
	}
	if err := row.Apply(config.TaskSettleAuto); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		session.TaskNotice{Elapsed: time.Second})})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || card.asks {
		t.Fatalf("the card is still asking under auto: %+v", card)
	}
	text := taskText(a)
	if strings.Contains(text, settleAskWord) {
		t.Fatalf("the card asks while aforge is deciding:\n%s", text)
	}
	// The landing itself is untouched — the state, the word and the mark are the
	// same whoever is deciding.
	if !strings.Contains(text, taskUnverifiedWord) {
		t.Fatalf("the landing stopped saying what it is:\n%s", text)
	}
}
