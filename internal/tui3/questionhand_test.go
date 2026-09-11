package tui3

import (
	tea "charm.land/bubbletea/v2"

	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── WHOSE KEYS THESE ARE ────────────────────────────────────────────────────
//
// One rule, in one place: a question is drawn where it can be answered and
// nowhere else, and what is not drawn takes no keys ([app.questionOffFrame]).
// These hold the four ways that was broken.

// A QUESTION RAISED WHILE NOBODY WAS AT THE WINDOW IS STILL ON THE BLOCK.
//
// Away used to REPLACE the block with a note and a bell: nothing was pinned,
// the chip counted nothing, `alt+a` refused, and nothing re-delivered when the
// person came back. A ten-minute turn that ends in a question is exactly the
// case, and it left a stopped screen with nothing on it to work.
func TestAQuestionRaisedWhileNobodyWasThereIsStillOnTheBlock(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.lastQuestionKey = lab.at.Add(-awayAfter - time.Minute)
	if got := lab.a.questionPresenceNow(); got != questionAway {
		t.Fatalf("presence reads %v, want away", got)
	}
	lab.fromLane(consentAsk())
	if lab.a.questionCount() != 1 {
		t.Fatalf("the chip counts %d questions, want the one that was raised while nobody was here", lab.a.questionCount())
	}
	if rows := plain(strings.Join(lab.a.questionRows(lab.a.width), "\n")); !strings.Contains(rows, "allow once") {
		t.Fatalf("the question is not on the block:\n%s", rows)
	}
	// AND THE PHONE AND THE BELL ARE WHAT AWAY ADDS ON TOP, never what it
	// swaps the block for.
	rule := newQuestionDeliveryRule()
	out := rule.deliver(consentAsk(), "", questionAway, lab.at)
	if out.Pin == nil || !out.Phone {
		t.Fatalf("away delivered %+v, want the block AND the phone", out)
	}
}

// AND EVERY SIGN OF A PERSON IS A PERSON. It read keypresses alone, so a window
// somebody was scrolling or had just clicked into was "away".
func TestThePointerAndTheWindowComingForwardCountAsSomebodyBeingThere(t *testing.T) {
	for _, sign := range []struct {
		name string
		msg  tea.Msg
	}{
		{"a click", tea.MouseClickMsg{}},
		{"the pointer moving", tea.MouseMotionMsg{}},
		{"the window coming forward", tea.FocusMsg{}},
	} {
		a := newTestApp(&fakeAgent{})
		a.lastQuestionKey = time.Now().Add(-awayAfter - time.Minute)
		a.Update(sign.msg)
		if a.questionPresenceNow() == questionAway {
			t.Errorf("%s did not count as somebody being at the window", sign.name)
		}
	}
}

// THE SHEET TAKES NO KEYS FROM BEHIND A PAGE. It had no off-frame guard of its
// own, so ↑↓, tab and the digits were taken from behind whatever the person was
// actually looking at.
func TestTheSheetTakesNoKeysFromBehindAPage(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.questionBatch = newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskPermission, "read vendor/?",
			session.AnswerOption{Key: "1", Label: "allow once"}, session.AnswerOption{Key: "2", Label: "not now"}),
		sheetQuestion(2, session.AskPermission, "write .github/?",
			session.AnswerOption{Key: "1", Label: "allow once"}, session.AnswerOption{Key: "2", Label: "not now"}),
	})
	a.showPage(pageTasks)
	if _, took := a.questionKey(key("down")); took {
		t.Fatal("the sheet walked its cursor from behind a place")
	}
	if _, took := a.questionKey(key("1")); took {
		t.Fatal("the sheet answered a question from behind a place")
	}
	if a.questionBatch.answeredCount() != 0 {
		t.Fatalf("%d rows were answered from behind a page", a.questionBatch.answeredCount())
	}
	// AND NOTHING OF IT IS DRAWN THERE EITHER, which is the same rule's other
	// half: what is not on the frame is not on the keyboard.
	if rows := a.questionRows(a.width); len(rows) != 0 {
		t.Fatalf("the sheet drew %d rows from behind a place", len(rows))
	}
}

