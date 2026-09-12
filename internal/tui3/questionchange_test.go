package tui3

// The surface's half of changing an answer, and of stopping a clock that
// answers (docs/design/questions/DESIGN.md, "An answer is a message").

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// questionHeldCall is one hold this window sent, as the fake engine heard it.
type questionHeldCall struct {
	kind  session.QuestionKind
	token string
}

func (q *questionScript) HoldQuestion(kind session.QuestionKind, token string) {
	q.held = append(q.held, questionHeldCall{kind: kind, token: token})
}

// askedQuestion is the model's own question, which is the shape both of these
// are about: reversible, with a pick, and answerable with a digit.
func askedQuestion() session.Question {
	return session.Question{
		ID: 3, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "Which storage shape should this use?",
		Reason: "two shapes fit and the record does not choose between them",
		Options: []session.AnswerOption{
			{Key: "1", Label: "sqlite"},
			{Key: "2", Label: "jsonl"},
		},
		Pick:     &session.Pick{Key: "1", Reason: "the readers we have are sqlite"},
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
	}
}

// `c change` ON A RECEIPT PUTS THE QUESTION BACK, and the answer it takes says
// it is a change — without that bit the engine reads it as a late answer and
// drops it in silence.
func TestChangeOnAReceiptAsksTheQuestionAgainAndTheAnswerSaysItIsAChange(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(askedQuestion())
	lab.tick(questionSettle)
	lab.rows()
	lab.press("1")
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 || !strings.Contains(rows[0], "c change") {
		t.Fatalf("the receipt does not offer the change:\n%s", lab.screen())
	}
	if !lab.press("c") {
		t.Fatal("`c` on the receipt was not taken")
	}
	if lab.a.questionCount() != 1 {
		t.Fatalf("the question did not come back: %d open", lab.a.questionCount())
	}
	if strings.Contains(lab.screen(), "decided") {
		t.Fatalf("the old receipt is still drawn under the question it is about:\n%s", lab.screen())
	}
	lab.tick(questionSettle)
	lab.rows()
	lab.press("2")
	if len(lab.answer) != 2 {
		t.Fatalf("the change never reached the door: %+v", lab.answer)
	}
	if rows := questionPlainRows(lab.rows()); len(rows) != 1 || !strings.Contains(rows[0], "changed from sqlite") {
		t.Fatalf("the new receipt does not say what it replaced:\n%s", lab.screen())
	}
	changed := lab.answer[1]
	if !changed.Revises {
		t.Fatalf("the second answer does not say it is a change: %+v", changed)
	}
	if changed.FirstKey() != "2" {
		t.Fatalf("the change carries %q", changed.FirstKey())
	}
	if lab.answer[0].Revises {
		t.Fatal("the first answer claimed to be a change")
	}
}

// A DECISION THAT CANNOT BE WALKED BACK OFFERS NO KEY. The receipt says `cannot
// change`, and `c` is a letter somebody is typing.
func TestAnIrreversibleReceiptTakesNoChangeKey(t *testing.T) {
	lab := newQuestionLab(t)
	q := askedQuestion()
	q.Stakes = session.StakesIrreversible
	lab.raise(q)
	lab.tick(questionSettle)
	lab.rows()
	lab.press("1")
	if lab.press("c") {
		t.Fatal("`c` was taken on an irreversible decision")
	}
	if lab.a.questionCount() != 0 {
		t.Fatal("an irreversible decision was asked again")
	}
}

// AND A HALF-TYPED SENTENCE KEEPS ITS LETTERS. The receipt's key is only the
// receipt's while the box is empty.
func TestTheChangeKeyIsALetterWhileSomebodyIsTyping(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(askedQuestion())
	lab.tick(questionSettle)
	lab.rows()
	lab.press("1")
	lab.a.input.setText("cannot be")
	if lab.press("c") {
		t.Fatal("`c` was taken out of a sentence somebody was typing")
	}
}

// A KEY ON A QUESTION WHOSE CLOCK ANSWERS STOPS THAT CLOCK IN THE ENGINE, and
// the row says `paused` on the same frame rather than counting down to a
// decision nobody is going to make.
func TestAKeyOnACountdownHoldsItInTheEngineAndTheRowSaysPaused(t *testing.T) {
	lab := newQuestionLab(t)
	q := askedQuestion()
	q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: time.Minute}
	q.Deadline = lab.at.Add(30 * time.Second)
	lab.raise(q)
	lab.tick(questionSettle)
	rows := strings.Join(lab.rows(), "\n")
	if !strings.Contains(rows, "sqlite in 30s") {
		t.Fatalf("the countdown does not say which answer it will take:\n%s", rows)
	}
	lab.press("down")
	if lab.agent.held != nil {
		t.Fatal("the engine was told from inside the key routine rather than from a command")
	}
	// THE DOOR IS ASKED FROM A COMMAND, which is what [app.Update] drains on the
	// line every message passes through; a test that reached the engine without
	// running one would be testing a call this surface may not make.
	hold := lab.a.takeQuestionHolds()
	if hold == nil {
		t.Fatal("no hold was handed back as a command")
	}
	hold()
	if held := lab.agent.held; len(held) != 1 || held[0].kind != session.QuestionAsk || held[0].token != "3" {
		t.Fatalf("the engine was not told to stop counting: %+v", held)
	}
	rows = strings.Join(lab.rows(), "\n")
	if !strings.Contains(rows, questionPausedWord) {
		t.Fatalf("the row does not say it is paused:\n%s", rows)
	}
	if strings.Contains(rows, "in 30s") {
		t.Fatalf("the countdown is still drawn after the hold:\n%s", rows)
	}
}

