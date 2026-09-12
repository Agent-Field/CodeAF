package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// setQuestion is one model question raised by the step `batch`: two answers and
// the first recommended, which is the ordinary shape of an `ask`.
func setQuestion(id uint64, batch, head string) session.Question {
	return session.Question{
		ID: id, Kind: session.QuestionAsk, Ask: session.AskChoice, Batch: batch,
		Asker: session.Asker{Kind: session.AskerModel},
		Head:  head,
		Options: []session.AnswerOption{
			{Key: "1", Label: "sqlite", Consequence: "one file"},
			{Key: "2", Label: "jsonl", Consequence: "append only"},
		},
		Pick:     &session.Pick{Key: "1"},
		Stakes:   session.StakesReversible,
		Blocking: session.Blocking{Turn: true},
	}
}

// setPermission is one approval raised by the step `batch`, in the engine's own
// answers for that lane, about a call to `tool` on `target` that is already a
// row in the conversation (the frame reads the call off its row).
func setPermission(lab *questionLab, id uint64, batch, tool, target string, stakes session.Stakes) session.Question {
	call := "call-" + itoa(int(id))
	lab.a.entries = append(lab.a.entries, entry{
		kind: entryTool, tool: tool, text: tool + " " + target, turn: lab.a.turn,
		status: toolConsent, callID: call,
	})
	return session.Question{
		ID: id, Kind: session.QuestionConsent, Ask: session.AskPermission, Batch: batch,
		Asker:    session.Asker{Kind: session.AskerEngine},
		Head:     "needs your ok to run " + tool,
		Subject:  session.SubjectRef{Kind: session.SubjectCall, CallID: call, Name: tool},
		Options:  session.AnswerOptions(session.QuestionConsent),
		Stakes:   stakes,
		Blocking: session.Blocking{Turn: true},
		Scope:    []session.AnswerScope{session.ScopeOnce, session.ScopeAlways},
	}
}

// setLab raises three questions from one step and spends the settle guard.
func setLab(t *testing.T) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	lab.raise(setQuestion(1, "step:4", "Which storage for the session index?"))
	lab.raise(setQuestion(2, "step:4", "What should the index file be called?"))
	lab.raise(setQuestion(3, "step:4", "Which tests should cover it?"))
	lab.tick(questionSettle * 2)
	lab.rows()
	return lab
}

func heldOf(lab *questionLab, id uint64) *session.Answer {
	for _, q := range lab.a.questions {
		if q.question.ID == id {
			return q.staged
		}
	}
	return nil
}

// SEVERAL QUESTIONS FROM ONE STEP ARE ONE PANEL: a tab each and a review in the
// top edge, the question on screen in the body, and `←→ question` first on the
// bottom edge.
func TestSeveralQuestionsFromOneStepAreOnePanelWithATabEach(t *testing.T) {
	lab := setLab(t)
	drawn := lab.plain()
	for _, want := range []string{"1 of 3", questionReviewWord, "←→ " + questionTabWord, "Which storage for the session index?", "sqlite"} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the set's panel does not say %q:\n%s", want, drawn)
		}
	}
	if strings.Contains(drawn, "more") {
		t.Fatalf("a set drew its own members as a queue behind it:\n%s", drawn)
	}
	if got := lab.a.questionViewOf(lab.a.questions[0], lab.a.width); got != viewTabs {
		t.Fatalf("the chooser gave the set's question view %d, want the tabs", got)
	}
}

// A QUESTION FROM ANOTHER STEP IS NOT A TAB, and neither is one raised outside
// any step: the set is the engine's step and nothing else.
func TestOnlyTheSameStepMakesASet(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(setQuestion(1, "step:4", "Which storage?"))
	lab.raise(setQuestion(2, "step:5", "Which name?"))
	lone := setQuestion(3, "", "Which tests?")
	lab.raise(lone)
	if set := lab.a.questionSet(); set != nil {
		t.Fatalf("questions from three different steps made a set of %d", len(set))
	}
	if drawn := lab.plain(); !strings.Contains(drawn, "2 more") {
		t.Fatalf("the questions behind the first are not counted:\n%s", drawn)
	}
}

