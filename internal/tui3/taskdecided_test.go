package tui3

// A PROPOSAL CARD THAT ARRIVES ALREADY DECIDED. The engine restates a proposal
// the moment anybody settles it and leaves the open card out of a turn's replay
// in its favour (session's [TaskNotice.Decided]), so this is what a window that
// was looking at another tab is handed when it comes back: the assignment, with
// the answer underneath it, and nothing to answer.

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// decidedProposal is one restated card, as the turn's replay carries it.
func decidedProposal(id uint64, answer session.TaskAnswer) session.Event {
	return session.Event{Kind: session.EventTaskProposal, Tool: "propose_task", Task: &session.TaskNotice{
		ID:      id,
		Title:   "Port the resume picker",
		Summary: "two lines the person reads",
		Brief:   "the whole brief",
		Decided: &answer,
	}}
}

// settledCard is the proposal card this window is drawing, and false where it
// has none.
func settledCard(a *app, id uint64) (*taskCard, bool) {
	for i := range a.entries {
		if a.entries[i].kind == entryTask && a.entries[i].card != nil && a.entries[i].card.id == id {
			return a.entries[i].card, true
		}
	}
	return nil, false
}

// TestAProposalDecidedBeforeThisWindowArrivedIsDrawnAnsweredAndNotAsked is the
// owner's defect from the surface's side: approve a task, switch tab, come back,
// and be asked the same question again — every time, with the question taking
// the keyboard so the conversation behind it could not be scrolled either.
func TestAProposalDecidedBeforeThisWindowArrivedIsDrawnAnsweredAndNotAsked(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.proposeTask(decidedProposal(7, session.TaskAnswer{Approved: true}))

	if len(a.questions) != 0 {
		t.Fatalf("the block is asking %d questions about a decision already made", len(a.questions))
	}
	card, drawn := settledCard(a, 7)
	if !drawn {
		t.Fatal("the conversation kept no card for a proposal that was approved")
	}
	if !card.settled() {
		t.Fatal("the card is still counting on a proposal that was answered")
	}
	if card.verdict != taskApprovedWord {
		t.Fatalf("the card says %q, want %q", card.verdict, taskApprovedWord)
	}
	if card.answer != "start it" {
		t.Fatalf("the card's answer reads %q, want the option's own label", card.answer)
	}
}

// TestADeclinedProposalDrawnOnALaterAttachSaysSo is the other answer, so the
// replay cannot draw every settled card as a yes.
func TestADeclinedProposalDrawnOnALaterAttachSaysSo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.proposeTask(decidedProposal(8, session.TaskAnswer{}))

	card, drawn := settledCard(a, 8)
	if !drawn {
		t.Fatal("the conversation kept no card for a proposal that was declined")
	}
	if card.verdict != taskDeclinedWord {
		t.Fatalf("the card says %q, want %q", card.verdict, taskDeclinedWord)
	}
	if len(a.questions) != 0 {
		t.Fatalf("the block is asking %d questions about a decision already made", len(a.questions))
	}
}

// TestAProposalNobodyHasDecidedIsStillAsked keeps the fix from being a deletion:
// an open card is exactly what a surface must be asked about.
func TestAProposalNobodyHasDecidedIsStillAsked(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	open := decidedProposal(9, session.TaskAnswer{})
	open.Task.Decided = nil
	a.proposeTask(open)

	if len(a.questions) != 1 {
		t.Fatalf("the block is asking %d questions about an open proposal, want 1", len(a.questions))
	}
	if card, drawn := settledCard(a, 9); !drawn || card.settled() {
		t.Fatalf("an open proposal's card is drawn as %+v, want one still waiting", card)
	}
}
