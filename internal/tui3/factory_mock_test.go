package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/mock"
)

// THE MOCK FLOOR, DRIVEN END TO END THROUGH THE KEYS AND THE BEAT. New work is
// typed in, run, carried by the mock clock's own beat until it lands (with
// every question it raises on the way answered `y`), and signed off.
//
// This is the one file in internal/tui3 that imports the mock, and it is a
// test file: the surface itself never does, and internal/factory/mock's
// REMOVING.md names this file as one of the paths that go with the mock.
func TestFactoryMockFloorRunsAnItemFromWordsToShipped(t *testing.T) {
	a := placeApp(t)
	a.factory = mock.New(7, 6, 60, 6, time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC))
	a.width, a.height = 150, 44
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the mock")
	}
	if a.fp.act.beatGen == 0 {
		t.Fatal("a seam with a clock armed no beat")
	}

	drive(t, a, key("n"))
	factoryType(t, a, "fix the meter at midnight")
	drive(t, a, key("enter"))
	it, ok := a.factoryCursorItem()
	if !ok || it.Title != "fix the meter at midnight" || it.State != factory.StateNew {
		t.Fatalf("new work did not put the cursor on the new item: %+v", it.Ref())
	}
	id := it.ID

	drive(t, a, key("r"))
	if it, _ := a.factoryCursorItem(); it.ID != id || (it.State != factory.StateRunning && it.State != factory.StateQueued) {
		t.Fatalf("after r the cursor is on %s in %s", it.Ref(), it.State)
	}

	answered := 0
	for beat := 0; ; beat++ {
		if beat > 4000 {
			t.Fatalf("the item never landed: %s", a.fp.snap.Now)
		}
		it, _ := a.factoryCursorItem()
		if it.ID != id {
			t.Fatalf("the beat moved the cursor off the item to %s", it.Ref())
		}
		if it.State == factory.StateLanded {
			break
		}
		if it.State == factory.StateNeedsYou {
			drive(t, a, key("y"))
			answered++
			continue
		}
		drive(t, a, factoryBeatMsg{gen: a.fp.act.beatGen})
	}

	// A CLEAN SHEET SHIPS FROM ITS PROOF on the item page: `enter` on the row
	// opens the page on the proof, and `enter` there signs off.
	it, _ = a.factoryCursorItem()
	if factoryFirstFailed(it) == "" {
		drive(t, a, key("enter"), key("enter"))
		a.factoryCloseItem()
	} else {
		drive(t, a, key("a"))
	}
	if it, _ := a.factoryCursorItem(); it.ID != id || it.State != factory.StateShipped {
		t.Fatalf("the sign-off left %s in %s", it.Ref(), it.State)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "fix the meter at midnight") {
		t.Fatalf("the shipped item is not in the pane:\n%s", text)
	}
	t.Logf("landed and shipped after %d answers", answered)

	// AND THE BEAT STOPS WHEN THE PAGE DOES.
	a.leavePlace()
	if cmd := a.factoryBeat(a.fp.act.beatGen); cmd != nil {
		t.Fatal("the beat went on with the page closed")
	}
}
