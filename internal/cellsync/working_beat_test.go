package cellsync

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

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

// A turn that ended with its last turns still going up stays working until
// they are published: another computer takes the chat when it stops working,
// and a take naming the save point before that publish is refused.
func TestATurnsEndIsToldOnlyOnceItsTurnsArePublished(t *testing.T) {
	b := &Batcher{Driving: &Driving{}}
	b.Working(true)
	b.mu.Lock()
	b.toldWork = true
	b.mu.Unlock()
	b.Note(cellstore.Turn{ID: "t1"})
	b.Working(false)
	if !b.toldWorking() {
		t.Fatal("the turn's end was due before its last turn was published")
	}
	b.durable("t1")
	if b.toldWorking() {
		t.Fatal("the turn's last turn is published, yet its end is not due")
	}
}
