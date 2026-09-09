package tui3

// THE PAGE A QUESTION OPENS INTO.
//
// These tests hold what docs/design/questions/DESIGN.md promises about the room
// form and what the surface's own laws demand of anything drawn over the
// conversation: the four attribution facts are on it, the pick is marked with
// its reason and what would change its mind, `x` compares only what differs and
// stacks under eighty columns, `c` and `?` write under the part they were
// pressed on, the foot composes `pick · with · notes · scope`, `d` shows the
// pick before it hands over, the settle guard drops the first quarter second of
// keys, `esc` folds without answering, and nothing on the page takes a letter
// out of a box somebody is typing in.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// answeringAgent is a session that can resolve a question and remembers what it
// was handed, which is the whole of what these tests need from an engine.
type answeringAgent struct {
	*fakeAgent
	answers []session.Answer
	refuse  error
}

func (g *answeringAgent) ResolveQuestion(answer session.Answer) error {
	if g.refuse != nil {
		return g.refuse
	}
	g.answers = append(g.answers, answer)
	return nil
}

// standingInAQuestion is a surface with the worked example open and the settle
// guard already spent.
func standingInAQuestion(t *testing.T, q session.Question) (*app, *answeringAgent) {
	t.Helper()
	agent := &answeringAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	a.openQuestionRoom(q)
	a.qroom.shown = a.qroom.shown.Add(-time.Second)
	return a, agent
}

// pageText is the body region as a reader sees it.
func pageText(a *app) string {
	rows := a.questionRoomRows(a.width)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = plain(r.text)
	}
	return strings.Join(out, "\n")
}

// footText is the pinned foot as a reader sees it.
func footText(a *app) string {
	rows := a.questionFootRows(a.width)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = plain(r)
	}
	return strings.Join(out, "\n")
}

func tap(a *app, key string) {
	a.questionRoomKey(tea.KeyPressMsg{Code: keyCodeOf(key), Text: key})
}

// keyCodeOf spells a one-character key the way bubbletea does, so a test presses
// what a terminal sends.
func keyCodeOf(key string) rune {
	if len([]rune(key)) == 1 {
		return []rune(key)[0]
	}
	return 0
}

// tapNamed sends a key that has a name rather than a character.
func tapNamed(a *app, code rune, mod tea.KeyMod) (tea.Cmd, bool) {
	return a.questionRoomKey(tea.KeyPressMsg{Code: code, Mod: mod})
}

// THE FOUR ATTRIBUTION FACTS ARE ON THE PAGE. DESIGN.md names them together —
// "asked by · why now · what is paused on it · what goes on without it" — and
// each one on its own is a page that reads as an interruption rather than a
// decision.
func TestTheRoomSaysWhoIsAskingWhyNowAndWhatIsWaiting(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	drawn := pageText(a)
	for _, want := range []string{
		"which store should the ledger sit on?",
		"the model",
		"a schema change is next and it is cheaper before there are rows",
		questionWaitsWord,
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the page does not say %q:\n%s", want, drawn)
		}
	}
}

// A QUESTION NOTHING IS WAITING ON SAYS SO. The emptiness law is not "draw
// nothing" here: the fact a person wants is whether the machine has stopped, and
// silence answers it wrongly in the direction that makes them hurry.
func TestAQuestionNothingWaitsOnSaysSoRatherThanNothing(t *testing.T) {
	q := demoQuestionReading()
	q.Blocking = session.Blocking{}
	a, _ := standingInAQuestion(t, q)
	if drawn := pageText(a); !strings.Contains(drawn, questionNothingWord) {
		t.Errorf("a question nothing waits on should say so:\n%s", drawn)
	}
}

