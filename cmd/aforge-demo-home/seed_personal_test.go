package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE PERSONAL FIXTURE IS READ BACK BY THE READERS THE SURFACE USES, rooted at
// the directory it was given as AFORGE_HOME: three named conversations whose
// workspace is inside that root, and one finished piece of work joined to a
// conversation that is really there.
func TestThePersonalFixtureIsReadBackFromItsOwnStateRoot(t *testing.T) {
	state := filepath.Join(t.TempDir(), "profile")
	manifest, err := seedPersonal(state, time.Now())
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	world := session.ReadWorld(filepath.Join(state, "v3", "projects"))
	rows := world.Sessions()
	if len(rows) != len(personalConversations) {
		t.Fatalf("the fixture reads back %d conversations, wrote %d", len(rows), len(personalConversations))
	}
	byID := map[string]session.SessionRow{}
	for _, row := range rows {
		if row.Title == "" || !strings.HasPrefix(row.Workspace, state) {
			t.Fatalf("a fixture conversation reads back as %+v", row)
		}
		byID[row.ID] = row
	}
	for _, id := range []string{manifest.Shared, manifest.Roadmap, manifest.Unfiled} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("the manifest names %s, which is not on the disk", id)
		}
	}
	tasks := session.ReadTaskIndex(filepath.Join(byID[manifest.Roadmap].Dir, "..", "tasks.jsonl"))
	if len(tasks) != 1 || tasks[0].SessionID != manifest.Roadmap || tasks[0].ID != manifest.TaskID {
		t.Fatalf("the fixture's work reads back as %+v", tasks)
	}
	for _, path := range []string{manifest.Spec, manifest.Report} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the fixture file %s is missing: %v", path, err)
		}
	}
	// AND THE DOOR REFUSES A ROOT THAT ALREADY HOLDS SOMETHING, so a mistyped
	// --into can never write fixture conversations into a person's real profile.
	if err := runPersonal(state, time.Now()); err == nil {
		t.Fatal("the personal fixture wrote over a directory that already held one")
	}
}
