package tui3

import (
	"os"
	"path/filepath"
	"testing"
)

// A PIN NAMES THE FOLDER THE LINK REACHES, NOT THE SPELLING THAT WAS TYPED.
// The typed /project regression for issue #1761: a path taken through a symlink
// used to pin the alias, so home held one project under the link's name while a
// launch from the resolved path held another — two projects, two histories, two
// engines for one folder. The pin now resolves before it lands.
func TestProjectPinResolvesASymlinkedSpelling(t *testing.T) {
	a, _, root := mixedLab(t)
	runCmd(a.openHome())

	real := filepath.Join(root, "realrepo")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linkedrepo")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	runCmd(a.homeSlash("/project " + link))

	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if a.target.where != resolved {
		t.Fatalf("/project through a symlink pinned %q, want the folder it reaches %q", a.target.where, resolved)
	}

	// And the folder's own spelling pins the same place, since one folder is
	// one project.
	runCmd(a.homeSlash("/project " + real))
	if a.target.where != resolved {
		t.Fatalf("/project from the target pinned %q, want %q", a.target.where, resolved)
	}
	if a.home.msg != "" {
		t.Fatalf("a taken path wrote %q over home's keys row", a.home.msg)
	}
}