// THE PICK IS MARKED, WITH ITS REASON, ITS CONFIDENCE AND WHAT WOULD CHANGE IT.
// The last of those is the most useful line on the page: a person who disagrees
// with a pick nearly always disagrees with exactly that condition.
func TestThePickCarriesItsReasonConfidenceAndWhatWouldChangeIt(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	drawn := pageText(a)
	for _, want := range []string{
		questionPickWord,
		"fairly sure",
		"because it is the only store the reporting job already reads",
		questionWouldSwitchWord + "the ledger ever has to run on a machine with no server on it",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the pick does not carry %q:\n%s", want, drawn)
		}
	}
}

// THE PAGE OPENS ON THE PICK AND FOLDS THE REST. A page that opened everything
// is a wall, and one that opened nothing makes a person press a key to read the
// answer the asker would take.
func TestThePageOpensOnThePickAndFoldsTheRest(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	drawn := pageText(a)
	if !strings.Contains(drawn, "Rows already carry a foreign key") {
		t.Errorf("the pick should be open:\n%s", drawn)
	}
	if strings.Contains(drawn, "One file in the repository") {
		t.Errorf("everything but the pick should be folded:\n%s", drawn)
	}
	if !strings.Contains(drawn, tokens.GlyphCollapsed) || !strings.Contains(drawn, tokens.GlyphExpanded) {
		t.Errorf("the sections should draw both fold marks:\n%s", drawn)
	}
}

// A DIGIT TAKES AN ANSWER AND OPENS IT, and the foot says what enter would send.
func TestADigitTakesAnAnswerAndTheFootSaysWhatWouldBeSent(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "2")
	if drawn := pageText(a); !strings.Contains(drawn, "One file in the repository") {
		t.Errorf("picking an answer should open it:\n%s", drawn)
	}
	if foot := footText(a); !strings.Contains(foot, questionAnsweringWord+"2 sqlite beside the project") {
		t.Errorf("the foot should say what would be sent:\n%s", foot)
	}
}

// THE EMPTINESS LAW ON THE FOOT: nothing chosen draws no answer line and no
// promise about enter.
func TestTheFootOffersNoAnswerUntilThereIsOne(t *testing.T) {
	q := demoQuestionReading()
	q.Pick = nil
	a, _ := standingInAQuestion(t, q)
	foot := footText(a)
	if !strings.Contains(foot, questionNoPickWord) {
		t.Errorf("the foot should say nothing is chosen:\n%s", foot)
	}
	if strings.Contains(foot, questionKeyWord(questionActTake)) {
		t.Errorf("a question with no pick must not offer enter as taking one:\n%s", foot)
	}
}

// `x` COMPARES ONLY WHAT DIFFERS. Every answer in the fixture runs somewhere
// different, so all three axes stay; an axis they agreed on would be dropped and
// the foot says the table is differences only.
func TestCompareLaysTheAnswersOutAndSaysItIsDifferencesOnly(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "x")
	drawn := pageText(a)
	for _, want := range []string{"runs on", "backing up", "reporting", questionCompareOnly} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the compare table does not carry %q:\n%s", want, drawn)
		}
	}
	// The header row names every answer, which is what makes it a comparison.
	if !strings.Contains(drawn, "1 postgres") || !strings.Contains(drawn, "3 a file per day") {
		t.Errorf("the compare table should head each answer:\n%s", drawn)
	}
}

// AN AXIS EVERY ANSWER READS THE SAME ON IS NOT A COMPARISON. It is dropped
// rather than drawn grey, because the row it would take is the row the axis that
// decides the question has to be found in.
func TestCompareDropsAnAxisEveryAnswerAgreesOn(t *testing.T) {
	q := demoQuestionReading()
	for i := range q.Options {
		q.Options[i].Dimensions = map[string]string{"runs on": "this machine", "backing up": q.Options[i].Key}
	}
	a, _ := standingInAQuestion(t, q)
	tap(a, "x")
	drawn := pageText(a)
	if strings.Contains(drawn, "runs on") {
		t.Errorf("an axis every answer agrees on should be dropped:\n%s", drawn)
	}
	if !strings.Contains(drawn, "backing up") {
		t.Errorf("the axis they differ on should stay:\n%s", drawn)
	}
}

