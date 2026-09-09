package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// questionLab is one app with the block on it and nothing else moving: a fixed
// clock, so the settle guard and the policy line are decidable rather than
// raced, and a scripted agent that records what the one door was handed.
type questionLab struct {
	a      *app
	agent  *questionScript
	at     time.Time
	answer []session.Answer
}

// questionScript is a [questionAgent] over the scripted agent every other test
// in this package uses, so the block gets its optional half without every fake
// in the tree learning about questions ([questionAgent] states the law).
type questionScript struct {
	*fakeAgent
	open   []session.Question
	lane   chan session.Event
	answer func(session.Answer) error
}

func (q *questionScript) OpenQuestions() []session.Question { return q.open }

func (q *questionScript) WatchQuestions() (<-chan session.Event, func()) {
	if q.lane == nil {
		q.lane = make(chan session.Event, 8)
	}
	return q.lane, func() {}
}

func (q *questionScript) ResolveQuestion(answer session.Answer) error {
	if q.answer != nil {
		return q.answer(answer)
	}
	return nil
}

func newQuestionLab(t *testing.T) *questionLab {
	t.Helper()
	at := time.Date(2026, time.September, 9, 14, 2, 0, 0, time.UTC)
	script := &questionScript{fakeAgent: &fakeAgent{}}
	lab := &questionLab{agent: script, at: at}
	script.answer = func(answer session.Answer) error {
		lab.answer = append(lab.answer, answer)
		return nil
	}
	a := newTestApp(script)
	a.width, a.height = 140, 30
	a.clock = func() time.Time { return lab.at }
	lab.a = a
	return lab
}

// tick moves the lab's clock, which is how a test buys its way past the settle
// guard without sleeping.
func (l *questionLab) tick(d time.Duration) { l.at = l.at.Add(d) }

// raise puts one question on the block and draws a frame so it has been SEEN —
// the settle guard is a claim about the screen ([app.markQuestionShown]).
//
// IT GOES ROUND [app.questionDrawnHere] ON PURPOSE. That gate is the MIGRATION's
// seam — which lanes have had their older block retired — and it is a different
// question from whether the renderer draws a shape correctly. A renderer test
// pinned to the migration's progress would go red on the wave that retires the
// next block, which is the wave it is meant to be protecting.
// [TestTheLaneRaisesOnlyWhatThisBlockHasTakenOver] is the seam's own test.
func (l *questionLab) raise(q session.Question) {
	if q.Asked.IsZero() {
		q.Asked = l.at
	}
	l.a.raiseQuestion(questionShown{question: q})
	l.rows()
}

// fromLane is the same question arriving the way the engine sends it, through
// the standing subscription.
func (l *questionLab) fromLane(q session.Question) {
	if q.Asked.IsZero() {
		q.Asked = l.at
	}
	l.a.questionFold(session.Event{Kind: session.EventQuestion, Question: &q})
	l.rows()
}

func (l *questionLab) rows() []string { return l.a.questionRows(l.a.width) }

func (l *questionLab) screen() string { return strings.Join(l.rows(), "\n") }

// plain is the block with every escape taken off. Assertions read it rather
// than the painted rows because a painted row puts an SGR reset between a key
// and its word — `[1]` and ` allow once` are two spans on purpose (the key is
// the only bold cell) — so a substring assertion against the paint would be
// asserting the painting rather than the sentence.
func (l *questionLab) plain() string { return plain(l.screen()) }

// questionPlainRows is [questionLab.plain] a row at a time, for the assertions
// that are about WHICH ROW a thing landed on rather than about the block as a
// whole. (The package's own [plainRows] is the CONVERSATION's rows; this one is
// the block's, which the frame draws separately.)
func questionPlainRows(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, plain(row))
	}
	return out
}

func (l *questionLab) press(key string) bool {
	_, taken := l.a.questionKey(questionPressOf(key))
	return taken
}

// questionPressOf spells one key the way bubbletea hands it over, so a test
// presses what a terminal sends rather than what the routing happens to
// compare against.
func questionPressOf(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
}

