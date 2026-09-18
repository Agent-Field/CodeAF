package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// AND THEN SOMEBODY LOOKS AT IT.
//
// A screen nobody has looked at is not done. This test draws each form the
// block has through the REAL renderer at the real palette — truecolor, the
// nerd-font tier off so the marks are the geometric floor every terminal draws
// — and writes the escapes to `screens/<case>.ansi`, which is rendered to a PNG
// and read by a person before the change lands.
//
// IT IS THE RENDERER AND NOT A MOCK OF IT. Every row below comes out of
// [app.questionRows] with the palette the surface paints with, so what is
// looked at is what is drawn: a harness that re-implemented the layout to make
// a picture would be a picture of a program that does not exist.
//
// It writes nothing unless CODEAF_SCREENS names a directory, so the ordinary
// suite runs it as a drawing check — every case must produce rows — and the
// screens are taken on purpose.
func TestQuestionScreens(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("CODEAF_SCREENS"))
	at := time.Date(2026, time.September, 9, 14, 2, 0, 0, time.UTC)

	shot := func(name string, build func(*questionLab)) {
		t.Helper()
		lab := newQuestionLab(t)
		lab.at = at
		// THE SCREEN'S OWN TERMINAL: truecolor, so the hues are the ones a
		// person sees rather than the 256-colour approximation the suite pins,
		// and the plain glyph floor, so the marks are the shapes every terminal
		// can draw.
		lab.a.pal = newPalette(tokens.TrueColor, false)
		lab.a.actionAuto = tokens.Plain
		lab.a.settleIcons()
		lab.a.width, lab.a.height = 120, 14
		build(lab)
		// `held` is the ONE case whose whole point is that the block draws
		// nothing: a question waiting behind a half-typed sentence, counted in
		// the status line and nowhere else.
		drew := len(lab.rows()) > 0
		if lab.a.questionRoomOpen() {
			drew = len(lab.a.questionRoomRows(lab.a.width)) > 0
		}
		if !drew && name != "held" {
			t.Fatalf("%s drew nothing", name)
		}
		if dir == "" {
			return
		}
		// THE WHOLE FRAME AND NOT THE BLOCK ALONE. What is being looked at is
		// where the block SITS — pinned above the box, under the conversation,
		// over the status row — and a picture of the rows on their own would
		// answer none of the questions a person looks at a screen to answer.
		body, _, _ := lab.a.frame()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	shot("pending", func(l *questionLab) {
		l.raise(consentAsk())
	})

	shot("card", func(l *questionLab) {
		l.raise(session.Question{
			ID: 11, Kind: session.QuestionTask, Ask: session.AskPermission, Form: session.FormCard,
			Asker:  session.Asker{Kind: session.AskerModel},
			Head:   "wants to start a task: rewrite the packer",
			Reason: "it will run on its own branch and open a pull request",
			Options: []session.AnswerOption{
				{Key: "1", Label: "start it", Consequence: "on a branch of its own"},
				{Key: "2", Label: "not now", Safe: true, Consequence: "nothing runs"},
				{Key: "3", Label: "change it first", Consequence: "the card comes back"},
			},
			Pick:     &session.Pick{Key: "1", Reason: "it starts on its own unless you say otherwise"},
			Policy:   session.Policy{Kind: session.PolicyRecommendThenAuto, After: 9 * time.Second},
			Deadline: l.at.Add(9 * time.Second),
			Stakes:   session.StakesCostly,
		})
	})

	// THE MODEL SHORTLIST, WHICH IS A HOLE IN A SENTENCE. It is the same card
	// with one row more: a proposal whose `model` argument fitted two models
	// this install has ([session.TaskModelShape]), drawn with `←→` on the offer
	// row and nothing about it on the digits.
	shot("card-model", func(l *questionLab) {
		options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
		l.raise(session.Question{
			ID: 12, Kind: session.QuestionTask, Ask: session.AskPermission, Form: session.FormCard,
			Asker:  session.Asker{Kind: session.AskerModel},
			Head:   session.TaskProposalLead + "rewrite the packer",
			Reason: "it will run on its own branch and open a pull request",
			Options: []session.AnswerOption{
				{Key: "1", Label: "start it", Consequence: "on a branch of its own"},
				{Key: "2", Label: "no", Safe: true, Consequence: "nothing runs"},
			},
			Input:    session.TaskModelShape(session.TaskNotice{Model: options[0], ModelOptions: options}),
			Pick:     &session.Pick{Key: "1", Reason: session.TaskProposalPickReason},
			Policy:   session.Policy{Kind: session.PolicyRecommendThenAuto, After: 9 * time.Second},
			Deadline: l.at.Add(9 * time.Second),
			Stakes:   session.StakesCostly,
		})
	})

	// AND THE SAME CARD AFTER `→`, so what the key DOES is on a screen rather
	// than only in a sentence about it.
	shot("card-model-moved", func(l *questionLab) {
		options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
		l.raise(session.Question{
			ID: 12, Kind: session.QuestionTask, Ask: session.AskPermission, Form: session.FormCard,
			Asker:  session.Asker{Kind: session.AskerModel},
			Head:   session.TaskProposalLead + "rewrite the packer",
			Reason: "it will run on its own branch and open a pull request",
			Options: []session.AnswerOption{
				{Key: "1", Label: "start it", Consequence: "on a branch of its own"},
				{Key: "2", Label: "no", Safe: true, Consequence: "nothing runs"},
			},
			Input:    session.TaskModelShape(session.TaskNotice{Model: options[0], ModelOptions: options}),
			Pick:     &session.Pick{Key: "1", Reason: session.TaskProposalPickReason},
			Policy:   session.Policy{Kind: session.PolicyRecommendThenAuto, After: 9 * time.Second},
			Deadline: l.at.Add(9 * time.Second),
			Stakes:   session.StakesCostly,
		})
		l.tick(questionSettle)
		l.press("right")
	})

	// TYPING IS ANSWERING: the block up, words in the box under it, and `enter`
	// about to send them as the answer rather than as a message.
	shot("typing", func(l *questionLab) {
		l.raise(consentAsk())
		l.a.input.setText("no, because it would take the fixtures with it")
		l.a.questionTyped = l.at.Add(-questionQuiet)
		l.tick(questionSettle)
	})

	// AND THE BOX IS NEVER MOVED UNDER A HAND: the same question arriving on
	// top of a half-typed sentence, holding, with the chip counting it.
	shot("held", func(l *questionLab) {
		l.a.input.setText("half a sentence somebody is still writ")
		l.a.questionTyped = l.at
		l.raise(consentAsk())
	})

	shot("queued", func(l *questionLab) {
		for i := 0; i < 3; i++ {
			ask := consentAsk()
			ask.ID = uint64(20 + i)
			ask.Asked = l.at.Add(time.Duration(i) * time.Second)
			l.raise(ask)
		}
	})

	shot("receipt", func(l *questionLab) {
		l.raise(consentAsk())
		l.tick(questionSettle)
		l.rows()
		l.press("1")
		second := consentAsk()
		second.ID = 8
		second.Head = "allow this too?"
		second.Reason = `bash pattern "git push*"`
		l.raise(second)
	})

	shot("ratify", func(l *questionLab) {
		l.a.raiseQuestion(questionShown{
			question: session.Question{
				ID: 5, Kind: session.QuestionTask, Ask: session.AskRatify,
				Asker: session.Asker{Kind: session.AskerModel},
				Head:  "renamed 12 files under src/ · nothing else changed",
				Options: []session.AnswerOption{
					{Key: "1", Label: "put them back", Safe: true},
					{Key: "2", Label: "already done"},
				},
				Stakes: session.StakesReversible, Asked: l.at,
			},
			undoable: true,
			local:    func(session.Answer) tea.Cmd { return nil },
		})
	})

	shot("withdrawn", func(l *questionLab) {
		ask := consentAsk()
		l.raise(ask)
		gone := ask
		gone.Withdrawn = &session.Withdrawal{Reason: "the turn moved on without it", At: l.at}
		l.a.withdrawQuestion(gone, gone.Withdrawn.Reason)
	})

	shot("rule", func(l *questionLab) {
		l.a.questionYeses = map[string]int{
			questionShape(session.Question{
				Kind: session.QuestionConsent, Ask: session.AskPermission,
				Subject: session.SubjectRef{Name: "bash"},
			}): questionRuleAfter - 1,
		}
		ask := consentAsk()
		ask.Subject = session.SubjectRef{Kind: session.SubjectCall, Name: "bash"}
		l.raise(ask)
	})

	shot("stop", func(l *questionLab) {
		l.a.raiseQuestion(questionShown{
			question: session.Question{
				ID: 3, Kind: session.QuestionTask, Ask: session.AskConfirmation, Form: session.FormCard,
				Asker:  session.Asker{Kind: session.AskerSurface},
				Head:   "Stop this run?",
				Reason: "In-flight nodes halt; partial results stay.",
				Options: []session.AnswerOption{
					{Key: "1", Label: "stop it", Consequence: "the nodes in flight are cut"},
					{Key: "2", Label: "keep going", Safe: true, Consequence: "nothing changes"},
				},
				Stakes: session.StakesIrreversible, Asked: l.at,
			},
			local: func(session.Answer) tea.Cmd { return nil },
		})
		l.rows()
	})

	// THE EVIDENCE, AT THE THREE WIDTHS THE OWNER PICKED FOR (2026-09-11).
	// Preview A beside the list at a hundred columns and wider, preview-narrow B
	// unfolded under the pointer below that, and page A — the page `o` opens —
	// as two panes where they fit and one column where they do not. Each is the
	// same answer's evidence through the same renderer, so the six pictures are
	// one account of it drawn at three widths.
	for _, width := range []int{120, 96, 56} {
		shot("evidence-"+itoa(width), func(l *questionLab) {
			l.a.width, l.a.height = width, 40
			l.raise(demoQuestionReading())
			l.tick(questionSettle)
		})
		shot("page-"+itoa(width), func(l *questionLab) {
			l.a.width, l.a.height = width, 40
			// THE COLUMN STOWED (`ctrl+g`), so the page has the frame's width
			// to split: the page is drawn in the body, beside the task column
			// where one stands, and `page-120-column` is the same page with it up.
			l.a.railAway = true
			l.raise(demoQuestionReading())
			l.tick(questionSettle)
			l.press("o")
		})
	}
	// THE PAGE BESIDE THE TASK COLUMN. A hundred and twenty columns less the
	// column's own is a body under the width two panes need, so the page draws
	// its one column — the evidence unfolded under the pointer — rather than
	// two panes too narrow to read.
	shot("page-120-column", func(l *questionLab) {
		l.a.width, l.a.height = 120, 40
		l.raise(demoQuestionReading())
		l.tick(questionSettle)
		l.press("o")
	})
	// AND THE SPLIT PAGE BESIDE THE TASK COLUMN, which is the one combination
	// every other shot here stowed the column to avoid. A hundred and forty
	// columns less the column's own leaves a body still over [railSlimFloor], so
	// the page splits AND the rail stands — and that is exactly where the page's
	// two rules were drawn in different frames: the top rule in the body, the
	// foot's rule in the full terminal, running on under the rail. A picture of
	// it is the only thing that would have shown it, so there is one now.
	shot("page-140-column", func(l *questionLab) {
		l.a.width, l.a.height = 140, 40
		l.raise(demoQuestionReading())
		l.tick(questionSettle)
		l.press("o")
	})
	// AND A LAYOUT BLOCK ON THE PAGE, whose two panes stand side by side when
	// each one's widest line fits its half and stack when one does not.
	shot("page-layout-120", func(l *questionLab) {
		l.a.width, l.a.height = 120, 40
		l.a.railAway = true
		l.raise(demoQuestionLayout())
		l.tick(questionSettle)
		l.press("o")
	})
}

