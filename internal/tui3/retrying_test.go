package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// WHAT THE SURFACE DOES WITH A CUT REQUEST.
//
// The engine cuts a model request that has gone quiet or come apart and asks it
// again (internal/provider's streamguard.go). Two things are owed to the person
// on this side of that: the words change from "waiting for" to "trying again",
// and whatever the dead attempt drew comes off the page, because the transcript
// no longer contains a word of it.

// TestARetryChangesTheWordsAndKeepsTheClock is the whole indicator: the model
// name goes (the line that named it was just cut), "trying again" arrives, and
// the seconds keep counting because a person watching a stall wants to know how
// long the second attempt has been out too.
func TestARetryChangesTheWordsAndKeepsTheClock(t *testing.T) {
	a := newWaitingApp(t, 0)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventRetrying, Text: "the model went quiet mid-reply — asking again",
	}})
	a.awaited = time.Now().Add(-12 * time.Second)

	text := plain(mustLine(t, a))
	if !strings.HasSuffix(text, "trying again · 12s") {
		t.Fatalf("a retry did not say so with a clock on it: %q", text)
	}
	if strings.Contains(text, "waiting for") || strings.Contains(text, "kimi-k3") {
		t.Fatalf("a retry still named the model it was waiting for: %q", text)
	}
}

// A RETRY WAITS OUT NO GRACE. The plain wait hides for four seconds because a
// fast provider must never flash a clock; a retry has no such ordinary case —
// something visibly went wrong a moment ago and the reason is owed at once.
func TestARetrySaysSoImmediately(t *testing.T) {
	a := newWaitingApp(t, 0)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventRetrying, Text: "nothing came back from the model — asking again",
	}})
	if got := a.waitingWords(); !strings.Contains(got, "trying again") {
		t.Fatalf("a fresh retry said %q, want it to say so at once", got)
	}
}

// TestTheSurfaceNeverInventsARetry is the honesty half. However long a first
// attempt runs, the line says what the surface can see — that it is waiting —
// and never guesses that somebody is trying again.
func TestTheSurfaceNeverInventsARetry(t *testing.T) {
	a := newWaitingApp(t, 3*time.Minute)
	if text := plain(mustLine(t, a)); strings.Contains(text, "trying again") {
		t.Fatalf("a long first attempt was reported as a retry: %q", text)
	}
}

// TestTheRetryWordStopsAtTheFirstThingTheNewStreamSays: "trying again" is only
// honest while trying is what is happening.
func TestTheRetryWordStopsAtTheFirstThingTheNewStreamSays(t *testing.T) {
	for _, kind := range []session.EventKind{
		session.EventTextDelta, session.EventReasoning, session.EventThinking,
		session.EventToolForming, session.EventToolAnnounced, session.EventToolBegin,
	} {
		a := newWaitingApp(t, 0)
		drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventRetrying, Text: "asking again"}})
		drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: kind, Text: "hello"}})
		if a.retrying {
			t.Fatalf("%v left the surface still saying it was trying again", kind)
		}
	}
}

// TestACutAttemptLeavesNothingOnThePage is the property that matters most on
// this side: the engine kept none of that text, so the page must keep none of
// it either. A person whose next question is answered underneath an abandoned
// half-answer has no way of telling which of the two the model actually read.
func TestACutAttemptLeavesNothingOnThePage(t *testing.T) {
	a := newWaitingApp(t, 0)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "All imports work: all imports OK and",
	}})
	if !strings.Contains(transcriptText(a), "All imports work") {
		t.Fatal("the streamed text never reached the page, so this test proves nothing")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventRetrying, Text: "the reply lost its thread — that text was dropped, asking again",
	}})
	if strings.Contains(transcriptText(a), "All imports work") {
		t.Fatalf("a cut attempt's text stayed on the page:\n%s", transcriptText(a))
	}
	// And the reason is on the page in its place, because a person who watched
	// an answer disappear is owed the sentence that says why.
	if !strings.Contains(transcriptText(a), "lost its thread") {
		t.Fatalf("the cut was silent:\n%s", transcriptText(a))
	}
	if a.live >= 0 {
		t.Fatalf("the dropped block is still the live one (live = %d)", a.live)
	}
}

// A hedge replacement takes the dead lane's words off the page before the
// rescuing lane continues. The one line explaining the switch stays where the
// withdrawn answer was, and the two answers never become one glued paragraph.
func TestAHedgeReplacementLeavesOnlyTheRescuedAnswerOnThePage(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state, a.turn, a.linear = stateWorking, 1, true
	a.event(session.Event{Kind: session.EventTextDelta, Text: "PARTIALTEXT PARTIALTEXT PARTIALTEXT PARTIALTEXT PARTIALTEXT PARTIALTEXT"})
	a.event(session.Event{Kind: session.EventRetrying, Text: "that lane went quiet — this answer is coming from another one"})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "STUBANSWER the link is back."})
	a.event(session.Event{Kind: session.EventAssistantDone})

	page := livePage(a)
	if !strings.Contains(page, "STUBANSWER the link is back.") {
		t.Fatalf("the rescuing lane's answer is missing:\n%s", page)
	}
	if strings.Contains(page, "PARTIALTEXT") {
		t.Fatalf("the dead lane's answer stayed on the page:\n%s", page)
	}
	if got := strings.Count(page, "that lane went quiet"); got != 1 {
		t.Fatalf("the replacement line appears %d times, want once:\n%s", got, page)
	}
	if !strings.Contains(page, "this answer is coming from") || !strings.Contains(page, "another one") {
		t.Fatalf("the replacement line is incomplete:\n%s", page)
	}
}

// TestARetryDoesNotDisturbWhatCameBeforeIt: only the CURRENT attempt is void.
// Everything the turn already finished — the person's own message, an earlier
// answer — is untouched.
func TestARetryDoesNotDisturbWhatCameBeforeIt(t *testing.T) {
	a := newWaitingApp(t, 0)
	a.entries = append(a.entries, entry{kind: entryUser, text: "what does the guard do?"})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTextDelta, Text: "soup"}})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventRetrying, Text: "asking again"}})
	if !strings.Contains(transcriptText(a), "what does the guard do?") {
		t.Fatalf("a retry took the person's own message with it:\n%s", transcriptText(a))
	}
}

// transcriptText is every entry's text, joined — enough to say whether
// something is on the page at all.
func transcriptText(a *app) string {
	var out strings.Builder
	for i := range a.entries {
		out.WriteString(a.entries[i].text)
		out.WriteString("\n")
	}
	return out.String()
}
