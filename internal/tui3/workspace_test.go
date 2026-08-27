package tui3

import "testing"

func TestWorkspaceCommandMovesTheLiveSurfaceToTheResolvedProject(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.owned = true
	a.anchorWorkspace = func(path string) (string, error) {
		if path != "~/code/project" {
			t.Fatalf("anchor received %q", path)
		}
		return "/Users/me/code/project", nil
	}
	a.slash("/workspace ~/code/project")
	if a.workspace != "/Users/me/code/project" || a.owned {
		t.Fatalf("surface workspace = %q, owned = %v", a.workspace, a.owned)
	}
	if got := lastNote(t, a); got != "workspace · /Users/me/code/project" {
		t.Fatalf("note = %q", got)
	}
	if a.anchorWorkspace != nil {
		t.Fatal("the one-shot command remained available after anchoring")
	}
}
