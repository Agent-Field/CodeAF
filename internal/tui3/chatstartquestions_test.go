package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── QUESTIONS BEHIND THE NEW-CHAT START PAGE ───────────────────────────────
//
// The start page is a composer over a conversation, not another view of that
// conversation's answer lane. These tests hold the rule from both sides: every
// key on the page belongs to the first message, and the unified question block
// stays with the chat behind it until somebody goes back there to answer it.

// startPageQuestion is one lane that can be waiting in the chat under the page.
// The shapes come from the engine's own builders where those are public; the
// remaining two carry the same fields their builders write in question.go.
type startPageQuestion struct {
	name string
	q    session.Question
}

func startPageQuestions() []startPageQuestion {
	return []startPageQuestion{
		{name: "approval", q: consentAsk()},
		{name: "task proposal", q: session.Question{
			ID: 8, Kind: session.QuestionTask, Ask: session.AskPermission,
			Form: session.FormCard, Asker: session.Asker{Kind: session.AskerModel},
			Head: "start Fix the nil-map crash?", Reason: "the parser needs it",
			Options:  session.AnswerOptions(session.QuestionTask),
			Stakes:   session.StakesCostly,
			Blocking: session.Blocking{Turn: true},
		}},
		{name: "standing order", q: session.Question{
			ID: 9, Kind: session.QuestionStanding, Ask: session.AskChoice,
			Form: session.FormCard, Asker: session.Asker{Kind: session.AskerEngine},
			Head: "keep running the checks?", Reason: session.StandingAskReason,
			Options: session.AnswerOptions(session.QuestionStanding),
			Stakes:  session.StakesReversible,
		}},
		{name: "connect offer", q: session.ConnectQuestion("notion", "Notion", false, "", false)},
		{name: "harness offer", q: session.HarnessQuestion(10, session.Event{
			Text: "research", Hint: "finds an answer across sources",
		})},
	}
}

// questionUnderStart raises and draws one question, then wires the otherwise
// unrelated door that makes the New chat page reachable in a test window.
func questionUnderStart(t *testing.T, q session.Question) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	lab.a.file, lab.a.title = "/tmp/lab/one.jsonl", "Shipping the parser"
	lab.raise(q)
	if lab.a.questionCount() != 1 || len(lab.rows()) == 0 {
		t.Fatal("the fixture did not draw its question")
	}
	made := 0
	startDoor(lab.a, &made)
	emptyMachine(lab.a)
	return lab
}

// THE EXACT REPRO. Answer digits, the old answer letters and the words around
// them all belong to the first sentence. None may resolve or postpone the
// question hidden with its conversation.
func TestTheStartPageGetsEveryCharacterOverEveryQuestion(t *testing.T) {
	for _, test := range startPageQuestions() {
		t.Run(test.name, func(t *testing.T) {
			lab := questionUnderStart(t, test.q)
			drive(t, lab.a, key(newChatChord))
			typeInto(t, lab.a, "123 ynatd hello there")

			if got := lab.a.input.String(); got != "123 ynatd hello there" {
				t.Fatalf("the start page kept %q, want the whole first sentence", got)
			}
			if len(lab.answer) != 0 {
				t.Fatalf("typing on the start page answered the session: %+v", lab.answer)
			}
			if lab.a.questionCount() != 1 {
				t.Fatalf("the chat kept %d questions, want the unanswered one", lab.a.questionCount())
			}
		})
	}
}

// A QUESTION IS DRAWN WHERE IT CAN BE ANSWERED AND NOWHERE ELSE. The unified
// block takes no rows from the start page, and the frame says none of its head
// or reason there.
func TestNoQuestionIsDrawnOnTheStartPage(t *testing.T) {
	for _, test := range startPageQuestions() {
		t.Run(test.name, func(t *testing.T) {
			lab := questionUnderStart(t, test.q)
			drive(t, lab.a, key(newChatChord))

			if rows := lab.a.questionRows(lab.a.width); len(rows) != 0 {
				t.Fatalf("the start page drew question rows: %q", rows)
			}
			if got := lab.a.questionHeight(); got != 0 {
				t.Fatalf("the hidden question costs %d rows, want zero", got)
			}
			got := plain(frame(lab.a))
			for _, absent := range []string{test.q.Head, test.q.Reason} {
				if absent != "" && strings.Contains(got, absent) {
					t.Fatalf("the start page drew %q:\n%s", absent, got)
				}
			}
		})
	}
}

// THE POINTER MAP MOVES WITH THE DRAW. A target is first laid out in the
// conversation, then covered by the page; a press where it used to be must not
// answer through spans left from the previous frame.
func TestAPressWhereAHiddenQuestionUsedToBeAnswersNothing(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	_ = frame(lab.a)
	// A PERMISSION IS THE PANEL, so its targets are BANDS — a whole row each,
	// counted from the top of the block (questionpanel.go). It was a row of
	// spans when this test was written, and the fact being held is the same
	// either way: the map moves with the draw.
	if len(lab.a.questionBands) == 0 {
		t.Fatal("the visible question wrote no answer targets")
	}
	band := lab.a.questionBands[0]
	x := band.span.from + 1
	y := chromeRowY(t, lab.a, band.row)

	drive(t, lab.a, key(newChatChord))
	_ = frame(lab.a)
	// The press hands back the command an answer travels on now (offloop.go), so
	// what is read is whether it was TAKEN.
	if _, took := lab.a.questionPress(x, y); took {
		t.Fatal("a press where the hidden question used to be was taken")
	}
	if len(lab.answer) != 0 || lab.a.questionCount() != 1 {
		t.Fatalf("the stale press left answers=%+v questions=%d", lab.answer, lab.a.questionCount())
	}
}