// COMPARE STACKS UNDER EIGHTY COLUMNS, which is DESIGN.md's own number. A column
// cut to nine characters is a column that lies.
func TestCompareStacksOnANarrowPage(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	a.width = 64
	tap(a, "x")
	drawn := pageText(a)
	// Stacked, every answer's own word is on a row of its own with its readings
	// underneath, so the three answers appear on three separate lines.
	lines := strings.Split(drawn, "\n")
	heads := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "1 postgres") ||
			strings.HasPrefix(strings.TrimSpace(line), "2 sqlite") ||
			strings.HasPrefix(strings.TrimSpace(line), "3 a file") {
			heads++
		}
	}
	if heads != 3 {
		t.Errorf("a stacked compare should head each answer on its own row, got %d:\n%s", heads, drawn)
	}
	for _, line := range lines {
		if got := len([]rune(line)); got > a.width {
			t.Errorf("a stacked compare row overflowed the page (%d > %d): %q", got, a.width, line)
		}
	}
}

// THE FALLBACK DERIVES AXES FROM THE `+`/`−` LINES. Most askers never fill in
// dimensions, and nearly all of them write consequence lines, so a table that
// needed the structured field would be a table nobody ever saw.
func TestCompareFallsBackToTheConsequenceLines(t *testing.T) {
	q := demoQuestionReading()
	for i := range q.Options {
		q.Options[i].Dimensions = nil
	}
	a, _ := standingInAQuestion(t, q)
	tap(a, "x")
	drawn := pageText(a)
	for _, want := range []string{questionGainWord, questionCostWord, "one place to back up"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the fallback table does not carry %q:\n%s", want, drawn)
		}
	}
}

// NO DIMENSIONS AND NO CONSEQUENCE LINES MEANS NO `x` ON THE OFFER ROW. The
// emptiness law: a key that would draw an empty table is a key that must not be
// named.
func TestAQuestionWithNothingToCompareDoesNotOfferCompare(t *testing.T) {
	q := demoQuestionReading()
	for i := range q.Options {
		q.Options[i].Dimensions, q.Options[i].Body = nil, "just this"
	}
	a, _ := standingInAQuestion(t, q)
	if foot := footText(a); strings.Contains(foot, "x "+questionKeyWord(questionActCompare)) {
		t.Errorf("nothing to compare should not offer x:\n%s", foot)
	}
}

// `c` WRITES UNDER THE ANSWER IT WAS PRESSED ON, in the person's own ink, led by
// the composer's own prompt mark.
func TestCommentLandsUnderTheAnswerItWasPressedOn(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "c")
	if foot := footText(a); !strings.Contains(foot, questionCommentWord) {
		t.Errorf("the foot should say the box is writing a comment:\n%s", foot)
	}
	a.input.setText("only if the reporting job keeps its own copy")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	drawn := pageText(a)
	if !strings.Contains(drawn, tokens.GlyphPromptChat+" only if the reporting job keeps its own copy") {
		t.Errorf("the comment should be drawn under its answer:\n%s", drawn)
	}
	if foot := footText(a); !strings.Contains(foot, "1"+questionNotesWord) {
		t.Errorf("the foot should count the comment:\n%s", foot)
	}
}

// AND IT GOES INTO THE RECORD AS A NOTE ON A PART, never as the answer itself.
func TestCommentsRideTheAnswerAsNotesOnParts(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "c")
	a.input.setText("keep the sqlite file")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	tap(a, "1")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	answer := agent.answers[0]
	if answer.Comments["1"] != "keep the sqlite file" {
		t.Errorf("the comment should be filed under its answer's key: %#v", answer.Comments)
	}
	if answer.Change != "" {
		t.Errorf("a comment is not the words beside the pick: %q", answer.Change)
	}
}

