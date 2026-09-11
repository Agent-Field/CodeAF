package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── WHICH DRAWING A QUESTION GETS ───────────────────────────────────────────
//
// ONE FUNCTION DECIDES, AND IT ASKS THE QUESTION'S OWN PROPERTIES.
//
// It used to be four decisions in four places, each keyed on a different thing:
// [app.questionForm] read the asker's `Form` word and the count of its options,
// [app.questionNarrowed] read the width, the sheet drew itself whenever it had
// rows, and home and the errand pane each called a renderer by name. A person
// could not predict which shape a question would take, and neither could a lane.
//
// So the ladder below is the whole of it, it is DATA rather than a chain of
// names, and every rung is a property of the question or of the frame — never
// its [session.QuestionKind]. A kind is where a question came from; what decides
// how much room it needs is what it CARRIES. The first rung that says yes wins,
// and each one states why it is above the next.

// questionView is which drawing a question gets.
type questionView uint8

const (
	// viewNone is a question that is not on this screen at all: the status
	// line's chip is carrying it, and the chip says its words.
	viewNone questionView = iota
	// viewPhone is the bottom sheet a phone-width frame gets
	// (questionnarrow.go).
	viewPhone
	// viewTabs is two or more questions from one step, drawn as one panel with
	// a tab per question and a review tab, or as one permission frame where
	// every one of them is a permission (questionset.go).
	viewTabs
	// viewPanel is the framed panel a question hangs in above the box
	// (questionpanel.go). It is the ordinary answer.
	viewPanel
	// viewSplit is the same panel with its answers in a list on the left and
	// the evidence of the one the pointer is on in a pane on the right, which
	// follows the pointer (owner ruling 2026-09-11, preview pick A).
	viewSplit
	// viewRow is one row: a question whose whole decision fits beside its own
	// answers, and the ratify line, which is not asking anything.
	viewRow
)

// questionRung is one rung of the ladder: the view it gives, why it is where it
// is, and the property that decides it.
type questionRung struct {
	view questionView
	// why is the rung's own reason, in the prose the rest of this surface is
	// written in. It is read by [TestTheChooserIsOneLadderOfProperties] and by
	// whoever adds the next rung.
	why  string
	when func(a *app, q questionShown, width int) bool
}

// questionLadder is the ladder, top first.
var questionLadder = []questionRung{
	{
		view: viewNone,
		why: "nothing of this window is showing the question — a page has the whole frame, " +
			"or the box holds a half-typed sentence the block may not move — so the chip carries it",
		when: func(a *app, q questionShown, width int) bool {
			return a.questionOffFrame() || (q.shown.IsZero() && !a.questionQuieted())
		},
	},
	{
		view: viewPhone,
		why: "a frame this narrow has no columns to put an answer beside its consequence, and no " +
			"keyboard to press a digit with: every answer becomes a band a thumb can land on",
		when: func(a *app, q questionShown, width int) bool {
			return a.questionNarrowed(q, width)
		},
	},
	{
		view: viewTabs,
		why: "two or more questions from one step are one decision taken in parts, so they are one " +
			"panel with a tab each — and where every one of them is a permission, one frame that " +
			"answers all of them at once",
		when: func(a *app, q questionShown, width int) bool {
			return a.questionTabbed(q)
		},
	},
	{
		view: viewRow,
		why: "a ratify line is a statement about something already done and nothing waits on it, " +
			"so it is one row whatever it carries",
		when: func(a *app, q questionShown, width int) bool {
			return q.question.Ask == session.AskRatify
		},
	},
	{
		view: viewPanel,
		why: "the answer is a shape to work rather than a word to pick — blanks, a checklist, a " +
			"dial, a sentence — and a shape needs rows",
		when: func(a *app, q questionShown, width int) bool {
			return q.question.Input.Kind != session.InputNone
		},
	},
	{
		view: viewSplit,
		why: "an answer brought something to look at and the frame is wide enough to lay it beside " +
			"the list, so the evidence follows the pointer in a pane of its own rather than being " +
			"folded under one row",
		when: func(a *app, q questionShown, width int) bool {
			return questionAnswersCarryBlocks(q.question) && besideFits(width)
		},
	},
	{
		view: viewPanel,
		why: "an answer brought something to look at, and evidence sets the size of the drawing — " +
			"narrower than two panes, the answer the pointer is on unfolds it under its own row",
		when: func(a *app, q questionShown, width int) bool {
			return questionCarriesBlocks(q.question)
		},
	},
	{
		view: viewPanel,
		why: "there is more to weigh than the answers' own words — what one costs, what it means, " +
			"why the asker would take one, or a command a person has to read before allowing it",
		when: func(a *app, q questionShown, width int) bool {
			return questionCarriesWeight(q.question)
		},
	},
	{
		view: viewPanel,
		why: "nobody but a person may answer this, so it is drawn as the object it is rather than " +
			"as a line in the chrome",
		when: func(a *app, q questionShown, width int) bool {
			return questionHandsOnly(q.question)
		},
	},
	{
		view: viewRow,
		why:  "everything left is a question whose whole decision is its answers' own words",
		when: func(a *app, q questionShown, width int) bool { return true },
	},
}

// questionViewOf is the ladder walked.
func (a *app) questionViewOf(q questionShown, width int) questionView {
	for _, rung := range questionLadder {
		if rung.when(a, q, width) {
			return rung.view
		}
	}
	return viewRow
}

// questionCarriesBlocks reports whether any answer brought something to look
// at, or the question itself did.
func questionCarriesBlocks(q session.Question) bool {
	return len(q.Attach) > 0 || questionAnswersCarryBlocks(q)
}

// questionAnswersCarryBlocks reports whether any ANSWER brought something to
// look at — the evidence that is different from one answer to the next, which is
// the only evidence a pane following the pointer has anything to show about.
// The question's own [session.Question.Attach] is about the whole decision and
// is the page's to draw ("what it showed you").
func questionAnswersCarryBlocks(q session.Question) bool {
	for _, option := range q.Options {
		if len(option.Blocks) > 0 {
			return true
		}
	}
	return false
}

// questionCarriesWeight reports whether there is anything to weigh beyond the
// answers' own words: what taking one produces, what one means, a pick the asker
// gave a reason for, or a call the question is about.
//
// THE CALL IS ONE OF THEM, and it is the permission's whole case: a person
// allowing a command has to READ the command, so it stands on the panel's first
// row in the payload hue with what it touches beside it (owner ruling
// 2026-09-11, consent pick B).
func questionCarriesWeight(q session.Question) bool {
	if q.Subject.Kind == session.SubjectCall {
		return true
	}
	if q.Pick != nil && strings.TrimSpace(q.Pick.Reason) != "" {
		return true
	}
	for _, option := range q.Options {
		if strings.TrimSpace(option.Consequence) != "" || strings.TrimSpace(option.Body) != "" {
			return true
		}
	}
	return false
}

// questionForm is which key set a question's drawing offers, which is the view
// said in the key table's own words ([questionKeys]).
//
// FORMS PROMOTE AND NEVER DEMOTE was a law about the ASKER'S wish, and it is
// kept where it belongs: the ladder above reads what the question carries, and
// an asker that wrote `form: card` on something with nothing to weigh gets the
// row its evidence asks for. What the asker's wish still does is promote — a
// `room` is a page that opens on `o`, never a drawing that arrives.
func (a *app) questionForm(q session.Question) questionForms {
	if q.Ask == session.AskRatify {
		return formsRatify
	}
	width, _ := a.size()
	if a.questionViewOf(questionShown{question: q, shown: a.now()}, width) == viewRow {
		return formsLine
	}
	return formsCard
}
