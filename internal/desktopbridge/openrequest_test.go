package desktopbridge

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// referrer is the rig's agent plus the one door a place chat refers folders through.
type referrer struct {
	*usingAgent
	mu    sync.Mutex
	asked []string
	how   []session.PlaceArrival
}

func (a *referrer) ReferPlace(path string, how session.PlaceArrival) (session.PlaceRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, path)
	a.how = append(a.how, how)
	return session.PlaceRef{Path: path, Arrival: how}, nil
}

func newReferringRig(t *testing.T) (*folderRig, *referrer) {
	rig := newFolderRig(t)
	agent := &referrer{usingAgent: rig.agent}
	rig.b.UseOpenIn(func(workspace, file string) (Connection, error) {
		return Connection{Agent: agent, Welcome: remote.Welcome{SessionFile: rig.file, Workspace: workspace, Model: Model, Persistent: true, Launch: rig.launch}, Local: true, Close: func() {}}, nil
	})
	return rig, agent
}

func threeFolders(t *testing.T) (a, b, c string) {
	root := t.TempDir()
	a, b, c = filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	for _, d := range []string{a, b, c} {
		os.MkdirAll(d, 0o755)
	}
	for i, d := range []*string{&a, &b, &c} {
		real, _ := filepath.EvalSymlinks(*d)
		*d = real
		_ = i
	}
	return
}

func TestANewChatInAPlaceOpensInItsFirstFolder(t *testing.T) {
	rig, _ := newReferringRig(t)
	a, b, _ := threeFolders(t)
	place := rig.placeWith("P", folder(t, a), folder(t, b))
	snap, code, _ := rig.snapshotOf(map[string]any{"placeId": place})
	if code != 200 || snap.Workspace != a {
		t.Fatalf("code %d workspace %q want %q", code, snap.Workspace, a)
	}
}

func TestOtherFolderSourcesAreReferredNotMovedInto(t *testing.T) {
	rig, agent := newReferringRig(t)
	a, b, c := threeFolders(t)
	place := rig.placeWith("P", folder(t, a), folder(t, b), folder(t, c))
	snap, code, _ := rig.snapshotOf(map[string]any{"placeId": place})
	if code != 200 || snap.Workspace != a {
		t.Fatalf("code %d workspace %q", code, snap.Workspace)
	}
	if len(agent.asked) != 2 || agent.asked[0] != b || agent.asked[1] != c {
		t.Fatalf("referred %v, want [%s %s]", agent.asked, b, c)
	}
	for _, how := range agent.how {
		if how != session.PlaceSaid {
			t.Fatalf("arrival %q, want said", how)
		}
	}
}

func TestTheChatJoinsThePlaceAsYou(t *testing.T) {
	rig, _ := newReferringRig(t)
	a, _, _ := threeFolders(t)
	place := rig.placeWith("P", folder(t, a))
	rig.snapshotOf(map[string]any{"placeId": place})
	state, _ := rig.p.Store.Snapshot()
	ms := state.PlacesOf(rig.chat)
	if len(ms) != 1 || ms[0].PlaceID != place || ms[0].AddedBy != placegraph.AddedByYou {
		t.Fatalf("memberships %+v", ms)
	}
}

func TestAMissingFolderFallsBackToTheDefaultWorkspace(t *testing.T) {
	rig, agent := newReferringRig(t)
	a, _, _ := threeFolders(t)
	place := rig.placeWith("P", folder(t, a))
	os.RemoveAll(a)
	_, code, _ := rig.snapshotOf(map[string]any{"placeId": place})
	if code != 200 || rig.opens.Load() != 1 || len(agent.asked) != 0 {
		t.Fatalf("code %d default opens %d referred %v", code, rig.opens.Load(), agent.asked)
	}
}

func TestARendererPathIsNeverAccepted(t *testing.T) {
	rig, _ := newReferringRig(t)
	a, _, _ := threeFolders(t)
	place := rig.placeWith("P", folder(t, a))
	for _, key := range []string{"workspace", "path", "folder"} {
		_, code, _ := rig.snapshotOf(map[string]any{"placeId": place, key: t.TempDir()})
		if code != 400 {
			t.Fatalf("%s accepted: %d", key, code)
		}
	}
	if len(rig.foldersAsked()) != 0 {
		t.Fatalf("an engine was opened for a rejected body: %v", rig.foldersAsked())
	}
}

func TestAPlaceIdBesideASessionFileIsRefused(t *testing.T) {
	rig, _ := newReferringRig(t)
	a, _, _ := threeFolders(t)
	place := rig.placeWith("P", folder(t, a))
	_, code, _ := rig.snapshotOf(map[string]any{"placeId": place, "sessionFile": rig.file})
	if code != 400 {
		t.Fatalf("code %d", code)
	}
}
