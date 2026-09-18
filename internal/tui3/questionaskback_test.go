package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── ASKING BACK GOES THROUGH THE DOOR THAT KEEPS THE QUESTION OPEN ──────────
//
// #910 built the engine's half: an answer carrying nothing but
// [session.Answer.AskedBack] returns the parked `ask` call with the person's
// words and a lead saying the question is still on their screen and not to ask
// it again (tools_ask.go's `askedBackLead`), and [session.AnswerResolves] says
// so at both ends. Before this the surface had no way to reach it: `?` sent the
// sentence as an ordinary new message, which arrived at the model with none of
// that context and left the call parked behind it.
//
// AND THE LANES WITHOUT THE SEAM KEEP THE OLD ROAD. `AnswerResolves` RESOLVES a
// consent that carries only words asked back — approving it with no key — so
// routing every lane through the door would turn a question into an approval.
// That is why [app.askBack] asks the predicate rather than listing kinds.

// askLab is the model's own question, which is the lane that has the seam.
func askLab(t *testing.T) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 4401, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Head: "which store should it write to?", Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{
			{Key: "1", Label: "sqlite"},
			{Key: "2", Label: "postgres"},
		},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	return lab
}

func TestAskingBackOnTheModelsQuestionGoesThroughTheDoorAndNotTheBox(t *testing.T) {
	lab := askLab(t)
	head, ok := lab.a.questionHead()
	if !ok {
		t.Fatal("the lab raised nothing")
	}
	if cmd := lab.a.askBack(head, "", "what does postgres cost me here?"); cmd != nil {
		cmd()
	}
	if len(lab.answer) != 1 {
		t.Fatalf("asking back reached the engine's door %d times, want once: %+v", len(lab.answer), lab.answer)
	}
	got := lab.answer[0]
	if len(got.AskedBack) != 1 || got.AskedBack[0].Asked != "what does postgres cost me here?" {
		t.Fatalf("the person's words did not travel as an exchange: %+v", got.AskedBack)
	}
	if len(got.Keys()) != 0 || strings.TrimSpace(got.Words()) != "" {
		t.Fatalf("asking back carried an answer with it, so the engine would settle the question: %+v", got)
	}
	// AND THE ENGINE'S OWN READING AGREES that this leaves the question open,
	// which is the whole reason the door may be used at all.
	if session.AnswerResolves(got) {
		t.Fatal("the answer this sent would END the question — asking back is not answering")
	}
}

// AND THE SECTION THE READER IS STANDING ON TRAVELS WITH IT. The page knows
// which answer the question is about and the block does not; "" is a question
// about the question itself ([session.Exchange.Option]).
func TestAskingBackFromThePageSaysWhichAnswerItIsAbout(t *testing.T) {
	lab := askLab(t)
	head, _ := lab.a.questionHead()
	if cmd := lab.a.askBack(head, "2", "how long would the migration take?"); cmd != nil {
		cmd()
	}
	if len(lab.answer) != 1 {
		t.Fatalf("the page's ask-back reached the door %d times: %+v", len(lab.answer), lab.answer)
	}
	if got := lab.answer[0].AskedBack; len(got) != 1 || got[0].Option != "2" {
		t.Fatalf("the answer it was asked about did not travel: %+v", got)
	}
}

// AND A LANE WITHOUT THE SEAM IS NOT SENT THROUGH IT. A consent answered with
// nothing but words asked back is RESOLVED by that door — approved with no key
// — so it keeps the ordinary turn until its lane grows the same seam.
func TestAskingBackOnAConsentDoesNotAnswerIt(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle * 2)
	lab.rows()
	head, ok := lab.a.questionHead()
	if !ok {
		t.Fatal("the lab raised nothing")
	}
	if cmd := lab.a.askBack(head, "", "what is this command going to touch?"); cmd != nil {
		cmd()
	}
	if len(lab.answer) != 0 {
		t.Fatalf("asking back about a permission ANSWERED it: %+v", lab.answer)
	}
}

// ── THE CHIP COUNTS WHAT IS WAITING ON YOU ──────────────────────────────────
//
// #910 gave the engine one reading of "is somebody being waited for" —
// [session.Question.Waiting] — and the SHAPE half of it is
// [session.AskKind.Waits]: a ratify waits on nobody by definition, whatever else
// it carries. The status chip was counting rows instead, so a standing ratify
// put `? 1 question · alt+y` on every page and the key took the person to a
// screen with nothing on it for them to decide.

func TestTheChipDoesNotCountAQuestionNobodyIsWaitingOn(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 5501, Kind: session.QuestionAsk, Ask: session.AskRatify,
		Head: "I renamed the column to created_at", Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "fine"}},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	if n := lab.a.questionCount(); n != 0 {
		t.Fatalf("the chip counts %d, and a ratify waits on nobody (session.AskKind.Waits)", n)
	}
	if chip := plain(lab.a.questionSegment()); chip != "" {
		t.Fatalf("the chip is drawn over a question nobody is waiting on: %q", chip)
	}
	// AND THE BLOCK STILL HAS IT. What was settled is the STATUS ROW's claim
	// across every page, not whether the row exists — the ratify is still drawn
	// and still answerable where it is drawn.
	if !lab.a.questioning() {
		t.Fatal("the ratify left the block as well: it is not counted, it is not hidden")
	}
}

// AND ONE THAT IS WAITING IS STILL COUNTED BESIDE IT, which is the half that
// proves the first is a filter and not an off switch.
func TestTheChipCountsTheWaitingOneBesideTheRatify(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 5502, Kind: session.QuestionAsk, Ask: session.AskRatify,
		Head: "I renamed the column to created_at", Asker: session.Asker{Kind: session.AskerModel},
		Options: []session.AnswerOption{{Key: "1", Label: "fine"}},
	})
	lab.raise(consentAsk())
	lab.tick(questionSettle * 2)
	lab.rows()
	if n := lab.a.questionCount(); n != 1 {
		t.Fatalf("the chip counts %d with one ratify and one permission open, want just the permission", n)
	}
	// AND IT SAYS WHICH ONE, in that question's own words rather than as a
	// number (owner ruling 2026-09-11): with exactly one thing waiting, a count
	// is a fact somebody has to press a key to act on. The count above is what
	// proves the ratify was filtered; this proves the words are the right
	// question's.
	chip := plain(lab.a.questionSegment())
	if !strings.Contains(chip, "allow this?") {
		t.Fatalf("the chip does not name the one thing that IS waiting: %q", chip)
	}
	if strings.Contains(chip, "created_at") {
		t.Fatalf("the chip named the ratify nothing is waiting on: %q", chip)
	}
}
