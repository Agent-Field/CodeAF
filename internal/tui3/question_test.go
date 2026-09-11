package tui3

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// questionLab is one app with the block on it and nothing else moving: a fixed
// clock, so the settle guard and the policy line are decidable rather than
// raced, and a scripted agent that records what the one door was handed.
type questionLab struct {
	t      *testing.T
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
	// held is every clock this window asked the engine to stop
	// (questionchange_test.go).
	held []questionHeldCall
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
	lab := &questionLab{t: t, agent: script, at: at}
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
	cmd, taken := l.a.questionKey(questionPressOf(key))
	// AND WHAT THE KEY HANDED BACK IS RUN. The engine's door is asked from the
	// command a key returns and never from the update loop (offloop.go), so a
	// lab that dropped the command would be a lab in which no answer ever
	// reached the engine.
	l.spend(cmd)
	return taken
}

// spend runs one command the way the loop would, including whatever it hands
// back — the fold of a door's answer among it (offloop.go).
func (l *questionLab) spend(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	l.t.Helper()
	drive(l.t, l.a, runCmd(cmd)...)
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

// TestTheLineDrawsItsHeadItsAnswersAndItsReasonAndNothingElse is the whole
// shape of a permission, asserted as rows rather than as a substring: a block
// that grew a row nobody decided on is a block that moved the box.
//
// IT IS THE PANEL AND NOT A LINE. A person allowing a call has to READ the call
// (owner ruling 2026-09-11, consent pick B), so the chooser sends every
// permission here: the head and who is asking in the frame's top edge, the call
// and the policy's own words on the first row, a blank, a row per answer, a
// blank, the keys that answer in the bottom edge, and the quieter keys on one
// dim row under it.
//
// AND NO LIFETIMES ROW, though this fixture offers two. The consent gate reads
// an answer's key and never [session.Answer.Scope], so the row is off a
// permission until the engine honours it ([questionScopes], and the count here
// is what notices if it comes back before that).
func TestTheLineDrawsItsHeadItsAnswersAndItsReasonAndNothingElse(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	rows := lab.rows()
	if len(rows) != 9 {
		t.Fatalf("the panel took %d rows, not nine:\n%s", len(rows), lab.screen())
	}
	screen := plain(strings.Join(rows, "\n"))
	for _, want := range []string{"allow this?", "1  allow once", "2  always", "3  deny", "esc later"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the panel does not say %q:\n%s", want, screen)
		}
	}
	if !strings.Contains(plain(rows[1]), `bash pattern "rm -rf *"`) {
		t.Fatalf("the call's own words are not the first row: %q", rows[1])
	}
	if strings.Contains(screen, "cancel") {
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
	// A QUESTION PUT OFF FOLDS IN PLACE TO ONE TITLED RULE (owner ruling
	// 2026-09-11, fold pick A): it never jumps to the status line, and the rule
	// says what is waiting and which key opens it again.
	folded := lab.rows()
	if len(folded) != 1 {
		t.Fatalf("the folded question is not one rule:\n%s", strings.Join(folded, "\n"))
	}
	if row := plain(folded[0]); !strings.Contains(row, "allow this?") ||
		!strings.Contains(row, questionOpenFoldWord) {
		t.Fatalf("the folded rule does not say what is waiting or how to open it: %q", row)
	}
	if lab.a.questionCount() != 1 {
		t.Fatalf("the folded question stopped being counted: %d", lab.a.questionCount())
	}
	if seg := lab.a.questionSegment(); !strings.Contains(seg, "allow this?") {
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
	// THE SETTLED MARK OPENS IT, and the row says what was decided, by whom and
	// when — and what is still possible about it, which is a key only because
	// #954 built the door behind it. The key was taken OFF this row for a while,
	// when it named a door that did not exist; it is back with the door and not
	// before, which is the manual law said about a keyboard.
	for _, said := range []string{tokens.Plain.Glyph(tokens.GSettled), "allow once", "you", "14:02"} {
		if !strings.Contains(rows[0], said) {
			t.Fatalf("the receipt does not say %q: %q", said, rows[0])
		}
	}
	if strings.Contains(rows[0], "decided") {
		t.Fatalf("the receipt still opens with the word `decided`: %q", rows[0])
	}
	// AND EVERY KEY IT NAMES HAS A DOOR. The two readings the receipt offers keys
	// from are the two the keys themselves ask, so a row cannot name one the
	// press would refuse.
	if strings.Contains(rows[0], questionCommentKey+" change") && !questionCanChange(lab.a.questionRecords[0]) {
		t.Fatalf("the receipt offers a key with no door behind it: %q", rows[0])
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
	if got := lab.plain(); !strings.Contains(got, "r make it a rule for everywhere") {
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
	// THE FRAME'S OWN TWO ROWS ARE THE FIRST AND THE LAST, the head is written
	// into the top edge with who is asking beside it, and the keys into the
	// bottom edge — so the drawing is the head, the asker's sentence, a blank,
	// a row per answer, the pick's case under the pointer, a blank, the keys,
	// and the quieter keys under the frame.
	if len(rows) != 9 {
		t.Fatalf("the card took %d rows:\n%s", len(rows), lab.screen())
	}
	if !strings.Contains(rows[0], "wants to start a task: rewrite the packer") || !strings.Contains(rows[0], product) {
		t.Fatalf("the head and who is asking are not the top edge: %q", rows[0])
	}
	if !strings.Contains(rows[1], "it will run on its own branch") {
		t.Fatalf("the asker's sentence is not under the head: %q", rows[1])
	}
	// THE POINTER OPENS ON THE ASKER'S PICK AND THE PICK IS SAID IN A WORD.
	// They are two marks with two meanings — `▸` is where enter lands, `◆
	// recommended` is what the asker would take — and on this question they
	// are on one row because the person has not moved yet.
	pointer, pick := tokens.Plain.Glyph(tokens.GPointer), tokens.Plain.Glyph(tokens.GRecommended)
	if !strings.Contains(rows[3], pointer) || !strings.Contains(rows[3], "start it") ||
		!strings.Contains(rows[3], pick+" "+questionRecommendedWord) {
		t.Fatalf("the asker's pick is not marked: %q", rows[3])
	}
	if !strings.Contains(rows[4], "it starts on its own unless you say otherwise") {
		t.Fatalf("the pick's case is not under the pointed row: %q", rows[4])
	}
	if strings.Contains(rows[5], pointer) || strings.Contains(rows[5], pick) {
		t.Fatalf("a second answer carries a mark of the first: %q", rows[5])
	}
	if !strings.Contains(rows[5], "nothing runs") {
		t.Fatalf("the consequence is missing: %q", rows[5])
	}
	if !strings.Contains(rows[7], "enter take it") || !strings.Contains(rows[7], "esc later") {
		t.Fatalf("the bottom edge does not carry the keys that answer: %q", rows[7])
	}
	if !strings.Contains(rows[8], "c change") || strings.Contains(rows[7], "c change") {
		t.Fatalf("the quieter keys are not the tier under the frame: %q / %q", rows[7], rows[8])
	}
}

// TestEnterOnAPermissionDeniesUntilTheEngineGradesTheCall is where the pointer
// stands when the asker named no pick, and it is the half of the pointer that
// keeps it safe.
//
// `enter` takes the answer the pointer is on, so a permission whose pointer
// opened on the first answer would make `enter` mean `allow once` — on a gate
// over `rm -rf *` as readily as over a `git status` the rules had merely not
// seen before. The lane already says which answer costs nothing
// ([session.AnswerOption.Safe]) and that is the one EVERY permission opens on.
//
// IT IS ONE RULE AND IT IS NOT KEYED ON THE STAKES, which is what this test is
// shaped to prove. The 2026-09-11 design ruling was that an ordinary call should
// open on `allow once` and only a grave one on deny; the owner's follow-up the
// same day was DENY-FIRST UNTIL GRADED, because nothing upstream grades a call —
// the gate stamps `costly` on every consent alike and internal/approval never
// produces irreversible at all ([TestTheGateGradesNoCallAsIrreversibleAndMarks\
// DenyTheSafeAnswer] in internal/session holds that down). A by-stakes rule
// would therefore have read "ordinary" for a recursive delete in production and
// its grave branch would have been reachable only from a fixture — which is
// exactly how the version of this test that FORCED `StakesIrreversible` by hand
// passed while `enter` allowed everything a person actually met.
//
// So it walks every stakes a permission can carry rather than choosing one, and
// states the rule's ONE exception with it: a permission a lane marked plainly
// REVERSIBLE is not hands-only and opens on its first answer. Nothing produces
// one today — the gate stamps `costly` — and the day something does, this is
// where the claim is written down rather than discovered.
func TestEnterOnAPermissionDeniesUntilTheEngineGradesTheCall(t *testing.T) {
	for _, one := range []struct {
		stakes session.Stakes
		// refuses says the pointer opens on the answer that loses nothing, so
		// `enter` on an untouched frame denies the call.
		refuses bool
	}{
		{session.StakesCostly, true},       // what the gate actually stamps
		{session.StakesIrreversible, true}, // what it would stamp if it graded
		{session.StakesReversible, false},  // the documented exception
	} {
		t.Run(string(one.stakes), func(t *testing.T) {
			lab := newQuestionLab(t)
			ask := consentAsk()
			ask.Form = session.FormCard
			ask.Stakes = one.stakes
			lab.raise(ask)
			lab.tick(questionSettle)
			lab.rows()

			// THE ANSWER IS READ OFF THE QUESTION AND NOT SPELLED HERE: a test
			// naming `3` would still pass the day the engine renumbered its
			// answers and moved the refusal somewhere else.
			want := 0
			if one.refuses {
				want = questionSafeAt(ask)
				if ask.Options[want].Label != "deny" {
					t.Fatalf("the answer that loses nothing is not the refusal: %q",
						ask.Options[want].Label)
				}
			}
			head, _ := lab.a.questionHead()
			if head.pick != want {
				t.Fatalf("the pointer opened on answer %d, not %d", head.pick, want)
			}
			if !lab.press("enter") {
				t.Fatal("enter should take the answer the pointer is on")
			}
			key := ask.Options[want].Key
			if len(lab.answer) != 1 || lab.answer[0].FirstKey() != key {
				t.Fatalf("enter answered %+v, not %q", lab.answer, key)
			}
		})
	}
}

// TestEnterTakesTheAskersPickWhereThereIsOne is the other half of the same key,
// and the reason the rule above is written BELOW the pick and not above it: an
// asker that recommended an answer said so on the row a person is reading, and
// `enter` taking the recommendation IS the pointer's law. A task proposal is
// unaffected by anything the permission rule does.
func TestEnterTakesTheAskersPickWhereThereIsOne(t *testing.T) {
	lab := newQuestionLab(t)
	ask := consentAsk()
	ask.Form = session.FormCard
	ask.Pick = &session.Pick{Key: "1", Reason: "the narrow answer"}
	ask.Stakes = session.StakesReversible
	lab.raise(ask)
	lab.tick(questionSettle)
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
		local: func(answer session.Answer) tea.Cmd { took = append(took, answer.FirstKey()); return nil },
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
		local:    func(session.Answer) tea.Cmd { return nil },
	})
	rows := questionPlainRows(lab.rows())
	if len(rows) != 1 {
		t.Fatalf("the ratify line took %d rows:\n%s", len(rows), lab.screen())
	}
	for _, said := range []string{
		tokens.Plain.Glyph(tokens.GSettled), "renamed 12 files under src/", "u undo", "c change",
	} {
		if !strings.Contains(rows[0], said) {
			t.Fatalf("the ratify line does not say %q: %q", said, rows[0])
		}
	}
	if strings.Contains(rows[0], tokens.Plain.Glyph(tokens.GNeedsHuman)) {
		t.Fatal("the ratify line wears the attention mark; nothing is waiting on it")
	}
}

// TestARatifiedActOffersTheWayBackWithoutBeingTold is the ladder's third rung
// arriving the way the engine actually sends it: nothing but the object, and the
// row still finds the way back on it.
//
// Nothing set [questionShown.undoable] outside a test until this landed, so
// `[u] undo` was a key in the table, a paragraph in the manual, and a cell no
// ratify line ever drew.
func TestARatifiedActOffersTheWayBackWithoutBeingTold(t *testing.T) {
	lab := newQuestionLab(t)
	lab.fromLane(session.Question{
		ID: 9, Kind: session.QuestionAsk, Ask: session.AskRatify,
		Asker: session.Asker{Kind: session.AskerModel},
		Head:  "renamed 12 files under src/",
		Options: []session.AnswerOption{
			{Key: "1", Label: "put them back", Safe: true},
			{Key: "2", Label: "already done"},
		},
		Stakes: session.StakesReversible,
	})
	if got := lab.plain(); !strings.Contains(got, "u undo") {
		t.Fatalf("the ratify line offers no way back: %q", got)
	}
	// AND THE KEY REACHES THE UNWIND THE ASKER DESCRIBED, which is the whole of
	// what makes offering it honest.
	lab.tick(questionSettle)
	head, ok := lab.a.questionHead()
	if !ok {
		t.Fatal("the ratify line left the block")
	}
	cmd, took := lab.a.questionVerbKey(head, questionUndoKey)
	if !took {
		t.Fatal("`u` was drawn and did nothing")
	}
	lab.spend(cmd)
	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "1" {
		t.Fatalf("`u` sent %+v, want the answer that puts the work back", lab.answer)
	}
}

// TestACostlyRatifyDoesNotOfferTheWayBack is the other half of the same reading:
// the rung is REVERSIBLE work, and a row that offered to unwind anything else
// would be promising something the world will not do.
func TestACostlyRatifyDoesNotOfferTheWayBack(t *testing.T) {
	lab := newQuestionLab(t)
	lab.fromLane(session.Question{
		ID: 10, Kind: session.QuestionAsk, Ask: session.AskRatify,
		Head: "bought the machine hour", Stakes: session.StakesCostly,
		Options: []session.AnswerOption{{Key: "1", Label: "refund it", Safe: true}},
	})
	if got := lab.plain(); strings.Contains(got, "[u] undo") {
		t.Fatalf("a ratify line for costly work offered a way back: %q", got)
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
		local: func(session.Answer) tea.Cmd { return nil },
	})
	if got := lab.plain(); strings.Contains(got, "[u] undo") {
		t.Fatalf("a ratify line with nothing to undo offered the key: %q", got)
	}
}

// TestAnAssumptionsCardWearsItsOwnMarkAndItsOwnClock is the ladder's SECOND rung
// drawn as what it is: nothing is waiting on anybody, so it does not wear the
// mark that says something is, and the countdown says what actually happens when
// it runs out.
//
// Both halves were the task proposal's before this landed — the amber `?` and
// `starts on its own in 9m 57s` — about a card that asks nothing and starts
// nothing.
func TestAnAssumptionsCardWearsItsOwnMarkAndItsOwnClock(t *testing.T) {
	lab := newQuestionLab(t)
	lab.fromLane(session.Question{
		ID: 12, Kind: session.QuestionAsk, Ask: session.AskAssumption,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "going ahead on these unless you strike one",
		Reason: "nobody said which store to use",
		Options: []session.AnswerOption{
			{Key: "1", Label: "the sqlite file is the source of truth"},
			{Key: "2", Label: "the old rows can be dropped"},
		},
		Stakes:   session.StakesReversible,
		Policy:   session.Policy{Kind: session.PolicyRecommendThenAuto, After: 10 * time.Minute},
		Deadline: lab.at.Add(9*time.Minute + 57*time.Second),
	})
	got := lab.plain()
	// The mark is read off the HEAD ROW alone: `?` is also the ask-back key's
	// own cell further down the block, and that one is a key rather than a mark.
	head := questionPlainRows(lab.rows())[0]
	if !strings.HasPrefix(head, tokens.Plain.Glyph(tokens.GAssumed)+" ") {
		t.Fatalf("the assumptions card does not open with %q: %q", tokens.Plain.Glyph(tokens.GAssumed), head)
	}
	if strings.HasPrefix(head, tokens.Plain.Glyph(tokens.GNeedsHuman)) {
		t.Fatalf("the assumptions card wears the attention mark; nothing is waiting on it: %q", head)
	}
	if !strings.Contains(got, questionAssumptionClockWord+" in ") {
		t.Fatalf("the clock does not say what an assumption's clock does:\n%s", got)
	}
	if strings.Contains(got, questionProposalClockWord) {
		t.Fatalf("the assumptions card borrowed the task proposal's sentence:\n%s", got)
	}
}

// TestAQuestionThatIsWaitingKeepsTheAttentionMark is the other side of the same
// reading: the shapes that DO want a key are unmoved.
func TestAQuestionThatIsWaitingKeepsTheAttentionMark(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	// The mark is written into the frame's top edge, where the head is.
	head := questionPlainRows(lab.rows())[0]
	if !strings.Contains(head, tokens.Plain.Glyph(tokens.GNeedsHuman)+" allow this?") {
		t.Fatalf("a permission lost the attention mark: %q", head)
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
	if strings.Count(got, "esc later") != 1 {
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
		// AND THE STANDING CARD, whose chip row, cursor, digits and hint line are
		// deleted: standing.go draws the CARD — the words, the bands, the meter —
		// and the asking is here.
		session.QuestionStanding,
		// AND THE HARNESS LANE'S TWO QUESTIONS — the offer to run a saved program
		// and the judgement on a finished design. Between them they had three
		// grammars and three drawings (harness.go's row, harnesscard.go's card
		// columns, roomapproval.go's pinned chords); all three are deleted.
		session.QuestionHarness,
		// AND THE ACCOUNT OFFER, whose own block, answers row, click targets and
		// key router are deleted: connect.go keeps the lane's facts and the
		// browser flow, which is a report and not a question.
		session.QuestionConnect,
	} {
		if !a.questionDrawnHere(session.Question{Kind: kind}) {
			t.Fatalf("%s has no other block and is not drawn here either", kind)
		}
	}
	for _, kind := range []session.QuestionKind{
		session.QuestionSubharness,
	} {
		if a.questionDrawnHere(session.Question{Kind: kind}) {
			t.Fatalf("%s is drawn here AND by its own block; one decision, two rows", kind)
		}
	}
}

// THE RECEIPT GIVES UP A CLAUSE AND IS NEVER CUT FROM THE RIGHT.
//
// The tail is where everything a person cannot work out for themselves lives —
// who answered, when, and whether it can still be changed — and it was the half
// a long `with:` clause took off the end at a hundred columns.
func TestTheReceiptGivesUpAClauseRatherThanLosingItsTail(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 100
	lab.a.questionRecords = append(lab.a.questionRecords, questionRecord{
		record: session.DecisionRecord{
			Head: "which store should the ledger sit on?", Picked: []string{"2"},
			Labels: []string{"sqlite beside the project"},
			Change: "2, but keep the sqlite file as the source of truth and write the migration first",
			By:     session.DecidedByPerson, At: lab.at,
		},
		// The question is on the receipt because `c change` puts it back, and
		// the row only offers the key where it would work (questionchange.go).
		question: session.Question{
			ID: 1, Kind: session.QuestionAsk, Head: "which store should the ledger sit on?",
			Options: []session.AnswerOption{{Key: "1", Label: "one file"}, {Key: "2", Label: "sqlite beside the project"}},
		},
		at: lab.at, reversible: true,
	})
	row := plain(lab.a.questionRecordRow(lab.a.questionRecords[0], lab.a.width))
	if ansi.StringWidth(row) > lab.a.width {
		t.Fatalf("the receipt is %d cells wide at %d: %q", ansi.StringWidth(row), lab.a.width, row)
	}
	for _, kept := range []string{
		"which store should the ledger sit on? → sqlite beside the project",
		"you", "14:02",
	} {
		if !strings.Contains(row, kept) {
			t.Fatalf("the receipt lost %q: %q", kept, row)
		}
	}
	// The change said beside the pick is the one clause that goes, because it is
	// the one the transcript and decisions.jsonl both still carry in full.
	if strings.Contains(row, "with:") {
		t.Fatalf("the receipt kept the clause it should have given up first: %q", row)
	}
	// AND WHO DECIDED SURVIVES A ROW TOO NARROW EVEN FOR THE QUESTION, because
	// it is the one thing on the line nobody can work out for themselves.
	narrow := plain(lab.a.questionRecordRow(lab.a.questionRecords[0], 70))
	if !strings.Contains(narrow, "you") {
		t.Fatalf("a narrow receipt stopped saying who decided: %q", narrow)
	}
	// AND AT A WIDTH THAT HOLDS EVERYTHING, NOTHING IS GIVEN UP.
	lab.a.width = 200
	wide := plain(lab.a.questionRecordRow(lab.a.questionRecords[0], lab.a.width))
	if !strings.Contains(wide, "with: 2, but keep the sqlite file") {
		t.Fatalf("a wide receipt dropped a clause it had room for: %q", wide)
	}
}

// AND `cannot change` IS NEVER GIVEN UP, because it is a limit rather than a
// detail: a row that dropped it would read as a decision somebody could walk
// back.
func TestAnIrreversibleReceiptKeepsItsLimitAtAnyWidth(t *testing.T) {
	lab := newQuestionLab(t)
	record := questionRecord{
		record: session.DecisionRecord{
			Head: "send the quarterly digest to every address on the list?", Picked: []string{"1"},
			Labels: []string{"send it"}, Change: "send it, but hold the two bounced addresses back until they are checked",
			By: session.DecidedByPerson, At: lab.at, Stakes: session.StakesIrreversible,
		},
		at: lab.at,
	}
	row := plain(lab.a.questionRecordRow(record, 70))
	if !strings.Contains(row, "cannot change") {
		t.Fatalf("a narrow receipt gave up the one clause that is a limit: %q", row)
	}
}

// ── the size of the evidence decides the form ───────────────────────────────

// THE ASKER'S `line` IS A WISH AND THE EVIDENCE DECIDES. A model wrote
// `form: line` over a checklist of eight, and what came out was one row cut at
// the edge — `[landscape] Landscapes & seascapes · … [surrealis…` — with no
// key under any answer past the fourth. A checklist is rows by nature.
func TestAChecklistAskedForAsALineIsACardWithARowPerAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 100
	labels := []string{"Landscapes & seascapes", "Portraits", "Still life & botanical", "Abstract",
		"Impressionism", "Surrealism", "History / mythological painting", "Street scenes / cityscapes"}
	options := make([]session.AnswerOption, 0, len(labels))
	for i, label := range labels {
		options = append(options, session.AnswerOption{Key: itoa(i + 1), Label: label})
	}
	options[0].Body = "Turner, Hokusai and the Hudson River School"
	lab.raise(session.Question{
		ID: 41, Kind: session.QuestionAsk, Ask: session.AskChoice, Form: session.FormLine,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which painting genres do you like?",
		Reason: "you asked to be asked with choices", Options: options,
		Input: session.InputShape{Kind: session.InputChecklist}, Stakes: session.StakesReversible,
		Pick: &session.Pick{Key: "5", Reason: "every gallery has a wall of it"},
	})
	screen := lab.plain()
	// THE NOTE UNDER AN ANSWER IS DRAWN, the asker's pick is a word on its row
	// rather than the pointer, the pointer is the person's and starts on the
	// first row, and the key row offers the walk and the tick — never `take
	// the pick`, which a checklist's enter does not do.
	if !strings.Contains(screen, "Turner, Hokusai") {
		t.Fatalf("the note under the first answer is not drawn:\n%s", screen)
	}
	// THE PICK IS A WORD AT THE RIGHT EDGE OF ITS ROW in every view, and a
	// checklist's rows carry their tick in a cell between the key and the word,
	// so the digit that toggles a row is still on it.
	if !strings.Contains(screen, "5   Impressionism") ||
		!strings.Contains(screen, tokens.GlyphRecommended+" "+questionRecommendedWord) {
		t.Fatalf("the asker's pick is not said on its row:\n%s", screen)
	}
	if !strings.Contains(screen, tokens.GlyphPointer+" 1   Landscapes") {
		t.Fatalf("the pointer does not start on the first row:\n%s", screen)
	}
	if strings.Contains(screen, "take the pick") || !strings.Contains(screen, "tab next row") {
		t.Fatalf("the key row is wrong for a checklist:\n%s", screen)
	}
	if strings.Contains(screen, "…") {
		t.Fatalf("an answer was cut instead of given its own row:\n%s", screen)
	}
	for i, label := range labels {
		if !strings.Contains(screen, itoa(i+1)+"   "+label) {
			t.Fatalf("answer %d is not on a row of its own:\n%s", i+1, screen)
		}
	}
	if strings.Contains(screen, "[1] Landscapes") {
		t.Fatalf("the answers were also spelled on the offer row:\n%s", screen)
	}
	// THE ROWS ARE THE TICKS. A digit ticks its row, enter sends what is ticked
	// — in the block, without opening the page — and enter over nothing ticked
	// sends nothing. (Past the settle guard, which drops the first quarter
	// second of keys on every question.)
	lab.tick(time.Second)
	if !lab.press("enter") || len(lab.answer) != 0 {
		t.Fatalf("enter over an empty checklist answered · %+v", lab.answer)
	}
	lab.press("3")
	lab.press("7")
	screen = lab.plain()
	if !strings.Contains(screen, "3 "+tokens.GlyphSettled+" Still life") || !strings.Contains(screen, "7 "+tokens.GlyphSettled+" History") {
		t.Fatalf("the ticked rows do not wear their ticks:\n%s", screen)
	}
	if !strings.Contains(screen, "send what is ticked") {
		t.Fatalf("the key row does not say enter sends:\n%s", screen)
	}
	lab.press("3")
	if screen = lab.plain(); strings.Contains(screen, "3 "+tokens.GlyphSettled+" Still life") {
		t.Fatalf("a second press did not untick the row:\n%s", screen)
	}
	// A digit leaves the pointer on its row, tab walks it on, and space ticks
	// where it stands.
	lab.press("tab")
	if screen = lab.plain(); !strings.Contains(screen, tokens.GlyphPointer+" 4   Abstract") {
		t.Fatalf("tab did not walk the pointer to the next row:\n%s", screen)
	}
	lab.press("space")
	if screen = lab.plain(); !strings.Contains(screen, "4 "+tokens.GlyphSettled+" Abstract") {
		t.Fatalf("space did not tick the pointed row:\n%s", screen)
	}
	lab.press("space")
	lab.press("enter")
	if len(lab.answer) != 1 || strings.Join(lab.answer[0].Picked, ",") != "7" {
		t.Fatalf("the answer is not what was ticked · %+v", lab.answer)
	}
}

// AND A LONG ANSWER WRAPS RATHER THAN BEING CUT, on rows that all press the
// same answer.
func TestALongAnswerWrapsOntoRowsThatPressTheSameAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 60
	long := "Keep the sqlite file as the source of truth and rebuild every derived table from it on start"
	lab.raise(session.Question{
		ID: 42, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which storage shape?",
		Reason: "two shapes fit", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: long, Consequence: "one file, slower starts"},
			{Key: "2", Label: "Postgres", Consequence: "a server to run"},
		},
	})
	screen := lab.plain()
	// The only ellipsis a panel is allowed is the `something else…` row, which
	// is an answer that OPENS rather than one that was cut.
	if strings.Contains(strings.ReplaceAll(screen, questionPanelOtherWord, ""), "…") {
		t.Fatalf("an answer was cut:\n%s", screen)
	}
	for _, piece := range []string{"1  Keep the sqlite file", "derived table", "one file, slower starts", "2  Postgres"} {
		if !strings.Contains(screen, piece) {
			t.Fatalf("the wrapped answer lost %q:\n%s", piece, screen)
		}
	}
	rows := lab.rows()
	first, last := -1, -1
	for i, band := range lab.a.questionBands {
		if band.at == 0 {
			if first < 0 {
				first = band.row
			}
			last = band.row
		}
		_ = i
	}
	if first < 0 || last <= first {
		t.Fatalf("the wrapped answer is not pressable on every row it took · bands=%+v rows=%d", lab.a.questionBands, len(rows))
	}
}