// ENTER HOLDS THE ANSWER AND MOVES ON. Nothing reaches the engine until the
// review sends it.
func TestEnterOnATabHoldsTheAnswerAndMovesToTheNextQuestion(t *testing.T) {
	lab := setLab(t)
	lab.press("enter")
	if len(lab.answer) != 0 {
		t.Fatalf("a tab sent its answer before the review: %+v", lab.answer)
	}
	if held := heldOf(lab, 1); held == nil || held.Key != "1" {
		t.Fatalf("the first question holds %+v, want its pick", held)
	}
	if head, _ := lab.a.questionHead(); head.question.ID != 2 {
		t.Fatalf("the panel stayed on question %d, want the next one waiting", head.question.ID)
	}
	if drawn := lab.plain(); !strings.Contains(drawn, "2 of 3") || !strings.Contains(drawn, "What should the index file be called?") {
		t.Fatalf("the second tab is not the one on screen:\n%s", drawn)
	}
}

// ←→ GO BETWEEN QUESTIONS, AND AN ANSWER HELD CAN BE CHANGED — "go back to the
// previous question".
func TestTheArrowsGoBackAndAHeldAnswerCanBeChanged(t *testing.T) {
	lab := setLab(t)
	lab.press("enter")
	lab.press("left")
	if head, _ := lab.a.questionHead(); head.question.ID != 1 {
		t.Fatalf("← left the panel on question %d, want the first", head.question.ID)
	}
	lab.press("2")
	if held := heldOf(lab, 1); held == nil || held.Key != "2" {
		t.Fatalf("the changed answer is %+v, want 2", held)
	}
	if len(lab.answer) != 0 {
		t.Fatalf("changing a held answer sent it: %+v", lab.answer)
	}
	// AND THE ENDS STOP: ← on the first tab goes nowhere.
	lab.press("left")
	lab.press("left")
	if head, _ := lab.a.questionHead(); head.question.ID != 1 {
		t.Fatalf("← wrapped round to question %d", head.question.ID)
	}
}

// THE REVIEW SENDS EVERY HELD ANSWER, IN TAB ORDER, THROUGH THE ONE DOOR IN ONE
// COMMAND — "answer them all at once".
func TestTheReviewSendsEveryAnswerInTabOrderInOneCommand(t *testing.T) {
	lab := setLab(t)
	lab.press("2")
	lab.press("enter")
	lab.press("enter")
	if !lab.a.questionSetReviewing(lab.a.questionSet()) {
		t.Fatalf("with every question answered the panel is not on the review:\n%s", lab.plain())
	}
	drawn := lab.plain()
	if !strings.Contains(drawn, questionSendWord(3, 3)) || !strings.Contains(drawn, questionResumesWord) {
		t.Fatalf("the review does not offer to send all three:\n%s", drawn)
	}
	cmd, took := lab.a.questionKey(questionPressOf("enter"))
	if !took || cmd == nil {
		t.Fatal("enter on the review sent nothing")
	}
	if msgs := runCmd(cmd); len(msgs) != 1 {
		t.Fatalf("the review became %d door trips, want one", len(msgs))
	} else {
		drive(t, lab.a, msgs...)
	}
	if len(lab.answer) != 3 {
		t.Fatalf("the door was told %d answers, want three", len(lab.answer))
	}
	for i, want := range []string{"2", "1", "1"} {
		if lab.answer[i].ID != uint64(i+1) || lab.answer[i].Key != want {
			t.Fatalf("answer %d went out as %d/%q, want %d/%q", i, lab.answer[i].ID, lab.answer[i].Key, i+1, want)
		}
	}
	if len(lab.a.questions) != 0 {
		t.Fatalf("%d questions are still open after the review sent them", len(lab.a.questions))
	}
}

