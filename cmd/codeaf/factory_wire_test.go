package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// factoryDoors is every door a seam can carry, by the name [factory.Seam.Has]
// answers to.
var factoryDoors = []string{
	"load", "launch", "stop", "pause", "dismiss", "answer", "steer", "signoff",
	"sendback", "reverify", "bank", "new", "sync", "askauthor", "setstage",
	"addstage", "setgate", "setcap", "seteffort",
}

// A launch with no store behind it hands the page the zero seam, whose page is
// the one dim line saying nothing is connected yet.
func TestFactorySeamWithNoStoreIsTheZeroSeam(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	seam := factorySeam(nil)
	for _, door := range factoryDoors {
		if seam.Has(door) {
			t.Fatalf("a seam over no store carries %q", door)
		}
	}
}

// The person's own floor has the doors the local seam can keep and no engine
// door, so the page offers no launch it cannot do.
func TestFactorySeamOverAStoreIsTheLocalFloor(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st)
	for _, door := range []string{"load", "new", "dismiss", "setgate", "setstage", "setcap"} {
		if !seam.Has(door) {
			t.Fatalf("the floor over a store is missing %q", door)
		}
	}
	for _, door := range []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sync", "askauthor"} {
		if seam.Has(door) {
			t.Fatalf("the floor over a store carries the engine door %q with nothing behind it", door)
		}
	}
	snap, err := seam.Load()
	if err != nil {
		t.Fatalf("an empty store did not load: %v", err)
	}
	if len(snap.Items) != 0 {
		t.Fatalf("an empty store loaded %d items", len(snap.Items))
	}
	// A floor with no items has no repos, so the store refuses work on none,
	// and the window's own workspace is the answer the page's `n` falls to.
	if _, err := seam.New("", "fix the ledger double count"); err == nil {
		t.Fatal("the store took work on no repository")
	}
	seam = factoryHere(seam, "/home/you/ledger")
	// And what `n` writes is what the next load reads back, on that repo.
	id, err := seam.New("", "fix the ledger double count, $8, plan first")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	snap, err = seam.Load()
	if err != nil || len(snap.Items) != 1 || snap.Items[0].ID != id {
		t.Fatalf("the item `n` made is not on the next load: %+v, %v", snap.Items, err)
	}
	if got := snap.Items[0].Repo; got != "ledger" {
		t.Fatalf("new work on an empty floor landed on %q, want the workspace's name", got)
	}
	// A repo the floor named is kept.
	if _, err := seam.New("acme/api", "tidy the logs"); err != nil {
		t.Fatalf("new on a named repo: %v", err)
	}
	snap, _ = seam.Load()
	repos := map[string]bool{}
	for _, it := range snap.Items {
		repos[it.Repo] = true
	}
	if !repos["acme/api"] {
		t.Fatalf("a named repo was overwritten by the workspace: %v", repos)
	}
}

// The fixture switch still wins over a store, so the page can be looked at
// with a full floor on a machine whose own floor is empty.
func TestFactoryFixtureWinsOverAStore(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "1")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st)
	if seam.Has("new") {
		t.Fatal("the fixture switch was on and the store's own `new` door answered")
	}
	snap, err := seam.Load()
	if err != nil || len(snap.Items) == 0 {
		t.Fatalf("the fixture switch was on and the floor is not the fixture: %d items, %v", len(snap.Items), err)
	}
}

// A store that could not be opened is a nil door, spelled as the interface's
// own nil, so a typed-nil pointer never puts `factory_add` on the belt.
func TestFactoryDoorOfANilStoreIsNil(t *testing.T) {
	if door := factoryDoor(nil); door != nil {
		t.Fatalf("a nil store became a non-nil door: %#v", door)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if factoryDoor(st) == nil {
		t.Fatal("an open store became no door")
	}
}

// The store lives in the v3 factory folder under the codeaf home, which is
// what the manual tells a person who asks where their items are saved.
func TestFactoryStoreIsUnderTheCodeafHome(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	st := v3Factory()
	if st == nil {
		t.Fatal("the factory store did not open under a writable home")
	}
	if got, want := st.Root(), home.Join("v3", "factory"); got != want {
		t.Fatalf("the factory store is at %q, want %q", got, want)
	}
}

// THE TOOL WAITS FOR A PERSON, so the shared assembly never hands it out: a
// headless launch, an engine's launch and a --once run all come out of
// [openV3Launch] with no factory door, and only the interactive door sets one
// (chatv3.go).
func TestFactoryDoorIsAbsentFromEveryAssembledConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	for _, door := range []string{"chat", "engine"} {
		proc, err := openV3Process(door)
		if err != nil {
			t.Fatalf("%s: the process did not open: %v", door, err)
		}
		t.Cleanup(proc.closeAll)
		for _, interactive := range []bool{false, true} {
			launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir(), Interactive: interactive, NoStandingTicks: true})
			if err != nil {
				t.Fatalf("%s: the launch did not open: %v", door, err)
			}
			if launch.Config.Factory != nil {
				t.Fatalf("%s (interactive %v): the shared assembly handed out a factory door", door, interactive)
			}
		}
	}
}