// A PLAIN LINE STAYS A LINE. Three short answers and nothing beside them fit
// one row, whatever the asker called the form.
func TestThreePlainAnswersStillDrawAsOneRow(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 43, Kind: session.QuestionAsk, Ask: session.AskChoice, Form: session.FormLine,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "publish the draft?",
		Reason: "nobody has read it", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "publish it"}, {Key: "2", Label: "hold it"}, {Key: "3", Label: "ask Sam"}},
	})
	// NO BRACKETS ANYWHERE ON THIS SURFACE ANY MORE: a key is the payload hue
	// and its word is dim, on the row exactly as on the panel's edges.
	screen := lab.plain()
	if !strings.Contains(screen, tokens.GlyphPointer+"1 publish it") ||
		!strings.Contains(screen, "2 hold it") || !strings.Contains(screen, "3 ask Sam") {
		t.Fatalf("three plain answers left the line:\n%s", screen)
	}
	if strings.Contains(screen, "[1]") {
		t.Fatalf("the row still spells its keys in brackets:\n%s", screen)
	}
}

// ── a conversation switch leaves the other one's questions behind ────────────

func TestSwitchingConversationsLeavesTheOtherOnesQuestionsBehind(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 44, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which painting genres do you like?",
		Reason: "you asked", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "landscape"}, {Key: "2", Label: "portrait"}},
	})
	lab.a.withdrawQuestion(lab.a.questions[0].question, "the turn moved on without it")
	if !strings.Contains(lab.plain(), "Which painting genres") {
		t.Fatal("the withdrawn line is not on the screen to begin with")
	}
	other := &questionScript{fakeAgent: &fakeAgent{}}
	drain(t, lab.a, lab.a.attachConversation(Conversation{Agent: other, SessionFile: t.TempDir() + "/other.jsonl"}, nil))
	if screen := lab.plain(); strings.Contains(screen, "Which painting genres") {
		t.Fatalf("the other conversation's question followed the switch:\n%s", screen)
	}
	if len(lab.a.questions) != 0 || len(lab.a.questionRecords) != 0 {
		t.Fatalf("questions or records survived the switch · %d / %d", len(lab.a.questions), len(lab.a.questionRecords))
	}
}