// A QUESTION NOBODY ANSWERED STAYS OPEN, and the review says so rather than
// sending a pick nobody pressed.
func TestTheReviewSendsWhatIsHeldAndLeavesTheRestOpen(t *testing.T) {
	lab := setLab(t)
	lab.press("enter")
	lab.press("right")
	lab.press("right")
	lab.press("right")
	drawn := lab.plain()
	if !strings.Contains(drawn, questionNotAnsweredWord) || !strings.Contains(drawn, questionSendWord(1, 3)) {
		t.Fatalf("the review does not say two are unanswered:\n%s", drawn)
	}
	lab.press("enter")
	if len(lab.answer) != 1 || lab.answer[0].ID != 1 {
		t.Fatalf("the review sent %+v, want only the one answered", lab.answer)
	}
	if len(lab.a.questions) != 2 {
		t.Fatalf("%d questions are left, want the two nobody answered", len(lab.a.questions))
	}
	if set := lab.a.questionSet(); len(set) != 2 {
		t.Fatalf("the two left are not still a set: %d", len(set))
	}
}

// A REVIEW WITH NOTHING HELD HAS NOTHING TO SEND, and says where the answers are
// given on the rows themselves — once each, and never a fourth sentence under
// them saying the set is empty (the emptiness law).
func TestAReviewWithNothingHeldOffersNoSend(t *testing.T) {
	lab := setLab(t)
	for i := 0; i < 3; i++ {
		lab.press("right")
	}
	drawn := lab.plain()
	if strings.Contains(drawn, "enter send") {
		t.Fatalf("an empty review offered to send:\n%s", drawn)
	}
	if got, want := strings.Count(drawn, questionNotAnsweredWord), 3; got != want {
		t.Fatalf("an empty review says where to answer %d times, want %d (one per row):\n%s", got, want, drawn)
	}
	lab.press("enter")
	if len(lab.answer) != 0 {
		t.Fatalf("an empty review sent %+v", lab.answer)
	}
}

// ESC IS LATER FOR THE WHOLE SET. Every tab folds to one rule, the count does not
// drop, and opening it again brings back every tab with every held answer.
func TestEscIsLaterForTheWholeSetAndKeepsWhatWasHeld(t *testing.T) {
	lab := setLab(t)
	lab.press("enter")
	lab.press("esc")
	if len(lab.answer) != 0 {
		t.Fatalf("esc answered something: %+v", lab.answer)
	}
	if lab.a.questioning() {
		t.Fatal("esc left part of the set on the block")
	}
	if got := lab.a.questionCount(); got != 3 {
		t.Fatalf("the chip counts %d after esc, want all three", got)
	}
	if drawn := lab.plain(); !strings.Contains(drawn, "3 questions") {
		t.Fatalf("the folded set's rule does not say how many:\n%s", drawn)
	}
	lab.a.raiseFolded()
	if set := lab.a.questionSet(); len(set) != 3 {
		t.Fatalf("opening the fold brought back %d tabs, want three", len(set))
	}
	if held := heldOf(lab, 1); held == nil {
		t.Fatal("the answer held before esc was lost")
	}
}

// AN IRREVERSIBLE QUESTION NEVER JOINS. It stands on its own frame with the
// pointer on the answer that loses nothing.
func TestAnIrreversibleQuestionNeverJoinsASet(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(setQuestion(1, "step:4", "Which storage?"))
	risky := setQuestion(2, "step:4", "Drop the old table?")
	risky.Stakes, risky.Pick = session.StakesIrreversible, nil
	lab.raise(risky)
	if set := lab.a.questionSet(); set != nil {
		t.Fatalf("an irreversible question joined a set of %d", len(set))
	}
}

// ONLY A KEY ON THE PANEL HOLDS AN ANSWER. The same question answered from home
// or by the project's rule goes straight through the door.
func TestAnAnswerFromAnywhereButThePanelIsNotHeld(t *testing.T) {
	lab := setLab(t)
	member := lab.a.questions[1]
	lab.spend(lab.a.answerQuestion(member, session.Answer{Key: "2", Picked: []string{"2"}}))
	if len(lab.answer) != 1 || lab.answer[0].ID != 2 {
		t.Fatalf("an answer from outside the panel was held: %+v", lab.answer)
	}
	if set := lab.a.questionSet(); len(set) != 2 {
		t.Fatalf("the set did not lose the question answered elsewhere: %d", len(set))
	}
}

