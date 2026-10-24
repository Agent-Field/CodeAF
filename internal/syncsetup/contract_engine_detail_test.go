package syncsetup

import (
	"context"
	"testing"
)

// TestC4RealEnginePublishesTheLocalHead proves C4: moving a chat preserves its history and its single holder.
func TestC4RealEnginePublishesTheLocalHead(t *testing.T) {
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("one")
	if err := a.drive.Close(context.Background()); err != nil {
		t.Fatalf("publish: %+v", err)
	}
	v, err := h.a.Dir.Cell(context.Background(), h.cell.ID)
	if err != nil {
		t.Fatalf("directory: %v", err)
	}
	if v.Cell.Head != h.head() {
		t.Fatalf("heads differ relay=%s local=%s", v.Cell.Head, h.head())
	}
}