// AND THE PANEL MAY NOT PUSH ITS OWN HEAD OFF THE SCREEN: eight answers each
// carrying a paragraph draw one note — the pointed answer's — held to two rows,
// and the whole panel stays inside half the screen.
func TestALongNoteUnderEveryAnswerIsCutToWhatTheScreenHolds(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 120, 36
	note := "Compositions of objects, vanitas pieces, floral arrangements, the Dutch Golden Age, Chardin, Morandi, and modern tabletop work that carries the same stillness"
	options := make([]session.AnswerOption, 0, 8)
	for i := 0; i < 8; i++ {
		options = append(options, session.AnswerOption{Key: itoa(i + 1), Label: "Genre " + itoa(i+1), Body: note})
	}
	lab.raise(session.Question{
		ID: 44, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which genres?",
		Reason: "a tour needs a shape", Options: options,
		Input: session.InputShape{Kind: session.InputChecklist}, Stakes: session.StakesReversible,
	})
	// THE NOTE IS UNDER THE POINTER AND NOWHERE ELSE (owner ruling 2026-09-11,
	// recommended pick A). Eight answers each carrying a paragraph is a panel
	// taller than the conversation under it; the pointer is the person saying
	// which one they are weighing, and `o open full` holds the rest.
	screen := lab.plain()
	if n := strings.Count(screen, glyphMore); n > 1 {
		t.Fatalf("a note was drawn for an answer nobody is on, %d marks:\n%s", n, screen)
	}
	if strings.Count(screen, "Chardin") != 1 {
		t.Fatalf("the note is drawn for more than the pointed answer:\n%s", screen)
	}
	if rows := lab.a.questionHeight(); rows > 8+2*questionPanelBodyRows+6 {
		t.Fatalf("the panel spends %d rows of a 36-row screen:\n%s", rows, screen)
	}
	// And the pointer moved onto another answer takes the note with it.
	lab.tick(time.Second)
	lab.press("tab")
	if screen = lab.plain(); !strings.Contains(screen, "2   Genre 2") || strings.Count(screen, "Chardin") != 1 {
		t.Fatalf("the note did not follow the pointer:\n%s", screen)
	}
}

