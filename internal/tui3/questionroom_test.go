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

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// answeringAgent is question_test.go's own [questionAgent] fake with the answers
// it was handed kept beside it. It is that fake and not a second one, because a
// second reading of "what is an engine to this surface" would be exactly the
// duplication [questionAgent] exists to prevent.
type answeringAgent struct {
	*questionScript
	answers []session.Answer
	refuse  error
}

func newAnsweringAgent() *answeringAgent {
	agent := &answeringAgent{questionScript: &questionScript{fakeAgent: &fakeAgent{model: "m"}}}
	agent.questionScript.answer = func(answer session.Answer) error {
		if agent.refuse != nil {
			return agent.refuse
		}
		agent.answers = append(agent.answers, answer)
		return nil
	}
	return agent
}

// standingInAQuestion is a surface with the worked example open and the settle
// guard already spent, at a width that draws the page as ONE column.
func standingInAQuestion(t *testing.T, q session.Question) (*app, *answeringAgent) {
	t.Helper()
	return standingInAQuestionAt(t, q, 92, 40)
}

// standingInAQuestionAt is the same page at a size of the test's choosing — a
// hundred columns and wider draws it as two panes.
func standingInAQuestionAt(t *testing.T, q session.Question, width, height int) (*app, *answeringAgent) {
	t.Helper()
	agent := newAnsweringAgent()
	a := newTestApp(agent)
	a.width, a.height = width, height
	a.raiseQuestionRoom(questionShown{question: q, shown: a.now(), pick: questionPointerStart(q)})
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

// tap presses one key on the page and spends what it handed back: an answer
// travels on the command a key returns, never from the loop (offloop.go).
func tap(t *testing.T, a *app, key string) {
	t.Helper()
	cmd, _ := a.questionRoomKey(tea.KeyPressMsg{Code: keyCodeOf(key), Text: key})
	spend(t, a, cmd)
}

// keyCodeOf spells a one-character key the way bubbletea does, so a test presses
// what a terminal sends.
func keyCodeOf(key string) rune {
	if len([]rune(key)) == 1 {
		return []rune(key)[0]
	}
	return 0
}

// tapNamed sends a key that has a name rather than a character, and spends what
// it handed back for [tap]'s reason.
func tapNamed(t *testing.T, a *app, code rune, mod tea.KeyMod) (tea.Cmd, bool) {
	t.Helper()
	cmd, took := a.questionRoomKey(tea.KeyPressMsg{Code: code, Mod: mod})
	spend(t, a, cmd)
	return cmd, took
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
		questionAskerWord(session.Asker{Kind: session.AskerModel}),
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
		questionRecommendedWord,
		"fairly sure",
		"because it is the only store the reporting job already reads",
		questionWouldSwitchWord + "the ledger ever has to run on a machine with no server on it",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the pick does not carry %q:\n%s", want, drawn)
		}
	}
}

// THE PAGE OPENS ON THE PICK WITH ITS EVIDENCE, AND EVERY OTHER ANSWER IS A ROW.
// A page that drew every answer's evidence is a wall, and one that drew none
// makes a person press a key to read the answer the asker would take. There are
// no sections to fold any more: the evidence follows the pointer.
func TestThePageOpensOnThePickWithItsEvidence(t *testing.T) {
	for _, width := range []int{92, 140} {
		a, _ := standingInAQuestionAt(t, demoQuestionReading(), width, 40)
		drawn := pageText(a)
		if !strings.Contains(drawn, "Rows already carry a foreign key") {
			t.Errorf("at %d columns the pick's evidence should be on the page:\n%s", width, drawn)
		}
		if strings.Contains(drawn, "One file in the repository") {
			t.Errorf("at %d columns only the answer the pointer is on shows its evidence:\n%s", width, drawn)
		}
		// The collapsed mark shares its byte with the pointer, so the open one is
		// the fold mark a page of sections could not draw without.
		if strings.Contains(drawn, tokens.GlyphExpanded) {
			t.Errorf("at %d columns the page still draws fold marks:\n%s", width, drawn)
		}
	}
}

// A DIGIT MOVES THE POINTER TO ITS ANSWER, and the foot says what enter would send.
func TestADigitTakesAnAnswerAndTheFootSaysWhatWouldBeSent(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "2")
	if drawn := pageText(a); !strings.Contains(drawn, "One file in the repository") {
		t.Errorf("a digit should put its answer's evidence on the page:\n%s", drawn)
	}
	if foot := footText(a); !strings.Contains(foot, questionAnsweringWord+"2 sqlite beside the project") {
		t.Errorf("the foot should say what would be sent:\n%s", foot)
	}
}

