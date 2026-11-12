package main

// THE FOREMAN OUT OF THE CODEAF HOME, STARTED OVER, AND TAKEN OUT OF CLOSED.
//
// The Factory manager the desk made on one Mac worked in the codeaf home
// itself: every save swept nineteen gigabytes and its tool calls waited behind
// a capture that never finished, so it answered no question at all. It could
// also be archived, after which its one door opened a chat nobody could write
// to. These pin the three repairs: a foreman in a refused folder is made again
// in its own; factory.foreman_fresh starts one over; and the door, which is
// the person's own open, takes a put-away manager back out.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// foremanHandleHolder is the conversation key holding `@foreman` in the
// profile's teams, "" for none, and how many members hold it.
func foremanHandleHolder(t *testing.T, profile string) (key string, holders int) {
	t.Helper()
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, team := range f.Teams {
		for _, m := range team.Members {
			if m.Handle == foremanHandle {
				key, holders = m.Key, holders+1
			}
		}
	}
	return key, holders
}

func teamKey(transcript string) string {
	key := transcript
	if real, err := filepath.EvalSymlinks(transcript); err == nil {
		key = real
	}
	return filepath.Clean(key)
}

func TestAForemanWorkingInTheCodeafHomeIsMadeAgainInAFolderOfItsOwn(t *testing.T) {
	state, floor := chatFolderHome(t)
	profile := t.TempDir()

	// The foreman as the older build made it: in the engine's own folder,
	// which under the desk is the codeaf home.
	bucket, err := v3ProjectDir(state)
	if err != nil {
		t.Fatal(err)
	}
	place, err := v3MintSession(bucket, state, v3StampLaunchDir(v3LaunchDir(), state), false)
	if err != nil {
		t.Fatal(err)
	}
	old := place.Transcript()
	if err := session.SeedConversation(old, state, foremanTitle, "the older brief"); err != nil {
		t.Fatal(err)
	}
	if err := foremanJoinTeam(profile, old, state); err != nil {
		t.Fatal(err)
	}
	if err := floor.SetForeman(old); err != nil {
		t.Fatal(err)
	}

	maker := foremanMaker(floor, factoryHostWorkspace(state), profile, nil)
	made, err := maker(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sameSessionFile(made, old) {
		t.Fatal("the foreman was opened where it was, in the codeaf home")
	}
	meta, err := session.LoadMetaIn(session.DirOf(made), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(floor.Root(), "chats", "foreman"); meta.Workspace != want {
		t.Fatalf("the foreman made again works in %q; want %q", meta.Workspace, want)
	}
	if named, _ := floor.Foreman(); !sameSessionFile(named, made) {
		t.Fatalf("the store still names %q as its foreman", named)
	}
	if key, holders := foremanHandleHolder(t, profile); holders != 1 || key != teamKey(made) {
		t.Fatalf("@foreman is held by %q (%d holders); want the new conversation alone", key, holders)
	}
	// ITS HISTORY IS NOT DELETED: the owner of the conversation puts it away.
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("the conversation it replaced is gone from disk: %v", err)
	}
	// AND THE NEXT ASK OPENS THE SAME ONE, as every ask after a making does.
	if again, err := maker(context.Background()); err != nil || !sameSessionFile(again, made) {
		t.Fatalf("the next open answered %q, %v; want %q", again, err, made)
	}
}

func TestForemanFreshStartsTheManagerOver(t *testing.T) {
	st, web, profile := talkLab(t)
	first, err := foremanMaker(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := foremanFresh(st, web, profile, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sameSessionFile(first, fresh) {
		t.Fatal("a fresh foreman is the conversation it was")
	}
	data, err := os.ReadFile(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), foremanOpening) {
		t.Errorf("the fresh foreman lacks its brief:\n%s", data)
	}
	if key, holders := foremanHandleHolder(t, profile); holders != 1 || key != teamKey(fresh) {
		t.Fatalf("@foreman is held by %q (%d holders); want the fresh conversation alone", key, holders)
	}
	// THE FLOOR IS THE FRESH ONE'S AND NO LONGER THE OLD ONE'S: the tool
	// follows the store's mark, so the conversation put away reads nothing.
	if foremanFloorDoor(st, web, profile, fresh) == nil {
		t.Error("the fresh foreman has no factory_floor")
	}
	if foremanFloorDoor(st, web, profile, first) != nil {
		t.Error("the replaced foreman still carries factory_floor")
	}
	if again, err := foremanMaker(st, web, profile, nil)(context.Background()); err != nil || !sameSessionFile(again, fresh) {
		t.Fatalf("the open after a fresh start answered %q, %v; want the fresh one", again, err)
	}
}

// THE BRIEF NO LONGER FENCES THE MANAGER INTO ONE TOOL. It was told to answer
// "only from" factory_floor, so it could not use what the app tells it with
// each message, nor answer anything a plain assistant can.
func TestTheForemansBriefLetsItAnswerBeyondTheFloor(t *testing.T) {
	for _, want := range []string{"factory_floor", "tagged afi-now", "is the person, never you", "your other tools", "only when the person asks", "Never stop, ship, merge or post"} {
		if !strings.Contains(foremanOpening, want) {
			t.Errorf("the brief lacks %q: %s", want, foremanOpening)
		}
	}
	if strings.Contains(foremanOpening, "only from it") {
		t.Errorf("the brief still fences every answer into the floor: %s", foremanOpening)
	}
	// The journal escapes these, and the brief is found in it by its words.
	if strings.ContainsAny(foremanOpening, `<>"&`) {
		t.Errorf("the brief carries a character the journal escapes: %s", foremanOpening)
	}
}

func TestTheFactoryManagersDoorReopensItAndFreshPutsTheOldOneAway(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	st := s.start()
	ref := map[string]any{"factory": st.Factory}

	var first appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &first)
	if !s.backend.isHeld(first.Agent) {
		t.Fatal("the manager is not held open")
	}

	// PUT AWAY BY THE PERSON, then opened by its own button: it comes back.
	if err := s.backend.Archive([]string{first.Agent}); err != nil {
		t.Fatal(err)
	}
	if !s.backend.tidied(first.Agent) || s.backend.isHeld(first.Agent) {
		t.Fatal("the archive did not put the manager away; the premise is gone")
	}
	var again appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &again)
	if again.Agent != first.Agent {
		t.Fatalf("the door answered %q after an archive; want the same manager %q", again.Agent, first.Agent)
	}
	if s.backend.tidied(first.Agent) || !s.backend.isHeld(first.Agent) {
		t.Fatal("the manager's own door left it archived")
	}

	// STARTED OVER: a new manager, held open; the old one stopped and archived.
	var fresh appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanFresh, ref, &fresh)
	if fresh.Agent == "" || fresh.Agent == first.Agent {
		t.Fatalf("a fresh start answered %q; want a new conversation", fresh.Agent)
	}
	if !s.backend.isHeld(fresh.Agent) || s.backend.tidied(fresh.Agent) {
		t.Fatal("the fresh manager is not held open")
	}
	if !s.backend.tidied(first.Agent) || s.backend.isHeld(first.Agent) {
		t.Fatal("the manager it replaced was left open")
	}
	var after appwire.FactoryForeman
	s.must(appwire.FeatureFactoryForemanOpen, ref, &after)
	if after.Agent != fresh.Agent {
		t.Fatalf("the door after a fresh start answered %q; want %q", after.Agent, fresh.Agent)
	}
	// AND EACH IS THE FACTORY'S, never one of the person's own chats.
	for _, id := range []string{first.Agent, fresh.Agent} {
		a, err := s.backend.find(id)
		if err != nil {
			t.Fatal(err)
		}
		owned := false
		s.backend.locked(func() { owned = a.view.FactoryOwned })
		if !owned {
			t.Errorf("%s is not marked the factory's", id)
		}
	}
	s.mustFail(appwire.FeatureFactoryForemanFresh, map[string]any{"factory": "factory_other"}, appwire.CodeNotFound)
}
