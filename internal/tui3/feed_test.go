package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// TestOneCallBecomesOneRowFromTheFirstFragmentToTheResult drives the reducer
// with NO VIEW BEHIND IT AT ALL, which is the half of the law this lane is for:
// a transcript is grown by [feed] and by nothing else, so a feed with no hooks
// installed has to produce the same one row a chat does — arriving, queued,
// running, resolved — rather than four.
//
// It is written against the bare type rather than through an app on purpose. A
// test that went through the surface would pass just as well if the ingestion
// were still spelled out on [app], and the thing being pinned here is that it
// is not.
func TestOneCallBecomesOneRowFromTheFirstFragmentToTheResult(t *testing.T) {
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	f := &feed{live: -1, think: -1, turn: 1, hooks: feedHooks{now: func() time.Time { return at }}}

	f.ingest(session.Event{Kind: session.EventToolForming, Tool: "bash", CallID: "c1", Bytes: 12})
	if len(f.entries) != 1 || f.entries[0].status != toolForming {
		t.Fatalf("the first fragment did not draw one arriving row: %+v", f.entries)
	}
	f.ingest(session.Event{Kind: session.EventToolAnnounced, Tool: "bash", CallID: "c1", Args: `{"cmd":"go test"}`})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "c1", Args: `{"cmd":"go test"}`})
	if len(f.entries) != 1 || f.entries[0].status != toolRunning {
		t.Fatalf("the announcement and the begin drew rows of their own: %+v", f.entries)
	}
	f.ingest(session.Event{Kind: session.EventToolFinished, Tool: "bash", CallID: "c1", Took: 4 * time.Second})
	f.ingest(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "c1", Output: "ok"})

	if len(f.entries) != 1 {
		t.Fatalf("one call left %d rows behind", len(f.entries))
	}
	e := f.entries[0]
	if e.status != toolOK || e.ended.IsZero() || e.ran != 4*time.Second || e.detail.Output != "ok" {
		t.Fatalf("the row did not settle on its own result: %+v", e)
	}
}

// TestTheViewsOwnActsReachTheReducerThroughHooksAndNotThroughACopy is the other
// half: the three things a CHAT does inside ingestion — form its spawn card,
// read a refusal off a result, and count a call that closed — arrive as hooks,
// and every one of them is optional. That is what lets the task room run this
// same reducer without inheriting a card it has nowhere to draw.
func TestTheViewsOwnActsReachTheReducerThroughHooksAndNotThroughACopy(t *testing.T) {
	var formed, closing int
	var closedOn string
	f := &feed{live: -1, think: -1, turn: 1, hooks: feedHooks{
		forming: func(session.Event) { formed++ },
		closing: func(session.Event) { closing++ },
		closed:  func(e *entry, _ session.Event) { closedOn = e.tool },
	}}

	f.ingest(session.Event{Kind: session.EventToolForming, Tool: taskTool, CallID: "c1", Bytes: 3})
	f.ingest(session.Event{Kind: session.EventToolForming, Tool: taskTool, CallID: "c1", Bytes: 9})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: taskTool, CallID: "c1"})
	f.ingest(session.Event{Kind: session.EventToolEnd, Tool: taskTool, CallID: "c1", Output: "refused"})

	if formed != 2 {
		t.Fatalf("the view saw %d of the two fragments", formed)
	}
	if closing != 1 || closedOn != taskTool {
		t.Fatalf("the result reached the view %d times, on %q", closing, closedOn)
	}

	// AND A REDUCER NOBODY INSTALLED ANYTHING ON STILL WORKS, which is the room's
	// whole configuration in the lane after this one: the same events, no hooks,
	// no panic, and the same row at the end of it.
	bare := &feed{live: -1, think: -1, turn: 1}
	bare.ingest(session.Event{Kind: session.EventToolForming, Tool: "read", CallID: "c9"})
	bare.ingest(session.Event{Kind: session.EventToolBegin, Tool: "read", CallID: "c9"})
	bare.ingest(session.Event{Kind: session.EventToolEnd, Tool: "read", CallID: "c9", Output: "a file"})
	if len(bare.entries) != 1 || bare.entries[0].status != toolOK {
		t.Fatalf("a feed with no view behind it did not resolve its own row: %+v", bare.entries)
	}
}
