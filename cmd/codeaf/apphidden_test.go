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
