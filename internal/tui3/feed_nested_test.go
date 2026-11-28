package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A SCRIPT'S CALL IS DRAWN UNDER ITS SCRIPT LIVE, as it is on the plan page:
// the event's parent is the fact, and the row carries it.
func TestFeedDrawsAScriptsCallNested(t *testing.T) {
	f := &feed{live: -1, think: -1}
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "script", CallID: "s"})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	f.ingest(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-2", Parent: "s"})
	f.ingest(session.Event{Kind: session.EventToolFailed, Tool: "bash", CallID: "s-script-2", Parent: "s", Output: "boom"})
	var script, nested int
	for _, e := range f.entries {
		if e.kind != entryTool {
			continue
		}
		if e.nested {
			nested++
		} else {
			script++
		}
	}
	if script != 1 || nested != 2 {
		t.Errorf("rows: %d top-level, %d nested; want 1 and 2", script, nested)
	}
}

// A SCRIPT'S OWN CALL ENDING IS NOT THE BATCH ENDING. The script is still
// running, so no request is out and the wait for the model must not start;
// the script's own end starts it.
func TestAScriptsOwnCallEndingDoesNotStartTheWaitForTheModel(t *testing.T) {
	a := newWaitingApp(t, 0)
	a.awaited = time.Time{}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "s-script-1", Parent: "s"}})
	if !a.awaited.IsZero() {
		t.Fatal("a script's own call ending started the wait for the model while the script still ran")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolEnd, Tool: "script", CallID: "s"}})
	if a.awaited.IsZero() {
		t.Fatal("the script's own end did not start the wait for the model")
	}
}

// A SCRIPT'S CALLS HANG UNDER THE SCRIPT, NOT UNDER THE ROW ABOVE THEM. When
// the model batches a script with another call, both rows begin first; the
// script's calls then go straight after the script, as a reopened
// conversation draws them.
func TestFeedHangsAScriptsCallsUnderTheScriptInABatch(t *testing.T) {
	f := &feed{live: -1, think: -1}
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "script", CallID: "s"})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "read", CallID: "r"})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-1", Parent: "s"})
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-2", Parent: "s"})
	var order []string
	for _, e := range f.entries {
		if e.kind == entryTool {
			order = append(order, e.callID)
		}
	}
	want := []string{"s", "s-script-1", "s-script-2", "r"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("rows = %v, want %v", order, want)
	}
}
