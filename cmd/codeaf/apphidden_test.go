package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A LINE THE SESSION WROTE TO ITS MODEL IS NEVER AN ENTRY, and neither is a
// reply that is only the withdrawal token; a team's delivery and the model's
// own words still are.
func TestLinesForTheModelAreNotEntries(t *testing.T) {
	items := []session.DisplayEntry{
		{Role: "user", Text: "rename it"},
		{Role: "assistant", Text: "done", Answer: true},
		{Role: "aside", Text: session.ResumeAfterQuitNote, ForModel: true},
		{Role: "assistant", Text: session.NoChangeReply, Answer: true},
		{Role: "aside", Text: "@web: index is ready", ForModel: true, Team: []session.TeamLine{{}}},
	}
	entries := appEntriesOf("a", items, time.Now(), nil)
	for _, e := range entries {
		if e.Text == session.ResumeAfterQuitNote || e.Text == session.NoChangeReply {
			t.Fatalf("a line for the model became an entry: %+v", e)
		}
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
}

// A TEAM'S DELIVERY READS AS ITS LINES ALONE. The session's words to its
// model around them, and a nudge that rode in the same message, are never
// shown.
func TestATeamDeliveryShowsOnlyItsLines(t *testing.T) {
	text := "Your team's replies started this turn; the person did not speak.\n\n" +
		"Team traffic in \"harbor\":\nfrom @web #4: index is ready\n(rule)\n\n[stuck] You have repeated read 3 times."
	items := []session.DisplayEntry{{Role: "aside", Text: text, ForModel: true, Team: []session.TeamLine{
		{Team: "harbor", From: "web", Text: "index is ready"},
		{Team: "harbor", From: "manager", Text: "ship it"},
	}}}
	entries := appEntriesOf("a", items, time.Now(), nil)
	if len(entries) != 1 || entries[0].Text != "@web: index is ready\nmanager: ship it" {
		t.Fatalf("entries = %+v", entries)
	}
}

// AN ASK SAYS WHAT IT IS ABOUT: an automation by its order id, a task by its
// app id, a call by its id.
func TestAnAskCarriesItsSubject(t *testing.T) {
	q := session.Question{ID: 4, Kind: session.QuestionAutomation, Head: "wants to schedule work",
		Subject: session.SubjectRef{Kind: session.SubjectOrder, ID: 4, Ref: "fd6f"}}
	if s := appAskOf("a", q).Subject; s == nil || s.Kind != appwire.SubjectAutomation || s.Ref != "fd6f" {
		t.Fatalf("subject = %+v", s)
	}
	q.Subject = session.SubjectRef{Kind: session.SubjectNode, ID: 7}
	if s := appAskOf("a", q).Subject; s == nil || s.Kind != appwire.SubjectTask || s.Ref != "a:7" {
		t.Fatalf("subject = %+v", s)
	}
	q.Subject = session.SubjectRef{}
	if s := appAskOf("a", q).Subject; s != nil {
		t.Fatalf("an ask about nothing has subject %+v", s)
	}
}
