package tui3

// THE FOUR SHAPES BELOW A QUESTION, and the blocks an asker hangs off one.
//
// These tests hold rung six of the ladder — "structured input: blanks ·
// checklist · this-or-this · dial, never free text where a key would do" — and
// the six block kinds that carry an answer's evidence. What each one owes is the
// same three things: the keys the shared grammar promises, an answer that comes
// back structured rather than as a sentence somebody has to parse, and a linear
// shape for the reader tier.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE SENTENCE HAS HOLES IN IT, and each hole opens on the asker's default —
// a form that opened empty asks a person to retype what the asker already knew.
func TestBlanksDrawTheAskersSentenceWithItsHolesFilled(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionBlanks())
	drawn := pageText(a)
	for _, want := range []string{"land them in [ ~/notes ]", "as [ a new file ]", "keep the old copy: [ no"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the sentence does not carry %q:\n%s", want, drawn)
		}
	}
}

// `tab` WALKS THE HOLES AND STOPS AT THE ENDS. A form whose tab jumped from the
// last hole to the first is a form people fill in twice.
func TestTabWalksTheBlanksAndStopsAtTheEnd(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionBlanks())
	for range 5 {
		tapNamed(a, tea.KeyTab, 0)
	}
	if got := a.qroom.input.focus; got != 2 {
		t.Errorf("tab should stop at the last hole, got %d", got)
	}
	for range 5 {
		tapNamed(a, tea.KeyTab, tea.ModShift)
	}
	if got := a.qroom.input.focus; got != 0 {
		t.Errorf("shift+tab should stop at the first hole, got %d", got)
	}
}

// `←→` WALKS A CHOICE HOLE'S CHOICES, which is a dial one shape down: the hole
// has a short list and the arrows are how a person sees it without opening
// anything.
func TestArrowsWalkAChoiceBlank(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionBlanks())
	a.qroom.input.focus = 2
	tapNamed(a, tea.KeyRight, 0)
	if got := a.qroom.input.blanks[2].value; got != "yes" {
		t.Errorf("the choice should have moved on, got %q", got)
	}
	tapNamed(a, tea.KeyRight, 0)
	if got := a.qroom.input.blanks[2].value; got != "yes" {
		t.Errorf("a choice at its end should stay there, got %q", got)
	}
}

// EVERY BLANK IS VALIDATED BY ITS KIND, and it says so under the sentence rather
// than at the moment enter is pressed.
func TestABlankSaysWhatItTakesAndWhyWhatIsInItWillNotDo(t *testing.T) {
	q := demoQuestionBlanks()
	q.Input.Blanks = []session.Blank{{Label: "how many", Kind: session.BlankNumber, Default: "twelve"}}
	q.Input.Prompt = "keep {how many} of them"
	a, _ := standingInAQuestion(t, q)
	if drawn := pageText(a); !strings.Contains(drawn, "that is not a number") {
		t.Errorf("a number hole with letters in it should say so:\n%s", drawn)
	}
	q.Input.Blanks[0].Default = "12"
	a, _ = standingInAQuestion(t, q)
	if drawn := pageText(a); !strings.Contains(drawn, "a number") || strings.Contains(drawn, "not a number") {
		t.Errorf("a number hole with a number in it should say what it takes:\n%s", drawn)
	}
}

// AND THE HOLES COME BACK AS FIELDS, keyed by the label the asker gave them,
// never as one sentence somebody has to parse.
func TestBlanksRideTheAnswerAsFields(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionBlanks())
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	blanks := agent.answers[0].Blanks
	if blanks["folder"] != "~/notes" || blanks["name"] != "a new file" || blanks["keep"] != "no" {
		t.Errorf("the holes should ride the answer as fields: %#v", blanks)
	}
}

// `space` TICKS AND `a` TAKES THE ASKER'S SUGGESTION.
func TestAChecklistTicksAndTakesTheSuggestion(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionChecklist())
	tapNamed(a, tea.KeySpace, 0)
	if !a.qroom.input.ticks[0] {
		t.Error("space should tick the focused row")
	}
	if drawn := pageText(a); !strings.Contains(drawn, tokens.GlyphSettled+" 1 rewrite the imports") {
		t.Errorf("a ticked row should draw the tick:\n%s", drawn)
	}
	tap(a, "a")
	ticks := a.qroom.input.ticks
	if !ticks[0] || !ticks[1] || ticks[2] || ticks[3] {
		t.Errorf("the suggestion should be what the asker marked: %#v", ticks)
	}
}

// AND WHAT IS TICKED IS WHAT THE FOOT WOULD SEND, which is one fact rather than
// two.
func TestAChecklistComesBackAsSeveralKeys(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionChecklist())
	tap(a, "a")
	if foot := footText(a); !strings.Contains(foot, "1 rewrite the imports, 2 move the tests beside them") {
		t.Errorf("the foot should list what is ticked:\n%s", foot)
	}
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	if got := agent.answers[0].Picked; len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("a checklist answers with several keys: %#v", got)
	}
}