// A TAB IS CALLED BY WHAT TELLS IT APART: the words every question opens with
// are taken off, and a call is called by what it touches.
func TestATabIsCalledByWhatTellsItApart(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(setQuestion(1, "step:4", "read package.json?"))
	lab.raise(setQuestion(2, "step:4", "read go.mod?"))
	labels := lab.a.questionTabLabels(lab.a.questionSet())
	if strings.Join(labels, "|") != "package.json|go.mod" {
		t.Fatalf("labels = %q", labels)
	}
}

// THE TABS READ IN THE TRANSCRIPT'S ORDER. The gates of one step finish in any
// order; the panel lists the calls the way the transcript drew them, each called
// by the file that tells it apart rather than by the folder they share.
func TestTheTabsFollowTheRowsTheyAreAbout(t *testing.T) {
	lab := newQuestionLab(t)
	var qs []session.Question
	for i, name := range []string{"e", "f", "g", "h"} {
		qs = append(qs, setPermission(lab, uint64(i+1), "step:9", "read", "/tmp/work/"+name+".txt", session.StakesReversible))
	}
	for _, i := range []int{2, 1, 0, 3} {
		lab.raise(qs[i])
	}
	set := lab.a.questionSet()
	var ids []string
	for _, q := range set {
		ids = append(ids, itoa(int(q.question.ID)))
	}
	if got := strings.Join(ids, " "); got != "1 2 3 4" {
		t.Fatalf("the tabs are in arrival order, not the transcript's: %s", got)
	}
	if got := strings.Join(lab.a.questionTabLabels(set), "|"); got != "e.txt|f.txt|g.txt|h.txt" {
		t.Fatalf("labels = %q", got)
	}
}

// THE STEP SURVIVES WHICHEVER COPY LANDS LAST. A lane's dressed copy carries no
// batch, and it may arrive after the engine's.
func TestTheStepSurvivesADressedCopyArrivingSecond(t *testing.T) {
	lab := newQuestionLab(t)
	q := setQuestion(1, "step:4", "Which storage?")
	lab.raise(q)
	dressed := q
	dressed.Batch = ""
	lab.a.raiseQuestion(questionShown{question: dressed, answered: func(a session.Answer) session.Answer { return a }})
	if got := lab.a.questions[0].question.Batch; got != "step:4" {
		t.Fatalf("the dressed copy took the step off the question: %q", got)
	}
}

// ── one permission frame ────────────────────────────────────────────────────

// questionRowSaying is the ONE drawn row carrying `word`, for the assertions
// that are about which row a mark landed on. A frame-wide
// [strings.Contains] cannot tell `deny all  safe answer` from `allow all 4
// safe answer`, and the difference between those two screens is the whole
// question of whether a person can trust the mark.
func questionRowSaying(t *testing.T, lab *questionLab, word string) string {
	t.Helper()
	var found []string
	for _, row := range questionPlainRows(lab.rows()) {
		if strings.Contains(row, word) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one row saying %q, got %d:\n%s", word, len(found), lab.plain())
	}
	return found[0]
}

func permissionLab(t *testing.T, stakes session.Stakes) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	for i, target := range []string{"internal/session/store.go", "internal/session/loop.go", "internal/session/agent.go", "go.mod"} {
		lab.raise(setPermission(lab, uint64(i+1), "step:9", "read", target, stakes))
	}
	lab.tick(questionSettle * 2)
	lab.rows()
	return lab
}

