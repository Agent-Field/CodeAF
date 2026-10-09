package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE TOP BAR'S BUTTON SENDS ITS INTENT, AND A DOUBLED ASK NEVER UNDOES
// ITSELF (the owner saw paused, resumed, paused in one second on 2026-10-09,
// from a toggle pressed twice). Hold on twice holds once and says paused
// once; Hold off twice goes on once, in the same chat, and says resumed once.
func TestHoldTwiceHoldsOnceAndGoOnTwiceGoesOnOnce(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	for i := 0; i < 2; i++ {
		if err := g.r.Hold(id, true); err != nil {
			t.Fatalf("hold %d: %v", i+1, err)
		}
	}
	it := g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	time.Sleep(30 * time.Millisecond)
	if it, _ = g.st.Get(id); !it.Stream.Paused || logCount(it, "paused") != 1 {
		t.Fatalf("two holds: paused %v, said paused %d times", it.Stream.Paused, logCount(it, "paused"))
	}
	for i := 0; i < 2; i++ {
		if err := g.r.Hold(id, false); err != nil {
			t.Fatalf("go on %d: %v", i+1, err)
		}
	}
	waitStarted(t, p)
	if j := p.job(1); j.Resume != "chat-x" || j.Round != 1 {
		t.Fatalf("the resumed job = round %d resume %q", j.Round, j.Resume)
	}
	it, _ = g.st.Get(id)
	if it.Stream.Paused || logCount(it, "resumed") != 1 {
		t.Fatalf("two go-ons: paused %v, said resumed %d times", it.Stream.Paused, logCount(it, "resumed"))
	}
	close(p.release)
	g.waitState(id, factory.StateLanded)
}

// A HOLD ON AN ITEM NOTHING IS RUNNING IS REFUSED, in the runner's own words.
func TestHoldOnAnItemNotRunningIsRefused(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{}, nil)
	id := g.add("one", chat("write"))
	if err := g.r.Hold(id, true); err == nil || err.Error() != "#1 is not running" {
		t.Fatalf("hold on a new item = %v", err)
	}
	if err := g.r.Hold(id, false); err == nil || err.Error() != "#1 is not running" {
		t.Fatalf("go on with a new item = %v", err)
	}
}

// logCount is how many lines of the item's log say words exactly.
func logCount(it factory.Item, words string) int {
	n := 0
	if it.Stream == nil {
		return 0
	}
	for _, l := range it.Stream.Log {
		if l.Text == words {
			n++
		}
	}
	return n
}
