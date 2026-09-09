package main

import (
	"bytes"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAHeadlessChatDoorWritesEveryEndingToStandardError(t *testing.T) {
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "the reply"}
	events <- session.Event{Kind: session.EventNotice, Text: "finishing here · what was asked is done"}
	close(events)

	var stdout, stderr bytes.Buffer
	if err := drainOnceEvents(events, &stdout, &stderr); err != nil {
		t.Fatalf("drainOnceEvents: %v", err)
	}
	if got, want := stdout.String(), "the reply\n"; got != want {
		t.Fatalf("stdout = %q, want only the reply %q", got, want)
	}
	if got, want := stderr.String(), "finishing here · what was asked is done\n"; got != want {
		t.Fatalf("stderr = %q, want the ending %q", got, want)
	}
}