// EVERY QUESTION HAS A POINTER THE ARROWS WALK AND ENTER TAKES. It starts on
// the asker's pick, `↓` moves the ▸, `enter` answers the pointed row, and the
// key row says both; a digit still answers at once.
func TestArrowsWalkThePointerOnACardAndEnterTakesIt(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 100
	lab.raise(session.Question{
		ID: 45, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "How is the breath paced?",
		Reason: "the orb's behaviour is undefined", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "Fixed guided pacing", Body: "Deterministic 4 in, 2 hold, 6 out", Consequence: "no sensors"},
			{Key: "2", Label: "Adaptive to the body", Body: "Dwell lengthens as HRV rises", Consequence: "needs a watch"},
			{Key: "3", Label: "Free timer", Consequence: "cheapest"},
		},
		Pick: &session.Pick{Key: "2", Reason: "most guidance"},
	})
	screen := lab.plain()
	if !strings.Contains(screen, tokens.GlyphPointer+" 2  Adaptive") {
		t.Fatalf("the pointer does not start on the asker's pick:\n%s", screen)
	}
	// THE PICK IS A MARK AND A WORD AT THE RIGHT EDGE OF ITS ROW, never a word
	// appended to what the answer costs (owner ruling 2026-09-11, recommended
	// pick A) — `needs a watch · suggested` read as one more thing it would do.
	if !strings.Contains(screen, tokens.GlyphRecommended+" "+questionRecommendedWord) {
		t.Fatalf("the asker's pick is not said on its row:\n%s", screen)
	}
	if !strings.Contains(screen, "↑↓ choose") || !strings.Contains(screen, "enter take it") {
		t.Fatalf("the key row does not say how the pointer works:\n%s", screen)
	}
	lab.tick(time.Second)
	lab.press("down")
	if screen = lab.plain(); !strings.Contains(screen, tokens.GlyphPointer+" 3  Free timer") {
		t.Fatalf("down did not walk the pointer:\n%s", screen)
	}
	lab.press("up")
	lab.press("up")
	if screen = lab.plain(); !strings.Contains(screen, tokens.GlyphPointer+" 1  Fixed") {
		t.Fatalf("up did not walk the pointer back:\n%s", screen)
	}
	lab.press("up")
	if screen = lab.plain(); !strings.Contains(screen, tokens.GlyphPointer+" 1  Fixed") {
		t.Fatalf("the pointer walked off the top:\n%s", screen)
	}
	lab.press("enter")
	if len(lab.answer) != 1 || lab.answer[0].Key != "1" {
		t.Fatalf("enter did not take the pointed answer · %+v", lab.answer)
	}
}