// `shift+↑↓` ORDERS THE ROWS, and the order the person put them in is the order
// the answer carries.
func TestAChecklistCanBeOrdered(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionChecklist())
	tap(a, "a")
	a.qroom.input.focus = 1
	tapNamed(a, tea.KeyUp, tea.ModShift)
	if got := a.qroom.input.walk(); got[0] != 1 || got[1] != 0 {
		t.Errorf("shift+up should lift the row past its neighbour: %#v", got)
	}
	a.questionSyncTicks()
	if got := a.qroom.picked; len(got) != 2 || got[0] != "2" {
		t.Errorf("the answer should carry the person's own order: %#v", got)
	}
}

// A PAIR HAS EXACTLY THREE ANSWERS AND THE THIRD IS "IT DOES NOT MATTER". A form
// that refused it collects a coin flip and records it as a preference.
func TestAPairTakesTheThirdAnswerAndMovesOn(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionPairs())
	tap(a, "a")
	if got := a.qroom.input.pairs[0].answer; got != "a" {
		t.Errorf("a should answer the first pair, got %q", got)
	}
	if got := a.qroom.input.focus; got != 1 {
		t.Errorf("an answered pair should move on by itself, got %d", got)
	}
	tap(a, "=")
	if got := a.qroom.input.pairs[1].answer; got != questionSameKey {
		t.Errorf("= should be the third answer, got %q", got)
	}
}

// AND `a` MEANS THE FIRST SIDE ON A PAIR AND THE SUGGESTION ON A CHECKLIST —
// the one collision in the grammar, resolved by what is on screen and never by
// giving one of them a second key.
func TestTheOneCollisionInTheGrammarIsResolvedByWhatIsOnScreen(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionPairs())
	tap(a, questionPairAKey)
	if got := a.qroom.input.pairs[0].answer; got != "a" {
		t.Errorf("on a pair, a is the first side, got %q", got)
	}
	b, _ := standingInAQuestion(t, demoQuestionChecklist())
	tap(b, questionSuggestKey)
	if !b.qroom.input.ticks[0] {
		t.Error("off a pair, a takes the suggestion")
	}
}

// THE PAIRS COME BACK IN THE WORDS OF THE SIDE THAT WON, never as `a` or `b` —
// a record that said "a" would be unreadable the moment the question is gone.
func TestPairsComeBackInTheWinningSidesOwnWords(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionPairs())
	tap(a, "a")
	tap(a, "b")
	tap(a, "=")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	blanks := agent.answers[0].Blanks
	if blanks["speed against completeness"] != "finishing tonight" {
		t.Errorf("the first pair should carry its own words: %#v", blanks)
	}
	if blanks["now against later"] != "either" {
		t.Errorf("the third answer should read as itself: %#v", blanks)
	}
}

// THE DIAL SAYS WHAT THE SETTING DOES, which is the difference between a slider
// and a decision.
func TestTheDialSaysWhatTheSettingDoes(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionDial())
	drawn := pageText(a)
	if !strings.Contains(drawn, "[tell me, then act]") {
		t.Errorf("the dial should mark where it sits:\n%s", drawn)
	}
	if !strings.Contains(drawn, "it will tell me, then act") {
		t.Errorf("the dial should say what the setting does:\n%s", drawn)
	}
	tapNamed(a, tea.KeyRight, 0)
	if drawn := pageText(a); !strings.Contains(drawn, "[just do it]") {
		t.Errorf("→ should move the dial:\n%s", drawn)
	}
	tapNamed(a, tea.KeyRight, 0)
	if got := a.qroom.input.notch; got != 2 {
		t.Errorf("a dial at its end should stay there, got %d", got)
	}
}

// THE READER TIER NEVER DRAWS A DIAL AS A PICTURE. DESIGN.md says so by name: a
// row of cells says "third of five" to an eye and nothing at all to a screen
// reader, so the same fact is spelled as a number.
func TestTheReaderTierGetsANumberRatherThanADial(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionDial())
	a.pal.linear = true
	drawn := pageText(a)
	if !strings.Contains(drawn, "2 of 3") {
		t.Errorf("the reader tier should get a number:\n%s", drawn)
	}
	if strings.Contains(drawn, "[tell me, then act]") {
		t.Errorf("the reader tier should not get the picture:\n%s", drawn)
	}
}

// AND THE DIAL COMES BACK AS A NUMBER IN THE ASKER'S OWN UNITS, through the
// pointer field that tells "no dial" apart from "a dial left at zero".
func TestTheDialRidesTheAnswerAsANumber(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionDial())
	tapNamed(a, tea.KeyRight, 0)
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	dial := agent.answers[0].Dial
	if dial == nil || *dial != 2 {
		t.Errorf("the dial should ride the answer as its own number: %#v", dial)
	}
}