// THE EMPTINESS LAW ON THE FOOT: nothing chosen draws no answer line, and a
// question with nothing for enter to take does not name the key.
//
// A QUESTION WITH ANSWERS ALWAYS HAS SOMETHING TO TAKE, which is the pointer's
// own law (#789): `enter` takes the answer the pointer is standing on, whether
// or not the asker recommended one. So the shape that drops the key is the one
// with no answers written down at all.
func TestTheFootOffersNoAnswerUntilThereIsOne(t *testing.T) {
	q := demoQuestionReading()
	q.Pick = nil
	a, _ := standingInAQuestion(t, q)
	if foot := footText(a); !strings.Contains(foot, questionAnsweringWord+"1 postgres") {
		t.Errorf("with no pick the foot answers the pointer's own answer:\n%s", foot)
	}
	// AND ON `something else…` WITH AN EMPTY BOX THERE IS NOTHING TO SEND.
	for i := 0; i < len(q.Options); i++ {
		tapNamed(t, a, tea.KeyDown, 0)
	}
	foot := footText(a)
	if !strings.Contains(foot, questionNoPickWord) || strings.Contains(foot, questionKeyWord(questionEnterKey)) {
		t.Errorf("the other row with nothing typed should offer nothing to send:\n%s", foot)
	}
	bare := demoQuestionReading()
	bare.Pick, bare.Options, bare.Attach = nil, nil, nil
	a, _ = standingInAQuestion(t, bare)
	foot = footText(a)
	if !strings.Contains(foot, questionNoPickWord) {
		t.Errorf("the foot should say nothing is chosen:\n%s", foot)
	}
	if strings.Contains(foot, questionKeyWord(questionEnterKey)) {
		t.Errorf("a question with nothing to take must not offer enter:\n%s", foot)
	}
}

// `x` COMPARES ONLY WHAT DIFFERS. Every answer in the fixture runs somewhere
// different, so all three axes stay; an axis they agreed on would be dropped and
// the foot says the table is differences only.
func TestCompareLaysTheAnswersOutAndSaysItIsDifferencesOnly(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "x")
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
	tap(t, a, "x")
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
	tap(t, a, "x")
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
	tap(t, a, "x")
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
	if foot := footText(a); strings.Contains(foot, "x "+questionKeyWord(questionCompareKey)) {
		t.Errorf("nothing to compare should not offer x:\n%s", foot)
	}
}

// `c` WRITES UNDER THE ANSWER IT WAS PRESSED ON, in the person's own ink, led by
// the composer's own prompt mark.
func TestCommentLandsUnderTheAnswerItWasPressedOn(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "c")
	if foot := footText(a); !strings.Contains(foot, questionCommentWord) {
		t.Errorf("the foot should say the box is writing a comment:\n%s", foot)
	}
	a.input.setText("only if the reporting job keeps its own copy")
	tapNamed(t, a, tea.KeyEnter, 0)
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
	tap(t, a, "c")
	a.input.setText("keep the sqlite file")
	tapNamed(t, a, tea.KeyEnter, 0)
	tap(t, a, "1")
	tapNamed(t, a, tea.KeyEnter, 0)
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
	tap(t, a, "?")
	a.input.setText("does the reporting job read it directly")
	tapNamed(t, a, tea.KeyEnter, 0)
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
	tap(t, a, "?")
	a.input.setText("one")
	tapNamed(t, a, tea.KeyEnter, 0)
	tap(t, a, "?")
	if foot := footText(a); !strings.Contains(foot, questionAskedWord) {
		t.Errorf("a second ask should say why it was refused:\n%s", foot)
	}
}

// AND THE EXCHANGE CLOSES WITH THE QUESTION AND LANDS IN THE RECORD.
func TestTheExchangeRidesTheAnswer(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "?")
	a.input.setText("does it read it directly")
	tapNamed(t, a, tea.KeyEnter, 0)
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "it does"})
	pageText(a)
	tap(t, a, "1")
	tapNamed(t, a, tea.KeyEnter, 0)
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
	tap(t, a, "d")
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
	tap(t, a, "d")
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
	tap(t, a, "d")
	tap(t, a, "2")
	tap(t, a, "d")
	if len(agent.answers) != 0 {
		t.Fatalf("d after another key must ask again, not hand over: %#v", agent.answers)
	}
}

