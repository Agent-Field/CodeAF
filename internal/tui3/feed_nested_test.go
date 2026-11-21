package tui3

import (
	"testing"

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
