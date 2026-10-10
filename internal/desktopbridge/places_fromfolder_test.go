package desktopbridge

import (
	"os"
	"path/filepath"
	"testing"
)

type folderAnswer struct {
	Mutation
	Result FolderResult `json:"result"`
}

func TestFromFolderRouteMakesTheRepoPlaceOnceAndOffersUnfiledChats(t *testing.T) {
	rig := newPlacesRig(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "now")
	sub := filepath.Join(repo, "web")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := row("c-in", "in", placesEpoch)
	inside.Workspace = sub
	filed := row("c-filed", "filed", placesEpoch)
	filed.Workspace = repo
	away := row("c-away", "away", placesEpoch)
	away.Workspace = base
	rig.setRows(inside, filed, away)
	other := rig.mk("Other")
	if code := rig.do("POST", "/places/"+other+"/members", map[string]any{"chats": []string{"c-filed"}}, nil); code != 200 {
		t.Fatalf("filing: %d", code)
	}

	var first folderAnswer
	if code := rig.do("POST", "/places/from-folder", map[string]any{"path": sub}, &first); code != 200 {
		t.Fatalf("from-folder: %d", code)
	}
	if !first.Result.Created || first.Place == nil || first.Place.Name != "now" || len(first.Result.MatchingChats) != 1 || first.Result.MatchingChats[0] != "c-in" {
		t.Fatalf("first drop: %+v", first)
	}
	var second folderAnswer
	rig.do("POST", "/places/from-folder", map[string]any{"path": repo}, &second)
	if second.Result.Created || second.Place == nil || second.Place.ID != first.Place.ID || !second.Noop {
		t.Fatalf("second drop: %+v", second)
	}
}

func TestFromFolderRouteRefusesAMissingOrEmptyPath(t *testing.T) {
	rig := newPlacesRig(t)
	var e apiError
	if code := rig.do("POST", "/places/from-folder", map[string]any{"path": " "}, &e); code != 400 {
		t.Fatalf("empty: %d", code)
	}
	if code := rig.do("POST", "/places/from-folder", map[string]any{"path": filepath.Join(t.TempDir(), "gone")}, &e); code != 422 {
		t.Fatalf("missing: %d %+v", code, e)
	}
}