// `n` IS THE ANSWER THAT IS NOT ON THE LIST, and it comes back as a reframe
// rather than as a pick.
func TestReframeComesBackAsTheRealQuestion(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "n")
	if foot := footText(a); !strings.Contains(foot, questionReframeWord) {
		t.Errorf("the foot should say the box is writing the real question:\n%s", foot)
	}
	a.input.setText("whether the ledger belongs in this project at all")
	tapNamed(t, a, tea.KeyEnter, 0)
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
	agent := newAnsweringAgent()
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	a.raiseQuestionRoom(questionShown{question: demoQuestionReading(), shown: a.now()})
	tap(t, a, "2")
	if got := a.qroom.focus; got != 0 {
		t.Errorf("a key inside the settle window must be dropped, the pointer moved to %d", got)
	}
	a.qroom.shown = a.qroom.shown.Add(-questionSettle)
	tap(t, a, "2")
	if got := a.qroom.focus; got != 1 {
		t.Errorf("a key after the settle window should be taken, the pointer is on %d", got)
	}
}

// `esc` IS LATER AND NEVER AN ANSWER. The question folds away and nothing is
// decided — DESIGN.md is explicit that the turn stays paused on it.
func TestEscapeFoldsTheQuestionWithoutAnsweringIt(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "1")
	if _, took := tapNamed(t, a, tea.KeyEscape, 0); !took {
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
	tap(t, a, "c")
	tapNamed(t, a, tea.KeyEscape, 0)
	if !a.questionRoomOpen() {
		t.Fatal("the first esc should only leave the comment")
	}
	if a.qroom.commenting != "" {
		t.Error("the first esc should clear what the box is writing to")
	}
	tapNamed(t, a, tea.KeyEscape, 0)
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
	tap(t, a, "1")
	a.input.setText("but keep the sqlite file as the source of truth")
	if foot := footText(a); !strings.Contains(foot, questionWithWord+"but keep the sqlite file") {
		t.Errorf("the foot should show what would go with the pick:\n%s", foot)
	}
	tapNamed(t, a, tea.KeyEnter, 0)
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
	a.raiseQuestionRoom(questionShown{question: demoQuestionReading(), shown: a.now()})
	a.qroom.shown = a.qroom.shown.Add(-time.Second)
	tap(t, a, "1")
	tapNamed(t, a, tea.KeyEnter, 0)
	if foot := footText(a); !strings.Contains(foot, "not answer it") {
		t.Errorf("a window with no door should say so:\n%s", foot)
	}
}

