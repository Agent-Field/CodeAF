package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/decide"
)

// The receipt aside is the transcript's record that the app answered a
// question for the person. Each answer queues one note carrying its receipt in
// the note facts; the drain folds every receipt note of one batch into a single
// note, so three things done in one turn read "Did 3 things" and not three
// rows. The facts are journaled with the note, so a reopened conversation
// redraws exactly what the live one drew ([noteFacts]).

// queueDecisionReceipt puts one receipt on the steering queue. It never wakes a
// turn: an answer the app already gave is owed no sentence from the model.
func (a *Agent) queueDecisionReceipt(r decide.Receipt) {
	if a == nil || strings.TrimSpace(r.Text) == "" {
		return
	}
	note := decisionNote([]decide.Receipt{r})
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.steering = append(a.steering, note)
}

// decisionNote is the note for these receipts. Its text is for the model, which
// should know the person was not asked; the surface draws the aside instead.
func decisionNote(rs []decide.Receipt) userMessage {
	lines := make([]string, 0, len(rs))
	for _, r := range rs {
		lines = append(lines, strings.TrimSuffix(r.Text, " · Why?"))
	}
	note := userText(strings.Join(lines, "\n"))
	note.authored = true
	note.facts = noteFacts{Kind: NoteKindDecision, Receipts: append([]decide.Receipt(nil), rs...)}
	return note
}

// isDecisionNote reports whether a queued note is only a receipt.
func isDecisionNote(n userMessage) bool {
	return n.facts.Kind == NoteKindDecision && len(n.facts.Receipts) > 0 && !n.wake && !n.steered
}

// foldDecisionNotes merges every receipt note in one drain into one, placed
// where the first stood, and leaves all other notes in order.
func foldDecisionNotes(queued []userMessage) []userMessage {
	var all []decide.Receipt
	first, count := -1, 0
	for i, n := range queued {
		if isDecisionNote(n) {
			if first < 0 {
				first = i
			}
			count++
			all = append(all, n.facts.Receipts...)
		}
	}
	if count < 2 {
		return queued
	}
	out := make([]userMessage, 0, len(queued)-count+1)
	for i, n := range queued {
		switch {
		case i == first:
			out = append(out, decisionNote(all))
		case !isDecisionNote(n):
			out = append(out, n)
		}
	}
	return out
}

// decisionAside is the aside a note's facts describe, or nil for any other
// note. A mixed batch loses its kind ([mergeNoteFacts]) and so draws no receipt.
func decisionAside(f noteFacts) *decide.Aside {
	if f.Kind != NoteKindDecision {
		return nil
	}
	if aside, ok := decide.Compose(f.Receipts); ok {
		return &aside
	}
	return nil
}
