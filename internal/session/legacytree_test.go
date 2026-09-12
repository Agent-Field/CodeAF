package session

import (
	"os"
	"path/filepath"
	"testing"
)

// THE COPIES AN OLDER BUILD LEFT IN PEOPLE'S REPOSITORIES ARE STILL REACHABLE.
//
// Every build before 2026-09-12 cut a `git worktree` in the person's own
// repository for each folder they attached, and wrote it into that
// conversation's meta.json under `trees`. Deleting the field outright would
// have made every one of those registrations — and its `chat/…` branch —
// permanent litter in somebody's project, which is the exact failure the wave
// that removed the copy was closing. [Meta.TreesLegacy] is what the sweep still
// reads to take them back, and this is the law that it survives a read and a
// write.
func TestAnOldConversationsCopiesAreStillReadAndKept(t *testing.T) {
	dir := t.TempDir()
	written := `{"id":"s1","trees":[{"folder":"/code/app","dir":"/state/trees/folder-app-a1","root":"/code/app","branch":"chat/app-a1","wrote":["x.go"]}]}`
	if err := os.WriteFile(filepath.Join(dir, placeMeta), []byte(written), 0o600); err != nil {
		t.Fatalf("writing the old meta: %v", err)
	}

	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(meta.TreesLegacy) != 1 {
		t.Fatalf("an old conversation's copies read back as %+v — the sweep can no longer reach them", meta.TreesLegacy)
	}
	if got := meta.TreesLegacy[0]; got.Dir != "/state/trees/folder-app-a1" || got.Root != "/code/app" || got.Branch != "chat/app-a1" {
		t.Fatalf("the record reads back as %+v, want the three facts taking it back needs", got)
	}

	// AND A STAMP DOES NOT THROW IT AWAY. Every stamp is a read-patch-save of the
	// whole file, so a field the patch never touches has to survive the trip or
	// the first thing an old conversation does on this build is lose the record
	// of the copy it is still holding.
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	again, err := LoadMeta(dir)
	if err != nil || len(again.TreesLegacy) != 1 {
		t.Fatalf("after one stamp the copies read back as %+v (%v)", again.TreesLegacy, err)
	}
}

// AND NOTHING IN THIS BUILD EVER WRITES ONE. A conversation opened today holds
// no copy of anything, so its meta.json carries no `trees` key at all — which is
// what makes the field deletable the day no file on any machine still has one.
func TestAConversationOnThisBuildWritesNoCopies(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	folder := canonicalPath(t.TempDir())
	if _, err := agent.ReferPlace(folder, PlaceSaid); err != nil {
		t.Fatalf("ReferPlace: %v", err)
	}
	if meta := agent.fillMetaLocked(Meta{}); len(meta.TreesLegacy) != 0 {
		t.Fatalf("this build stamped copies: %+v", meta.TreesLegacy)
	}
}
