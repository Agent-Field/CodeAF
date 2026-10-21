package syncsetup

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A chat taken where its project is not keeps a note of the folder it was
// left in, because this machine's own seals overwrite the seal's record of it.
func TestTakeRecordsTheFolderTheChatWasLeftIn(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	machine, err := preflight.OpenMachine(h.cell.Root, h.work)
	if err != nil {
		t.Fatal(err)
	}
	a := h.openObserved(h.a, h.engine, h.cell, h.work, nameA, machine.Observer())
	a.mustSay("first")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h.awayFromA()
	got, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o := session.OriginOf(workspaceOf(got.Taken.Cell.Root)); o != h.work {
		t.Fatalf("origin = %q, want the folder it was left in, %q", o, h.work)
	}
}