// consentAsk is the approval gate's own question, built the way the engine
// builds it (internal/session's [Agent.consentQuestion]) so the block is drawn
// against the real object rather than a convenient one.
func consentAsk() session.Question {
	return session.Question{
		ID: 7, Kind: session.QuestionConsent, Ask: session.AskPermission,
		Form: session.FormLine, Asker: session.Asker{Kind: session.AskerEngine},
		Head: "allow this?", Reason: `bash pattern "rm -rf *"`,
		Options:  session.AnswerOptions(session.QuestionConsent),
		Stakes:   session.StakesCostly,
		Blocking: session.Blocking{Turn: true},
		Scope:    []session.AnswerScope{session.ScopeOnce, session.ScopeAlways},
	}
}

// TestTheLineDrawsItsHeadItsAnswersAndItsReasonAndNothingElse is the line
// form's whole shape, asserted as rows rather than as a substring: a block that
// grew a row nobody decided on is a block that moved the box.
func TestTheLineDrawsItsHeadItsAnswersAndItsReasonAndNothingElse(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	rows := lab.rows()
	if len(rows) != 2 {
		t.Fatalf("the line took %d rows, not two:\n%s", len(rows), lab.screen())
	}
	head := plain(rows[0])
	for _, want := range []string{"allow this?", "[1] allow once", "[2] always", "[3] deny", "[esc] later"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the answers row does not say %q:\n%s", want, head)
		}
	}
	if !strings.Contains(plain(rows[1]), `bash pattern "rm -rf *"`) {
		t.Fatalf("the reason is not under it: %q", rows[1])
	}
	if strings.Contains(head, "cancel") {
		t.Fatal("esc still says cancel; it is `later` now and nothing is cancelled")
	}
}

// TestTheMarkOnAQuestionIsTheVocabularysAndIsAmber holds the icon law and the
// hue law at once: the mark comes through the one door and wears the one colour
// this surface reserves for a person being waited on.
func TestTheMarkOnAQuestionIsTheVocabularysAndIsAmber(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	head := lab.rows()[0]
	if !strings.Contains(head, tokens.Plain.Glyph(tokens.GNeedsHuman)) {
		t.Fatalf("the question mark is not the vocabulary's: %q", head)
	}
	amber := lab.a.pal.warnBold(tokens.Plain.Glyph(tokens.GNeedsHuman))
	if !strings.Contains(head, amber) {
		t.Fatalf("the mark is not amber:\n%q\nwanted %q", head, amber)
	}
}

// TestAKeyPressedBeforeTheQuestionSettledIsDroppedAndNeverApplied is THE SETTLE
// GUARD. It is the one law on this block whose failure is invisible: a question
// answered by a keystroke aimed at the sentence somebody was typing looks
// exactly like a question somebody answered.
func TestAKeyPressedBeforeTheQuestionSettledIsDroppedAndNeverApplied(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	if !lab.press("1") {
		t.Fatal("the block let an early key through to whatever is under it")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("a key inside the settle guard answered: %+v", lab.answer)
	}
	lab.tick(questionSettle)
	lab.rows()
	if !lab.press("1") {
		t.Fatal("the settled question did not take its own key")
	}
	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "1" {
		t.Fatalf("the answer that landed was %+v", lab.answer)
	}
}

// TestEscIsLaterAndCancelsNothing is the law that retires the consent block's
// "IT SUSPENDS THE KEYBOARD": the rows go, the question stays, and the count
// does not drop.
func TestEscIsLaterAndCancelsNothing(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	if !lab.press("esc") {
		t.Fatal("esc was not the question's")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("esc answered something: %+v", lab.answer)
	}
	if rows := lab.rows(); len(rows) != 0 {
		t.Fatalf("the folded question is still drawing rows:\n%s", strings.Join(rows, "\n"))
	}
	if lab.a.questionCount() != 1 {
		t.Fatalf("the folded question stopped being counted: %d", lab.a.questionCount())
	}
	if seg := lab.a.questionSegment(); !strings.Contains(seg, "1 question") {
		t.Fatalf("the chip does not carry the folded question: %q", seg)
	}
	lab.a.raiseFolded()
	if rows := lab.rows(); len(rows) == 0 {
		t.Fatal("the chip did not bring the question back")
	}
}

