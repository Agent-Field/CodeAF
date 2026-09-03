package tui3

// /manual — WHAT AFORGE KNOWS ABOUT ITSELF, READ RATHER THAN RETOLD.
//
// The manual (internal/manual's chat pages) had exactly one reader for its whole
// life and it was not the person: the only door onto it was the belt's `manual`
// tool, which is a model call. That means a key, a bill on every lookup, and —
// the part that actually costs something — a PARAPHRASE. What came back was the
// model's retelling of a page, and a retelling is indistinguishable from an
// invention right up until somebody acts on it, which is the exact failure the
// pages were written to prevent.
//
// So this command hands over the writing itself. It reaches the same corpus the
// tool reaches and prints it AS WRITTEN, with the page and heading over every
// piece, so what is on the screen can be traced back to the page that authorized
// it. It makes no model call and spends nothing: the pages are inside the binary
// and reading them is a lookup, not a turn.
//
// THE COMMAND LINE HAS THE SAME DOOR (cmd/aforge's manual.go) and it is not a
// duplicate of this one — it is the door for the questions people ask BEFORE
// there is a key to open a conversation with.

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// manualChatSections is how many sections a question typed here is answered
// from. It is the number the belt tool hands a model ([manual.DefaultResults])
// doubled, for the reason the terminal door uses a bigger one: that four is a
// context budget, and a person reading their own manual is not on one.
const manualChatSections = 2 * manual.DefaultResults

// runManualCommand is /manual: the pages there are, one page, or the sections
// that answer a question.
//
// ONE WORD IS A NAME AND MORE THAN ONE IS A QUESTION. A name is an exact request
// and gets an exact answer or an exact refusal — never a near miss shown as
// though it had been asked for, which would read as though the page existed —
// and the refusal prints the pages that do exist, because somebody one letter
// away from the name they wanted should not have to guess at it twice.
// THE LISTING IS A BLOCK AND NOT PROSE, and that is the whole of row 7 of the
// polish audit. [manual.Corpus.Listing] builds one line per page — the name a
// person types, then the title the page gives itself — and an ordinary note
// RE-FLOWS its text to the frame ([app.note], render.go's [wrap]). So a title
// longer than the room left after the name column wrapped, and its last word
// landed on the next line flush at the column the page NAMES are in: the first
// list of pages anybody ever sees had a page called `later` on it, and typing
// `/manual later` then answered "there is no manual page named later". A
// listing that shows a page that does not exist is worse than one that shows
// fewer pages.
//
// [app.noteBlock] is the door for exactly this shape — a note whose LINE
// STRUCTURE IS ITS MEANING — and it cuts a line too wide rather than re-flowing
// it, so a long title now ends in an ellipsis on its own row and the name
// column is the only thing at the margin.
func (a *app) runManualCommand(rest string) {
	asked := strings.TrimSpace(rest)
	switch {
	case asked == "":
		a.noteBlock(manual.Chat().Listing())
	case strings.ContainsAny(asked, " \t"):
		a.runManualQuestion(asked)
	default:
		text, found := manual.Chat().Page(asked)
		if !found {
			a.noteBlock("there is no manual page named " + asked + "\n\n" + manual.Chat().Listing())
			return
		}
		a.note(text)
	}
}

// runManualQuestion answers in the person's own words, out of every page at
// once, so nobody has to know which page a thing is written on before they can
// ask about it.
func (a *app) runManualQuestion(question string) {
	sections := manual.Chat().Search(question, manualChatSections)
	if len(sections) == 0 {
		// NOT A REFUSAL. The manual having nothing on a topic is a fact about
		// aforge worth saying — it usually means the answer is "no, it does not
		// do that" — and the pages go under it so the next question is one
		// keystroke away rather than a guess.
		// AND THE LISTING UNDER IT IS A BLOCK for [app.runManualCommand]'s reason
		// exactly: one page per line, cut rather than re-flowed.
		a.noteBlock("the manual has nothing on that, which usually means aforge does not do it\n\n" + manual.Chat().Listing())
		return
	}
	a.note(manual.RenderWhole(sections))
}
