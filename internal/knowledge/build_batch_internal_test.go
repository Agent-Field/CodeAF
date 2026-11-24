package knowledge

import (
	"reflect"
	"testing"
	"time"
)

// A QUIET CHAT WAITS FOR THE BUSY ONE, BUT NOT FOREVER. While one chat is
// being written in, a quiet chat with work waiting is held so both are compiled
// in one pass; past buildBatchMax it is due, and every quiet chat goes at once.
func TestAQuietChatWaitsForTheBusyOneButNotForever(t *testing.T) {
	base := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	oldNow, oldIdle, oldBatch := buildNow, buildIdle, buildBatchMax
	t.Cleanup(func() { buildNow, buildIdle, buildBatchMax = oldNow, oldIdle, oldBatch })
	buildIdle, buildBatchMax = 2*time.Minute, 10*time.Minute
	journals := []Journal{
		{ID: "a", Session: "quiet", Modified: base},
		{ID: "b", Session: "busy", Modified: base.Add(5 * time.Minute)},
		{ID: "c", Session: "long-done", Modified: base.Add(-time.Hour)},
	}
	pending := map[string]bool{"quiet": true, "busy": true}

	buildNow = func() time.Time { return base.Add(6 * time.Minute) }
	held, look := hotSessions(journals, false, pending)
	if want := []string{"busy", "quiet"}; !reflect.DeepEqual(held, want) {
		t.Fatalf("held %v while busy was written in; want %v", held, want)
	}
	if want := base.Add(7 * time.Minute); !look.Equal(want) {
		t.Fatalf("looks again at %v; want when busy goes quiet, %v", look, want)
	}

	buildNow = func() time.Time { return base.Add(8 * time.Minute) }
	if held, _ = hotSessions(journals, false, pending); len(held) != 0 {
		t.Fatalf("held %v once nothing is written in; want both compiled in one pass", held)
	}

	journals[1].Modified = base.Add(11 * time.Minute)
	buildNow = func() time.Time { return base.Add(12 * time.Minute) }
	if held, _ = hotSessions(journals, false, pending); !reflect.DeepEqual(held, []string{"busy"}) {
		t.Fatalf("held %v after quiet waited its ten minutes; want only the busy chat", held)
	}
	if held, _ = hotSessions(journals, true, pending); held != nil {
		t.Fatalf("a Build held %v; want nothing held", held)
	}
}