// `?` ASKS THE ASKER ONE THING WITH THE QUESTION STILL OPEN, and the reply is
// drawn in place under the row it was asked from.
func TestAskBackSendsOneThingAndDrawsTheReplyInPlace(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "?")
	a.input.setText("does the reporting job read it directly")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.qroom == nil {
		t.Fatal("asking back must not close the question")
	}
	if drawn := pageText(a); !strings.Contains(drawn, "does the reporting job read it directly") {
		t.Errorf("the question asked should be on the page:\n%s", drawn)
	}
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "it does, over the same connection string"})
	drawn := pageText(a)
	if !strings.Contains(drawn, tokens.GlyphReplyIn+" it does, over the same connection string") {
		t.Errorf("the reply should be drawn in place:\n%s", drawn)
	}
}

// ONE EXCHANGE PER ANSWER, and the refusal says so rather than doing nothing.
func TestAskBackIsBoundedAtOnePerAnswerAndSaysSo(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "?")
	a.input.setText("one")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	tap(a, "?")
	if foot := footText(a); !strings.Contains(foot, questionAskedWord) {
		t.Errorf("a second ask should say why it was refused:\n%s", foot)
	}
}

// AND THE EXCHANGE CLOSES WITH THE QUESTION AND LANDS IN THE RECORD.
func TestTheExchangeRidesTheAnswer(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "?")
	a.input.setText("does it read it directly")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "it does"})
	pageText(a)
	tap(a, "1")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	back := agent.answers[0].AskedBack
	if len(back) != 1 || back[0].Asked != "does it read it directly" || back[0].Replied != "it does" {
		t.Errorf("the exchange should ride the answer whole: %#v", back)
	}
}

// `d` SHOWS THE PICK AND THE REASON BEFORE IT HANDS OVER. Delegating a decision
// sight-unseen is how a person finds out later that they agreed to something.
func TestYouDecideShowsThePickAndReasonBeforeHandingOver(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "d")
	foot := footText(a)
	if !strings.Contains(foot, questionDecideWord+"1 postgres") {
		t.Errorf("the first press should show what it would take:\n%s", foot)
	}
	if !strings.Contains(foot, "because it is the only store the reporting job already reads") {
		t.Errorf("the first press should show why:\n%s", foot)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("the first press must not answer anything: %#v", agent.answers)
	}
	tap(a, "d")
	if len(agent.answers) != 1 {
		t.Fatalf("the second press should hand over, got %d answers", len(agent.answers))
	}
	if by := agent.answers[0].DecidedBy; by != session.DecidedByAsker {
		t.Errorf("a handed-over decision is the asker's, not the person's: %q", by)
	}
}

// AND ANY OTHER KEY TAKES THE PROMISE BACK, which is what makes the first press
// safe to try.
func TestAnyOtherKeyCancelsYouDecide(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "d")
	tap(a, "2")
	tap(a, "d")
	if len(agent.answers) != 0 {
		t.Fatalf("d after another key must ask again, not hand over: %#v", agent.answers)
	}
}

// `n` IS THE ANSWER THAT IS NOT ON THE LIST, and it comes back as a reframe
// rather than as a pick.
func TestReframeComesBackAsTheRealQuestion(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "n")
	if foot := footText(a); !strings.Contains(foot, questionReframeWord) {
		t.Errorf("the foot should say the box is writing the real question:\n%s", foot)
	}
	a.input.setText("whether the ledger belongs in this project at all")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	answer := agent.answers[0]
	if answer.Reframe != "whether the ledger belongs in this project at all" {
		t.Errorf("the reframe should ride the answer: %q", answer.Reframe)
	}
	if len(answer.Picked) != 0 {
		t.Errorf("a reframe is not a pick: %#v", answer.Picked)
	}
}