// ESC BELONGS TO THE PAGE TOO. It returns the exact draft of the conversation
// underneath and leaves the question standing and unanswered there.
func TestEscapeFromTheStartPageReturnsToTheUnansweredQuestion(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	lab.a.input.setText("the chat's own draft")
	drive(t, lab.a, key(newChatChord))
	typeInto(t, lab.a, "new conversation draft")
	drive(t, lab.a, key("esc"))

	if lab.a.startingChat() {
		t.Fatal("esc left the start page open")
	}
	if got := lab.a.input.String(); got != "the chat's own draft" {
		t.Fatalf("esc returned the conversation draft as %q", got)
	}
	if len(lab.answer) != 0 || lab.a.questionCount() != 1 {
		t.Fatalf("esc left answers=%+v questions=%d", lab.answer, lab.a.questionCount())
	}
	if got := plain(frame(lab.a)); !strings.Contains(got, "allow this?") {
		t.Fatalf("the question did not return with its chat:\n%s", got)
	}
}

// THE CHAT STILL SAYS IT NEEDS SOMEBODY. Covering the block changes neither
// the tab's signal nor the question count.
func TestTheAskingChatKeepsItsMarkUnderTheStartPage(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	drive(t, lab.a, key(newChatChord))

	if got := lab.a.frontSignal(); got != tabNeedsPerson {
		t.Fatalf("the asking chat's tab signal is %v, want the question mark", got)
	}
	if lab.a.questionCount() != 1 {
		t.Fatalf("the page left %d questions, want one", lab.a.questionCount())
	}
}

// THE READING CLOCK NEVER ANSWERS FOR ANYBODY. It may run down while its chat
// is behind the page, but expiry only pauses it; returning finds the same open
// question with that state said on its row.
func TestAQuestionBehindTheStartPagePausesAtExpiryAndNeverAnswers(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	lab.a.questions[0].clockAt = lab.at
	lab.a.questions[0].clockFor = 10 * time.Second
	drive(t, lab.a, key(newChatChord))

	lab.tick(11 * time.Second)
	drive(t, lab.a, frameMsg{})
	if len(lab.answer) != 0 || lab.a.questionCount() != 1 {
		t.Fatalf("expiry left answers=%+v questions=%d", lab.answer, lab.a.questionCount())
	}
	if !lab.a.questions[0].clockHeld {
		t.Fatal("expiry did not pause the hidden question")
	}

	drive(t, lab.a, key("esc"))
	if got := plain(frame(lab.a)); !strings.Contains(got, "paused") {
		t.Fatalf("the question did not return paused:\n%s", got)
	}
}

// THE CONTROL. A numbered answer still belongs to a visible, settled question
// inside its conversation; the start-page guard must not disable that road.
func TestAVisibleQuestionStillAnswersItsNumber(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	drive(t, lab.a, key("1"))

	if len(lab.answer) != 1 || lab.answer[0].FirstKey() != "1" {
		t.Fatalf("the visible question answered as %+v", lab.answer)
	}
}

// A WAITING PERMISSION MUST NOT MAKE `ctrl+t` A DEAD KEY, and on a short
// terminal it did — silently, which is the worst of the three things a key can
// do (lane T found it merging this branch with lane F's).
//
// [app.chromeHeight] charges the body region for [app.questionHeight], and a
// permission hangs in a frame now: eleven rows where it used to be one. At sixty
// by twenty that took the whole body, so [app.openChatStart] read
// `viewHeight() <= 0` and returned nothing at all. The measurement was of a
// frame that never exists — the block is NOT drawn while the start page is up
// ([app.questionRows] returns nothing on `startingChat`), so those rows come
// back to the body the moment the page opens.
func TestAWaitingPermissionDoesNotMakeTheStartPageRefuseSilently(t *testing.T) {
	lab := questionUnderStart(t, consentAsk())
	// SIXTY BY SIXTEEN IS THE REPRO ON THIS BRANCH: the panel is eleven rows and
	// the chrome fifteen, so the body region with the block on it is exactly
	// nothing. THE PRECONDITION IS ASSERTED AND NEVER SKIPPED — a test that
	// stepped over its own repro would go green without running it, which is the
	// shape of #576 said about one test instead of a suite.
	lab.a.width, lab.a.height = 60, 16
	if got := len(lab.rows()); got < 8 {
		t.Fatalf("the fixture's permission is %d rows; this test is about a tall one", got)
	}
	if got := lab.a.viewHeight(); got > 0 {
		t.Fatalf("the repro needs a frame whose body the block takes whole; this one has %d rows left", got)
	}
	drive(t, lab.a, key(newChatChord))
	if !lab.a.startingChat() {
		t.Fatal("ctrl+t did nothing and said nothing over a waiting permission")
	}
	// AND THE PAGE HAS THE ROWS IT MEASURED FOR: the block is gone while it is
	// up, so the body it was promised is the body it got.
	if got := lab.a.viewHeight(); got <= 0 {
		t.Fatalf("the start page opened onto a body of %d rows", got)
	}
}
