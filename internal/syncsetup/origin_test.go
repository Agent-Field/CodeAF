package syncsetup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// TestC5ArrivingCopiesKeepTheirOriginAndAUsableWorkspace proves C5: a first
// arrival is named for its origin and opens its own copy, even on a machine
// that happens to have a folder with the origin's name.
func TestC5ArrivingCopiesKeepTheirOriginAndAUsableWorkspace(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run(fmt.Sprint("origin-present-", present), func(t *testing.T) {
			h := newTwoHomes(t)
			observer, err := preflight.OpenMachine(h.cell.Root, h.work)
			if err != nil {
				t.Fatal(err)
			}
			appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"user","content":"first arrival","timestamp":"2026-10-21T10:00:00Z"}`+"\n")
			a := h.openObserved(h.a, h.engine, h.cell, h.work, nameA, observer.Observer())
			a.mustSay("first arrival")
			if err := a.drive.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			origin := h.work
			if !present {
				if err := os.Rename(h.work, h.work+".away"); err != nil {
					t.Fatal(err)
				}
			}
			c := h.continuerB()
			c.opt.BucketFor = func(from string) string { return filepath.Join(h.rootsB, "copy-"+filepath.Base(from)) }
			got, err := c.Take(context.Background(), h.cell.ID)
			if err != nil {
				t.Fatal(err)
			}
			at := got.Taken.Cell
			meta, err := session.LoadMeta(at.Root)
			if err != nil {
				t.Fatal(err)
			}
			if meta.Origin != origin || !meta.Owned || workspaceOf(at.Root) != filepath.Join(at.Root, "work") {
				t.Fatalf("arrived metadata %+v, workspace %s", meta, workspaceOf(at.Root))
			}
			if filepath.Base(filepath.Dir(at.Root)) != "copy-"+filepath.Base(origin) {
				t.Fatalf("copy filed under %s", at.Root)
			}
			world := session.ReadWorld(h.rootsB)
			if len(world.Projects) != 1 || !world.Projects[0].Moved || world.Projects[0].Name != filepath.Base(origin)+session.MovedCopyWord {
				t.Fatalf("copy project: %+v", world.Projects)
			}
		})
	}
}