// A PAGE CLOSES WITH THE QUESTION IT IS ABOUT. Nothing closed it, so a page over
// a question answered in another window went on taking ↑↓ and enter for a
// decision that had already been made.
func TestThePageOverAQuestionClosesWhenTheQuestionIsAnsweredElsewhere(t *testing.T) {
	lab := newQuestionLab(t)
	q := consentAsk()
	lab.raise(q)
	head, ok := lab.a.questionHead()
	if !ok {
		t.Fatal("the question never reached the block")
	}
	lab.a.openQuestionRoom(head)
	if lab.a.qroom == nil {
		t.Fatal("the page did not open")
	}
	answer := session.Answer{Kind: q.Kind, ID: q.ID, Key: "1", Picked: []string{"1"}, DecidedBy: session.DecidedByWindow}
	lab.a.questionFold(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer})
	if lab.a.qroom != nil {
		t.Fatal("the page is still standing over a question somebody else answered")
	}
}

// AND WITH A QUESTION THE ASKER TOOK BACK.
func TestThePageOverAQuestionClosesWhenTheQuestionIsWithdrawn(t *testing.T) {
	lab := newQuestionLab(t)
	q := consentAsk()
	lab.raise(q)
	head, _ := lab.a.questionHead()
	lab.a.openQuestionRoom(head)
	lab.a.withdrawQuestion(q, "the turn moved on without it")
	if lab.a.qroom != nil {
		t.Fatal("the page is still standing over a question that was taken back")
	}
}

// A CHECKLIST HAS ONE CURSOR. `↑`/`↓` walked one mark while `space`, `tab` and
// the digits acted on another, and the card drew both — so the row a person
// moved to was not the row the next key ticked.
func TestAChecklistTicksTheRowTheArrowsLeftTheCursorOn(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(demoQuestionChecklist())
	lab.tick(questionSettle)
	lab.rows()
	if !lab.press("down") || !lab.press("down") {
		t.Fatal("the checklist would not walk its cursor")
	}
	head, ok := lab.a.questionHead()
	if !ok {
		t.Fatal("the checklist left the block")
	}
	if head.holes.focus != 2 || head.pick != head.holes.focus {
		t.Fatalf("two cursors: the arrows left focus on %d and the pointer on %d", head.holes.focus, head.pick)
	}
	if !lab.press("space") {
		t.Fatal("space did not reach the checklist")
	}
	head, _ = lab.a.questionHead()
	if !head.holes.ticks[2] {
		t.Fatalf("space ticked %v, want the row the arrows were on", head.holes.ticks)
	}
}

// THE SECOND BEAT IS A ROW OF ANSWERS AND HAS A CURSOR LIKE ANY OTHER. It drew
// none and swallowed every arrow aimed at it, so the shapes could only be
// answered by guessing which digit was which.
func TestTheSecondBeatWalksItsShapesAndEnterTakesTheOneUnderTheCursor(t *testing.T) {
	_, a, saved := shapeAsk(t, "git status --short")
	drive(t, a, key("2"))
	head, ok := a.questionHead()
	if !ok || len(head.beat) == 0 {
		t.Fatal("the beat never came up")
	}
	// IT OPENS ON THE SHAPE THAT GRANTS LEAST — the last of them, the line
	// itself — so a person who walks nowhere grants the least it can grant.
	if head.beatAt != len(head.beat)-1 {
		t.Fatalf("the beat opened on shape %d of %d, want the one that grants least", head.beatAt, len(head.beat))
	}
	drive(t, a, key("up"))
	head, _ = a.questionHead()
	if head.beatAt != len(head.beat)-2 {
		t.Fatalf("the arrow moved the beat's cursor to %d", head.beatAt)
	}
	want := head.beat[head.beatAt]
	drive(t, a, key("enter"))
	if len(saved.commands) != 1 || saved.commands[0] != want {
		t.Fatalf("enter banked %v, want the shape the cursor was on (%q)", saved.commands, want)
	}
}