// TestTheBlockIsNeverModalAndHandsBackEveryKeyItDoesNotDraw is the difference
// between this block and the one it replaces, stated as the two facts that
// matter: a letter that is not on the row falls through, and every printable
// key falls through while there are words in the box.
func TestTheBlockIsNeverModalAndHandsBackEveryKeyItDoesNotDraw(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	if lab.press("z") {
		t.Fatal("the block swallowed a key it never drew")
	}
	lab.a.input.setText("half a sentence")
	if lab.press("1") {
		t.Fatal("a digit was taken out of a sentence somebody was typing")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("typing answered the question: %+v", lab.answer)
	}
	if !lab.press("esc") {
		t.Fatal("esc stopped being the question's while the box had words")
	}
}

// TestAQuestionThatArrivesOnAHalfTypedSentenceWaitsForTheBoxToBeStill is THE
// BOX IS NEVER MOVED UNDER A HAND. The question is open and counted the whole
// time; what it may not do is take rows out from under somebody mid-word.
func TestAQuestionThatArrivesOnAHalfTypedSentenceWaitsForTheBoxToBeStill(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.input.setText("half a sentence")
	lab.a.questionTyped = lab.at
	lab.raise(consentAsk())
	if rows := lab.rows(); len(rows) != 0 {
		t.Fatalf("the block took rows under a hand:\n%s", strings.Join(rows, "\n"))
	}
	if lab.a.questionCount() != 1 {
		t.Fatal("the held question was not counted while it waited")
	}
	lab.tick(questionQuiet)
	if rows := lab.rows(); len(rows) == 0 {
		t.Fatal("the question never took its rows after the hands stopped")
	}
}

// TestTheAnswerLeavesTheEnginesOwnRecordWhereTheQuestionWas is THE ANSWER IS
// THE RECORD, and it checks the ONE SOURCE OF TRUTH half of it: the line above
// the box is [session.DecisionRecord.Line]'s, not a second rendering of it.
func TestTheAnswerLeavesTheEnginesOwnRecordWhereTheQuestionWas(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	lab.press("1")
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 {
		t.Fatalf("the answered question left %d rows:\n%s", len(rows), lab.screen())
	}
	want := session.DecisionRecord{
		ID: 7, Kind: session.QuestionConsent, Ask: session.AskPermission,
		Head: "allow this?", Picked: []string{"1"}, Labels: []string{"allow once"},
		By: session.DecidedByPerson, Stakes: session.StakesCostly,
		Scope: session.ScopeOnce, At: lab.at,
	}
	if !strings.Contains(rows[0], want.Line()) {
		t.Fatalf("the receipt is not the record's own line:\n%q\nwanted %q", rows[0], want.Line())
	}
	for _, said := range []string{"decided", "allow once", "you", "14:02", "c change"} {
		if !strings.Contains(rows[0], said) {
			t.Fatalf("the receipt does not say %q: %q", said, rows[0])
		}
	}
}

// TestAWithdrawnQuestionSaysWhyOnceAndStopsBeingCounted is WITHDRAWN, WITH A
// REASON. The word "cancelled" is banned from it: what a person experiences is
// the thing no longer needing them.
func TestAWithdrawnQuestionSaysWhyOnceAndStopsBeingCounted(t *testing.T) {
	lab := newQuestionLab(t)
	ask := consentAsk()
	lab.raise(ask)
	gone := ask
	gone.Withdrawn = &session.Withdrawal{Reason: "the turn moved on without it", At: lab.at}
	lab.a.questionFold(session.Event{Kind: session.EventQuestionWithdrawn, Question: &gone})
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 {
		t.Fatalf("the withdrawal left %d rows:\n%s", len(rows), lab.screen())
	}
	for _, said := range []string{
		tokens.Plain.Glyph(tokens.GWithdrawn), "allow this?",
		"no longer needed", "the turn moved on without it",
	} {
		if !strings.Contains(rows[0], said) {
			t.Fatalf("the withdrawn line does not say %q: %q", said, rows[0])
		}
	}
	if strings.Contains(rows[0], "cancel") || strings.Contains(rows[0], "expired") {
		t.Fatalf("the withdrawn line uses machinery vocabulary: %q", rows[0])
	}
	if lab.a.questionCount() != 0 {
		t.Fatal("a withdrawn question is still being counted")
	}
}