// PERMISSIONS FROM ONE STEP ARE ONE FRAME: what each call wants, then `allow all
// 4 · one by one · deny all`, with the pointer exactly where each question's own
// pointer would be — on an ORDINARY call, which four reads are, that is `allow
// all` ([questionGroupStart] over [questionPointerStart], the gate's grade
// deciding: #953).
//
// AND `deny all` SAYS `safe answer` THOUGH THE POINTER IS NOT ON IT. The frame
// forms only where every member has an answer that loses nothing, so the row
// always is one, and naming it matters MOST here — the pointer is standing on
// the act. The frame said it only under its own pointer until this test read
// the two rulings together: that was the same sentence while every permission
// opened on `deny` (#933), and silence on every ordinary frame once the gate
// graded.
func TestPermissionsFromOneStepAreOneFrame(t *testing.T) {
	lab := permissionLab(t, session.StakesCostly)
	drawn := lab.plain()
	for _, want := range []string{"allow these 4?", "internal/session/loop.go", "go.mod",
		questionAllowAllWord + "4", questionApartWord, questionDenyAllWord} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the permission frame does not say %q:\n%s", want, drawn)
		}
	}
	if got := lab.a.questionGroupPick(lab.a.questionSet()); got != questionGroupAllow {
		t.Fatalf("the pointer opened on row %d of an ordinary group, want allow all", got)
	}
	// AND THE MARK IS ON THE REFUSAL'S OWN ROW, asserted as a row and never as a
	// substring of the frame: `strings.Contains(drawn, questionSafeWord)` passed
	// just as happily with the mark moved onto `allow all`, which is the one
	// reading that would tell a person their keystroke loses nothing when it
	// grants four calls.
	if row := questionRowSaying(t, lab, questionDenyAllWord); !strings.Contains(row, questionSafeWord) {
		t.Fatalf("`deny all` does not carry %q:\n%s", questionSafeWord, drawn)
	}
	if row := questionRowSaying(t, lab, questionAllowAllWord+"4"); strings.Contains(row, questionSafeWord) {
		t.Fatalf("`allow all` wears the mark that belongs to the refusal:\n%s", drawn)
	}
	// "approve all of these" is one key, and it is each question's own grant.
	lab.press("1")
	if len(lab.answer) != 4 {
		t.Fatalf("allow all sent %d answers, want four", len(lab.answer))
	}
	for i, answer := range lab.answer {
		if answer.ID != uint64(i+1) || answer.Key != "1" {
			t.Fatalf("answer %d went out as %d/%q", i, answer.ID, answer.Key)
		}
	}
}

// THE BEAT'S ROW IS WHERE THE BEAT IS DRAWN, at every length of head.
//
// [app.questionBeatRow]'s second argument is written straight into
// [app.questionSpanRow], which is the screen row a click on the shapes resolves
// against. It used to be the literal `2`, correct only while the head fitted the
// frame's top edge: behind a head long enough to wrap, every press on the shapes
// landed one row off per wrapped line. It is now measured from what the caller
// has laid above the body, so the panel of one and the tab are both right.
func TestTheBeatsRowIsWhereTheBeatIsDrawnUnderAHeadThatWraps(t *testing.T) {
	lab := newQuestionLab(t)
	long := "needs your ok to run a command that " + strings.Repeat("goes on and on and on ", 12) + "before it ends"
	q := setPermission(lab, 1, "step:9", "bash", "git status --short", session.StakesCostly)
	q.Head = long
	lab.raise(q)
	lab.a.questions[0].shapes = func() []string { return []string{"git *", "git status*", "git status --short"} }
	lab.tick(questionSettle * 2)
	lab.rows()
	lab.press("2")

	rows := questionPlainRows(lab.rows())
	if len(wrap(long, 1)) < 2 {
		t.Fatal("the head under test does not wrap; the case it guards is not being exercised")
	}
	drawn := -1
	for i, row := range rows {
		if strings.Contains(row, "3 just this line") {
			drawn = i
		}
	}
	if drawn < 0 {
		t.Fatalf("the beat was never drawn:\n%s", strings.Join(rows, "\n"))
	}
	if lab.a.questionSpanRow != drawn {
		t.Fatalf("a press on the shapes resolves against row %d, but they are drawn on row %d:\n%s",
			lab.a.questionSpanRow, drawn, strings.Join(rows, "\n"))
	}
}

