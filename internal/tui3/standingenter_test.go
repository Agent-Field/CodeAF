package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// standingQuestionOf is the question a standing card raises: the same shape
// [app.standingQuestion] builds for the lane's notice, spelled here so the
// test owns the shape it is holding [app.questionEnter] to.
func standingQuestionOf() session.Question {
	it := standing.Item{When: standing.When{Kind: standing.WhenHold}}
	return session.Question{
		ID:      5,
		Kind:    session.QuestionStanding,
		Ask:     session.AskChoice,
		Form:    session.FormCard,
		Asker:   session.Asker{Kind: session.AskerModel},
		Head:    session.StandingHeadRule,
		Reason:  session.StandingAskReason,
		Subject: session.SubjectRef{Kind: session.SubjectOrder, ID: 5, Name: "daily build check"},
		Options: session.StandingOptions(it),
		Stakes:  session.StakesReversible,
		Scope:   []session.AnswerScope{session.ScopeOnce, session.ScopeAlways},
		Input:   session.InputShape{Kind: session.InputText, Prompt: session.StandingChangeHint(it)},
		Asked:   time.Date(2026, time.September, 25, 14, 0, 0, 0, time.UTC),
	}
}

// ENTER ON A STANDING CARD TAKES THE ANSWER THE POINTER IS ON. The card's box
// is its correction lane ([session.InputText]), but the card is still a
// question WITH ANSWERS, and every question with answers has a pointer the
// arrows walk — so `enter` over an empty box takes the pointed answer rather
// than doing nothing. The owner, 2026-09-25, #1506: Enter selected nothing;
// only a mouse click answered.
func TestEnterOnAStandingCardTakesTheAnswerThePointerIsOn(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(standingQuestionOf())
	lab.tick(2 * time.Second) // past the settle guard

	// The box is empty and the pointer stands on the first answer.
	if !lab.press(questionEnterKey) {
		t.Fatalf("enter was not taken on the standing card at all")
	}
	if len(lab.answer) != 1 {
		t.Fatalf("enter answered nothing: %v", lab.answer)
	}
	got := lab.answer[0]
	if len(got.Picked) == 0 || got.Picked[0] != "1" {
		t.Fatalf("enter took %v, want the first answer (keep the rule)", got.Picked)
	}
	if got.Change != "" {
		t.Fatalf("enter sent words %q from an empty box", got.Change)
	}
}

// THE ARROWS MOVE THE POINTER AND ENTER MOVES WITH IT, so a person who walked
// down to the second answer and pressed enter answered that one — not the
// first row, and not nothing.
func TestEnterOnAStandingCardTakesTheAnswerThePointerWalkedTo(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(standingQuestionOf())
	lab.tick(2 * time.Second) // past the settle guard

	if !lab.press("down") {
		t.Fatalf("the walk was not taken on the standing card")
	}
	if !lab.press(questionEnterKey) {
		t.Fatalf("enter was not taken after the walk")
	}
	if len(lab.answer) != 1 {
		t.Fatalf("enter answered nothing: %v", lab.answer)
	}
	if len(lab.answer[0].Picked) == 0 || lab.answer[0].Picked[0] == "1" {
		t.Fatalf("enter took %v, want the second answer the pointer stood on", lab.answer[0].Picked)
	}
}

// AND THE CARD ANSWERS TO THE WORDS THE PROMPT ASKS FOR, which is the
// correction lane this card always had: typing before enter sends the words,
// not the pick ([app.questionEnter]).
func TestAStandingCardTypedAnswerStillTravelsAsWords(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(standingQuestionOf())
	lab.tick(2 * time.Second)

	lab.a.input.setText("only on weekdays")
	if !lab.press(questionEnterKey) {
		t.Fatalf("enter was not taken with words in the box")
	}
	if len(lab.answer) != 1 || lab.answer[0].Change != "only on weekdays" {
		t.Fatalf("enter took %+v, want the correction as words", lab.answer)
	}
}

// AND THE CONNECT KEY OFFER KEEPS ITS OWN LAW, which is what the pointer
// give-up must not reach: one answer, the way out, taken by a digit — enter
// over an empty box does nothing there (connectkey_test.go's own test holds
// this too; this one pins the standing shape BESIDE it).
func TestEnterOnATextQuestionWithNoPickStillDoesNothing(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID:    7,
		Kind:  session.QuestionConnect,
		Ask:   session.AskClarification,
		Form:  session.FormCard,
		Head:  "connect your Notion account?",
		Options: []session.AnswerOption{{Key: "2", Label: "not now", Safe: true}},
		Stakes: session.StakesReversible,
		Scope:  []session.AnswerScope{session.ScopeOnce},
		Input:  session.InputShape{Kind: session.InputText, Prompt: "paste your Notion key"},
		Asked:  time.Date(2026, time.September, 25, 14, 0, 0, 0, time.UTC),
	})
	lab.tick(2 * time.Second)

	if lab.press(questionEnterKey) && len(lab.answer) != 0 {
		t.Fatalf("enter answered a words question with nothing to take: %v", lab.answer)
	}
	if len(lab.a.questions) != 1 {
		t.Fatalf("the offer did not stand")
	}
	// the way out is still the digit
	if !lab.press("2") {
		t.Fatalf("the way out was not taken")
	}
	if len(lab.answer) != 1 {
		t.Fatalf("the digit answered nothing: %v", lab.answer)
	}
}

// AND THE OFFER ROW SAYS WHAT ENTER DOES ON A STANDING CARD, once the table
// offers it ([app.questionAnswerKeys] is one reading for drawn and taken
// alike): the walk and the pick are named where the question offers them.
func TestAStandingCardTableOffersEnterWhileThePointerStandsOnAnAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(standingQuestionOf())

	keys := lab.a.questionAnswerKeys(lab.a.questions[0], formsCard)
	var words []string
	for _, verb := range keys {
		words = append(words, questionKeySpelling(verb.key)+" "+verb.word)
	}
	row := strings.Join(words, questionKeyGap)
	if !strings.Contains(row, "enter take it") {
		t.Fatalf("the standing card's table never offered enter: %s", row)
	}
	if !strings.Contains(row, "↓ choose") {
		t.Fatalf("the standing card's table never offered the walk: %s", row)
	}
}