// TestTheThirdSameShapedYesOffersARuleAndNeverTheFirst is RULES ARE OFFERED,
// VISIBLE, FORGETTABLE. The offer's scope is written into the words, because a
// rule whose reach is not on the row is a hidden rule.
func TestTheThirdSameShapedYesOffersARuleAndNeverTheFirst(t *testing.T) {
	lab := newQuestionLab(t)
	for i := 0; i < questionRuleAfter-1; i++ {
		ask := consentAsk()
		ask.ID = uint64(100 + i)
		ask.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "bash"}
		lab.raise(ask)
		lab.tick(questionSettle)
		lab.rows()
		if strings.Contains(lab.plain(), "make it a rule") {
			t.Fatalf("a rule was offered on yes number %d:\n%s", i+1, lab.screen())
		}
		lab.press("1")
	}
	ask := consentAsk()
	ask.ID = 999
	ask.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "bash"}
	lab.raise(ask)
	if got := lab.plain(); !strings.Contains(got, "[r] make it a rule for everywhere") {
		t.Fatalf("the third same-shaped yes did not offer a rule:\n%s", got)
	}
}

// TestACardDrawsARowPerAnswerAndMarksTheAskersPick is the card form, and the
// one thing about it that is easy to get wrong: the mark is the ASKER'S
// recommendation and is not a cursor.
func TestACardDrawsARowPerAnswerAndMarksTheAskersPick(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 11, Kind: session.QuestionTask, Ask: session.AskPermission, Form: session.FormCard,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "wants to start a task: rewrite the packer",
		Reason: "it will run on its own branch",
		Options: []session.AnswerOption{
			{Key: "1", Label: "start it", Consequence: "on a branch of its own"},
			{Key: "2", Label: "not now", Safe: true, Consequence: "nothing runs"},
		},
		Pick:   &session.Pick{Key: "1", Reason: "it starts on its own unless you say otherwise"},
		Stakes: session.StakesCostly,
	})
	rows := questionPlainRows(lab.rows())
	if len(rows) != 5 {
		t.Fatalf("the card took %d rows:\n%s", len(rows), lab.screen())
	}
	if !strings.Contains(rows[0], "wants to start a task: rewrite the packer") {
		t.Fatalf("the head is not the first row: %q", rows[0])
	}
	if !strings.Contains(rows[1], "it will run on its own branch") || !strings.Contains(rows[1], product) {
		t.Fatalf("the attribution row is wrong: %q", rows[1])
	}
	mark := tokens.Plain.Glyph(tokens.GCollapsed)
	if !strings.Contains(rows[2], mark) || !strings.Contains(rows[2], "start it") {
		t.Fatalf("the asker's pick is not marked: %q", rows[2])
	}
	if strings.Contains(rows[3], mark) {
		t.Fatalf("a second answer carries the pick mark: %q", rows[3])
	}
	if !strings.Contains(rows[3], "nothing runs") {
		t.Fatalf("the consequence is missing: %q", rows[3])
	}
	if !strings.Contains(rows[4], "[enter] take the pick") {
		t.Fatalf("the answers row does not offer the pick: %q", rows[4])
	}
}

// TestEnterTakesThePickOnlyWhereThereIsOne is the emptiness law on a key: no
// pick, no `enter →` line, and enter goes back to meaning whatever it meant.
func TestEnterTakesThePickOnlyWhereThereIsOne(t *testing.T) {
	lab := newQuestionLab(t)
	ask := consentAsk()
	ask.Form = session.FormCard
	lab.raise(ask)
	lab.tick(questionSettle)
	if got := lab.plain(); strings.Contains(got, "take the pick") {
		t.Fatalf("a question with no pick offered one:\n%s", got)
	}
	if lab.press("enter") {
		t.Fatal("enter was taken by a question with no pick")
	}
	ask.Pick = &session.Pick{Key: "1", Reason: "the narrow answer"}
	lab.raise(ask)
	lab.rows()
	if !lab.press("enter") {
		t.Fatal("enter was not taken by a question WITH a pick")
	}
	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "1" {
		t.Fatalf("enter took something other than the pick: %+v", lab.answer)
	}
}

