package tui3

// /manual — A QUESTION ABOUT codeaf, PUT TO THE MODEL WITH THE MANUAL OPEN.
//
// The manual (internal/manual's chat pages) is what the model reads to answer
// anything about codeaf itself — the belt's `manual` tool, which the system
// prompt sends every such question to. This command is the person's door onto
// that same reading: the words after `/manual` go to the model as a turn, told
// to answer out of the manual and to say which page the answer came from, and
// the answer lands in the conversation the way every other answer does.
//
// IT USED TO PRINT THE PAGES AS WRITTEN, with no model call, on the argument
// that a retelling is indistinguishable from an invention until somebody acts
// on it. That door was replaced on 2026-09-22: a person who typed `/manual how
// do I change the effort level` on home saw nothing at all, because the printed
// note landed in the conversation BEHIND home, and what they expected was the
// chosen model's answer in a conversation. The as-written reading lives on
// where it is most wanted — the command line's `codeaf manual` (cmd/codeaf's
// manual.go), for the questions people ask before there is a key to open a
// conversation with.
//
// ON HOME THE COMMAND OPENS A CONVERSATION FIRST (homeslash.go's
// [fateNeedsChat]): the folder and the model on the rule above the box, home
// closing behind you, and the question sent there. In a conversation it is a
// turn of that conversation.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The two sentences the model is handed. The person's own line in the
// transcript is what they typed — `/manual` or `/manual <question>` — and these
// are the words behind it ([app.submitShown] keeps the two apart). Both name
// the tool so the answer is read out of the pages rather than remembered from
// somewhere else, and both ask for the page, so the person can go on to read it.
const (
	// manualTourAsk is a bare /manual: what codeaf can do, from its own account.
	manualTourAsk = "What can codeaf do? Answer from codeaf's own manual — the manual tool — and name the pages worth reading first."
	// manualQuestionLead is put in front of a question typed after the word.
	manualQuestionLead = "Answer from codeaf's own manual — the manual tool — and say which page it came from: "
)

// runManualCommand is /manual: the question, or the tour, sent to the model as
// a turn of this conversation.
func (a *app) runManualCommand(rest string) tea.Cmd {
	asked := strings.TrimSpace(rest)
	if asked == "" {
		return a.submitShown(manualTourAsk, "/manual")
	}
	return a.submitShown(manualQuestionLead+asked, "/manual "+asked)
}