// AND A REFUSAL FROM THE ENGINE PUTS THE QUESTION BACK, IN THE ENGINE'S OWN
// WORDS. A refusal a person cannot see is an answer that silently did nothing.
//
// IT IS NO LONGER DRAWN ON THIS PAGE'S FOOT, and that is the answer road's
// shape rather than a lost sentence: the page settles on the keystroke and
// closes, because a page that sat unchanged for a round trip is a page somebody
// answers twice (offloop.go). What a refusal has to do is put the question
// somewhere it can be answered again, and that is the block — with the words the
// engine refused in ([app.reopenQuestion]).
func TestAnEngineRefusalPutsTheQuestionBackInTheEnginesWords(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	agent.refuse = errQuestionTest
	tap(t, a, "1")
	tapNamed(t, a, tea.KeyEnter, 0)
	if said := plain(lastNote(t, a)); !strings.Contains(said, errQuestionTest.Error()) {
		t.Errorf("the engine's refusal never reached the person: %q", said)
	}
	if !a.questioning() {
		t.Error("the refused question was not put back where it can be answered again")
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
				tap(t, a, name)
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
	tap(t, a, "2")
	tapNamed(t, a, tea.KeyEscape, 0)
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
			tap(t, a, key)
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
		tap(t, a, key)
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

// dialAgent is a session that can both resolve a question and keep a setting
// about a whole shape of them — which is what a real engine is, over a wire.
type dialAgent struct {
	*answeringAgent
	set map[session.AskKind]session.Policy
}

func (g *dialAgent) SetAutonomy(kind session.AskKind, policy session.Policy) error {
	if g.set == nil {
		g.set = map[session.AskKind]session.Policy{}
	}
	g.set[kind] = policy
	return nil
}

// `D` SAYS WHICH SHAPE IT WOULD TAKE OVER BEFORE IT TAKES IT OVER, and the
// second press SETS THE SHAPE AND ANSWERS NOTHING: a person saying "you handle
// these from now on" has said something about the future, and applying it to the
// question they are still reading would be the surface answering for them.
func TestDecideThisKindShowsTheShapeAndAnswersNothing(t *testing.T) {
	inner := newAnsweringAgent()
	agent := &dialAgent{answeringAgent: inner}
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	a.raiseQuestionRoom(questionShown{question: demoQuestionReading(), shown: a.now()})
	a.qroom.shown = a.qroom.shown.Add(-time.Second)

	tap(t, a, "D")
	if foot := footText(a); !strings.Contains(foot, questionAskWord(session.AskChoice)) {
		t.Errorf("the first press should name the shape:\n%s", foot)
	}
	if len(agent.set) != 0 {
		t.Fatalf("the first press must set nothing: %#v", agent.set)
	}
	tap(t, a, "D")
	if got := agent.set[session.AskChoice].Kind; got != session.PolicyDecide {
		t.Errorf("the second press should set the shape, got %q", got)
	}
	// AND IT ANSWERS THE ONE IN FRONT OF YOU TOO. `D` is pressed while looking at
	// a question, and a key that wrote a setting and left that question sitting
	// there would read as having done nothing (question.go's [app.questionDial]
	// holds both halves of the promise).
	if len(inner.answers) != 1 {
		t.Fatalf("the second press should also answer this one, got %d", len(inner.answers))
	}
	if a.questionRoomOpen() {
		t.Error("an answered question takes its page with it")
	}
}

// AND A SESSION WITH NOWHERE TO KEEP THE SETTING DOES NOT OFFER THE KEY. A key
// named in the shared grammar that this question cannot honour is the emptiness
// law's own case, one rung down from a row.
func TestDecideThisKindIsNotOfferedWithNowhereToKeepIt(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	if foot := footText(a); strings.Contains(foot, "D ") {
		t.Errorf("a session with no dial door should not name D:\n%s", foot)
	}
}

// A REFUSAL STANDS UNTIL THE NEXT KEY AND NOT A MOMENT LONGER — a page that kept
// one would be a page whose keys are invisible.
func TestARefusalClearsOnTheNextKey(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	tap(t, a, "?")
	a.input.setText("one thing")
	tapNamed(t, a, tea.KeyEnter, 0)
	tap(t, a, "?")
	if foot := footText(a); !strings.Contains(foot, questionAskedWord) {
		t.Fatalf("expected the refusal:\n%s", foot)
	}
	tap(t, a, "2")
	if foot := footText(a); strings.Contains(foot, questionAskedWord) {
		t.Errorf("the refusal should be gone after the next key:\n%s", foot)
	}
}

// ONE ANSWER LEAVES ONE RECORD, AND IT SAYS WHO GAVE IT.
//
// The page used to keep an account of its own where the foot had been, while the
// block — which still held the question, because nothing had told it otherwise —
// wrote the receipt as well. Two adjacent lines about one decision, in two
// spellings, and the block's said `another window` about a key pressed on this
// one: the answer came back down the questions lane, found the question still
// open here, and was read as somebody else's.
func TestAnAnswerGivenOnThePageLeavesOneRecordAndItSaysYou(t *testing.T) {
	agent := newAnsweringAgent()
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	q := demoQuestionReading()
	// THE PAGE IS OPENED THE WAY A PERSON OPENS ONE — off the block, with `o` —
	// because the defect was entirely in what the two of them did about each
	// other, and a page raised on its own has no block behind it to disagree.
	a.raiseQuestion(questionShown{question: q})
	a.questionRows(a.width)
	head, ok := a.questionHead()
	if !ok {
		t.Fatal("the question never reached the block")
	}
	a.openQuestionRoom(head)
	a.qroom.shown = a.qroom.shown.Add(-time.Second)

	tap(t, a, "2")
	tap(t, a, "enter")

	if len(agent.answers) != 1 || agent.answers[0].FirstKey() != "2" {
		t.Fatalf("the door was handed %+v", agent.answers)
	}
	// THE PAGE IS GONE. Its whole job was over when the answer was spent, and a
	// page reporting what it just did is a page a person has to press esc to
	// leave for no reason.
	if a.questionRoomOpen() {
		t.Fatalf("the page stayed up after it was answered:\n%s", footText(a))
	}
	block := plain(strings.Join(a.questionRows(a.width), "\n"))
	if got := strings.Count(block, "→ sqlite beside the project"); got != 1 {
		t.Fatalf("one answer left %d records:\n%s", got, block)
	}
	if strings.Contains(block, "another window") {
		t.Fatalf("the receipt says somebody else pressed the key:\n%s", block)
	}
	if !strings.Contains(block, "you") {
		t.Fatalf("the receipt does not say who decided:\n%s", block)
	}
}

// AND THE LANE'S OWN NEWS ABOUT THAT ANSWER ADDS NOTHING.
//
// The engine emits EventQuestionAnswered for every answer, including this
// window's own. The block already knows, so the round trip must be silent —
// otherwise closing the question here only moved the second line rather than
// deleting it.
func TestTheLanesNewsAboutAnAnswerGivenHereAddsNoSecondLine(t *testing.T) {
	agent := newAnsweringAgent()
	a := newTestApp(agent)
	a.width, a.height = 92, 30
	q := demoQuestionReading()
	a.raiseQuestion(questionShown{question: q})
	a.questionRows(a.width)
	head, _ := a.questionHead()
	a.openQuestionRoom(head)
	a.qroom.shown = a.qroom.shown.Add(-time.Second)
	tap(t, a, "2")
	tap(t, a, "enter")

	answer := agent.answers[0]
	a.questionFold(session.Event{
		Kind: session.EventQuestionAnswered, Question: &q, Answer: &answer,
	})
	block := plain(strings.Join(a.questionRows(a.width), "\n"))
	if got := strings.Count(block, "→ sqlite beside the project"); got != 1 {
		t.Fatalf("the lane's news made it %d records:\n%s", got, block)
	}
}

// ── THE POINTER ON THE PAGE, AND WHAT IT LOOKS LIKE ─────────────────────────
//
// The owner opened a question out on 2026-09-10 and reported two things about
// the page it opened into: "no arrow or click", and "make sure there is some
// textual hierarchy in design in options like the same line and next line in
// options look same and a bit weird". These are what closes both.

// pagePlainRows is the page, a row at a time, with every escape taken off — for
// the assertions that are about WHICH ROW a thing landed on.
func pagePlainRows(a *app) []string {
	rows := a.questionRoomRows(a.width)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = plain(r.text)
	}
	return out
}

// questionRoomRowY is the screen row one of the page's drawn rows stands on, so
// a test can press what a person presses rather than calling the hit-test's own
// arithmetic back at it. The page's rows ARE its window: they hang from the top
// of the body region.
func questionRoomRowY(a *app, at int) int {
	return a.bodyTop() + at
}

// questionRoomOptionRow is where one ANSWER's own row landed, by the map the
// drawing wrote.
func questionRoomOptionRow(t *testing.T, a *app, want int) int {
	t.Helper()
	a.questionRoomRows(a.bodyWidth())
	for at, spot := range a.qroom.spots {
		if spot.at == want {
			return at
		}
	}
	t.Fatalf("answer %d has no row of its own", want)
	return -1
}

// `↑` AND `↓` WALK THE ANSWERS AND THE EVIDENCE FOLLOWS. In one column the
// answer the pointer reaches unfolds under its row and the one it left folds
// back to a row; in two panes the pane beside the list changes to it.
func TestTheArrowsWalkTheAnswersAndTheEvidenceFollows(t *testing.T) {
	for _, width := range []int{92, 140} {
		a, _ := standingInAQuestionAt(t, demoQuestionReading(), width, 40)
		if a.qroom.focus != 0 {
			t.Fatalf("the page should open on the pick, not on %d", a.qroom.focus)
		}
		tapNamed(t, a, tea.KeyDown, 0)
		if a.qroom.focus != 1 {
			t.Errorf("at %d columns down should walk to the second answer, got %d", width, a.qroom.focus)
		}
		drawn := pageText(a)
		if !strings.Contains(drawn, "One file in the repository") || strings.Contains(drawn, "Rows already carry") {
			t.Errorf("at %d columns the evidence should be the second answer's and only its:\n%s", width, drawn)
		}
		tapNamed(t, a, tea.KeyUp, 0)
		if a.qroom.focus != 0 {
			t.Errorf("at %d columns up should walk back, got %d", width, a.qroom.focus)
		}
	}
}

// `→` HANDS THE ARROWS TO THE EVIDENCE PANE AND `←` HANDS THEM BACK, and only
// where there IS a pane: on a page of one column the side arrows do nothing and
// the foot does not name them.
func TestTheSideArrowsMoveBetweenTheListAndTheEvidence(t *testing.T) {
	q := demoQuestionReading()
	q.Options[0].Blocks = []session.Block{{Kind: session.BlockDiagram, Title: "a tall one",
		Body: strings.Repeat("row of the diagram\n", 40)}}
	a, _ := standingInAQuestionAt(t, q, 140, 30)
	if foot := footText(a); !strings.Contains(foot, questionDetailKey+" detail") {
		t.Fatalf("two panes should offer the way into the evidence:\n%s", foot)
	}
	tapNamed(t, a, tea.KeyRight, 0)
	if !a.qroom.reading {
		t.Fatal("right should hand the arrows to the evidence pane")
	}
	foot := footText(a)
	if !strings.Contains(foot, questionWalkDownKey+" scroll") || !strings.Contains(foot, questionAnswersKey+" back to the answers") {
		t.Errorf("while the arrows are the pane's the foot should say so:\n%s", foot)
	}
	before := pageText(a)
	tapNamed(t, a, tea.KeyDown, 0)
	if a.qroom.focus != 0 || a.qroom.detail != 1 {
		t.Errorf("down should scroll the pane and leave the pointer: focus %d detail %d", a.qroom.focus, a.qroom.detail)
	}
	if pageText(a) == before {
		t.Error("scrolling the pane changed nothing on screen")
	}
	tapNamed(t, a, tea.KeyLeft, 0)
	tapNamed(t, a, tea.KeyDown, 0)
	if a.qroom.reading || a.qroom.focus != 1 || a.qroom.detail != 0 {
		t.Errorf("left should hand the arrows back, and the next pane opens at its top: reading %v focus %d detail %d",
			a.qroom.reading, a.qroom.focus, a.qroom.detail)
	}

	narrow, _ := standingInAQuestionAt(t, q, 92, 30)
	if foot := footText(narrow); strings.Contains(foot, questionDetailKey+" detail") {
		t.Errorf("one column has no pane to move into, and must not offer one:\n%s", foot)
	}
	tapNamed(t, narrow, tea.KeyRight, 0)
	if narrow.qroom.reading {
		t.Error("a side arrow the foot does not name must do nothing")
	}
}

// AND `enter` TAKES THE ANSWER THE POINTER IS ON, which is the block's own law
// (#789) arriving on the page. It still takes the asker's pick off a fresh
// room, because the room opens standing on it.
func TestEnterTakesTheAnswerThePointerIsOn(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	tapNamed(t, a, tea.KeyDown, 0)
	tapNamed(t, a, tea.KeyEnter, 0)
	if len(agent.answers) != 1 {
		t.Fatalf("expected one answer, got %d", len(agent.answers))
	}
	if got := agent.answers[0].Key; got != "2" {
		t.Errorf("enter should take the answer the pointer is on, got %q", got)
	}

	fresh, freshAgent := standingInAQuestion(t, demoQuestionReading())
	tapNamed(t, fresh, tea.KeyEnter, 0)
	if len(freshAgent.answers) != 1 || freshAgent.answers[0].Key != "1" {
		t.Errorf("enter on a fresh page should still take the pick: %+v", freshAgent.answers)
	}
}

// A CLICK MOVES ONTO AN ANSWER AND A SECOND CLICK ON IT TAKES IT. One press to
// read and one to decide: a page where the first click answered would answer
// with evidence somebody had not read, and a page where no click ever answered
// would be one a person has to leave the mouse to finish.
func TestAClickOpensAnAnswerAndASecondClickTakesIt(t *testing.T) {
	a, agent := standingInAQuestion(t, demoQuestionReading())
	at := questionRoomOptionRow(t, a, 2)
	spend(t, a, a.press(4, questionRoomRowY(a, at)))
	if a.qroom == nil {
		t.Fatalf("one click must not answer the question")
	}
	if a.qroom.focus != 2 || !strings.Contains(pageText(a), "Append-only, one file a day") {
		t.Fatalf("a click should move the pointer onto that answer and show its evidence: focus %d\n%s",
			a.qroom.focus, pageText(a))
	}
	if len(agent.answers) != 0 {
		t.Fatalf("one click must not answer the question: %+v", agent.answers)
	}
	at = questionRoomOptionRow(t, a, 2)
	spend(t, a, a.press(4, questionRoomRowY(a, at)))
	if len(agent.answers) != 1 {
		t.Fatalf("a second click on the same answer should take it, got %d", len(agent.answers))
	}
	if got := agent.answers[0].Key; got != "3" {
		t.Errorf("the click should have taken the answer it was on, got %q", got)
	}
}

// AND WHAT LIGHTS IS EXACTLY WHAT A PRESS ACTS ON (hover.go's law). Only the
// answer's own row answers to a click, so only that row brightens under the
// pointer — a body, a diagram or somebody's own note does not.
func TestThePointerLightsTheAnswerAPressWouldTake(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	at := questionRoomOptionRow(t, a, 1)
	if hot := a.hoverTarget(4, questionRoomRowY(a, at)); hot.kind != hoverQuestionOption || hot.index != 1 {
		t.Errorf("the pointer on an answer's row should light that answer: %+v", hot)
	}
	body := questionRoomRowY(a, questionRoomOptionRow(t, a, 0)+1)
	if hot := a.hoverTarget(8, body); hot.kind == hoverQuestionOption {
		t.Errorf("an answer's body is prose and must not light as a target: %+v", hot)
	}
	// AND IN TWO PANES THE EVIDENCE BESIDE AN ANSWER'S ROW IS NOT THAT ANSWER:
	// the row is a target on the left of the seam and prose on the right of it.
	wide, _ := standingInAQuestionAt(t, demoQuestionReading(), 140, 40)
	row := questionRoomRowY(wide, questionRoomOptionRow(t, wide, 1))
	if hot := wide.hoverTarget(4, row); hot.kind != hoverQuestionOption || hot.index != 1 {
		t.Errorf("the list's row should light its answer: %+v", hot)
	}
	if hot := wide.hoverTarget(wide.qroom.seam+4, row); hot.kind == hoverQuestionOption {
		t.Errorf("the evidence beside a row must not light as that answer: %+v", hot)
	}
}

// ── THE HIERARCHY ───────────────────────────────────────────────────────────

// EVERY LINE UNDER AN OPEN ANSWER SAYS WHICH LINE IT IS. The consequence, the
// asker's reason and what would change its mind are all dim and all one
// sentence long, so without their own words they are three grey rows a person
// has to read in full to tell apart.
func TestAnOpenAnswerSaysWhichOfItsLinesIsWhich(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	drawn := pageText(a)
	for _, want := range []string{
		questionThenWord + "the ledger and the rest of the project share one connection",
		questionWhyWord + "because it is the only store the reporting job already reads",
		questionWouldSwitchWord + "the ledger ever has to run on a machine with no server on it",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the page does not say %q:\n%s", want, drawn)
		}
	}
}

