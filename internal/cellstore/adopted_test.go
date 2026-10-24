package cellstore

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func adoptingCell(t *testing.T) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	if err := appendTurn(c, newTurn("snap-1", "", time.UnixMilli(1), TurnInfo{}, "r1", Identity{})); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(rel(c, ReceiptsDir+"/r1.json"), []byte(`{"V":1}`)); err != nil {
		t.Fatal(err)
	}
	return c
}

// A head this device adopted is the head, its parent is the newest line of the
// log the chat carried, and the log shows it as one entry sealed elsewhere.
func TestAdoptedHeadIsTheHeadUntilTheNextSeal(t *testing.T) {
	c := adoptingCell(t)
	if err := noteAdopted(c, "snap-2"); err != nil {
		t.Fatal(err)
	}
	if err := AdoptedFrom(c, "spark"); err != nil {
		t.Fatal(err)
	}
	head, err := Head(c)
	if err != nil || head.Turn.ID != "snap-2" || head.Turn.Parent != "snap-1" {
		t.Fatalf("Head = %+v, %v; want the adopted head on top of the log", head, err)
	}
	entries, err := Log(c)
	if err != nil || len(entries) == 0 || !entries[0].IsAdopted || entries[0].Adopted != "spark" || entries[0].Turn.ID != "snap-2" {
		t.Fatalf("Log = %+v, %v; want the adopted head first, sealed on spark", entries, err)
	}
	if turns, _ := Turns(c); len(turns) != 1 {
		t.Fatalf("a turn record was invented: %+v", turns)
	}

	if err := appendTurn(c, newTurn("snap-3", "snap-2", time.UnixMilli(2), TurnInfo{}, "r3", Identity{})); err != nil {
		t.Fatal(err)
	}
	if _, ok := Adopted(c); ok {
		t.Fatal("an adopted head still counts after a seal")
	}
}
