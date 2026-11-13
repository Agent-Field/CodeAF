package tui3

import (
	"strings"
	"testing"
	"time"
)

// L4-M11b: A measured latency expires on the surface clock, even if the next ping never finishes.
func TestL4B2LatencyExpires(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.host = "desk-b"
	now := time.Unix(1_000_000, 0)
	a.clock = func() time.Time { return now }
	a.linkPingBack(linkPingMsg{elapsed: time.Millisecond})
	if !strings.Contains(a.linkSegment(), "1ms") {
		t.Fatal("answer missing")
	}
	now = now.Add(hostPingEvery + time.Second + time.Nanosecond)
	if strings.Contains(a.linkSegment(), "ms") {
		t.Fatalf("stale latency: %s", a.linkSegment())
	}
	a.linkPingBack(linkPingMsg{elapsed: time.Millisecond})
	if !strings.Contains(a.linkSegment(), "1ms") {
		t.Fatal("new answer did not restore latency")
	}
}