// AND THE THREE TIERS ARE THREE INKS: the answer's row is the panel's own, the
// body is the prose ink, and every aside under it is dim.
func TestAnAnswersLabelBodyAndAsidesAreThreeDifferentInks(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	rows := a.questionRoomRows(a.width)
	var head, body, aside string
	for _, r := range rows {
		switch {
		case strings.Contains(plain(r.text), "1  postgres"):
			head = r.text
		case strings.Contains(plain(r.text), "Rows already carry a foreign key"):
			body = r.text
		case strings.Contains(plain(r.text), questionThenWord):
			aside = r.text
		}
	}
	if head == "" || body == "" || aside == "" {
		t.Fatalf("the open answer should draw a heading, a body and an aside:\n%s", pageText(a))
	}
	// THE KEY IS THE PAYLOAD HUE AND THE WORD IS INK, which is the panel's own
	// grammar for an answer (owner ruling 2026-09-11, colour pick C: the amber
	// stays on the marks) — the page's rows ARE the panel's.
	inkOpen := strings.SplitN(a.pal.ink("x"), "x", 2)[0]
	if !strings.Contains(head, a.pal.data("1")) || !strings.Contains(head, inkOpen+"postgres") {
		t.Errorf("the label is not the key in the payload hue and the word in ink: %q", head)
	}
	if !strings.Contains(body, a.pal.ink("Rows already carry a foreign key into it and the migration is one file.")) {
		t.Errorf("the body should be the prose ink: %q", body)
	}
	if !strings.Contains(aside, a.pal.dim(questionThenWord+"the ledger and the rest of the project share one connection")) {
		t.Errorf("the consequence should be dim: %q", aside)
	}
}