// TestAConfirmationStartsOnTheSafeAnswerAndWalks keeps stop.go's and
// tabclose.go's law verbatim, which is the whole reason the confirmation kind
// is the one form on this block with a cursor at all.
func TestAConfirmationStartsOnTheSafeAnswerAndWalks(t *testing.T) {
	lab := newQuestionLab(t)
	var took []string
	lab.a.raiseQuestion(questionShown{
		question: session.Question{
			ID: 3, Kind: session.QuestionTask, Ask: session.AskConfirmation, Form: session.FormLine,
			Asker: session.Asker{Kind: session.AskerSurface},
			Head:  "Stop this run? In-flight nodes halt; partial results stay.",
			Options: []session.AnswerOption{
				{Key: "1", Label: "stop it"},
				{Key: "2", Label: "keep going", Safe: true},
			},
			Stakes: session.StakesIrreversible, Asked: lab.at,
		},
		local: func(answer session.Answer) { took = append(took, answer.FirstKey()) },
	})
	lab.rows()
	lab.tick(questionSettle)
	lab.rows()
	head, _ := lab.a.questionHead()
	if head.pick != 1 {
		t.Fatalf("the cursor did not start on the safe answer: %d", head.pick)
	}
	if !lab.press("enter") {
		t.Fatal("enter was not the confirmation's")
	}
	if len(took) != 1 || took[0] != "2" {
		t.Fatalf("enter did not take the safe answer: %+v", took)
	}
	if len(lab.answer) != 0 {
		t.Fatal("a question the surface raised went through the engine's door")
	}
}

// TestAConfirmationTakesNoTypedAnswer is the kind table's one-word rule: the
// two answers ARE the question, and a box under them would be a place to type
// something nothing reads.
func TestAConfirmationTakesNoTypedAnswer(t *testing.T) {
	if questionTakesWords(session.Question{Ask: session.AskConfirmation}) {
		t.Fatal("a confirmation offered a typed answer")
	}
	if !questionTakesWords(session.Question{Ask: session.AskPermission}) {
		t.Fatal("a permission refused a typed answer; free text is always available")
	}
}

// TestWordsTypedUnderABlockingQuestionAreTheAnswer is the ladder's last rung:
// free text is always available and never the only door.
func TestWordsTypedUnderABlockingQuestionAreTheAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	lab.a.input.setText("no, because it would delete the fixtures")
	if !lab.press("enter") {
		t.Fatal("the words never reached the question")
	}
	if len(lab.answer) != 1 {
		t.Fatalf("the typed answer did not go through the one door: %+v", lab.answer)
	}
	if lab.answer[0].Words() != "no, because it would delete the fixtures" {
		t.Fatalf("the words were changed on the way: %q", lab.answer[0].Words())
	}
	if !lab.a.input.empty() {
		t.Fatal("the box kept the words it had just sent")
	}
}

// TestANonBlockingQuestionNeverTakesTheBox is the other half of that law: a
// question the conversation is not waiting on has no claim on the sentence
// somebody is typing.
func TestANonBlockingQuestionNeverTakesTheBox(t *testing.T) {
	lab := newQuestionLab(t)
	ask := consentAsk()
	ask.Blocking = session.Blocking{}
	lab.raise(ask)
	lab.tick(questionSettle)
	lab.rows()
	lab.a.input.setText("an ordinary message")
	if lab.press("enter") {
		t.Fatal("a question that blocks nothing took the box's enter")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("it answered anyway: %+v", lab.answer)
	}
}

