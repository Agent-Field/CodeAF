package factory

import (
	"testing"
	"time"
)

// WHICH TASKS WILL ASK FIRST (v2 J6 "waits on 1 · asks first", screen 7
// "replies are public, so Afi asks you before posting each one"). The words a
// plan's tasks really use are read: a step that leaves the workstream asks, a
// noun that only looks like one does not, and nothing asks when the rule is
// off, a person or the factory does it, or it is finished.
func TestAPlanSaysWhichTasksWillAskFirst(t *testing.T) {
	afi := TaskDoneBy{Kind: TaskByAfi}
	on := DefaultRules("priya")
	for _, c := range []struct {
		title, request, want string
	}{
		{"Post the notes to Discord", "", "posting"},
		{"Reply to the three GitHub issues", "Answer each reporter.", "replying"},
		{"Publish the March release notes", "", "publishing"},
		{"Email the three pilot customers", "", "sending"},
		{"Launch email to subscribers", "Write it. Then send it to the list.", "sending"},
		{"Book the venue", "Pay the deposit for the hall.", "paying"},
		{"Draft the release notes", "Plain language, under 300 words.", ""},
		{"Launch email to subscribers for Pantry 2.4", "Write the Pantry 2.4 launch email to the ~40,000 subscribers.", ""},
		{"Support reply macro for the recipe import fix", "A macro agents paste into replies.", ""},
		{"Write a blog post about 2.4", "", ""},
		{"App Store and Play Store What's new text", "", ""},
		// The live L7 run: a draft whose request names the channel it is for
		// is not the post, and neither is one made "ready to post".
		{"Draft Discord announcement of Pantry 2.4", "Draft a short Discord announcement of Pantry 2.4 from the release notes, for the #announcements channel.", ""},
		{"Write the announcement", "Make it ready to post to Discord. Leave it for someone to send to the list.", ""},
		{"Post Pantry 2.4 announcement to Discord #announcements", "Post the drafted announcement.", "posting"},
	} {
		task := WorkstreamTask{Title: c.title, Request: c.request, DoneBy: afi, State: TaskUpNext}
		if got := AsksFirst(task, on); got != c.want {
			t.Errorf("%q / %q asks first %q, want %q", c.title, c.request, got, c.want)
		}
	}
	draft := WorkstreamTask{Title: "Draft Discord announcement of Pantry 2.4", DoneBy: afi, State: TaskUpNext,
		Brief: &TaskBrief{Deliverable: "Announcement text as a document in Files, ready to post to Discord #announcements."}}
	if got := AsksFirst(draft, on); got != "" {
		t.Errorf("a draft whose deliverable is ready to post asks first %q (live L7 run)", got)
	}
	post := WorkstreamTask{Title: "Post the notes to Discord", DoneBy: afi, State: TaskUpNext}
	off := on
	off.PublishesWithoutAsking = true
	if AsksFirst(post, off) != "" {
		t.Error("a workstream that publishes without asking still predicts an Ask")
	}
	for _, by := range []string{TaskByPerson, TaskByFactory} {
		p := post
		p.DoneBy = TaskDoneBy{Kind: by}
		if AsksFirst(p, on) != "" {
			t.Errorf("a task done by %s is predicted to ask", by)
		}
	}
	post.State = TaskDone
	if AsksFirst(post, on) != "" {
		t.Error("a finished task is predicted to ask")
	}
}

// THE RULE IS ON BY DEFAULT and turning it off or on is a decision, in the
// words the plan's side column shows.
func TestAskingBeforePublishingIsTheDefaultRule(t *testing.T) {
	if DefaultRules("priya").PublishesWithoutAsking || (OrgWorkstream{}).RulesOrDefault().PublishesWithoutAsking {
		t.Fatal("a workstream's default rules publish without asking")
	}
	before := DefaultRules("priya")
	after := before
	after.PublishesWithoutAsking = true
	n := 0
	id := func() string { n++; return "d" }
	none := func(string) string { return "" }
	if got := RuleDecisions(before, after, PersonRef{ID: "priya"}, none, id, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)); len(got) != 1 || got[0].Text != PublishesWithoutAskingRule {
		t.Fatalf("turning the rule off recorded %+v", got)
	}
	if got := RuleDecisions(after, before, PersonRef{ID: "priya"}, none, id, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)); len(got) != 1 || got[0].Text != AsksBeforePublishingRule {
		t.Fatalf("turning the rule on recorded %+v", got)
	}
}

// A CHECK SAYS WHAT IT WAS CHECKED AGAINST, with the decisions in force
// counted, and never doubles a lead the agent wrote itself.
func TestACheckNamesTheRequestAndTheDecisionsItWasCheckedAgainst(t *testing.T) {
	for _, c := range []struct {
		n           int
		claims, out string
	}{
		{2, "280 words, plain language", "Checked against the request and 2 decisions: 280 words, plain language"},
		{1, "Under 300 words", "Checked against the request and 1 decision: Under 300 words"},
		{0, "Under 300 words", "Checked against the request: Under 300 words"},
		{3, "Checked against the request: Both changes", "Checked against the request and 3 decisions: Both changes"},
		{0, "  ", ""},
	} {
		if got := CheckedAgainst(c.n, c.claims); got != c.out {
			t.Errorf("CheckedAgainst(%d, %q) = %q, want %q", c.n, c.claims, got, c.out)
		}
	}
}
