package standing

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// isolatedTask is a waking task whose approval card promised a separate Git
// worktree.
func isolatedTask(words string, moment time.Time) Item {
	return Item{
		Words:     words,
		Workspace: "/tmp/project",
		When:      When{Kind: WhenAt, Words: words, At: moment},
		Does:      Action{Kind: ActionTask, Brief: "fix the vet warnings", Isolate: true},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
}

// schemaOnDisk is the version an older build reads before deciding whether it
// understands the document at all.
func schemaOnDisk(t *testing.T, store *Store, id string) int {
	t.Helper()
	raw, err := os.ReadFile(store.ItemPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		t.Fatal(err)
	}
	return head.Schema
}

// AN OLDER BUILD MUST NOT FIRE AN ISOLATED ORDER IN THE PERSON'S CHECKOUT.
// Every build before isolation skips a document whose schema is newer than 1,
// and codeaf, devaf and stageaf read the same store, so the version on disk is
// the only thing standing between the approval card's promise and a commit on
// the person's branch. An ordinary order must stay readable by those builds.
func TestAnIsolatedOrderIsWrittenBeyondTheReachOfOlderBuilds(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	isolated, err := store.Create(isolatedTask("every night fix the vet warnings", now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ordinary, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := schemaOnDisk(t, store, isolated.ID); got <= 1 {
		t.Fatalf("an isolated order was written at schema %d, which a build without isolation reads and fires in the checkout", got)
	}
	if got := schemaOnDisk(t, store, ordinary.ID); got != 1 {
		t.Fatalf("an ordinary order was written at schema %d, which hides it from older builds for no reason", got)
	}

	// THIS BUILD STILL READS AND LISTS BOTH.
	got, err := store.Get(isolated.ID)
	if err != nil {
		t.Fatalf("this build cannot read its own isolated order: %v", err)
	}
	if !got.Does.Isolate {
		t.Fatal("the isolated order lost its isolation on the way back")
	}
	items, err := store.List()
	if err != nil || len(items) != 2 {
		t.Fatalf("list answered %d items (%v), wanted both", len(items), err)
	}

	// EVERY LATER WRITE KEEPS THE VERSION THE ITEM NEEDS: a save, a thinking
	// level, and a save that turns isolation off.
	store.clock = held(now.Add(time.Minute))
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	if v := schemaOnDisk(t, store, isolated.ID); v <= 1 {
		t.Fatalf("a save wrote the isolated order back at schema %d", v)
	}
	if err := store.SetStandingEffort(isolated.ID, ""); err != nil {
		t.Fatal(err)
	}
	if v := schemaOnDisk(t, store, isolated.ID); v <= 1 {
		t.Fatalf("setting its thinking level wrote the isolated order back at schema %d", v)
	}
	got.Does.Isolate = false
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	// IT NO LONGER NEEDS VERSION 2 FOR ISOLATION, but it has been rewritten by
	// this build, so it carries the guarded-write revision a baseline reader
	// would silently reset: the version is the deferred-delivery barrier's now,
	// not isolation's.
	if v := schemaOnDisk(t, store, isolated.ID); v != schemaDeferred {
		t.Fatalf("a rewritten order was written at schema %d, not the deferred barrier", v)
	}
}