// AND ONE BLANK ROW CLOSES AN OPEN ANSWER, so the next answer's heading is
// separated from the paragraph above it by something other than an indent.
func TestAnOpenAnswerIsClosedByABlankRowBeforeTheNext(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	rows := a.questionRoomRows(a.width)
	next := questionRoomOptionRow(t, a, 1)
	if next == 0 || strings.TrimSpace(plain(rows[next-1].text)) != "" {
		t.Errorf("the row above the second answer should be blank, got %q", plain(rows[next-1].text))
	}
}

// WHAT WOULD CHANGE THE ASKER'S MIND NEVER READS `if If`. A model answers that
// question with a sentence — "If you are targeting…" — and the row is a clause.
func TestWhatWouldChangeItsMindReadsAsOneClause(t *testing.T) {
	q := demoQuestionReading()
	q.Pick.WouldChange = "If you are targeting a calm audience"
	a, _ := standingInAQuestion(t, q)
	drawn := pageText(a)
	if strings.Contains(drawn, "if If") || strings.Contains(drawn, "if if") {
		t.Errorf("the row says the word twice:\n%s", drawn)
	}
	if !strings.Contains(drawn, questionWouldSwitchWord+"you are targeting a calm audience") {
		t.Errorf("the sentence should join onto the clause:\n%s", drawn)
	}
	if got := questionAfterIf("SQLite ever grows a second writer"); got != "SQLite ever grows a second writer" {
		t.Errorf("a name the asker wrote must be left alone: %q", got)
	}
}