// THE SETTLE GUARD DROPS THE FIRST QUARTER SECOND. A page that appeared under a
// hand already moving must not turn the next keystroke into an answer.
func TestTheSettleGuardDropsAKeyThatWasAlreadyTravelling(t *testing.T) {
	agent := &answeringAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	a.openQuestionRoom(demoQuestionReading())
	tap(a, "2")
	if got := a.qroom.picked; len(got) != 0 {
		t.Errorf("a key inside the settle window must be dropped, got %#v", got)
	}
	a.qroom.shown = a.qroom.shown.Add(-questionSettle)
	tap(a, "2")
	if got := a.qroom.picked; len(got) != 1 || got[0] != "2" {
		t.Errorf("a key after the settle window should be taken, got %#v", got)
	}
}

// `esc` IS LATER AND NEVER AN ANSWER. The question folds away and nothing is
// decided — DESIGN.md is explicit that the turn stays paused on it.
func TestEscapeFoldsTheQuestionWithoutAnsweringIt(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "1")
	if _, took := tapNamed(a, tea.KeyEscape, 0); !took {
		t.Fatal("esc should be taken by the page")
	}
	if a.questionRoomOpen() {
		t.Error("esc should fold the page away")
	}
	if len(agent.answers) != 0 {
		t.Errorf("esc must answer nothing: %#v", agent.answers)
	}
}

// AND esc LEAVES THE PART BEFORE IT LEAVES THE PAGE. A person who pressed `c`
// and changed their mind is asking for the comment to go, not for the page.
func TestEscapeLeavesTheCommentBeforeItLeavesThePage(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(a, "c")
	tapNamed(a, tea.KeyEscape, 0)
	if !a.questionRoomOpen() {
		t.Fatal("the first esc should only leave the comment")
	}
	if a.qroom.commenting != "" {
		t.Error("the first esc should clear what the box is writing to")
	}
	tapNamed(a, tea.KeyEscape, 0)
	if a.questionRoomOpen() {
		t.Error("the second esc should fold the page")
	}
}

// THE BOX WINS EVERY LETTER. A page that took `c` out of a sentence somebody was
// typing would make the box it points at unusable — which is the surface's own
// law about bare letters, stated once more here.
func TestALetterIsALetterTheMomentThereIsASentence(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	a.input.setText("I was writing something")
	if _, took := a.questionRoomKey(tea.KeyPressMsg{Code: 'c', Text: "c"}); took {
		t.Error("a letter must fall through to a box that has words in it")
	}
	if a.qroom.commenting != "" {
		t.Error("a letter that fell through must not have acted")
	}
}

// WHAT WAS TYPED BESIDE THE PICK RIDES THE ANSWER AS THE PERSON'S OWN WORDS —
// which is the half of an answer that carries their intent.
func TestWordsTypedBesideThePickRideTheAnswer(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(a, "1")
	a.input.setText("but keep the sqlite file as the source of truth")
	if foot := footText(a); !strings.Contains(foot, questionWithWord+"but keep the sqlite file") {
		t.Errorf("the foot should show what would go with the pick:\n%s", foot)
	}
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	if got := agent.answers[0].Change; got != "but keep the sqlite file as the source of truth" {
		t.Errorf("the words beside the pick should ride the answer: %q", got)
	}
}

// A SESSION THAT CAN READ A QUESTION AND NOT ANSWER ONE SAYS SO. A capability
// that cannot work is absent, and where a key is in the shared grammar the page
// has to say why it did nothing rather than swallowing the press.
func TestAWindowThatCannotResolveSaysSoRatherThanFailingSilently(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 92, 30
	a.openQuestionRoom(demoQuestionReading())
	a.qroom.shown = a.qroom.shown.Add(-time.Second)
	tap(a, "1")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if foot := footText(a); !strings.Contains(foot, "not answer it") {
		t.Errorf("a window with no door should say so:\n%s", foot)
	}
}