// AND ON A LINE THE POINTED ANSWER WEARS THE BAND: `→` moves it, enter takes it.
func TestArrowsWalkThePointerOnALineAndEnterTakesIt(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 100
	lab.raise(session.Question{
		ID: 46, Kind: session.QuestionAsk, Ask: session.AskChoice, Form: session.FormLine,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which format?",
		Reason: "both fit", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "json"}, {Key: "2", Label: "yaml"}, {Key: "3", Label: "toml"}},
	})
	screen := lab.plain()
	if !strings.Contains(screen, "1 json") || !strings.Contains(screen, "2 yaml") ||
		!strings.Contains(screen, "3 toml") {
		t.Fatalf("the line did not draw:\n%s", screen)
	}
	if !strings.Contains(screen, "enter take it") {
		t.Fatalf("enter is not offered on a question with answers:\n%s", screen)
	}
	lab.tick(time.Second)
	lab.press("right")
	lab.press("right")
	lab.press("enter")
	if len(lab.answer) != 1 || lab.answer[0].Key != "3" {
		t.Fatalf("enter did not take the pointed answer · %+v", lab.answer)
	}
}

// THE PAGE `enter` LANDS ON FROM A NEEDS-YOU ROW SHOWS THE QUESTION IT NEEDS
// YOU FOR. The task record card draws the landing's head, reason and answers
// row above its foot, walks the pointer with `←→`, and `enter`/`a`/`n` answer
// through the block's own door — a page that showed the report and hid the
// three answers was the owner's "i get this without question" (2026-09-10).
func TestTheTaskRecordPageDrawsTheLandingQuestionAndTakesItsKeys(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width, lab.a.height = 120, 40
	lab.a.file = t.TempDir() + "/abc123/transcript.jsonl"
	lab.raise(session.Question{
		ID: 6, Kind: session.QuestionLanding, Ask: session.AskLanding,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "tier-B subs landed",
		Reason: "nobody could check it", Stakes: session.StakesReversible,
		Options: session.AnswerOptions(session.QuestionLanding),
	})
	lab.a.raisePlace(pageTasks)
	lab.a.taskSheet = tasksPlace{detailOn: true, detail: session.TaskIndexEntry{
		ID: "6", SessionID: "abc123", Title: "tier-B subs", Label: "tier-B subs",
		Status: string(session.TaskUnverified),
	}}
	width, height := lab.a.size()
	lines, _, _, _ := lab.a.taskCardFrame(width, height)
	screen := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"tier-B subs landed", "nobody could check it",
		"a  accept", "n  not right", "s  tell it", "←→ choose · enter take it"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the record page is missing %q:\n%s", want, screen)
		}
	}
	// Another session's task wearing the same number draws no question.
	lab.a.taskSheet.detail.SessionID = "zzz999"
	lines, _, _, _ = lab.a.taskCardFrame(width, height)
	if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "a  accept") {
		t.Fatal("another session's task borrowed this one's question")
	}
	lab.a.taskSheet.detail.SessionID = "abc123"
	lab.tick(time.Second)
	lab.spend(lab.a.taskCardKey("right"))
	lab.spend(lab.a.taskCardKey("enter"))
	if len(lab.answer) != 1 || lab.answer[0].Key != session.LandingNoKey {
		t.Fatalf("enter did not take the pointed answer · %+v", lab.answer)
	}
}

