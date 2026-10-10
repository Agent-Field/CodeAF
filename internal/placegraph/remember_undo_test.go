package placegraph

import (
	"path/filepath"
	"testing"
)

func TestRememberUndoPreservesOtherWritesAndRefusesEditedLines(t *testing.T) {
	graph, err := Open(Options{Path: filepath.Join(t.TempDir(), "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := graph.CreatePlace(NewPlace{Name: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	line, _, err := graph.AddLine(Line{PlaceID: p.ID, Text: "Run tests", Source: LineSource{Kind: LineSaidInChat, ChatID: "chat"}})
	if err != nil {
		t.Fatal(err)
	}
	token := RememberUndoToken(line)
	keep, _, err := graph.AddLine(Line{PlaceID: p.ID, Text: "Other window's work", Source: LineSource{Kind: LineYouWrote}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.UndoRemember(token); err != nil {
		t.Fatal(err)
	}
	snap, _ := graph.Snapshot()
	if got := snap.Knowledge(p.ID); len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("Undo lost another write: %+v", got)
	}
	line.ID = ""
	line, _, err = graph.AddLine(line)
	if err != nil {
		t.Fatal(err)
	}
	token = RememberUndoToken(line)
	line.Text = "Edited later"
	if _, err := graph.UpdateLine(line); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.UndoRemember(token); err != ErrRevisionConflict {
		t.Fatalf("edited line Undo: %v", err)
	}
	if _, err := graph.UndoRemember(RememberUndoToken(keep)); err != ErrRevisionConflict {
		t.Fatalf("manually authored line Undo: %v", err)
	}
}