// TestTheChipIsDrawnOnTheStatusRowWhereEveryPageCanSeeIt is the folded case's
// screen, taken off the row the frame actually assembles rather than off the
// segment alone: the chip is only worth having if it survives the width ladder.
func TestTheChipIsDrawnOnTheStatusRowWhereEveryPageCanSeeIt(t *testing.T) {
	lab := newQuestionLab(t)
	// A live conversation has a model; an unlabelled seam deliberately stays bare.
	lab.a.model = "test/model"
	lab.a.pal = newPalette(tokens.TrueColor, false)
	lab.raise(consentAsk())
	lab.tick(questionSettle)
	lab.rows()
	lab.press("esc")
	got := plain(lab.a.legend(lab.a.width))
	// ONE QUESTION IS SAID IN ITS OWN WORDS, and only a queue is counted. The
	// chip rides the seam with the rest of the numbers (footswap.go).
	if !strings.Contains(got, "allow this? · "+questionChipKey) {
		t.Fatalf("the chip is not on the seam:\n%s", got)
	}
	if dir := strings.TrimSpace(os.Getenv("CODEAF_SCREENS")); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		lab.a.height = 14
		body, _, _ := lab.a.frame()
		if err := os.WriteFile(filepath.Join(dir, "chip.ansi"), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