// `c` AND `?` POINT THE BOX AT THE QUESTION, VISIBLY. The answers row becomes
// a prompt saying what the box is writing now, every letter types (a `d` is a
// letter, not `you decide`), enter sends the words with the pointed answer
// (`c`) or to the asker with the question still open (`?`), and esc points the
// box back at the conversation. Before this the keys changed nothing on the
// screen and the words went out as a chat message (the owner, 2026-09-10).
func TestChangeAndAskBackTurnTheRowIntoAPromptAndEnterSendsTheWords(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 120
	lab.raise(session.Question{
		ID: 47, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "How is the breath paced?",
		Reason: "undefined", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "Fixed"}, {Key: "2", Label: "Adaptive"}, {Key: "3", Label: "Free timer"}},
		Pick:    &session.Pick{Key: "2", Reason: "most guidance"},
	})
	lab.tick(time.Second)
	// The person is looking at the block rather than at the box, which is what
	// lets a letter reach it at all (questionkeys.go's THE BOX KEEPS THE FIRST
	// LETTER).
	aimed(lab.a)
	lab.press("c")
	screen := lab.plain()
	// `c` IS A SHORTCUT TO THE `something else…` ROW and that row IS the box
	// (owner ruling 2026-09-11, your-own-answer pick A). There is no hidden
	// mode and no sentence explaining one: the pointer is on the row, the row
	// says which answer the words will travel with, and the keys that mean
	// anything while it is open are the only ones the edge names.
	if !strings.Contains(screen, questionOtherWithWord+"2 Adaptive") {
		t.Fatalf("c did not turn the row into a prompt:\n%s", screen)
	}
	if !strings.Contains(screen, "enter send it") || !strings.Contains(screen, "↑ back to the list") {
		t.Fatalf("the row's own two keys are not on the edge:\n%s", screen)
	}
	if strings.Contains(screen, "d you decide") {
		t.Fatalf("the keys are still offered while the box is writing:\n%s", screen)
	}
	// A LETTER IS A LETTER ON THIS ROW: `d` types a `d` rather than handing the
	// decision back, which is what "the row is the box" has to mean for every
	// key that is also a verb somewhere else.
	if !lab.press("d") {
		t.Fatal("a letter did not reach the row's own box")
	}
	if len(lab.answer) != 0 {
		t.Fatalf("a letter answered: %+v", lab.answer)
	}
	open := lab.a.questionHeld(lab.a.questions[0].token())
	if open == nil {
		t.Fatal("the question is not open any more")
	}
	open.other.words.setText("keep the sensors optional")
	lab.press("enter")
	if len(lab.answer) != 1 || lab.answer[0].Key != "2" || lab.answer[0].Change != "keep the sensors optional" {
		t.Fatalf("enter did not send the words with the pointed answer · %+v", lab.answer)
	}
	if lab.a.input.String() != "" {
		t.Fatalf("the box kept the words: %q", lab.a.input.String())
	}
	// `?` — and esc points the box back.
	lab.raise(session.Question{
		ID: 48, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "Which store?",
		Reason: "two fit", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}, {Key: "2", Label: "postgres"}},
	})
	lab.tick(time.Second)
	// AND THE NEXT QUESTION HAS TO BE AIMED AT TOO: the hand is given up with
	// the question before it (questionkeys.go).
	aimed(lab.a)
	lab.press("?")
	if screen = lab.plain(); !strings.Contains(screen, "ask back: type your question, then enter · the question stays open · esc back") {
		t.Fatalf("? did not turn the row into a prompt:\n%s", screen)
	}
	lab.press("esc")
	if screen = lab.plain(); !strings.Contains(screen, "enter take it") || strings.Contains(screen, "ask back: type") {
		t.Fatalf("esc did not point the box back at the conversation:\n%s", screen)
	}
	if len(lab.a.questions) != 1 {
		t.Fatal("esc folded the question instead of ending the prompt")
	}
}