// ── THE EVIDENCE, LAID OUT WITH WHAT IT BELONGS TO ──────────────────────────

// AN ANSWER'S OWN BLOCKS ARE DRAWN WITH ITS EVIDENCE, at its body's indent, with
// a blank row before each — under its row in one column.
func TestAnAnswersBlocksAreDrawnUnderTheAnswer(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	rows := pagePlainRows(a)
	head, title := -1, -1
	for at, line := range rows {
		switch {
		case strings.Contains(line, "1  postgres"):
			head = at
		case strings.Contains(line, "what it would look like"):
			title = at
		case strings.Contains(line, "2  sqlite") && title < 0:
			t.Fatalf("the block should be drawn before the next answer:\n%s", strings.Join(rows, "\n"))
		}
	}
	if head < 0 || title < 0 || title < head {
		t.Fatalf("the block should sit under its own answer:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[title], questionBodyIndent) {
		t.Errorf("the block's title should be indented like the body: %q", rows[title])
	}
	if strings.TrimSpace(rows[title-1]) != "" {
		t.Errorf("a blank row should stand above a block, got %q", rows[title-1])
	}
}

// AND THE QUESTION'S OWN EVIDENCE IS ONE TITLED SECTION UNDER THE ANSWERS, in
// the list's own column (page pick A): it is the same whichever answer is being
// weighed, so it stands with the thing that stays still and never in the pane
// that changes with the pointer.
func TestTheQuestionsOwnEvidenceIsOneTitledSectionUnderTheAnswers(t *testing.T) {
	q := demoQuestionReading()
	q.Attach = []session.Block{
		{Kind: session.BlockDiagram, Title: "where the rows are today", Body: "app --> sqlite"},
		{Kind: session.BlockDiagram, Title: "where they would be", Body: "app --> pg"},
	}
	a, _ := standingInAQuestion(t, q)
	rows := pagePlainRows(a)
	section, first, answers := -1, -1, -1
	for at, line := range rows {
		switch {
		case strings.Contains(line, questionAttachWord):
			section = at
		case strings.Contains(line, "where the rows are today"):
			first = at
		case strings.Contains(line, "3  a file per day"):
			answers = at
		}
	}
	if section < 0 || first < 0 || answers < 0 {
		t.Fatalf("the page should title the question's own evidence:\n%s", strings.Join(rows, "\n"))
	}
	if !(answers < section && section < first) {
		t.Errorf("the evidence should be one section under the answers: %d %d %d", answers, section, first)
	}
	wide, _ := standingInAQuestionAt(t, q, 140, 40)
	for _, line := range pagePlainRows(wide) {
		if at := strings.Index(line, "where the rows are today"); at >= 0 && at > wide.qroom.seam {
			t.Errorf("the question's own evidence belongs left of the seam, under the list: %q", line)
		}
	}
	if !strings.HasPrefix(rows[section], questionIndent) || strings.HasPrefix(rows[section], questionIndent+" ") {
		t.Errorf("the section's title should sit at the page's own indent: %q", rows[section])
	}
	if !strings.HasPrefix(rows[first], questionAttachIndent) {
		t.Errorf("a block should be indented under its title, never at the gutter: %q", rows[first])
	}
}

// ── THE FOOT ────────────────────────────────────────────────────────────────

// THE KEYS ROW NAMES THE PAIR THAT ACTUALLY WALKS THIS PAGE. Its answers stand
// in a column of sections, so the pair is `↑↓`; the row said `←→` while neither
// of those keys moved anything on it.
func TestTheFootNamesTheArrowsThatWalkTheSections(t *testing.T) {
	a, _ := standingInAQuestion(t, demoQuestionReading())
	foot := footText(a)
	if !strings.Contains(foot, questionWalkDownKey+" "+questionKeyWord(questionWalkDownKey)) {
		t.Errorf("the foot should offer the pair that walks the sections:\n%s", foot)
	}
	if strings.Contains(foot, questionWalkKey+" "+questionKeyWord(questionWalkKey)) {
		t.Errorf("the foot must not name a pair that walks nothing here:\n%s", foot)
	}
}
