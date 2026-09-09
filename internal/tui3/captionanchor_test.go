package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── A CAPTION LANDS ON THE STEP IT IS ABOUT, WHENEVER IT ARRIVES ────────────
//
// These drive the reducer directly and interleave the events by hand. That is
// the point: the race they are about is a SCHEDULING race in the engine — the
// narrator's goroutine can pass its own cancellation check and then be
// descheduled past the end of its batch — and reproducing it by timing would be
// a test that fails once a week for the wrong reason. Ordering the events is the
// same fact, stated deterministically.

// captionFeed is a reducer with no view behind it, mid-turn.
func captionFeed() *feed { return &feed{live: -1, think: -1, turn: 1} }

// runBatch drives one batch of one call from its begin to its end.
func runBatch(f *feed, tool, callID string) {
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: tool, CallID: callID})
	f.ingest(session.Event{Kind: session.EventToolFinished, Tool: tool, CallID: callID, Took: time.Second})
	f.ingest(session.Event{Kind: session.EventToolEnd, Tool: tool, CallID: callID, Output: "ok"})
}

func toolRowFor(t *testing.T, f *feed, callID string) *entry {
	t.Helper()
	for i := range f.entries {
		if f.entries[i].kind == entryTool && f.entries[i].callID == callID {
			return &f.entries[i]
		}
	}
	t.Fatalf("no tool row for %q", callID)
	return nil
}

// THE DEFECT ITSELF: a caption about a FINISHED batch, arriving after the next
// batch has already drawn its row.
//
// Keyed by recency — "the newest tool row of this turn" — the sentence about
// the grep landed on the bash that replaced it, so the running step wore a title
// describing work that was over. It is silent, it is on the row a person is
// actually watching, and with the family beside the words it would have drawn
// the wrong MARK too.
func TestALateCaptionDoesNotRetitleTheStepThatFollowedIt(t *testing.T) {
	f := captionFeed()
	runBatch(f, "grep", "call-old")
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-new"})

	// The narrator's answer about the FIRST batch, arriving now.
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-old",
		Text: "looking for the caller", Category: session.ActionSearch,
	})

	old := toolRowFor(t, f, "call-old")
	if old.caption != "looking for the caller" || old.captionCat != session.ActionSearch {
		t.Fatalf("the caption did not land on its own step: %q/%q", old.caption, old.captionCat)
	}
	live := toolRowFor(t, f, "call-new")
	if live.caption != "" || live.captionCat != "" {
		t.Fatalf("a caption about the finished step retitled the running one: %q/%q",
			live.caption, live.captionCat)
	}
}

// AND THE ORDINARY CASE IS UNCHANGED: a caption about the batch that is running
// lands on it.
func TestACaptionLandsOnItsOwnRunningStep(t *testing.T) {
	f := captionFeed()
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-live"})
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-live",
		Text: "running the loader suite", Category: session.ActionTest,
	})
	live := toolRowFor(t, f, "call-live")
	if live.caption != "running the loader suite" || live.captionCat != session.ActionTest {
		t.Fatalf("the caption missed its own step: %q/%q", live.caption, live.captionCat)
	}
}

// A CAPTION FOR A STEP THIS FEED IS NOT HOLDING IS DROPPED. The rows of a turn
// that has been cleared, or of a conversation this window never had, are not
// here to be named — and naming the nearest row instead is exactly the defect
// above with a longer reach.
func TestACaptionForAStepThatIsNotHereIsDropped(t *testing.T) {
	f := captionFeed()
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-live"})
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-gone",
		Text: "about something else entirely", Category: session.ActionSearch,
	})
	live := toolRowFor(t, f, "call-live")
	if live.caption != "" || live.captionCat != "" {
		t.Fatalf("an unanchorable caption was applied anyway: %q/%q", live.caption, live.captionCat)
	}
}

// THE SECOND NARRATION OF ONE STEP REPLACES THE FIRST, BOTH HALVES AT ONCE. A
// later answer with no family clears the family rather than leaving the old one
// beside new words — a mark that was right about the previous sentence is wrong
// about this one, and the tools will answer correctly for it.
func TestASecondNarrationReplacesBothHalvesOfTheFirst(t *testing.T) {
	f := captionFeed()
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-live"})
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-live",
		Text: "running the loader suite", Category: session.ActionTest,
	})
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-live",
		Text: "starting the local server",
	})
	live := toolRowFor(t, f, "call-live")
	if live.caption != "starting the local server" {
		t.Fatalf("the later sentence did not land: %q", live.caption)
	}
	if live.captionCat != "" {
		t.Fatalf("the earlier family outlived the sentence it belonged to: %q", live.captionCat)
	}
}

// AN EMPTY CAPTION NAMES NOTHING AND CHANGES NOTHING. The engine never sends
// one; a link, a replay or a future producer might, and silently blanking a
// step's title would be the worst possible reading of "no news".
func TestAnEmptyCaptionLeavesTheStepAsItWas(t *testing.T) {
	f := captionFeed()
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-live"})
	f.ingest(session.Event{
		Kind: session.EventCaption, CallID: "call-live",
		Text: "running the loader suite", Category: session.ActionTest,
	})
	f.ingest(session.Event{Kind: session.EventCaption, CallID: "call-live", Text: "   "})
	live := toolRowFor(t, f, "call-live")
	if live.caption != "running the loader suite" || live.captionCat != session.ActionTest {
		t.Fatalf("an empty caption erased a real one: %q/%q", live.caption, live.captionCat)
	}
}

// AN ENGINE BUILT BEFORE THE ANCHOR IS SERVED EXACTLY AS IT ALWAYS WAS. Its
// captions carry no id; dropping them would silently take the narration away
// from every mixed-version link, so they keep the old recency rule — and with it
// the old race, which is the honest cost of speaking to an old peer.
func TestACaptionWithNoAnchorStillNamesTheNewestStep(t *testing.T) {
	f := captionFeed()
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "call-live"})
	f.ingest(session.Event{Kind: session.EventCaption, Text: "running the loader suite"})
	live := toolRowFor(t, f, "call-live")
	if live.caption != "running the loader suite" {
		t.Fatalf("an anchorless caption from an older engine was dropped: %q", live.caption)
	}
}