// THE PAGE KEEPS THE ANSWER IT WALKED TO IN VIEW. `↓` moved the focus and
// nothing scrolled, so on a page with evidence at the top the arrows walked into
// rows below the fold and the screen did not change.
func TestThePageScrollsToTheAnswerTheArrowsWalkTo(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 92, 14
	q := demoQuestionReading()
	a.raiseQuestion(questionShown{question: q})
	a.questionRows(a.width)
	head, ok := a.questionHead()
	if !ok {
		t.Fatal("the question never reached the block")
	}
	a.openQuestionRoom(head)
	a.qroom.shown = a.qroom.shown.Add(-time.Second)
	// Walk to the last answer, which is what makes the page taller than the
	// window — the answer the pointer is on carries its evidence, so a page
	// worth opening does not fit and this is the shape the defect needs.
	for range len(q.Options) {
		a.questionMoveFocus(1)
	}
	rows := a.questionRoomRows(a.bodyWidth())
	if len(rows) <= a.viewHeight() {
		t.Skipf("the page fits the window (%d rows in %d), so there is nothing to scroll", len(rows), a.viewHeight())
	}
	a.questionMoveFocus(0)
	offset := a.qroom.offset
	last := -1
	for at, spot := range a.qroom.spots {
		if spot.at == a.qroom.focus {
			last = at
		}
	}
	if last < offset || last >= offset+a.viewHeight() {
		t.Fatalf("the focused answer is on row %d and the window shows %d..%d", last, offset, offset+a.viewHeight())
	}
}

// WHERE THE ANSWERS ARE IS COUNTED FROM THE TOP OF THE BLOCK. The forms count
// their rows from the top of themselves, and the receipts above them are the
// block's rows too — so with a receipt standing, a click on the answers row
// landed one row out, which on a card is an answer nobody aimed at.
func TestTheAnswersRowIsWhereTheBlockSaysItIsWithAReceiptAboveIt(t *testing.T) {
	lab := newQuestionLab(t)
	// One decision already made, which is the row that used to shift everything
	// under it.
	done := consentAsk()
	lab.a.questionRecords = append(lab.a.questionRecords, questionRecord{
		record: session.DecisionRecord{
			ID: done.ID, Kind: done.Kind, Ask: done.Ask, Head: done.Head,
			Picked: []string{"1"}, By: session.DecidedByPerson, At: lab.at,
		},
		head: done.Head, at: lab.at,
	})
	second := consentAsk()
	second.ID = 8
	lab.raise(second)
	lab.tick(questionSettle)
	rows := questionPlainRows(lab.a.questionRows(lab.a.width))
	if len(rows) < 2 {
		t.Fatalf("the block drew %d rows", len(rows))
	}
	// THE ANSWERS ARE FOUND BY THE WORD A PERSON PRESSES, not by the frame's
	// chrome. An earlier draft looked for the later key in brackets, which tied
	// this law to one spelling of the bottom edge and broke the day the edge was
	// respelled; the option's own label is the thing the row exists to draw.
	if len(second.Options) == 0 {
		t.Fatal("the fixture raised a question with no answers to draw")
	}
	answer := plain(second.Options[0].Label)
	drawn := -1
	for at, row := range rows {
		if strings.Contains(row, answer) && drawn < 0 {
			drawn = at
		}
	}
	if drawn < 0 {
		t.Fatalf("no row of the block draws %q:\n%s", answer, strings.Join(rows, "\n"))
	}
	// AND THE BLOCK'S OWN ACCOUNT IS WHICHEVER ONE A PRESS RESOLVES AGAINST.
	// A form that lays its answers out one to a row keeps BANDS, and
	// [app.questionBandPress] compares them with the chrome's row index; a form
	// that draws them along one row keeps SPANS at [app.questionSpanRow]. The
	// law is the same for both and so is the defect it guards — a receipt above
	// the answers is an off-by-one on the pointer and the click — so it is asked
	// of whichever the block wrote.
	said, kind := lab.a.questionSpanRow, "the span row"
	if len(lab.a.questionBands) > 0 {
		said, kind = lab.a.questionBands[0].row, "the first band"
	}
	if said != drawn {
		t.Fatalf("%s says the answers are on row %d and they are drawn on row %d — every receipt above them is an off-by-one on the pointer and the click:\n%s",
			kind, said, drawn, strings.Join(rows, "\n"))
	}
}

// ── THE BOX KEEPS THE FIRST LETTER ──────────────────────────────────────────
//
// The defect, in the owner's own words: "the first key typed into an empty box
// must not answer — a digit picks, `d` hands the call back today." The rule and
// the reason the ANSWERS' keys are not on this road are in questionkeys.go.

