package desktopbridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitFixture creates real repository metadata because an empty .git folder
// must not claim every folder below it as part of a repository.
func initGitFixture(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "--quiet", "--initial-branch=main", path).CombinedOutput(); err != nil {
		t.Fatalf("git init fixture: %v: %s", err, out)
	}
}

func TestPlaceSourcesRejectAnEmptyGitMarker(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Folder")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	var refused apiError
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "repo", "ref": dir}, &refused); code != 422 || refused.Code != "refused_source" {
		t.Fatalf("empty marker accepted as a repository: %d %+v", code, refused)
	}
	child := mustDir(t, filepath.Join(dir, "child"))
	var added Mutation
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "folder", "ref": child}, &added); code != 200 {
		t.Fatalf("plain folder refused: %d", code)
	}
	if added.Place == nil || len(added.Place.Sources) != 1 || added.Place.Sources[0].Kind != "folder" {
		t.Fatalf("empty ancestor marker claimed the folder: %+v", added.Place)
	}
}
