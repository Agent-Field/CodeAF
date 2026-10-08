package main

// The foreman (`m`) and `factory_floor`'s door, held to their laws: the
// conversation is made once, in the one `factory` team, with the foreman's
// brief and the recipes' policy in front of it, and every later ask is the
// same one; the door's marks are the store's, and the page's next read carries
// them.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestFactoryForemanIsMadeOnceInTheFactoryTeam(t *testing.T) {
	st, web, profile := talkLab(t)
	seam := withForeman(factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, web))), st, web, profile)
	if !seam.Has("foreman") {
		t.Fatal("the floor's seam has no foreman door")
	}
	if _, err := seam.New("web", "fix the ledger double count"); err != nil {
		t.Fatal(err)
	}
	chat, err := seam.Foreman(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again, err := seam.Foreman(context.Background())
	if err != nil || again != chat {
		t.Fatalf("the second m answered %q, %v; want %q", again, err, chat)
	}
	data, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), foremanOpening) {
		t.Errorf("the opening brief lacks the foreman's sentence:\n%s", data)
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := filepath.EvalSymlinks(chat)
	held := 0
	for _, team := range f.Teams {
		if team.Holds(filepath.Clean(key)) {
			held++
			if team.Name != factoryTeamName || team.Parent != "" {
				t.Errorf("the foreman sits in %q under %q, want the top-level factory team", team.Name, team.Parent)
			}
		}
	}
	if held != 1 {
		t.Fatalf("the foreman is in %d teams, want 1: %+v", held, f.Teams)
	}

	// A FOREMAN WHOSE FOLDER WENT AWAY IS MADE AGAIN rather than opened as a
	// ghost.
	if err := os.Remove(chat); err != nil {
		t.Fatal(err)
	}
	fresh, err := seam.Foreman(context.Background())
	if err != nil || fresh == chat {
		t.Fatalf("a gone foreman answered %q, %v", fresh, err)
	}
}

func TestFactoryForemanBriefCarriesThePolicy(t *testing.T) {
	brief := foremanBrief(factory.Snapshot{Repos: []factory.Repo{{Name: "acme/api", Recipe: factory.Recipe{Policy: []string{"every billing change ships with a test"}}}}})
	for _, want := range []string{foremanOpening, "policy on acme/api: every billing change ships with a test"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if got := foremanBrief(factory.Snapshot{}); got != foremanOpening {
		t.Errorf("a floor with no policy says more than the opening: %q", got)
	}
}

// THE DOOR'S MARKS ARE THE FLOOR'S: a mark through `factory_floor`'s door is
// on the page's next read as Snapshot.Marked and the item's Marked, a new item
// only; and a nil store is a nil door, never a typed nil.
func TestFactoryFloorDoorMarksWhatThePageReads(t *testing.T) {
	if floorDoor(nil, "") != nil {
		t.Fatal("a nil store made a floor door")
	}
	st, web, _ := talkLab(t)
	page := factory.LocalSeam(st, time.Now())
	id, err := page.New("web", "fix the ledger double count")
	if err != nil {
		t.Fatal(err)
	}
	door := floorDoor(st, web)
	if err := door.Mark(context.Background(), []int{id}, true); err != nil {
		t.Fatal(err)
	}
	snap, err := page.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Marked) != 1 || snap.Marked[0] != id || !snap.Items[0].Marked {
		t.Fatalf("the page's read does not carry the mark: %v, %+v", snap.Marked, snap.Items)
	}
	if err := door.Mark(context.Background(), []int{id}, false); err != nil {
		t.Fatal(err)
	}
	if snap, _ = page.Load(); len(snap.Marked) != 0 || snap.Items[0].Marked {
		t.Fatalf("the mark survived its unmark: %v", snap.Marked)
	}
	if err := st.Update(id, func(it *factory.Item) error { it.State = factory.StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := door.Mark(context.Background(), []int{id}, true); err == nil {
		t.Fatal("a running item took a mark")
	}
}
