package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// sheetQuestion is one row of a batch, with its own answers on its own keys.
func sheetQuestion(id uint64, ask session.AskKind, head string, options ...session.AnswerOption) session.Question {
	return session.Question{
		ID: id, Kind: session.QuestionTask, Ask: ask, Head: head,
		Stakes: session.StakesReversible, Options: options,
		Asked: time.Date(2026, 9, 9, 12, 0, int(id), 0, time.UTC),
	}
}

func TestSameAnswerForAllLikeThisIsByKeyAndNeverByPosition(t *testing.T) {
	now := time.Now()
	// Both are choices, and `2` means something different on each of them.
	first := sheetQuestion(1, session.AskChoice, "which index?",
		session.AnswerOption{Key: "1", Label: "sqlite"}, session.AnswerOption{Key: "2", Label: "memory"})
	second := sheetQuestion(2, session.AskChoice, "which cache?",
		session.AnswerOption{Key: "7", Label: "on disk"}, session.AnswerOption{Key: "8", Label: "in memory"})
	third := sheetQuestion(3, session.AskChoice, "which pager?",
		session.AnswerOption{Key: "1", Label: "less"}, session.AnswerOption{Key: "2", Label: "none"})
	sheet := newQuestionSheet([]session.Question{first, second, third})
	if !sheet.answerAt(0, "2", now) {
		t.Fatal("the first row would not take a key it offered")
	}
	// `g` spreads the LAST ANSWER GIVEN, wherever the cursor has walked to.
	sheet.cursor = 1
	if reached := sheet.sameForAll(now); reached != 1 {
		t.Fatalf("same answer reached %d rows, want only the one that offers key 2", reached)
	}
	if _, ok := sheet.answered(second); ok {
		t.Fatal("a question that never offered key 2 was answered with it")
	}
}

func TestASheetRowTakesOnlyAKeyItOffered(t *testing.T) {
	sheet := newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskChoice, "which index?", session.AnswerOption{Key: "1", Label: "sqlite"}),
	})
	if sheet.answerAt(0, "9", time.Now()) {
		t.Fatal("a key nobody offered was taken as an answer")
	}
}

func TestAWithdrawnRowReflowsWithoutMovingAnotherRowsAnswer(t *testing.T) {
	now := time.Now()
	first := sheetQuestion(1, session.AskPermission, "read vendor/?",
		session.AnswerOption{Key: "1", Label: "allow once"})
	second := sheetQuestion(2, session.AskPermission, "write .github/?",
		session.AnswerOption{Key: "1", Label: "allow once"})
	sheet := newQuestionSheet([]session.Question{first, second})
	if !sheet.answerAt(1, "1", now) {
		t.Fatal("the second row would not take its own key")
	}
	if !sheet.withdraw(first) {
		t.Fatal("the sheet did not know the question it was holding")
	}
	if len(sheet.questions) != 1 || sheet.questions[0].ID != 2 {
		t.Fatalf("sheet after withdrawal: %#v", sheet.questions)
	}
	if _, ok := sheet.answered(sheet.questions[0]); !ok {
		t.Fatal("re-flowing the sheet lost the answer that was already on a row")
	}
}

func TestSendTakesTheAnsweredAndDelegatesOnlyWhatHasAPick(t *testing.T) {
	now := time.Now()
	answered := sheetQuestion(1, session.AskChoice, "which index?",
		session.AnswerOption{Key: "1", Label: "sqlite"}, session.AnswerOption{Key: "2", Label: "memory"})
	delegated := sheetQuestion(2, session.AskChoice, "which cache?",
		session.AnswerOption{Key: "1", Label: "on disk"}, session.AnswerOption{Key: "2", Label: "in memory"})
	delegated.Pick = &session.Pick{Key: "2", Reason: "it is what the last run used"}
	unpicked := sheetQuestion(3, session.AskJudgement, "is this ready to land?",
		session.AnswerOption{Key: "1", Label: "yes"}, session.AnswerOption{Key: "2", Label: "not yet"})
	sheet := newQuestionSheet([]session.Question{answered, delegated, unpicked})
	sheet.answerAt(0, "2", now)
	answers, held := sheet.send(now)
	if len(answers) != 2 {
		t.Fatalf("send produced %d answers, want the answered one and the delegated one", len(answers))
	}
	if answers[0].DecidedBy != session.DecidedByPerson || answers[0].Key != "2" {
		t.Fatalf("the answered row = %#v", answers[0])
	}
	if answers[1].DecidedBy != session.DecidedByAsker || answers[1].Key != "2" {
		t.Fatalf("the delegated row = %#v", answers[1])
	}
	if len(held) != 1 || held[0].ID != 3 {
		t.Fatalf("held = %#v, want the question nobody named a pick on", held)
	}
}