// TestTheRatifyLineSaysWhatWasDoneAndHowToUndoIt is the ladder's third rung
// drawn: nothing waits on it, so it is one row and wears the settled mark.
func TestTheRatifyLineSaysWhatWasDoneAndHowToUndoIt(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.raiseQuestion(questionShown{
		question: session.Question{
			ID: 5, Kind: session.QuestionTask, Ask: session.AskRatify,
			Asker: session.Asker{Kind: session.AskerModel},
			Head:  "renamed 12 files under src/",
			Options: []session.AnswerOption{
				{Key: "1", Label: "put them back", Safe: true},
				{Key: "2", Label: "already done"},
			},
			Stakes: session.StakesReversible, Asked: lab.at,
		},
		undoable: true,
		local:    func(session.Answer) {},
	})
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 {
		t.Fatalf("the ratify line took %d rows:\n%s", len(rows), lab.screen())
	}
	for _, said := range []string{
		tokens.Plain.Glyph(tokens.GSettled), "renamed 12 files under src/", "[u] undo", "[c] change",
	} {
		if !strings.Contains(rows[0], said) {
			t.Fatalf("the ratify line does not say %q: %q", said, rows[0])
		}
	}
	if strings.Contains(rows[0], tokens.Plain.Glyph(tokens.GNeedsHuman)) {
		t.Fatal("the ratify line wears the attention mark; nothing is waiting on it")
	}
}

// TestARatifyLineWithNothingRealToUndoDoesNotOfferTheKey is the emptiness law
// applied to an answer rather than to a number.
func TestARatifyLineWithNothingRealToUndoDoesNotOfferTheKey(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.raiseQuestion(questionShown{
		question: session.Question{
			ID: 6, Kind: session.QuestionTask, Ask: session.AskRatify,
			Head: "sent the digest", Stakes: session.StakesIrreversible, Asked: lab.at,
		},
		local: func(session.Answer) {},
	})
	if got := lab.plain(); strings.Contains(got, "[u] undo") {
		t.Fatalf("a ratify line with nothing to undo offered the key: %q", got)
	}
}

// TestTheChipCountsWhatIsWaitingAndNamesAKeyThatIsFree is the chip, and the
// half of it a terminal can break: the chord must be free in this surface's own
// table, or it means two things.
func TestTheChipCountsWhatIsWaitingAndNamesAKeyThatIsFree(t *testing.T) {
	lab := newQuestionLab(t)
	if seg := lab.a.questionSegment(); seg != "" {
		t.Fatalf("the chip drew something with nothing open: %q", seg)
	}
	lab.raise(consentAsk())
	second := consentAsk()
	second.ID = 8
	lab.raise(second)
	seg := lab.a.questionSegment()
	for _, said := range []string{tokens.Plain.Glyph(tokens.GNeedsHuman), "2 questions", questionChipKey} {
		if !strings.Contains(seg, said) {
			t.Fatalf("the chip does not say %q: %q", said, seg)
		}
	}
	if questionChipKey == "ctrl+?" || questionChipKey == "ctrl+_" {
		t.Fatalf("%q is DEL or ctrl+/ in most terminals and cannot be the chip's key", questionChipKey)
	}
}

// TestOneQuestionIsOneQuestionAndTheRestAreCounted is the queue: the block
// draws one and says how many are behind it, because a person who answers one
// and gets another must have been told it was coming.
func TestOneQuestionIsOneQuestionAndTheRestAreCounted(t *testing.T) {
	lab := newQuestionLab(t)
	for i := 0; i < 3; i++ {
		ask := consentAsk()
		ask.ID = uint64(20 + i)
		ask.Asked = lab.at.Add(time.Duration(i) * time.Second)
		lab.raise(ask)
	}
	got := lab.plain()
	if !strings.Contains(got, "2 more") {
		t.Fatalf("the queue count is missing:\n%s", got)
	}
	if strings.Count(got, "[esc] later") != 1 {
		t.Fatalf("more than one question is being drawn:\n%s", got)
	}
}

