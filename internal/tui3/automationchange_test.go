package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CHANGE THE CONVERSATION ASKS FOR IS DRAWN AS THE TYPED EDIT'S CARD: the
// same `save this change?` head and the same two answers, whichever door raised
// it, because an automation with an id on a card is a change to it
// ([session.AutomationHead]).
func TestAChangeTheModelAsksForDrawsTheEditsCard(t *testing.T) {
	a, _ := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	item := a.watch.list[0]
	item.Schedule.Every = "0 9 * * 2"
	proposal := automationProposalOf(7, item, "every Tuesday at 09:00")
	proposal.Automation.Next = time.Now().Add(time.Hour)
	a.proposeAutomation(proposal)
	if a.auto == nil || a.auto.settled() {
		t.Fatalf("the change raised no card: %+v", a.auto)
	}
	rows := AutomationCardRows(a, a.auto, 80, false)
	if len(rows) == 0 || !strings.Contains(plain(rows[0]), typedEditHead) {
		t.Fatalf("the change card's head is %q, want %q", plain(rows[0]), typedEditHead)
	}
	// AND ITS RUN IS ITS NEXT: the automation exists, so nothing about it is first.
	if drawn := plain(strings.Join(rows, "\n")); !strings.Contains(drawn, "next run ") || strings.Contains(drawn, "first run ") {
		t.Fatalf("the change card does not say its next run:\n%s", drawn)
	}
	question := a.automationShown(a.auto).question
	if question.Head != typedEditHead {
		t.Fatalf("the change's question is headed %q", question.Head)
	}
	var labels []string
	for _, option := range question.Options {
		labels = append(labels, option.Label)
	}
	if strings.Join(labels, "|") != typedSaveLabel+"|"+typedNoLabel {
		t.Fatalf("a change offers %v, want only %s and %s", labels, typedSaveLabel, typedNoLabel)
	}
	if question.Options[1].Key != session.AutomationNoKey || question.Options[1].Consequence != typedNoSays {
		t.Fatalf("the change's no is %+v", question.Options[1])
	}
}
