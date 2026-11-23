package syncsetup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestContinuingAChatInsideAGitDiscoveryBoundaryRestoresItsFilesInAnotherHome(t *testing.T) {
	fixture, err := os.MkdirTemp(".", "continue-ignored-")
	mustNil(t, err)
	fixture, err = filepath.Abs(fixture)
	mustNil(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(fixture) })
	ignored := filepath.Join(fixture, "work")
	mustNil(t, os.Mkdir(ignored, 0o700))
	mustNil(t, os.WriteFile(filepath.Join(ignored, "ignored.txt"), []byte("project files stay here"), 0o600))
	// The enclosing repository ignores work/, but Git cannot discover it here.
	h := newTwoHomes(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", fixture+string(os.PathListSeparator)+filepath.Dir(h.rootsB))
	h.awayFromA()
	h.work, h.engine.Workspace = ignored, ignored
	h.backToA()
	h.engine.Transport = nil
	h.engB.Transport = nil
	journal := transcriptOf(h.cell)
	header, err := os.ReadFile(journal)
	mustNil(t, err)
	mustNil(t, os.WriteFile(journal, bytes.Replace(header, []byte(`"version":3`), []byte(`"version":1`), 1), 0o600))
	h.spokenFixture()
	observer, err := preflight.OpenMachine(h.cell.Root, h.work)
	mustNil(t, err)
	a := h.openObserved(h.a, h.engine, h.cell, h.work, nameA, observer.Observer())
	a.mustSay("words kept on the first computer")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(t.Context()))
	c := h.continuerB()
	c.opt.BucketFor = func(origin string) string { return filepath.Join(h.rootsB, "copy-"+filepath.Base(origin)) }
	got, err := c.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	_, err = cell.OpenAt(got.Taken.Cell.Root, h.cell.ID)
	mustNil(t, err)
	words, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	mustNil(t, err)
	if !bytes.Contains(words, []byte("words kept on the first computer")) {
		t.Fatalf("the continued chat lost its words: %s", words)
	}
	if filepath.Base(filepath.Dir(got.Taken.Cell.Root)) != "copy-"+filepath.Base(h.work) {
		t.Fatalf("the continued chat was filed in %s", got.Taken.Cell.Root)
	}
	if content, err := os.ReadFile(filepath.Join(workspaceOf(got.Taken.Cell.Root), "ignored.txt")); err != nil || string(content) != "project files stay here" {
		t.Fatalf("the separate project's file was not copied: %q, %v", content, err)
	}
	b := h.openOn(h.b, h.engB, got.Taken.Cell, workspaceOf(got.Taken.Cell.Root), nameB)
	b.mustSay("words added on the second computer")
	mustNil(t, b.drive.Close(t.Context()))
}

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
			if meta.FromDevice != nameA || meta.FromWorkspace != origin || meta.Origin != origin || !meta.Owned || workspaceOf(at.Root) != filepath.Join(at.Root, "work") {
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