// TestAReplayedQuestionIsOneQuestionAndKeepsItsSettleStamp is the reattach
// case, and both halves are defects the lane would otherwise have: a queue that
// grew a row per replay, and a settle guard that reset every few seconds on a
// link that reconnects.
func TestAReplayedQuestionIsOneQuestionAndKeepsItsSettleStamp(t *testing.T) {
	lab := newQuestionLab(t)
	// A LANE THE BLOCK HAS TAKEN OVER, because this one is about the LANE — the
	// pump, the replay and the seam together — rather than about the drawing.
	ask := consentAsk()
	ask.Kind, ask.Ref = session.QuestionFuel, "run-1"
	ask.Head = "the run has spent its tank"
	lab.fromLane(ask)
	shown := lab.a.questions[0].shown
	lab.tick(questionSettle)
	lab.fromLane(ask)
	if len(lab.a.questions) != 1 {
		t.Fatalf("a replayed question was counted twice: %d", len(lab.a.questions))
	}
	if !lab.a.questions[0].shown.Equal(shown) {
		t.Fatal("the replay restamped the settle guard")
	}
	if !lab.press("1") || len(lab.answer) != 1 {
		t.Fatalf("the replayed question would not take its own key: %+v", lab.answer)
	}
}

// TestTheKeyTableIsOneTableAndEveryKeyOnARowIsRouted is ONE KEY GRAMMAR, and
// verbstrip.go's law about it: no key does anything that is not drawn, and
// nothing is drawn that does nothing.
func TestTheKeyTableIsOneTableAndEveryKeyOnARowIsRouted(t *testing.T) {
	seen := map[string]questionVerb{}
	for _, verb := range questionKeys {
		if verb.key == "" {
			t.Fatal("a row of the key table has no key")
		}
		if strings.TrimSpace(verb.word) == "" {
			t.Fatalf("%q has no word; the manual and the row read this field", verb.key)
		}
		if prior, twice := seen[verb.key]; twice {
			// Two rows on different FORMS are never on one screen at all, which is
			// the cheapest exclusion there is: the line, the card, the ratify row
			// and the room are four places, and a key drawn in one of them cannot
			// also be drawn in another at the same moment.
			if prior.forms&verb.forms == 0 {
				seen[verb.key] = verb
				continue
			}
			// ONE KEY, ONE MEANING AT A TIME. Two rows may share a key only where
			// their conditions cannot both hold — `a` is "take its suggestion" on a
			// checklist and "the first one" on a pair, `←→` walks a confirmation's
			// cursor and moves a dial — because those ARE one instinct at two
			// shapes, and a key that meant two things on ONE screen would be the
			// thing this law exists to prevent. The exclusion is proved below
			// rather than asserted here.
			if !questionNeedsExclusive(prior.needs, verb.needs) {
				t.Fatalf("%q is in the key table twice and both rows can be offered at "+
					"once; one key, one meaning", verb.key)
			}
		}
		seen[verb.key] = verb
		if verb.forms == 0 {
			t.Fatalf("%q belongs to no form and would never be drawn", verb.key)
		}
	}
	for _, want := range []string{
		questionEnterKey, questionLaterKey, questionOpenKey, questionCommentKey,
		questionCompareKey, questionAskBackKey, questionDecideKey, questionDialKey,
		questionRuleKey, questionUndoKey, questionBlankKey, questionToggleKey,
		questionWalkKey,
	} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("the grammar's %q is not in the table every reader reads", want)
		}
	}
}

// questionNeedsExclusive reports whether two conditions can never be true of one
// question at the same time. It is the proof behind the shared-key exception
// above, and it is written as the QUESTIONS that would satisfy each rather than
// as a list of pairs somebody has to keep true: every shape the object can take
// is tried, and the two conditions must never both answer yes on one of them.
func questionNeedsExclusive(one, other questionNeed) bool {
	if one == other {
		return false
	}
	a := newTestApp(&fakeAgent{})
	for _, shape := range questionShapes() {
		q := questionShown{question: shape}
		if a.questionOffers(q, one) && a.questionOffers(q, other) {
			return false
		}
	}
	return true
}