// handLab is one question on the block, settled, with an empty box under it.
func handLab(t *testing.T) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 9101, Head: "which column should it index?", Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{
			{Key: "1", Label: "created_at"},
			{Key: "2", Label: "updated_at"},
		},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	return lab
}

func TestTheFirstLetterOfASentenceIsNotAVerbOnTheBlock(t *testing.T) {
	lab := handLab(t)
	// `do the schema first` — the `d` used to hand the call back to the asker.
	if lab.press(questionDecideKey) {
		t.Fatal("the block took `d` from an empty box nobody had aimed at it")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("the first letter of a sentence answered the question: %+v", lab.answer)
	}
	// AND SO IS EVERY OTHER VERB ON THE TABLE. They are each the first letter of
	// a word somebody types into a box.
	for _, key := range []string{questionCommentKey, questionAskBackKey, questionRuleKey, questionUndoKey, questionOpenKey, questionCompareKey} {
		if lab.press(key) {
			t.Fatalf("the block took %q from an empty box nobody had aimed at it", key)
		}
	}
}

// AND THE SAME KEY WORKS THE MOMENT SOMEBODY AIMS AT THE BLOCK. One arrow is
// the whole of it — the grammar the block is drawn with, read in the order a
// hand uses it.
//
// WHAT IS HELD HERE IS WHERE THE KEY WENT, AND DELIBERATELY NOT WHAT IT THEN
// DID. The hand law is about one thing: a letter goes to the box until somebody
// aims at the block, and to the block afterwards. What `c` MEANS once it gets
// there is the block's own business and has already moved once — the owner's
// ruling of 2026-09-11 made it a shortcut to the `something else…` row wherever
// a question has one, where it used to point the message box — and a test that
// asserted the old meaning here would go red for a reason with nothing to do
// with the hand. So this asserts the two halves of the law itself: the block
// took the key, and the box did not.
func TestAVerbWorksOnceTheBlockHasBeenAimedAt(t *testing.T) {
	lab := handLab(t)
	if !lab.press("down") {
		t.Fatal("the arrow did not reach the block")
	}
	if !lab.press(questionCommentKey) {
		t.Fatal("`c` did not reach the block after the person aimed at it")
	}
	if typed := lab.a.input.String(); typed != "" {
		t.Fatalf("`c` was taken by the block and typed into the box as well: the box holds %q", typed)
	}
}

// AND THE ANSWERS' OWN KEYS ARE NEVER HELD BACK. `[1] created_at` is drawn on
// the row in front of the person; a key drawn as pressable that is not
// pressable is a worse defect than the one the rule closes.
func TestAnAnswersOwnKeyAnswersWithoutAiming(t *testing.T) {
	lab := handLab(t)
	if !lab.press("1") {
		t.Fatal("the digit drawn on the row did not answer")
	}
	if len(lab.answer) != 1 || lab.answer[0].Key != "1" {
		t.Fatalf("the digit did not send its own answer: %+v", lab.answer)
	}
}

// AND THE HAND IS GIVEN UP WITH THE QUESTION. What somebody aimed at says
// nothing about the question that lands after it — which is the case the rule
// exists for, one arriving between two keystrokes.
func TestTheNextQuestionDoesNotInheritTheHand(t *testing.T) {
	lab := handLab(t)
	lab.press("down")
	lab.press("1")
	lab.raise(session.Question{
		ID: 9102, Head: "drop the old table?", Ask: session.AskChoice,
		Asker:   session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "drop it"}, {Key: "2", Label: "keep it"}},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	if lab.press(questionDecideKey) {
		t.Fatal("the question that arrived next inherited the hand from the one before it")
	}
}

// aimed is the person having AIMED at the block — looked at the question rather
// than at the box under it — without moving anything on it.
//
// A TEST ABOUT WHAT A VERB DOES IS NOT A TEST ABOUT HOW THE BLOCK GETS THE
// KEYBOARD. The arrow that does it in life walks the pointer, which would put
// half these tests on a different answer than the one they are about; the two
// tests above are where the rule itself is held.
func aimed(a *app) {
	head, ok := a.questionHead()
	if !ok {
		return
	}
	a.aimQuestion(head.token())
}

