package tui3

import (
	tea "charm.land/bubbletea/v2"

	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	// Open every answer so the page is taller than the window, which is the
	// shape the defect needs and the shape a page worth opening has.
	for range len(q.Options) {
		a.questionMoveFocus(1)
		a.qroom.open[a.qroom.focus] = true
	}
	rows := a.questionRoomRows(a.bodyWidth())
	if len(rows) <= a.viewHeight() {
		t.Skipf("the page fits the window (%d rows in %d), so there is nothing to scroll", len(rows), a.viewHeight())
	}
	a.questionMoveFocus(0)
	offset := a.questionRoomOffsetFor(len(rows), a.viewHeight())
	last := -1
	for at, of := range a.qroom.spots {
		if of == a.qroom.focus {
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
	drawn := -1
	for at, row := range rows {
		if strings.Contains(row, "["+questionLaterKey+"]") {
			drawn = at
		}
	}
	if drawn < 0 {
		t.Fatalf("no row of the block offers its keys:\n%s", strings.Join(rows, "\n"))
	}
	if lab.a.questionSpanRow != drawn {
		t.Fatalf("the block says its answers are on row %d and they are drawn on row %d — every receipt above them is an off-by-one on the pointer and the click",
			lab.a.questionSpanRow, drawn)
	}
}