// questionShapes is every shape a question can take that the conditions read:
// each ask kind, each input kind, with and without options, a pick, a dial and a
// choice blank. It is deliberately generated rather than listed, so a condition
// added later is tried against all of them without anybody remembering to.
func questionShapes() []session.Question {
	asks := []session.AskKind{
		session.AskPermission, session.AskChoice, session.AskJudgement,
		session.AskClarification, session.AskConfirmation, session.AskLanding,
		session.AskAssumption, session.AskRatify,
	}
	inputs := []session.InputShape{
		{},
		{Kind: session.InputText},
		{Kind: session.InputChecklist},
		{Kind: session.InputPairs, Blanks: []session.Blank{{Label: "one", Choices: []string{"a", "b"}}}},
		{Kind: session.InputDial, Dial: &session.Dial{Max: 1}},
		{Kind: session.InputBlanks, Blanks: []session.Blank{{Label: "one", Choices: []string{"a", "b"}}}},
		{Kind: session.InputBlanks, Blanks: []session.Blank{{Label: "one"}}},
	}
	options := [][]session.AnswerOption{
		nil,
		{{Key: "1", Label: "one"}, {Key: "2", Label: "two"}},
		{{Key: "1", Label: "one"}, {Key: "2", Label: "two"}, {Key: "3", Label: "three"}},
	}
	out := make([]session.Question, 0, len(asks)*len(inputs)*len(options)*2)
	for _, ask := range asks {
		for _, input := range inputs {
			for _, opts := range options {
				for _, stakes := range []session.Stakes{session.StakesReversible, session.StakesIrreversible} {
					q := session.Question{Ask: ask, Input: input, Options: opts, Stakes: stakes}
					out = append(out, q)
					if len(opts) > 0 {
						q.Pick = &session.Pick{Key: opts[0].Key}
						out = append(out, q)
					}
				}
			}
		}
	}
	return out
}

// TestAnEngineWithNoQuestionsSideDrawsNoneAndBreaksNothing is A CAPABILITY
// THAT CANNOT WORK IS ABSENT, NOT BROKEN.
func TestAnEngineWithNoQuestionsSideDrawsNoneAndBreaksNothing(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 96, 30
	if _, ok := a.questionDoors(); ok {
		t.Fatal("a bare agent claimed a questions side")
	}
	if cmd := a.watchQuestions(); cmd != nil {
		t.Fatal("a surface with no questions side opened a lane anyway")
	}
	if rows := a.questionRows(a.width); len(rows) != 0 {
		t.Fatalf("it drew rows anyway: %v", rows)
	}
	if a.questionHeight() != 0 || a.questionSegment() != "" {
		t.Fatal("the empty block still cost the frame something")
	}
}

// TestTheLaneRaisesOnlyWhatThisBlockHasTakenOver is the migration's seam,
// asserted so it cannot rot: a lane drawn here AND by an older block would put
// one decision on the screen twice, which is worse than leaving it where it
// was.
func TestTheLaneRaisesOnlyWhatThisBlockHasTakenOver(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	for _, kind := range []session.QuestionKind{
		session.QuestionSubharnessAsk, session.QuestionFuel, session.QuestionConflict,
		session.QuestionAsk,
		// AND THE APPROVAL GATE, whose own block is deleted: consent.go draws
		// nothing now and what is left there is the lane's three surface-side
		// facts (the row, the widening write, the reading clock's length).
		session.QuestionConsent,
		// AND THE TASK PROPOSAL, whose choices row, meter and keyboard lane are
		// deleted: task.go draws the ASSIGNMENT in the transcript, which is what
		// the question is about rather than a second copy of the asking.
		session.QuestionTask,
	} {
		if !a.questionDrawnHere(session.Question{Kind: kind}) {
			t.Fatalf("%s has no other block and is not drawn here either", kind)
		}
	}
	for _, kind := range []session.QuestionKind{
		session.QuestionConnect, session.QuestionHarness,
		session.QuestionStanding,
	} {
		if a.questionDrawnHere(session.Question{Kind: kind}) {
			t.Fatalf("%s is drawn here AND by its own block; one decision, two rows", kind)
		}
	}
}
