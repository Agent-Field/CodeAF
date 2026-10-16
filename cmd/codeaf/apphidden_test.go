package main

import (
	"strings"
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

// appTurnShape runs one scripted turn and gives back what it said: every entry
// the turn added after the person's own, and the words each message streamed.
func appTurnShape(t *testing.T, script []session.Event) ([]appwire.Entry, map[int]string) {
	t.Helper()
	conv := &fakeConversation{script: script}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "draft the release notes", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var entries []appwire.Entry
	streamed := map[int]string{}
	for _, data := range rec.data {
		switch d := data.(type) {
		case appwire.EntryAdded:
			if d.Entry.Kind != appwire.EntryUser {
				entries = append(entries, d.Entry)
			}
		case appwire.TurnDelta:
			if d.Kind == appwire.DeltaText {
				streamed[d.Msg] += d.Text
			}
		}
	}
	return entries, streamed
}

// appSameEntries says whether the live entries name what the listing names
// for the same record: id, kind and words, in order. A call is a turn.tool
// live and is left out of both.
func appSameEntries(t *testing.T, live []appwire.Entry, items []session.DisplayEntry) {
	t.Helper()
	var listed []appwire.Entry
	for _, e := range appEntriesOf("a", items, time.Now(), nil) {
		if e.Kind != appwire.EntryUser && e.Kind != appwire.EntryTool {
			listed = append(listed, e)
		}
	}
	if len(live) != len(listed) {
		t.Fatalf("live said %d entries and the listing %d:\n live %+v\nlisted %+v", len(live), len(listed), live, listed)
	}
	for i := range live {
		if live[i].ID != listed[i].ID || live[i].Kind != listed[i].Kind || live[i].Text != listed[i].Text {
			t.Fatalf("entry %d: live %s %s %q, listed %s %s %q", i, live[i].ID, live[i].Kind, live[i].Text, listed[i].ID, listed[i].Kind, listed[i].Text)
		}
	}
}

// the carry-on line, as a person read it in the app.
const appCarryOnLine = "the ask is not finished · carrying on rather than stopping here"

// A REPLY SENT BACK TO WORK AND WITHDRAWN IS NOT GLUED TO THE ANSWER. The
// turn answered, the completion check carried it on, and the model withdrew
// with the token and nothing else: the answer stands alone, live and listed,
// and the line saying it was carried on is not a notice in the chat.
func TestAWithdrawnCarryOnIsNotGluedToTheAnswer(t *testing.T) {
	if !session.IsCarryOnNotice(appCarryOnLine) {
		t.Fatal("the carry-on line is not the session's")
	}
	answer := "I have assigned the sections and am standing by for drafts from the team."
	entries, streamed := appTurnShape(t, []session.Event{
		{Kind: session.EventTextDelta, Text: answer},
		{Kind: session.EventAssistantDone},
		{Kind: session.EventNotice, Text: appCarryOnLine},
		{Kind: session.EventTextDelta, Text: "[no "},
		{Kind: session.EventTextDelta, Text: "change]"},
		{Kind: session.EventAssistantDone},
		{Kind: session.EventTurnDone},
	})
	for msg, words := range streamed {
		if strings.Contains(words, session.NoChangeReply) {
			t.Fatalf("message %d streamed the withdrawal token: %q", msg, words)
		}
	}
	if streamed[0] != answer || len(streamed) != 1 {
		t.Fatalf("streamed = %v", streamed)
	}
	for _, e := range entries {
		if e.Kind == appwire.EntryNotice {
			t.Fatalf("the carry-on became a notice: %+v", e)
		}
	}
	appSameEntries(t, entries, []session.DisplayEntry{
		{Role: "user", Text: "draft the release notes"},
		{Role: "assistant", Text: answer, Answer: true},
		{Role: "assistant", Text: session.NoChangeReply, Answer: true},
	})
}

// A CARRY-ON ANSWERED IN WORDS IS A SECOND MESSAGE. A reply the engine closed
// in words alone is an answer, said at once, even when the turn is sent back
// and goes on to call a tool; only the reply its own tool call comes straight
// after is an update, as the listing names them ([appUpdates]). Neither nudge
// is said as a notice, and a notice that is not one of them still is.
func TestEveryReplyOfACarriedOnTurnIsItsOwnMessage(t *testing.T) {
	t.Run("words", func(t *testing.T) {
		entries, streamed := appTurnShape(t, []session.Event{
			{Kind: session.EventTextDelta, Text: "The parser is done."},
			{Kind: session.EventAssistantDone},
			{Kind: session.EventNotice, Text: appCarryOnLine},
			{Kind: session.EventTextDelta, Text: "The handlers are wired too."},
			{Kind: session.EventAssistantDone},
			{Kind: session.EventTurnDone},
		})
		if streamed[0] != "The parser is done." || streamed[1] != "The handlers are wired too." {
			t.Fatalf("streamed = %v", streamed)
		}
		appSameEntries(t, entries, []session.DisplayEntry{
			{Role: "user", Text: "draft the release notes"},
			{Role: "assistant", Text: "The parser is done.", Answer: true},
			{Role: "assistant", Text: "The handlers are wired too.", Answer: true},
		})
	})
	t.Run("a tool after", func(t *testing.T) {
		entries, _ := appTurnShape(t, []session.Event{
			{Kind: session.EventTextDelta, Text: "Let me make the question."},
			{Kind: session.EventAssistantDone},
			{Kind: session.EventNotice, Text: "it loaded a tool and stopped before using it · asking it to go on"},
			{Kind: session.EventNotice, Text: "Retry 1/3: removed max_tokens"},
			{Kind: session.EventTextDelta, Text: "Asking now."},
			{Kind: session.EventToolBegin, CallID: "c1", Tool: "read", Hint: "read go.mod"},
			{Kind: session.EventToolEnd, CallID: "c1", Tool: "read", Hint: "12 lines"},
			{Kind: session.EventTextDelta, Text: "Done."},
			{Kind: session.EventAssistantDone},
			{Kind: session.EventTurnDone},
		})
		var notices []string
		var said []appwire.Entry
		for _, e := range entries {
			if e.Kind == appwire.EntryNotice {
				notices = append(notices, e.Text)
				continue
			}
			said = append(said, e)
		}
		if len(notices) != 1 || notices[0] != "Retry 1/3: removed max_tokens" {
			t.Fatalf("notices = %q", notices)
		}
		if len(said) < 2 || said[0].Kind != appwire.EntryAnswer || said[1].Kind != appwire.EntryUpdate {
			t.Fatalf("the words closed alone must be an answer and the words before the call an update: %+v", said)
		}
		appSameEntries(t, said, []session.DisplayEntry{
			{Role: "user", Text: "draft the release notes"},
			{Role: "assistant", Text: "Let me make the question.", Answer: true},
			{Role: "assistant", Text: "Asking now."},
			{Role: "tool", CallID: "c1", Tool: "read"},
			{Role: "assistant", Text: "Done.", Answer: true},
		})
	})
}

// A TOOL'S RESULT MESSAGE IS NOT A SECOND CALL: the blank row the shaping keeps
// for it (no call id, no name) is not an entry, while every genuine call keeps
// its own stable id, in history order, with no turn an old record never had.
func TestAToolResultMessageIsNotAStep(t *testing.T) {
	items := []session.DisplayEntry{
		{Role: "aside", Text: "manager: start", Team: []session.TeamLine{{Team: "t", From: "manager", Text: "start"}}},
		{Role: "tool", Tool: "bash", CallID: "call_1", Hint: "run it"},
		{Role: "tool"},
		{Role: "assistant", Text: "ran once", Answer: false},
		{Role: "tool", Tool: "read", CallID: "call_2"},
		{Role: "tool"},
		{Role: "tool", Tool: "bash"},
	}
	var ids []string
	for _, e := range appEntriesOf("a", items, time.Now(), nil) {
		if e.Kind == appwire.EntryTool {
			ids = append(ids, e.ID)
			if e.Turn != 0 {
				t.Fatalf("an old record's tool was given turn %d", e.Turn)
			}
		}
	}
	want := []string{"tcall_1", "tcall_2", "t#6"}
	if len(ids) != len(want) {
		t.Fatalf("tool entries = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("tool entries = %v, want %v", ids, want)
		}
	}
}
