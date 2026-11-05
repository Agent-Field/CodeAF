package cellsync

import "testing"

// A turn starting or ending makes a beat due at once, and a beat that carried
// the flag settles it, so the same state again is not news.
func TestBatcherBeatsWhenATurnStartsOrEnds(t *testing.T) {
	b := &Batcher{}
	if !b.toldWorking() {
		t.Fatal("a quiet chat owes a beat")
	}
	b.Working(true)
	if b.toldWorking() {
		t.Fatal("a turn started, yet no beat is due")
	}
	select {
	case <-b.turns.ch():
	default:
		t.Fatal("a turn started, yet the beat loop was not woken")
	}
	b.mu.Lock()
	b.toldWork = b.working
	b.mu.Unlock()
	b.Working(true)
	select {
	case <-b.turns.ch():
		t.Fatal("the same state again woke the beat loop")
	default:
	}
	b.Working(false)
	if b.toldWorking() {
		t.Fatal("the turn ended, yet no beat is due")
	}
}
