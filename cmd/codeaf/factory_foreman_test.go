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
	"github.com/Agent-Field/codeaf/internal/session"
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
	if floorDoor(nil, "", "") != nil {
		t.Fatal("a nil store made a floor door")
	}
	st, web, _ := talkLab(t)
	page := factory.LocalSeam(st, time.Now())
	id, err := page.New("web", "fix the ledger double count")
	if err != nil {
		t.Fatal(err)
	}
	door := floorDoor(st, web, "")
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

// THE FLOOR IS ON THE FOREMAN'S BELT ONLY: a store that names no foreman gives
// no conversation the door; once it names one, that session file gets the door
// (by its own path or a symlink to it) and every other conversation gets nil.
func TestFactoryFloorDoorIsTheForemansOnly(t *testing.T) {
	if foremanFloorDoor(nil, "", "", "/some/transcript.jsonl") != nil {
		t.Fatal("a nil store made a floor door")
	}
	st, web, _ := talkLab(t)
	dir := t.TempDir()
	foreman := filepath.Join(dir, "foreman", "transcript.jsonl")
	other := filepath.Join(dir, "other", "transcript.jsonl")
	for _, p := range []string{foreman, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{foreman, other, ""} {
		if foremanFloorDoor(st, web, "", p) != nil {
			t.Fatalf("a floor with no foreman put the door on %q", p)
		}
	}
	if err := st.SetForeman(foreman); err != nil {
		t.Fatal(err)
	}
	if foremanFloorDoor(st, web, "", foreman) == nil {
		t.Fatal("the foreman's own conversation has no floor door")
	}
	if foremanFloorDoor(st, web, "", filepath.Join(dir, "foreman", ".", "transcript.jsonl")) == nil {
		t.Fatal("an unclean spelling of the foreman's path lost the door")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "foreman"), link); err == nil {
		if foremanFloorDoor(st, web, "", filepath.Join(link, "transcript.jsonl")) == nil {
			t.Fatal("the foreman's path through a symlink lost the door")
		}
	}
	if foremanFloorDoor(st, web, "", other) != nil {
		t.Fatal("a conversation that is not the foreman carries the floor door")
	}
}

// THE FOREMAN STARTS AND PRIORITISES AS THE JOB PAGE DOES. Asked "start the
// oldest p1", it sends the page's own Start for that job through the app's
// press (the person's credential, the factory's own checks); a priority goes
// the same road, and a window with no press says so and changes nothing.
func TestTheForemanStartsAndPrioritisesThroughTheJobPagesDoors(t *testing.T) {
	st, web, profile := talkLab(t)
	id, err := factory.LocalSeam(st, time.Now()).New("web", "add Ledger.Largest")
	if err != nil {
		t.Fatal(err)
	}
	door, ok := floorDoor(st, web, profile).(interface {
		StartItem(context.Context, int) (string, error)
		SetPriority(context.Context, int, string) (string, error)
	})
	if !ok {
		t.Fatal("the foreman's door cannot start or prioritise")
	}
	if prev := appLocalPress.Swap(nil); prev != nil {
		t.Cleanup(func() { appLocalPress.Store(prev) })
	}
	if _, err := door.SetPriority(context.Background(), id, "high"); err == nil || !strings.HasPrefix(err.Error(), "Nothing changed") {
		t.Fatalf("a priority with no press = %v", err)
	}
	type sent struct {
		job  int
		verb string
	}
	var got []sent
	press := localJobPress(func(_ context.Context, job int, verb string, _ any) error {
		got = append(got, sent{job, verb})
		return nil
	})
	appLocalPress.Store(&press)
	t.Cleanup(func() { appLocalPress.CompareAndSwap(&press, nil) })
	if line, err := door.SetPriority(context.Background(), id, "high"); err != nil || !strings.Contains(line, "p1 (high)") {
		t.Fatalf("priority = %q (%v)", line, err)
	}
	if line, err := door.StartItem(context.Background(), id); err != nil || !strings.Contains(line, "started") {
		t.Fatalf("start = %q (%v)", line, err)
	}
	if len(got) != 2 || got[0] != (sent{id, factoryVerbPriority}) || got[1] != (sent{id, factoryVerbStart}) {
		t.Fatalf("pressed %+v", got)
	}
	if !strings.Contains(foremanOpening, "only when the person asks") || strings.Contains(foremanOpening, " L") {
		t.Fatalf("the foreman's brief still describes a key nothing has: %q", foremanOpening)
	}
}

// THE FOREMAN IS CALLED THE FACTORY MANAGER. Its conversation's title and its
// word in the factory team say "Factory manager"; its team address stays
// @foreman. A foreman made before the rename keeps its conversation and is
// retitled when it next opens, never made a second time.
func TestTheFactoryManagerIsTheForemansName(t *testing.T) {
	st, web, profile := talkLab(t)
	seam := withForeman(factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, web))), st, web, profile)
	chat, err := seam.Foreman(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := filepath.EvalSymlinks(chat)
	key = filepath.Clean(key)
	named := func(why string) {
		t.Helper()
		meta, err := session.LoadMeta(session.DirOf(chat))
		if err != nil || meta.Title != "Factory manager" {
			t.Errorf("%s: the conversation is titled %q (%v)", why, meta.Title, err)
		}
		f, err := teams.Load(profile)
		if err != nil {
			t.Fatal(err)
		}
		for _, team := range f.Teams {
			for _, m := range team.Members {
				if m.Key == key && (m.Word != "Factory manager" || m.Handle != "foreman") {
					t.Errorf("%s: the team member is %q @%s", why, m.Word, m.Handle)
				}
			}
		}
	}
	named("a new foreman")

	// One made before the rename carries the old word in both places.
	dir := session.DirOf(chat)
	meta, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.Title = "foreman"
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	if err := teams.UpdateOpen(profile, func(f *teams.File) error {
		for ti := range f.Teams {
			for mi := range f.Teams[ti].Members {
				if f.Teams[ti].Members[mi].Key == key {
					f.Teams[ti].Members[mi].Word = "foreman"
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	again, err := seam.Foreman(context.Background())
	if err != nil || again != chat {
		t.Fatalf("the old foreman reopened as %q, %v; want %q", again, err, chat)
	}
	named("an old foreman reopened")
}
