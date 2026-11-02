package cellstore

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Contract E1 follows the latest spoken boundary despite later bookkeeping appends.
func TestSealRecoveryCoversTheTurnBeforeLaterBookkeeping(t *testing.T) {
	c := newCell(t)
	path := rel(c, cell.TranscriptPath)
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("first answer\n")
	var watch SealWatch
	watch.TrackTurn(c.Root)
	watch.Report(errors.New("first seal failed"))
	watch.Take()
	watch.TurnStarted()
	write("first answer\nlatest answer\n")
	watch.Wrote()
	watch.TurnEnded()
	engine := (&fakeEngine{}).engine(t)
	_, err := engine.Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	write("first answer\nlatest answer\nbookkeeping\n")
	watch.Report(nil)
	if watch.Failing() || !strings.Contains(watch.Take(), "sealing works again") {
		t.Fatal("later append hid the saved turn's recovery")
	}
	watch.TurnStarted()
	write("first answer\nlatest answer\nbookkeeping\nunsaved answer\n")
	watch.Wrote()
	watch.Report(errors.New("next seal failed"))
	watch.Take()
	watch.Report(nil)
	if !watch.Failing() || watch.Take() != "" {
		t.Fatal("an older seal claimed the latest answer was saved")
	}
}

// Contract E1 keeps an active turn from borrowing an older saved turn's recovery.
func TestSealRecoveryWaitsForTheActiveTurnToFinish(t *testing.T) {
	c := newCell(t)
	path := rel(c, cell.TranscriptPath)
	if err := os.WriteFile(path, []byte("previous answer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := (&fakeEngine{}).engine(t)
	if _, err := engine.Seal(t.Context(), c, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	var watch SealWatch
	watch.TrackTurn(c.Root)
	watch.TurnStarted()
	watch.Report(errors.New("current turn seal failed"))
	if !watch.Failing() {
		t.Fatal("an active turn borrowed the previous turn's saved head")
	}
	watch.Take()
	if err := os.WriteFile(path, []byte("previous answer\ncurrent answer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	watch.Wrote()
	if _, err := engine.Seal(t.Context(), c, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	watch.Report(nil)
	watch.TurnEnded()
	if watch.Failing() || !strings.Contains(watch.Take(), "sealing works again") {
		t.Fatal("the finished saved turn did not recover")
	}
}