func TestTheSheetDrawsGroupedRowsWithBothMarksAndItsOwnKeys(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	sheet := newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskPermission, "read vendor/?", session.AnswerOption{Key: "1", Label: "allow once"}),
		sheetQuestion(2, session.AskPermission, "write .github/?", session.AnswerOption{Key: "1", Label: "allow once"}),
		sheetQuestion(3, session.AskChoice, "which index?", session.AnswerOption{Key: "1", Label: "sqlite"}),
	})
	if !sheet.answerAt(0, "1", time.Now()) {
		t.Fatal("the first permission row would not take its own key")
	}
	text := ansi.Strip(strings.Join(a.questionSheetRows(sheet, 120), "\n"))
	for _, want := range []string{
		"3 questions" + questionSheetTogether,
		questionShapeWord(session.AskPermission),
		questionShapeWord(session.AskChoice),
		a.icon(tokens.GSettled) + " read vendor/?",
		a.icon(tokens.GNeedsHuman) + " write .github/?",
		"1 allow once",
		"[enter] " + questionSheetOpenWord,
		"[" + questionSendKey + "] " + questionSheetSendWord + " (1)",
		"[" + questionAlikeKey + "] " + questionSheetSameWord,
		"[esc] later",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("sheet is missing %q:\n%s", want, text)
		}
	}
}

func TestTheSheetOffersSameAnswerOnlyWhenItWouldReachARow(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	sheet := newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskChoice, "which index?", session.AnswerOption{Key: "1", Label: "sqlite"}),
		sheetQuestion(2, session.AskJudgement, "is this ready?", session.AnswerOption{Key: "1", Label: "yes"}),
	})
	sheet.answerAt(0, "1", time.Now())
	text := ansi.Strip(a.questionSheetOffer(sheet, 120))
	if strings.Contains(text, questionSheetSameWord) {
		t.Fatalf("same-answer was offered with no row of that shape to reach:\n%s", text)
	}
}

func TestEnterTakesOneRowOutOfTheSheetAndOntoTheBlock(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.questionBatch = newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskPermission, "read vendor/?", session.AnswerOption{Key: "1", Label: "allow once"}),
		sheetQuestion(2, session.AskPermission, "write .github/?", session.AnswerOption{Key: "1", Label: "allow once"}),
	})
	if _, took := a.openSheetRow(); !took {
		t.Fatal("enter did not open the focused row")
	}
	if a.sheetOpen() != 1 {
		t.Fatalf("the sheet still holds %d rows", a.sheetOpen())
	}
	head, ok := a.questionHead()
	if !ok || head.question.ID != 1 {
		t.Fatalf("the block is holding %#v", head.question)
	}
	// THE TWO ARE NEVER ON SCREEN TOGETHER: with a question on the block, the
	// rows belong to it and the rest of the batch waits behind the chip.
	text := ansi.Strip(strings.Join(a.questionRows(90), "\n"))
	if strings.Contains(text, questionSheetTogether) {
		t.Fatalf("the sheet drew under a question that was opened out of it:\n%s", text)
	}
	if a.questionCount() != 2 {
		t.Fatalf("the chip counts %d, want the opened row and the one still on the sheet", a.questionCount())
	}
}

func TestADigitAnswersTheFocusedRowInItsOwnKeysAndWalksOn(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.questionBatch = newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskPermission, "read vendor/?",
			session.AnswerOption{Key: "1", Label: "allow once"}, session.AnswerOption{Key: "2", Label: "not now"}),
		sheetQuestion(2, session.AskPermission, "write .github/?",
			session.AnswerOption{Key: "7", Label: "allow once"}, session.AnswerOption{Key: "8", Label: "not now"}),
	})
	if _, took := a.questionSheetKey(key("1")); !took {
		t.Fatal("the focused row would not take its own key")
	}
	if a.questionBatch.cursor != 1 {
		t.Fatalf("the cursor stayed on row %d instead of walking to the next one waiting", a.questionBatch.cursor)
	}
	// `1` is not an answer to the SECOND question, which offers 7 and 8.
	if _, took := a.questionSheetKey(key("1")); took {
		t.Fatal("a key this question never offered was taken as an answer")
	}
	if _, took := a.questionSheetKey(key("7")); !took {
		t.Fatal("the second row would not take its own key")
	}
	if a.questionBatch.answeredCount() != 2 {
		t.Fatalf("%d rows answered, want both", a.questionBatch.answeredCount())
	}
}

func TestEscFoldsTheSheetAndTheChipKeyBringsItBack(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.questionBatch = newQuestionSheet([]session.Question{
		sheetQuestion(1, session.AskChoice, "which index?", session.AnswerOption{Key: "1", Label: "sqlite"}),
		sheetQuestion(2, session.AskChoice, "which cache?", session.AnswerOption{Key: "1", Label: "on disk"}),
	})
	if _, took := a.questionSheetKey(key(questionLaterKey)); !took {
		t.Fatal("esc was not taken by the sheet")
	}
	if strings.Contains(ansi.Strip(strings.Join(a.questionRows(90), "\n")), questionSheetTogether) {
		t.Fatal("the sheet stayed on screen after esc")
	}
	if a.questionCount() != 2 {
		t.Fatalf("esc dropped the count to %d — later is not cancelled", a.questionCount())
	}
	a.raiseFolded()
	if !strings.Contains(ansi.Strip(strings.Join(a.questionRows(90), "\n")), questionSheetTogether) {
		t.Fatal("the chip key did not bring the sheet back")
	}
}