// ── THE HAND IS THE TOKEN, SO NOTHING INHERITS IT ───────────────────────────
//
// Both of these were live on the first cut of the rule, where the hand was a
// flag set by a key ARRIVING rather than by the block TAKING it.

// SCENARIO A: a key the block handed back does not hand it the keyboard. `enter`
// over a half-typed sentence is the conversation's — it sends the message — so
// the `d` of the next sentence is still the box's.
func TestEnterThatSentAMessageDoesNotGiveTheBlockTheKeyboard(t *testing.T) {
	lab := handLab(t)
	lab.a.input.setText("looks good")
	if _, took := lab.a.questionKey(questionPressOf(questionEnterKey)); took {
		t.Fatal("the block took the enter that was sending a message")
	}
	lab.a.input.reset()
	if lab.press(questionCommentKey) {
		t.Fatal("the enter that went to the conversation gave the block the keyboard")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("the first letter of the next sentence answered: %+v", lab.answer)
	}
}

// SCENARIO B: the question behind the one you put off does not inherit the hand.
func TestTheQuestionBehindTheFoldedOneDoesNotInheritTheHand(t *testing.T) {
	lab := handLab(t)
	lab.raise(session.Question{
		ID: 9103, Head: "drop the old table?", Ask: session.AskChoice,
		Asker:   session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "drop it"}, {Key: "2", Label: "keep it"}},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	first, _ := lab.a.questionHead()
	if !lab.press("down") {
		t.Fatal("the arrow did not reach the first question")
	}
	lab.press(questionLaterKey)
	head, ok := lab.a.questionHead()
	if !ok || head.token() == first.token() {
		t.Fatal("esc did not put the first question behind the second")
	}
	if lab.press(questionCommentKey) {
		t.Fatal("the second question inherited the hand aimed at the one that was folded")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("a question nobody had aimed at was answered: %+v", lab.answer)
	}
}

// AND A KEY THE SETTLE GUARD DROPPED SAYS NOTHING ABOUT THIS QUESTION EITHER:
// it was aimed at whatever was on screen before it arrived.
func TestAKeyDroppedByTheSettleGuardDoesNotGiveTheHand(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 9104, Head: "index which column?", Ask: session.AskChoice,
		Asker:   session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "created_at"}, {Key: "2", Label: "updated_at"}},
	})
	// No tick: the question has not been on screen long enough to take a key.
	lab.press("down")
	lab.tick(questionSettle * 2)
	lab.rows()
	if lab.press(questionCommentKey) {
		t.Fatal("a key the settle guard dropped handed the block the keyboard")
	}
}

// ── THE DOOR TAKES A LIST ───────────────────────────────────────────────────
//
// One frame can hold several questions — a step that asks for three permissions
// at once is one thing to decide (lane P's grouped frame) — and the person
// answering it makes one gesture. [app.answerQuestions] is that gesture: every
// row settles on the keystroke, the doors are asked in order on ONE goroutine
// off the loop, and a refusal comes back against its own row.

// batchLab raises n questions and hands back the lab and what to answer them
// with.
func batchLab(t *testing.T, n int) (*questionLab, []questionAnswer) {
	t.Helper()
	lab := newQuestionLab(t)
	all := make([]questionAnswer, 0, n)
	for i := 0; i < n; i++ {
		q := session.Question{
			ID: uint64(9200 + i), Kind: session.QuestionConsent, Ask: session.AskPermission,
			Head:  "run the migration step " + itoa(i+1) + "?",
			Asker: session.Asker{Kind: session.AskerModel},
			Options: []session.AnswerOption{
				{Key: "1", Label: "allow once"},
				{Key: "2", Label: "deny"},
			},
		}
		lab.raise(q)
		head := lab.a.questions[len(lab.a.questions)-1]
		all = append(all, questionAnswer{q: head, answer: session.Answer{Key: "1", Picked: []string{"1"}}})
	}
	lab.tick(questionSettle * 2)
	lab.rows()
	return lab, all
}

