package standing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// held is the clock every test in this package runs on: nothing here is allowed
// to depend on how long it took to run.
func held(moment time.Time) func() time.Time { return func() time.Time { return moment } }

// openStore is a store in a temp directory with a clock a test owns.
func openStore(t *testing.T, moment time.Time) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "standing"))
	if err != nil {
		t.Fatalf("cannot open a store: %v", err)
	}
	store.clock = held(moment)
	return store
}

// reminder is the smallest complete item: words, a workspace, a moment, a line
// to say, and rails.
func reminder(words string, moment time.Time) Item {
	return Item{
		Words:     words,
		Workspace: "/tmp/project",
		When:      When{Kind: WhenAt, Words: words, At: moment},
		Does:      Action{Kind: ActionSay, Say: "leave now"},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
}

func TestCreateStampsAnItemAndWritesIt(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	made, err := store.Create(reminder("remind me at 6 to leave", now.Add(8*time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(made.ID) != 16 {
		t.Fatalf("the id is %q, wanted sixteen hex characters", made.ID)
	}
	if made.Status != StatusActive {
		t.Fatalf("a fresh item is %q, wanted active", made.Status)
	}
	if !made.Created.Equal(now) || !made.Updated.Equal(now) {
		t.Fatalf("the stamps are %s and %s, wanted %s", made.Created, made.Updated, now)
	}
	if made.Schema != Schema {
		t.Fatalf("the schema is %d, wanted %d", made.Schema, Schema)
	}
	if !made.NextDue.Equal(now.Add(8 * time.Hour)) {
		t.Fatalf("the next moment is %s, wanted the moment asked for", made.NextDue)
	}

	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if back.Words != made.Words || back.Does.Say != "leave now" || back.Rails.MaxPerDay != 3 {
		t.Fatalf("the document came back changed: %+v", back)
	}
	if _, err := os.Stat(store.ItemPath(made.ID)); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
}

func TestCreateComputesTheNextMomentForEveryKindThatHasOne(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	rhythm := reminder("every Monday at 9, draft the update", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Words: "every Monday at 9", Every: "0 9 * * 1"}
	made, err := store.Create(rhythm)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	if !made.NextDue.Equal(want) {
		t.Fatalf("a rhythm's first moment is %s, wanted %s", made.NextDue, want)
	}

	watch := reminder("tell me when CI goes red", time.Time{})
	watch.When = When{Kind: WhenProbe, Probe: Probe{Command: "gh run list"}, ProbeEvery: 10 * time.Minute}
	made, err = store.Create(watch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !made.NextDue.Equal(now) {
		t.Fatalf("a watch's first look is %s, wanted now — a watch starts watching at once", made.NextDue)
	}
}

func TestCreateRefusesAnItemWithoutRails(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	bare := reminder("remind me", now)
	bare.Rails = Rails{}
	if _, err := store.Create(bare); err == nil {
		t.Fatal("an item with no rails was accepted")
	}
	if entries, _ := os.ReadDir(store.Root()); len(entries) != 0 {
		t.Fatalf("a refused item left %d files behind", len(entries))
	}
}

func TestSaveRewritesAndRestamps(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6", now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	later := now.Add(30 * time.Minute)
	store.clock = held(later)
	made.LastCheckLine = "nothing has changed"
	made.Runs = 2
	if err := store.Save(made); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !back.Updated.Equal(later) {
		t.Fatalf("save stamped %s, wanted %s", back.Updated, later)
	}
	if !back.Created.Equal(now) {
		t.Fatalf("save moved Created to %s", back.Created)
	}
	if back.Runs != 2 || back.LastCheckLine != "nothing has changed" {
		t.Fatalf("the rewrite lost something: %+v", back)
	}
}

func TestListIsNewestFirstAndSkipsWhatItCannotRead(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	store.clock = held(now)
	oldest, err := store.Create(reminder("the oldest", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	middle, err := store.Create(reminder("the middle", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(2 * time.Minute))
	newest, err := store.Create(reminder("the newest", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}

	// One document from a build that has not been written yet, and one that is
	// simply broken. NEITHER MAY STOP THE OTHERS FROM BEING LISTED.
	future := reminder("from the future", now.Add(time.Hour))
	future.ID = "ffffffffffffffff"
	future.Schema = Schema + 1
	future.Created = now.Add(3 * time.Minute)
	raw, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath(future.ID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath("eeeeeeeeeeeeeeee"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("list answered %d items, wanted the three readable ones", len(items))
	}
	if items[0].ID != newest.ID || items[1].ID != middle.ID || items[2].ID != oldest.ID {
		t.Fatalf("list is not newest first: %s %s %s", items[0].Words, items[1].Words, items[2].Words)
	}
	if _, err := store.Get(future.ID); err == nil {
		t.Fatal("a newer document was read as though this build understood it")
	} else if !strings.Contains(err.Error(), "newer aforge") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

func TestForWorkspaceGroupsByProject(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	here := reminder("in this project", now.Add(time.Hour))
	here.Workspace = "/home/someone/work/aforge"
	if _, err := store.Create(here); err != nil {
		t.Fatal(err)
	}
	elsewhere := reminder("somewhere else", now.Add(time.Hour))
	elsewhere.Workspace = "/home/someone/work/other"
	if _, err := store.Create(elsewhere); err != nil {
		t.Fatal(err)
	}

	// A trailing separator is the same project, because a path is a place and
	// not a string.
	items, err := store.ForWorkspace("/home/someone/work/aforge/")
	if err != nil {
		t.Fatalf("for workspace: %v", err)
	}
	if len(items) != 1 || items[0].Words != "in this project" {
		t.Fatalf("the grouping answered %d items: %+v", len(items), items)
	}
}

func TestGetIsNotFoundForAnIdThatIsNotHere(t *testing.T) {
	store := openStore(t, time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	if _, err := store.Get("0123456789abcdef"); err != ErrNotFound {
		t.Fatalf("a missing item answered %v, wanted ErrNotFound", err)
	}
	if _, err := store.Get("../escape"); err != ErrNotFound {
		t.Fatalf("an id that is a path answered %v, wanted ErrNotFound", err)
	}
}

func TestLogWritesOneLinePerEvent(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Log(made.ID, "stopped by you"); err != nil {
		t.Fatalf("log: %v", err)
	}
	if err := store.Log(made.ID, "said: leave now\nand a second line"); err != nil {
		t.Fatalf("log: %v", err)
	}
	raw, err := os.ReadFile(store.LogPath(made.ID))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the log has %d lines: %q", len(lines), string(raw))
	}
	if !strings.HasPrefix(lines[0], now.Format(time.RFC3339)) {
		t.Fatalf("a log line does not lead with when it happened: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "said: leave now and a second line") {
		t.Fatalf("a log line was not flattened: %q", lines[1])
	}
}