// A TAB PART-WAY THROUGH THE WIDENING ANSWER DRAWS THE BEAT, not the answers it
// replaced. `always` on a tab opens the shapes, and from that moment the digits
// pick a shape and `esc` backs out of the beat ([app.questionSetOwnsKey] hands
// both back to the question) — so a tab still drawing its answer rows, with
// `←→ question` on its edge, would be naming keys that do something else. It is
// the one drawing, [app.questionPanelInside], for the panel of one and the tab
// alike.
func TestATabPartWayThroughTheWideningAnswerDrawsTheBeat(t *testing.T) {
	lab := newQuestionLab(t)
	for i, target := range []string{"git status --short", "go build ./..."} {
		lab.raise(setPermission(lab, uint64(i+1), "step:9", "bash", target, session.StakesCostly))
	}
	for i := range lab.a.questions {
		lab.a.questions[i].shapes = func() []string { return []string{"git *", "git status*", "git status --short"} }
	}
	lab.tick(questionSettle * 2)
	lab.rows()
	// `one by one` opens the group as tabs, and `always` on the tab opens the beat.
	lab.press("2")
	lab.press("2")

	drawn := lab.plain()
	for _, want := range []string{"3 just this line", "1–3 shape", questionBeatBack} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the tab does not draw the beat's %q:\n%s", want, drawn)
		}
	}
	for _, gone := range []string{"allow once", questionTabWord} {
		if strings.Contains(drawn, gone) {
			t.Fatalf("the tab still draws %q while the shapes are up:\n%s", gone, drawn)
		}
	}
}

// ONE ROW PER THING WANTED: three calls nothing tells apart are one row that
// counts them, never the same row three times.
func TestAFrameCountsTheCallsNothingTellsApart(t *testing.T) {
	lab := newQuestionLab(t)
	for i := range 3 {
		lab.raise(setPermission(lab, uint64(i+1), "step:9", "ask", "", session.StakesCostly))
	}
	lab.tick(questionSettle * 2)
	drawn := lab.plain()
	if !strings.Contains(drawn, questionTimesWord+"3") {
		t.Fatalf("three alike calls are not one counted row:\n%s", drawn)
	}
	if strings.Count(drawn, "│ ask") != 1 {
		t.Fatalf("the frame draws the same call more than once:\n%s", drawn)
	}
}

// A CALL TOO LONG FOR ITS ROW LOSES ITS MIDDLE: every row still ends in the
// file that tells it apart from the others.
func TestAFrameRowKeepsTheEndOfALongCall(t *testing.T) {
	lab := newQuestionLab(t)
	lab.a.width = 72
	folder := "/tmp/" + strings.Repeat("a-long-folder-name/", 5)
	for i, name := range []string{"e", "f", "g", "h"} {
		lab.raise(setPermission(lab, uint64(i+1), "step:9", "read", folder+name+".txt", session.StakesReversible))
	}
	lab.tick(questionSettle * 2)
	drawn := lab.plain()
	for _, name := range []string{"e.txt", "f.txt", "g.txt", "h.txt"} {
		if !strings.Contains(drawn, name) {
			t.Fatalf("a row lost the end of its call %s:\n%s", name, drawn)
		}
	}
}

