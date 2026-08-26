package remote

import (
	"testing"
	"time"
)

// Ping is driven through Loopback so both the real client and real server have
// to understand the empty method. A scripted result would prove only the
// client's stopwatch and could leave the engine door missing.
func TestPingRoundTripsOverTheRealProtocol(t *testing.T) {
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	before := loop.Client.CallsMade()
	elapsed, err := loop.Client.Ping()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if elapsed <= 0 || elapsed > time.Second {
		t.Fatalf("round trip = %s, want one measured loopback trip", elapsed)
	}
	if calls := loop.Client.CallsMade() - before; calls != 1 {
		t.Fatalf("ping put %d calls on the wire, want 1", calls)
	}
}