// THE COUNTDOWN SAYS HOW TO STOP IT AND THAT IT CAN BE WALKED BACK. A clock
// nobody knows how to stop, on a pick nobody knows they can change, is the shape
// this program must never have (F41).
func TestAQuestionWithAClockSaysHowToStopItAndOnlyWhileItRuns(t *testing.T) {
	lab := newQuestionLab(t)
	q := askedQuestion()
	q.Policy = session.Policy{Kind: session.PolicyRecommendThenAuto, After: time.Minute}
	q.Deadline = lab.at.Add(30 * time.Second)
	lab.raise(q)
	lab.tick(questionSettle)
	rows := strings.Join(lab.rows(), "\n")
	if !strings.Contains(rows, questionClockAsideWord) {
		t.Fatalf("the countdown does not say what a person can do about it:\n%s", rows)
	}
	// AND IT GOES WITH THE CLOCK. A held question has nothing left to stop, and
	// a line about a key that would do nothing is a line that teaches a lie.
	lab.press("down")
	if hold := lab.a.takeQuestionHolds(); hold != nil {
		hold()
	}
	if held := strings.Join(lab.rows(), "\n"); strings.Contains(held, questionClockAsideWord) {
		t.Fatalf("the paused question still says any key stops the clock:\n%s", held)
	}
}

// A QUESTION WITH NO CLOCK IS TOLD NOTHING ABOUT ONE.
func TestAQuestionThatSimplyWaitsSaysNothingAboutAClock(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(askedQuestion())
	lab.tick(questionSettle)
	if rows := strings.Join(lab.rows(), "\n"); strings.Contains(rows, questionClockAsideWord) {
		t.Fatalf("a question that simply waits was given a countdown's line:\n%s", rows)
	}
}

// `u undo` HANDS BACK A PERMISSION THE PERSON HAS JUST GIVEN, and it is an
// answer like any other: the deny goes through the one door with `Revises` on
// it, so the engine takes back the memo and the account capability and the
// record says what it replaced.
func TestUndoOnAPermissionReceiptTakesTheStandingYesBack(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(alwaysAnsweredQuestion())
	lab.tick(questionSettle)
	lab.rows()
	// `2` is `always` on the consent lane's own answers.
	lab.press("2")
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 || !strings.Contains(rows[0], "u undo") {
		t.Fatalf("the receipt does not offer to hand the permission back:\n%s", lab.screen())
	}
	if !lab.press("u") {
		t.Fatal("`u` on the receipt was not taken")
	}
	if len(lab.answer) != 2 {
		t.Fatalf("the undo never reached the door: %+v", lab.answer)
	}
	undo := lab.answer[1]
	if !undo.Revises {
		t.Fatalf("the undo does not say it is a change: %+v", undo)
	}
	if action, ok := session.AnswerFromKey(session.QuestionConsent, undo.FirstKey()); !ok || action.Allow {
		t.Fatalf("the undo did not deny: %+v", undo)
	}
	// AND IT ASKS NOTHING. Undoing is not changing your mind about which answer
	// to give; the person has already said what they want.
	if lab.a.questionCount() != 0 {
		t.Fatalf("the undo put a question back: %d open", lab.a.questionCount())
	}
}

// AND A DECISION THAT GRANTED NOTHING OFFERS NO UNDO. `allow once` is already
// over, and a deny gave nothing away.
func TestOnlyAStandingYesIsOfferedAnUndo(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(alwaysAnsweredQuestion())
	lab.tick(questionSettle)
	lab.rows()
	lab.press("1")
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 {
		t.Fatalf("one answer did not leave one receipt:\n%s", lab.screen())
	}
	if strings.Contains(rows[0], "u undo") {
		t.Fatalf("an `allow once` was offered an undo:\n%s", rows[0])
	}
	if lab.press("u") {
		t.Fatal("`u` was taken on a decision with nothing to hand back")
	}
}

// alwaysAnsweredQuestion is the approval gate's own question, which is the one
// shape `u undo` is about: a yes to it can buy something standing.
func alwaysAnsweredQuestion() session.Question {
	return session.Question{
		ID: 4, Kind: session.QuestionConsent, Ask: session.AskPermission, Form: session.FormLine,
		Asker:    session.Asker{Kind: session.AskerEngine},
		Head:     "run `git push` in this project?",
		Subject:  session.SubjectRef{Kind: session.SubjectCall, Name: "run_shell"},
		Options:  session.AnswerOptions(session.QuestionConsent),
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
	}
}