func TestOneCommandCarriesEveryAnswerInTheFrame(t *testing.T) {
	lab, all := batchLab(t, 3)
	lab.spend(lab.a.answerQuestions(all))
	if len(lab.answer) != 3 {
		t.Fatalf("the door was told %d answers, want all three: %+v", len(lab.answer), lab.answer)
	}
	for i, answer := range lab.answer {
		if want := uint64(9200 + i); answer.ID != want {
			t.Fatalf("answer %d went to question %d, want %d", i, answer.ID, want)
		}
		if answer.Key != "1" {
			t.Fatalf("answer %d sent %q", i, answer.Key)
		}
	}
	// AND EVERY ROW SETTLED ON THE KEYSTROKE, before the door said anything.
	if len(lab.a.questions) != 0 {
		t.Fatalf("%d questions are still on the block", len(lab.a.questions))
	}
}

// AND THE DOOR IS ASKED ONCE, OFF THE LOOP. Three answers are three calls on one
// goroutine and one pass back, not three commands and three frames.
func TestTheListIsOneTripOffTheLoop(t *testing.T) {
	lab, all := batchLab(t, 3)
	cmd := lab.a.answerQuestions(all)
	if cmd == nil {
		t.Fatal("the list handed back no command, so nothing would ever be sent")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("the door was asked from the update loop: %+v", lab.answer)
	}
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("the list became %d messages, want one door trip", len(msgs))
	}
	if len(lab.answer) != 3 {
		t.Fatalf("one trip carried %d answers, want three", len(lab.answer))
	}
	drive(t, lab.a, msgs...)
}

// AND ONE REFUSAL AMONG SEVERAL SAYS NOTHING ABOUT THE REST: the answers that
// were taken stay taken, and the one that was not comes back with the engine's
// own sentence.
func TestARefusalInAFrameOnlyPutsItsOwnQuestionBack(t *testing.T) {
	lab, all := batchLab(t, 3)
	lab.agent.answer = func(answer session.Answer) error {
		lab.answer = append(lab.answer, answer)
		if answer.ID == 9201 {
			return errQuestionTest
		}
		return nil
	}
	lab.spend(lab.a.answerQuestions(all))
	if len(lab.a.questions) != 1 {
		t.Fatalf("the block holds %d questions, want only the refused one", len(lab.a.questions))
	}
	if got := lab.a.questions[0].question.ID; got != 9201 {
		t.Fatalf("the block put back question %d, want the refused 9201", got)
	}
	if said := plain(lastNote(t, lab.a)); !strings.Contains(said, errQuestionTest.Error()) {
		t.Fatalf("the engine's refusal never reached the person: %q", said)
	}
}

// ── THE PHONE TIER DRAWS WHERE THE KEYBOARD IS STANDING ─────────────────────
//
// `↑↓` walk this form's answers exactly as they walk the card's. Nothing was
// drawn for it: a person on a phone-width terminal moved a cursor they could
// not see and pressed `enter` on whichever row it had reached. The background
// under the hot row is the MOUSE's and says nothing on a screen nobody is
// hovering.
func TestThePhoneSheetDrawsThePointerTheArrowsMove(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 52
	lab.raise(session.Question{
		ID: 9301, Kind: session.QuestionConsent, Ask: session.AskPermission,
		Head: "run the migration?", Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{
			{Key: "1", Label: "allow once"},
			{Key: "2", Label: "always, this command"},
			{Key: "3", Label: "deny"},
		},
	})
	lab.tick(questionSettle * 2)
	mark := lab.a.icon(tokens.GCollapsed)
	pointed := func() string {
		for _, row := range questionPlainRows(lab.rows()) {
			if strings.Contains(row, mark) {
				return row
			}
		}
		return ""
	}
	start := pointed()
	if start == "" {
		t.Fatal("the phone sheet draws no pointer at all, so enter lands on a row nobody can see")
	}
	head, _ := lab.a.questionHead()
	if want := strings.TrimSpace(head.question.Options[head.pick].Label); !strings.Contains(start, want) {
		t.Fatalf("the pointer is drawn on %q and enter would take %q", start, want)
	}
	// THE ARROW PRESSED IS THE ONE WITH SOMEWHERE TO GO, and which one that is
	// depends on where this kind of question opens its pointer — which is
	// [questionPointerStart]'s business, not this law's, and has moved before.
	// The ends do not wrap, so a fixed direction would make this test go red for
	// a change in where the pointer STARTS rather than in whether the sheet draws
	// where it IS.
	arrow := "up"
	if head.pick <= 0 {
		arrow = "down"
	}
	lab.press(arrow)
	moved := pointed()
	if moved == start {
		t.Fatalf("%q moved the cursor and the sheet drew it in the same place: %q", arrow, moved)
	}
	head, _ = lab.a.questionHead()
	if want := strings.TrimSpace(head.question.Options[head.pick].Label); !strings.Contains(moved, want) {
		t.Fatalf("after the arrow the pointer is on %q and enter would take %q", moved, want)
	}
}