// THE GROUP POINTER IS EACH QUESTION'S OWN, READ OVER THE SET: `allow all` where
// every question alone would open on its grant, and `deny all` the moment one
// of them would not — here, because its asker recommended the deny. And enter
// on `deny all` gives each its own safe answer.
func TestTheGroupPointerIsEachQuestionsOwnReadOverTheSet(t *testing.T) {
	lab := permissionLab(t, session.StakesReversible)
	if got := lab.a.questionGroupPick(lab.a.questionSet()); got != questionGroupAllow {
		t.Fatalf("the pointer opened on row %d of a reversible group, want allow all", got)
	}
	lab.press("down")
	lab.press("down")
	lab.press("enter")
	if len(lab.answer) != 4 || lab.answer[0].Key != "3" {
		t.Fatalf("deny all sent %+v, want each question's safe answer", lab.answer)
	}

	wary := newQuestionLab(t)
	for i, target := range []string{"go.mod", "go.sum"} {
		q := setPermission(wary, uint64(i+1), "step:9", "read", target, session.StakesReversible)
		if i == 1 {
			q.Pick = &session.Pick{Key: "3"}
		}
		wary.raise(q)
	}
	if got := wary.a.questionGroupPick(wary.a.questionSet()); got != questionGroupDeny {
		t.Fatalf("one question would open on deny alone, and the frame opened on row %d", got)
	}
	// AND THIS FRAME MARKS NOTHING, because neither of its tabs would
	// ([questionSetMarksSafe] over [questionMarksSafe]). The second question's
	// asker picked an answer, so drawn alone its own row says `◆ recommended`
	// rather than `safe answer` — something already says why the pointer is
	// there — and a reversible call is not a question only a person may answer
	// in the first place. A frame that marked here would be claiming, for one
	// keystroke, something `2 one by one` then takes back.
	if strings.Contains(wary.plain(), questionSafeWord) {
		t.Fatalf("the frame marks a refusal its own tabs leave bare:\n%s", wary.plain())
	}
}

// THE FRAME'S POINTER IS THE ENGINE'S OWN GRADE, AND NO MODEL IS NEEDED TO SAY SO.
//
// The live suite's needle for this frame is the `safe answer` mark, and the mark
// is deliberately pointer-independent ([questionSetMarksSafe]) — so once it was,
// nothing anywhere checked the FRAME's pointer against what the gate actually
// stamps. That check cannot be left to a paid run: it is the difference between
// `enter` granting four calls and refusing them.
//
// BOTH HALVES COME OFF THE ENGINE, for [TestEnterOnTheEnginesOwnPermissionDenies
// TheCall]'s reason — a fixture that made up its own grade once described a frame
// nobody meets. The grave object is read from internal/session/testdata, and the
// ordinary answers are [session.AnswerOptions]'s own, not a list spelled here.
func TestTheFrameOpensWhereTheEngineGradedTheCalls(t *testing.T) {
	// THE CONTROL: the gate's own question about `rm -rf *` never joins a frame
	// at all, so `allow all` can never carry one along with its neighbours.
	raw, err := os.ReadFile(filepath.Join("..", "session", "testdata", "consentask-rm-rf.json"))
	if err != nil {
		t.Fatalf("read the engine's own consent question: %v", err)
	}
	var grave session.Question
	if err := json.Unmarshal(raw, &grave); err != nil {
		t.Fatalf("decode the engine's own consent question: %v", err)
	}
	if grave.Stakes != session.StakesIrreversible {
		t.Fatalf("the file is no longer the gate's answer about a grave call: %q", grave.Stakes)
	}
	grave.Batch = "step:9"
	guard := newQuestionLab(t)
	if guard.a.questionJoinsSet(questionShown{question: grave}) {
		t.Fatal("a call the gate graded irreversible joined a set: it is asked on its own, every time")
	}

	// AND THE ORDINARY CALLS: the grade the gate leaves on everything else, with
	// the engine's own answers under it. Four reads, so `allow all 4` is a real
	// bulk grant and not a frame of one.
	lab := newQuestionLab(t)
	for i, target := range []string{"notes/plan.md", "notes/todo.md", "notes/ideas.md", "notes/log.md"} {
		q := setPermission(lab, uint64(i+1), "step:9", "read", target, session.StakesCostly)
		q.Options = session.AnswerOptions(session.QuestionConsent)
		lab.raise(q)
	}
	lab.tick(questionSettle * 2)
	lab.rows()

	set := lab.a.questionSet()
	if len(set) != 4 {
		t.Fatalf("the four ordinary reads did not make one frame: %d", len(set))
	}
	if got := lab.a.questionGroupPick(set); got != questionGroupAllow {
		t.Fatalf("the pointer opened on row %d of a frame the gate graded ordinary, want allow all", got)
	}
	// `enter` ON AN UNTOUCHED FRAME, which is the claim a person's one keystroke
	// rests on — read off the pointer rather than spelled as a row number.
	if !lab.press("enter") {
		t.Fatal("enter was not taken by the frame")
	}
	if len(lab.answer) != 4 {
		t.Fatalf("enter sent %d answers, want each of the four its own", len(lab.answer))
	}
	grant, _, ok := questionGrantAndSafe(set[0].question)
	if !ok {
		t.Fatal("the engine's ordinary consent has no plain grant and safe answer")
	}
	for i, answer := range lab.answer {
		if answer.Key != grant {
			t.Fatalf("answer %d went out as %q, not the grant %q", i, answer.Key, grant)
		}
	}
}