// TestAnAnswerTheDoorNeverAcknowledgedIsStillThisWindowsAnswer is the receipt's
// own honesty, and it is a defect that was measured rather than imagined.
//
// The engine runs in its own process even on this machine, so an answer crosses
// a wire: [session.Agent.ResolveQuestion] applies it, emits
// [session.EventQuestionAnswered], and writes its reply afterwards. When that
// reply does not arrive — a deadline spent while the engine was busy, a pipe
// that went — this window is left holding a question the engine has already
// settled, and the lane's news about that settling reads, to
// [app.foldOthersAnswer]'s ordinary rule, exactly like somebody else's answer.
// On the ordinary road's own e2e that drew `decided … · another window ·` over a
// key pressed on this very screen, about one run in nine.
func TestAnAnswerTheDoorNeverAcknowledgedIsStillThisWindowsAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	// THE DOOR TAKES IT AND SAYS NOTHING, which is the whole shape of the
	// failure: refusing an answer and losing the reply to one look identical
	// from here, and only the second leaves the engine settled.
	lab.agent.answer = func(answer session.Answer) error {
		lab.answer = append(lab.answer, answer)
		return errors.New("the engine did not answer in time")
	}
	ask := consentAsk()
	lab.raise(ask)
	lab.tick(questionSettle)
	lab.rows()
	if !lab.press("1") {
		t.Fatal("the settled question did not take its own key")
	}
	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "1" {
		t.Fatalf("the door was handed %+v", lab.answer)
	}
	if notes := failureNotes(lab.a.entries); len(notes) != 1 || notes[0] != "the engine did not answer in time" {
		t.Fatalf("the door's refusal was not said: %q", notes)
	}
	// AND THE LANE BRINGS THAT VERY ANSWER BACK.
	settled, q := lab.answer[0], ask
	lab.a.questionFold(session.Event{
		Kind: session.EventQuestionAnswered, Question: &q, Answer: &settled,
	})
	got := lab.plain()
	if strings.Contains(got, "another window") {
		t.Fatalf("the receipt says somebody else pressed the key:\n%s", got)
	}
	if !strings.Contains(got, "you") {
		t.Fatalf("the receipt does not say who decided:\n%s", got)
	}
	if count := strings.Count(got, "→ allow once"); count != 1 {
		t.Fatalf("one answer left %d receipts:\n%s", count, got)
	}
	if len(lab.a.questions) != 0 {
		t.Fatalf("the settled question is still on the block: %d open", len(lab.a.questions))
	}
}