// ── A REFUSAL PUTS BACK ONLY WHAT IT ACTUALLY CLOSED ────────────────────────
//
// Review of #919, item 2. Two ways a refusal could undo a decision that was
// nothing to do with it.

// A WORDS ANSWER CLOSES NOTHING, so a refusal of it re-raises nothing. `tell it`
// and `change it` send words and leave the question standing; the person can
// then answer it outright on the next keystroke, and the refusal of the words
// used to arrive afterwards, delete that answer's receipt and put a settled
// question back.
func TestARefusedWordsAnswerDoesNotUndoTheAnswerThatFollowedIt(t *testing.T) {
	lab := handLab(t)
	head, _ := lab.a.questionHead()
	// `tell it` on a landing is the shape that sends words and settles nothing
	// ([session.AnswerResolves] names it and the harness's `change it`).
	words := session.Answer{
		Kind: session.QuestionLanding, Ask: session.AskLanding,
		Key: session.LandingTellKey, Picked: []string{session.LandingTellKey},
		Change: "index both, actually",
	}
	lab.a.closeQuestion(head, session.Answer{Key: "1", Picked: []string{"1"}})
	before := len(lab.a.questionRecords)
	lab.a.reopenQuestion(head, words, errQuestionTest)
	if lab.a.questioning() {
		t.Fatal("a refusal of words that closed nothing put a settled question back on the block")
	}
	if got := len(lab.a.questionRecords); got != before {
		t.Fatalf("the receipt count moved from %d to %d — the refusal took the real answer's receipt", before, got)
	}
}

// AND THE ENGINE'S OWN WORD OUTRANKS A REFUSAL THAT ARRIVED AFTER IT: a door
// that deadlined once the answer had already been applied.
func TestADeadlinedRefusalDoesNotReRaiseWhatTheLaneAlreadySettled(t *testing.T) {
	lab := handLab(t)
	head, _ := lab.a.questionHead()
	answer := session.Answer{Key: "1", Picked: []string{"1"}}
	lab.a.foldOthersAnswer(head.question, answer)
	if lab.a.questioning() {
		t.Fatal("the lane's answer did not close the question")
	}
	lab.a.reopenQuestion(head, answer, errQuestionTest)
	if lab.a.questioning() {
		t.Fatal("a refusal that arrived after the engine had applied the answer re-raised the question")
	}
}

// ── A REFUSAL THAT ARRIVED AFTER A SWITCH IS NOT LOST ───────────────────────
//
// Review of #919, item 6. The fold that could not speak — the person was
// looking at another conversation — used to drop the engine's sentence. The
// engine never took the answer, so the question comes back on the next attach,
// and it used to come back with no account of why.
func TestARefusalDuringASwitchComesBackWithTheQuestion(t *testing.T) {
	lab := handLab(t)
	head, _ := lab.a.questionHead()
	lab.a.closeQuestion(head, session.Answer{Key: "1", Picked: []string{"1"}})
	lab.a.keepQuestionRefusal(head.token(), errQuestionTest)
	// The engine re-delivers it, because it never took the answer.
	head.shown = time.Time{}
	lab.a.raiseQuestion(head)
	if said := plain(lastNote(t, lab.a)); !strings.Contains(said, errQuestionTest.Error()) {
		t.Fatalf("the question came back with no account of why: %q", said)
	}
	// AND IT IS SAID ONCE. A sentence repeated on every re-delivery would be a
	// conversation shouting one refusal at somebody for as long as they left the
	// question open.
	before := plain(lastNote(t, lab.a))
	lab.a.raiseQuestion(head)
	if now := plain(lastNote(t, lab.a)); now != before {
		t.Fatalf("the kept refusal was said a second time: %q", now)
	}
}
