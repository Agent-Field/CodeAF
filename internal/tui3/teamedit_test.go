package tui3

import (
	"errors"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// AN EDIT HERE IS A READ-MODIFY-WRITE, NOT A SAVE OF WHAT THIS WINDOW LOADED.
// The team tools a model calls write the same file from another process: a
// handle set there, a member added there, a manager made there. The interface
// then renames the team and adds a conversation, from a list it loaded before
// any of that, and every one of those writes is still on disk afterwards, and
// in the window's own memory too.
func TestTeamEditKeepsWhatAnotherProcessWrote(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithProfile(dir, nil)
	id, err := a.teamMake("harbor", []chatTab{
		{key: "k1", file: "f1", where: "/w", word: "parser work"},
		{key: "k2", file: "f2", where: "/w", word: "web front"},
	})
	if err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)

	// Another process, through the store's own door.
	if err := teamstore.Update(dir, func(f *teamstore.File) error {
		if err := f.AddMember(id, teamstore.Member{Key: "k3", File: "f3", Word: "docs pass"}); err != nil {
			return err
		}
		if err := f.SetHandle(id, "k1", "pp"); err != nil {
			return err
		}
		return f.SetManager(id, "k2")
	}); err != nil {
		t.Fatal(err)
	}

	// This window has not seen any of it, and edits.
	if err := a.teamRename(id, "dock"); err != nil {
		t.Fatal(err)
	}
	if err := a.teamAdd(id, []chatTab{{key: "k4", file: "f4", where: "/w", word: "lexer rewrite"}}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)

	check := func(where string, got team) {
		t.Helper()
		if got.Name != "dock" {
			t.Fatalf("%s: the rename was lost: %q", where, got.Name)
		}
		if got.Manager != "k2" {
			t.Fatalf("%s: the manager another process made is gone: %q", where, got.Manager)
		}
		if m, ok := got.ByHandle("pp"); !ok || m.Key != "k1" {
			t.Fatalf("%s: the handle another process set is gone: %+v", where, got.Members)
		}
		if !got.Holds("k3") {
			t.Fatalf("%s: the member another process added is gone: %+v", where, got.Members)
		}
		if m, ok := got.Member("k4"); !ok || m.Handle != "lexer" {
			t.Fatalf("%s: the conversation added here did not join with a handle: %+v", where, got.Members)
		}
	}
	disk, err := loadTeams(dir, nil)
	if err != nil || len(disk) != 1 {
		t.Fatalf("loaded %+v %v", disk, err)
	}
	check("on disk", disk[0])
	mine, _ := a.teamByID(id)
	check("in the window", mine)
}

// A CONVERSATION THAT JOINED BEFORE IT HAD A TITLE HAS NO HANDLE, and takes one
// the first time the team is written after its tab has a name. The handle is
// derived from the title and never changed afterwards by a later title.
func TestTeamUntitledMemberTakesAHandleOnceItHasATitle(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithProfile(dir, nil)
	id, err := a.teamMake("harbor", []chatTab{{key: "k1", file: "f1", word: "parser work"}})
	if err != nil {
		t.Fatal(err)
	}
	// A member with no title, as a conversation joining on its first key is.
	if err := a.teamEdit(func(f *teamstore.File) error {
		return f.AddMember(id, teamstore.Member{Key: "k2", File: "f2"})
	}); err != nil {
		t.Fatal(err)
	}
	if m, _ := a.wall.teams[0].Member("k2"); m.Handle != "" {
		t.Fatalf("an untitled member has handle %q", m.Handle)
	}
	a.chatTabs = []chatTab{{key: "k2", file: "f2", word: "lexer rewrite"}}
	if err := a.teamRecolor(id, teamHueSpec{Hue: 40, Tier: 1}); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	disk, _ := loadTeams(dir, nil)
	if m, _ := disk[0].Member("k2"); m.Handle != "lexer" || m.Word != "lexer rewrite" {
		t.Fatalf("the titled member was saved as %+v", m)
	}
	// A later title leaves the handle alone.
	a.chatTabs = []chatTab{{key: "k2", file: "f2", word: "totally different"}}
	if err := a.teamRename(id, "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	disk, _ = loadTeams(dir, nil)
	if m, _ := disk[0].Member("k2"); m.Handle != "lexer" {
		t.Fatalf("a new title moved the handle to %q", m.Handle)
	}
}

// A DISK THAT REFUSES LEAVES THE EDIT IN THE WINDOW. The change is made to what
// the window holds before the store is asked, and the store is asked off the
// loop; its refusal comes back as a note saying the change is kept for this
// window, and the person still sees what they did.
func TestTeamEditKeptInTheWindowWhenTheDiskRefuses(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithProfile(dir, nil)
	id, err := a.teamMake("harbor", []chatTab{{key: "k1", file: "f1", word: "parser work"}})
	if err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	refused := errors.New("refused")
	calls := 0
	err = a.teamEdit(func(f *teamstore.File) error {
		calls++
		if calls == 2 {
			return refused
		}
		i := teamIndex(f.Teams, id)
		f.Teams[i].Name = "dock"
		return nil
	})
	if err != nil {
		t.Fatalf("the window's own change was refused: %v", err)
	}
	teamsFlush(t, a)
	if got := lastNote(t, a); !strings.Contains(got, "kept for this window") || !strings.Contains(got, "refused") {
		t.Fatalf("the refusal was said as %q", got)
	}
	if got, _ := a.teamByID(id); got.Name != "dock" {
		t.Fatalf("the window lost the edit: %q", got.Name)
	}
	if disk, _ := loadTeams(dir, nil); disk[0].Name != "harbor" {
		t.Fatalf("a refused edit reached the disk: %q", disk[0].Name)
	}
}