// AND A REFUSAL FROM THE ENGINE IS SHOWN WHERE THE FOOT WAS. A refusal a person
// cannot see is an answer that silently did nothing.
func TestAnEngineRefusalIsDrawnWhereTheFootWas(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	agent.refuse = errQuestionTest
	tap(a, "1")
	a.questionRoomKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if foot := footText(a); !strings.Contains(foot, errQuestionTest.Error()) {
		t.Errorf("the refusal should be on screen:\n%s", foot)
	}
}

// errQuestionTest is a refusal with a person-facing sentence, which is what
// every refusal in this codebase is.
var errQuestionTest = errTest("that task already finished")

type errTest string

func (e errTest) Error() string { return string(e) }

// EVERY ROW FITS THE PAGE. A page drawn over the conversation that overflowed
// its width would wrap into the row below it and the geometry the frame counted
// would be a row short.
func TestEveryRowOfThePageFitsItsWidth(t *testing.T) {
	for _, width := range []int{40, 64, 80, 92, 120} {
		a, _ := standingInAQuestion(t, demoQuestionReading())
		a.width = width
		for _, name := range []string{"", "x"} {
			if name != "" {
				tap(a, name)
			}
			for _, r := range a.questionRoomRows(width) {
				if got := len([]rune(plain(r.text))); got > width {
					t.Errorf("at %d cols a row is %d wide: %q", width, got, plain(r.text))
				}
			}
			for _, line := range a.questionFootRows(width) {
				if got := len([]rune(plain(line))); got > width {
					t.Errorf("at %d cols a foot row is %d wide: %q", width, got, plain(line))
				}
			}
		}
	}
}

// THE PAGE IS NOT MODAL. Nothing under it stops: the conversation is still there
// and `esc` restores it with its scroll untouched, which is room.go's promise
// kept by a second page.
func TestThePageLeavesTheConversationExactlyWhereItWas(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	a.offset, a.stick = 7, false
	tap(a, "2")
	tapNamed(a, tea.KeyEscape, 0)
	if a.offset != 7 || a.stick {
		t.Errorf("the conversation's own scroll must not move: offset %d stick %v", a.offset, a.stick)
	}
}

// NO MACHINERY VOCABULARY ON ANYTHING A PERSON READS. DESIGN.md bans four words
// by name and the task states banned four more; this page says none of them.
func TestThePageSaysNoMachineryWords(t *testing.T) {
	banned := []string{"prompt", "modal", "dialog", "approval gate", "auditor", "verdict", "verified", "refuted"}
	a, _ := standingInAQuestion(t, demoQuestionReading())
	for _, key := range []string{"", "x", "c", "?", "d"} {
		if key != "" {
			tap(a, key)
		}
		drawn := strings.ToLower(pageText(a) + "\n" + footText(a))
		for _, word := range banned {
			if strings.Contains(drawn, word) {
				t.Errorf("after %q the page says %q:\n%s", key, word, drawn)
			}
		}
	}
}

// THE BOX IS A BOX FROM THE FIRST KEYSTROKE ONCE IT IS POINTED AT A PART. This
// cost the first two letters of every comment: `c` then "only if…" lost the `o`
// to the fold and the `n` to the reframe, because the box was still empty and
// the page was still reading letters as keys.
func TestOnceTheBoxIsPointedAtAPartEveryLetterIsText(t *testing.T) {
	for _, key := range []string{"c", "?", "n"} {
		a, _ := standingInAQuestion(t, demoQuestionReading())
		tap(a, key)
		for _, letter := range []string{"o", "n", "l", "y", "x", "d"} {
			if _, took := a.questionRoomKey(tea.KeyPressMsg{Code: []rune(letter)[0], Text: letter}); took {
				t.Errorf("after %q the page took %q instead of letting it reach the box", key, letter)
			}
		}
		if a.qroom.compare || len(a.qroom.picked) > 0 {
			t.Errorf("after %q a typed letter acted on the page", key)
		}
	}
}
