package tui3

// ── A CALL THAT NEVER CAME BACK ─────────────────────────────────────────────
//
// The report these tests hold shut: "the view_image tool row in the main chat
// always seems to be running".
//
// Two things were suspected and only one of them was true. Two calls of the same
// tool in one batch DO both settle — the first test pins that, because it is the
// shape the failing session actually had. What did not settle is a call the turn
// ended around: it kept `toolRunning` with no end stamped on it, which looks
// quiet while the session is idle and starts spinning again — from a beginning
// minutes ago — the moment the next turn starts.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	lookAtLinkedIn = `{"path":"marketing/agentfield-linkedin.png","question":"is the copy legible?"}`
	lookAtTwitter  = `{"path":"marketing/agentfield-twitter.png","question":"is the copy legible?"}`
)

// TWO CALLS OF ONE TOOL IN ONE BATCH BOTH SETTLE. The model asked for two looks
// in a single message, so the surface drew two rows of the same name and then
// took two ends carrying nothing to tell them apart but their arguments.
func TestTwoCallsOfOneToolInABatchBothSettle(t *testing.T) {
	agent := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolForming, CallID: "a", Tool: "view_image", Bytes: 40},
		{Kind: session.EventToolForming, CallID: "b", Tool: "view_image", Bytes: 40},
		{Kind: session.EventToolAnnounced, CallID: "a", Tool: "view_image",
			Hint: "view_image agentfield-linkedin.png", Args: lookAtLinkedIn},
		{Kind: session.EventToolAnnounced, CallID: "b", Tool: "view_image",
			Hint: "view_image agentfield-twitter.png", Args: lookAtTwitter},
		{Kind: session.EventToolBegin, Tool: "view_image",
			Hint: "view_image agentfield-linkedin.png", Args: lookAtLinkedIn},
		{Kind: session.EventToolBegin, Tool: "view_image",
			Hint: "view_image agentfield-twitter.png", Args: lookAtTwitter},
		// The ends arrive together, after the whole batch — internal/session
		// sends every begin before the batch runs and every end after it.
		{Kind: session.EventToolEnd, Tool: "view_image", Args: lookAtLinkedIn, Output: "seen by m: yes"},
		{Kind: session.EventToolEnd, Tool: "view_image", Args: lookAtTwitter, Output: "seen by m: no"},
	}}}}
	a := newTestApp(agent)
	typeLine(t, a, "check both renders")

	looks := 0
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.tool != "view_image" {
			continue
		}
		looks++
		if e.status.live() {
			t.Fatalf("a look that came back is still drawn as running:\n%s",
				strings.Join(plainRows(a), "\n"))
		}
	}
	if looks != 2 {
		t.Fatalf("two looks were asked for and %d rows were drawn:\n%s",
			looks, strings.Join(plainRows(a), "\n"))
	}
}

// A CALL THE TURN ENDED AROUND STOPS, AND STAYS STOPPED. This is the row that
// read as "always running": nothing resolved it, so the next turn's working
// state started it spinning again with an age nobody measured.
func TestACallLeftRunningDoesNotSpinAgainOnTheNextTurn(t *testing.T) {
	agent := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{
		{
			{Kind: session.EventToolBegin, Tool: "view_image",
				Hint: "view_image poster.png", Args: lookAtLinkedIn},
			// and no end: the stream closes around it.
		},
		{{Kind: session.EventTextDelta, Text: "Looking again."}},
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "look at the poster")
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	at := firstTool(t, a)
	if a.entries[at].ended.IsZero() {
		t.Fatalf("the turn ended and the call it left open never stopped:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
	// The mark is the dim one for something nothing more is coming for, and it
	// stays that way through the next turn — which is where the defect showed.
	if got := plain(a.mark(&a.entries[at])); !strings.Contains(got, glyphIdle) {
		t.Fatalf("an abandoned call draws %q, want the idle mark", got)
	}
	typeLine(t, a, "again")
	if got := plain(a.mark(&a.entries[at])); !strings.Contains(got, glyphIdle) {
		t.Fatalf("the next turn started the abandoned call spinning again: %q", got)
	}
	if word, _ := a.countClock(&a.entries[at]); word != "" {
		t.Fatalf("an abandoned call is counting up again: %q", word)
	}
	// And its status is untouched: nobody watched what became of it, so it is
	// not claimed to have failed.
	if a.entries[at].status != toolRunning {
		t.Fatalf("the abandoned call was relabelled %v", a.entries[at].status)
	}
}
