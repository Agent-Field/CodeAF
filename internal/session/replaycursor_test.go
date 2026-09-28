package session

import "testing"

func TestReplayCursorIsAtomicWithHistoryAndPreservesObserverIdentity(t *testing.T) {
	a := &Agent{}
	a.mu.Lock()
	h := a.newReplayHubLocked()
	a.hub = h
	a.running = true
	a.mu.Unlock()
	h.send(Event{Kind: EventTextDelta, Text: "invoice result"})
	_, events, stop, cursor := a.AttachReplayCursor()
	defer stop()
	if cursor.Owner == "" || cursor.Turn != 1 {
		t.Fatalf("cursor=%+v", cursor)
	}
	if ev := <-events; ev.Text != "invoice result" || ev.ReplayCursor != cursor {
		t.Fatalf("observer identity=%+v", ev)
	}
	a.mu.Lock()
	a.running = false
	a.mu.Unlock()
	h.close()
	_, events, stop, after := a.AttachReplayCursor()
	stop()
	if events != nil || after != cursor {
		t.Fatalf("completed boundary changed: %+v", after)
	}
	a.mu.Lock()
	next := a.newReplayHubLocked()
	a.mu.Unlock()
	if cursor.Covers(next.replayCursor) {
		t.Fatal("new turn covered by previous snapshot")
	}
	b := &Agent{}
	other := b.newReplayHubLocked()
	if cursor.Covers(other.replayCursor) {
		t.Fatal("new engine covered by old snapshot")
	}
}