// TestAnAnswerThisWindowLostStillWearsTheOtherWindowsName is the other half of
// that law, and the one the guard must not break: FIRST ANSWER WINS
// (docs/design/questions/DESIGN.md). Two people pressed two different keys, the
// engine took the other one, and the receipt this window draws is about a
// decision it did not make.
func TestAnAnswerThisWindowLostStillWearsTheOtherWindowsName(t *testing.T) {
	lab := newQuestionLab(t)
	lab.agent.answer = func(answer session.Answer) error {
		lab.answer = append(lab.answer, answer)
		return errors.New("that question has already been decided")
	}
	ask := consentAsk()
	lab.raise(ask)
	lab.tick(questionSettle)
	lab.rows()
	if !lab.press("1") {
		t.Fatal("the settled question did not take its own key")
	}
	if notes := failureNotes(lab.a.entries); len(notes) != 1 || notes[0] != "that question has already been decided" {
		t.Fatalf("the door's refusal was not said: %q", notes)
	}
	q := ask
	other := session.Answer{
		Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask,
		Key: "3", Picked: []string{"3"}, DecidedBy: session.DecidedByPerson, At: lab.at,
	}
	lab.a.questionFold(session.Event{
		Kind: session.EventQuestionAnswered, Question: &q, Answer: &other,
	})
	got := lab.plain()
	if !strings.Contains(got, "another window") {
		t.Fatalf("a key pressed on another screen was drawn as this window's:\n%s", got)
	}
}

// TestEnterOnTheEnginesOwnPermissionDeniesTheCall presses `enter` on the
// question THIS ENGINE ASKS about `rm -rf *`, rather than on one this file made
// up to look like it.
//
// That distinction is the whole reason the test exists. The version of the
// pointer rule that shipped in #933's first push was proved against a fixture
// that set `StakesIrreversible` by hand; the engine's gate stamps
// [session.StakesCostly] on every consent it raises, so the fixture described a
// frame nobody ever meets and the rule it proved allowed `rm -rf *` on `enter`
// in production while the test stayed green.
//
// So the object comes off disk, from internal/session/testdata, and
// TestTheGateGradesNoCallAsIrreversibleAndMarksDenyTheSafeAnswer in that package
// is what keeps the file equal to what `consentAsk` returns. Neither test can go
// green on a lie on its own: one holds the engine to the file, this one holds the
// surface to the engine.
func TestEnterOnTheEnginesOwnPermissionDeniesTheCall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "session", "testdata", "consentask-rm-rf.json"))
	if err != nil {
		t.Fatalf("read the engine's own consent question: %v", err)
	}
	var ask session.Question
	if err := json.Unmarshal(raw, &ask); err != nil {
		t.Fatalf("decode the engine's own consent question: %v", err)
	}
	// The file is the gate's answer about a recursive delete, so a test that
	// stopped being about one would say nothing worth knowing.
	if ask.Ask != session.AskPermission || ask.Subject.Kind != session.SubjectCall {
		t.Fatalf("the file no longer holds a permission about a call: %+v", ask)
	}

	lab := newQuestionLab(t)
	lab.raise(ask)
	lab.tick(questionSettle)
	lab.rows()

	safe := questionSafeAt(ask)
	if ask.Options[safe].Label != "deny" {
		t.Fatalf("the answer that loses nothing is not the refusal: %q", ask.Options[safe].Label)
	}
	head, _ := lab.a.questionHead()
	if head.pick != safe {
		t.Fatalf("the pointer opened on %q, not on `deny`", ask.Options[head.pick].Label)
	}
	if !lab.press("enter") {
		t.Fatal("enter was not taken by the engine's own permission")
	}
	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != ask.Options[safe].Key {
		t.Fatalf("enter on `rm -rf *` answered %+v, not the refusal %q",
			lab.answer, ask.Options[safe].Key)
	}
}