// ONE BY ONE ANSWERS NOTHING and opens the same questions as tabs.
func TestOneByOneOpensTheSameSetAsTabs(t *testing.T) {
	lab := permissionLab(t, session.StakesCostly)
	lab.press("2")
	if len(lab.answer) != 0 {
		t.Fatalf("one by one answered something: %+v", lab.answer)
	}
	drawn := lab.plain()
	if !strings.Contains(drawn, "1 of 4") || !strings.Contains(drawn, "internal/session/store.go") {
		t.Fatalf("one by one did not open the tabs:\n%s", drawn)
	}
}

// A GROUPED PERMISSION OFFERS NO LIFETIME AT ALL, and neither does a single one
// (questionscope.go): the answer's scope never reaches the consent gate, so a
// row saying `from now on` over four calls would promise four times over what it
// cannot do once.
//
// THIS COMMENT USED TO SAY "THIS TEST FAILS THE DAY THE GRADING LANDS (#953),
// which is when the row comes back — one row for the set, moved together". The
// grading landed (f3a734ba1) and this test did not fail, because nothing here
// was ever wired to the grading: the prophecy was load-bearing for exactly
// nobody, and a green test that named the day it should turn red is worse than
// no note at all. Whether the row is owed is issue #995, which is where that
// decision now lives. What this test asserts is unchanged and true today: the
// frame offers no lifetime, and takes no key for one.
func TestAGroupedPermissionOffersNoLifetimeRow(t *testing.T) {
	lab := permissionLab(t, session.StakesReversible)
	drawn := lab.plain()
	for _, gone := range []string{
		questionScopeWord(session.ScopeOnce),
		questionScopeWord(session.ScopeAlways),
		questionScopeKey + " how long",
	} {
		if strings.Contains(drawn, gone) {
			t.Fatalf("the frame offers %q, which no answer it sends can carry:\n%s", gone, drawn)
		}
	}
	// And the key is not taken either, so nothing moves where nothing is drawn.
	lab.press("down")
	lab.press("up")
	if lab.press(questionScopeKey) {
		t.Fatal("the frame took `t` for a lifetime row it does not draw")
	}
	for _, q := range lab.a.questions {
		if q.scope != "" {
			t.Fatalf("question %d is on %q, and the frame never offered a lifetime", q.question.ID, q.scope)
		}
	}
}

// A GROUP NEEDS A GRANT AND A SAFE ANSWER ON EVERY QUESTION. Two model
// permissions with no answer marked safe are tabs, not one frame.
func TestAPermissionWithNoSafeAnswerIsATabNotAGroup(t *testing.T) {
	lab := newQuestionLab(t)
	for i := 1; i <= 2; i++ {
		q := setQuestion(uint64(i), "step:4", "read file "+itoa(i)+"?")
		q.Ask, q.Pick = session.AskPermission, nil
		lab.raise(q)
	}
	set := lab.a.questionSet()
	if len(set) != 2 || lab.a.questionGrouped(set) {
		t.Fatalf("set of %d grouped=%v, want two tabs", len(set), lab.a.questionGrouped(set))
	}
}
