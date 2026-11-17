package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A chat may gain a stopped job between two automatic wakes in one shared
// floor interval. The next admission must see that handover immediately.
func TestAutomaticRefusalSeesHandoverBeforeNextFloorInterval(t *testing.T) {
	st, door, _, ids := watchFloor(t, 3, 1)
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	now := time.Now()
	previous := floorNow
	floorNow = func() time.Time { return now }
	t.Cleanup(func() { floorNow = previous })
	if why := door.AutomaticRefusal(talk); why != "" {
		t.Fatalf("an unmanaged chat refused: %q", why)
	}
	if err := st.Annotate(ids[0], func(it *factory.Item) error {
		it.Talk = talk
		factory.MarkPersonStopped(it)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if why := door.WatchedRefusal(talk); why != factory.PersonStopReason {
		t.Fatalf("the watcher missed the handover: %q", why)
	}
	if why := door.AutomaticRefusal(talk); why != factory.PersonStopReason {
		t.Fatalf("automatic admission missed the handover: %q", why)
	}
}
