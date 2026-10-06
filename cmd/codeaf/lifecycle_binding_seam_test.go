package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type anchorStub struct{ path string }

func (a *anchorStub) AnchorWorkspace(path string) (string, error) {
	a.path = path
	return path, nil
}

// THE SURFACE'S ANCHOR MOVES THE BOOT'S PROJECT TOO. [v3Seam.anchor] runs the
// agent's anchor and then keeps the boot config for later launches; a key left on
// the scratch folder there would scope every launch this window opens afterwards
// to a project it is no longer in, exactly as the agent's own stale key would.
func TestSeamAnchorMovesBootProjectKey(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	repo := t.TempDir()
	seam := &v3Seam{boot: &v3Launch{
		Workspace: "/old",
		Config:    session.Config{Workspace: "/old", MemoryProjectKey: "stale-scratch-key"},
		Place:     session.Place{Owned: true, Workspace: "/old"},
	}}
	stub := &anchorStub{}
	resolved, err := seam.anchor(stub, repo)
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if resolved != repo || stub.path != repo {
		t.Fatalf("the anchor did not name the repository: %q/%q", resolved, stub.path)
	}
	want := v3ProjectKey(repo)
	if want == "" {
		t.Fatal("the repository has no provable key")
	}
	if got := seam.boot.Config.MemoryProjectKey; got != want {
		t.Fatalf("the boot kept a stale project key: got %q want %q", got, want)
	}
	if seam.boot.Config.Workspace != repo || seam.boot.Workspace != repo {
		t.Fatalf("the boot workspace did not move: %q/%q", seam.boot.Config.Workspace, seam.boot.Workspace)
	}
	if seam.boot.Config.Place.Owned {
		t.Fatal("the boot place is still owned after an anchor")
	}
}