// A QUESTION WITH NO DIAL LEAVES THE FIELD ALONE, because nil and zero are
// different answers.
func TestAQuestionWithNoDialLeavesTheFieldNil(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "1")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if agent.answers[0].Dial != nil {
		t.Error("a question with no dial must not record one")
	}
}

// A DIAGRAM IS DRAWN AS IT WAS WRITTEN. Its lines mean what they are; a wrap
// would destroy it.
func TestADiagramIsDrawnAsItWasWritten(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	if drawn := pageText(a); !strings.Contains(drawn, "  app --> pg --> report") {
		t.Errorf("the diagram should keep its own spacing:\n%s", drawn)
	}
}

// A DIFF WEARS THE DIFF GLYPHS AND THE DIFF HUES, so a diff in a question looks
// like a diff in a tool call — both are drawn from one table.
func TestADiffBlockWearsTheDiffGlyphs(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionLayout())
	tap(a, "2")
	drawn := pageText(a)
	if !strings.Contains(drawn, tokens.GlyphDiffAdd+" model  money") {
		t.Errorf("an added line should wear the add glyph:\n%s", drawn)
	}
	if !strings.Contains(drawn, tokens.GlyphDiffDel+" model  context  money") {
		t.Errorf("a removed line should wear the del glyph:\n%s", drawn)
	}
}

// A TWO-PANE LAYOUT IS SIDE BY SIDE ABOVE A HUNDRED COLUMNS AND STACKED BELOW,
// which is DESIGN.md's own number: a pane cut to thirty characters is a pane
// that has stopped being pre-formatted.
func TestALayoutStacksOnANarrowPage(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionLayout())
	a.width = 120
	wide := pageText(a)
	sideBySide := false
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, "deepseek-v4-flash") && strings.Contains(line, "deepseek-v4-fl...") {
			sideBySide = true
		}
	}
	if !sideBySide {
		t.Errorf("above a hundred columns the panes stand side by side:\n%s", wide)
	}
	a.width = 80
	a.qroom.dirty = true
	narrow := pageText(a)
	for _, line := range strings.Split(narrow, "\n") {
		if strings.Contains(line, "deepseek-v4-flash") && strings.Contains(line, "deepseek-v4-fl...") {
			t.Errorf("under a hundred columns the panes stack:\n%s", narrow)
		}
	}
}

// AN IMAGE THIS TERMINAL CANNOT PAINT IS THE PATH, WHOLE, AND A WAY IN. A path
// with an ellipsis in it is a path nobody can open.
func TestAnImageThatCannotBePaintedIsThePathAndAWayIn(t *testing.T) {
	q := demoQuestionReading()
	q.Options[0].Blocks = []session.Block{{Kind: session.BlockImage, Path: "/tmp/aforge-question-fixture/cover.png"}}
	a, _ := standingInAQuestion(t, q)
	drawn := pageText(a)
	if !strings.Contains(drawn, "/tmp/aforge-question-fixture/cover.png") {
		t.Errorf("the path should be on the page whole:\n%s", drawn)
	}
	if !strings.Contains(drawn, strings.TrimSpace(questionOpenWord)) {
		t.Errorf("a picture that cannot be painted should offer the way in:\n%s", drawn)
	}
}

// A TABLE BLOCK KEEPS ITS COLUMNS AND ITS HEADER.
func TestATableBlockKeepsItsColumns(t *testing.T) {
	q := demoQuestionReading()
	q.Attach = []session.Block{{Kind: session.BlockTable, Title: "what is in there now", Rows: [][]string{
		{"table", "rows"}, {"ledger", "0"}, {"entries", "41,208"},
	}}}
	a, _ := standingInAQuestion(t, q)
	drawn := pageText(a)
	for _, want := range []string{"what is in there now", "table", "entries", "41,208"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the table does not carry %q:\n%s", want, drawn)
		}
	}
}

// EVERY ROW OF EVERY SHAPE FITS ITS PAGE, at the widths this surface actually
// gets drawn at.
func TestEveryInputShapeFitsEveryWidth(t *testing.T) {
	for name, build := range questionDemos {
		for _, width := range []int{40, 64, 80, 100, 120} {
			a, _ := standingInAQuestion(t, build())
			a.width = width
			a.qroom.dirty = true
			for _, r := range a.questionRoomRows(width) {
				if got := len([]rune(plain(r.text))); got > width {
					t.Errorf("%s at %d cols: a row is %d wide: %q", name, width, got, plain(r.text))
				}
			}
			for _, line := range a.questionFootRows(width) {
				if got := len([]rune(plain(line))); got > width {
					t.Errorf("%s at %d cols: a foot row is %d wide: %q", name, width, got, plain(line))
				}
			}
		}
	}
}
