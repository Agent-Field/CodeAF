package main

// The item's own conversation (`T`) and the stages door behind
// `factory_stages`, held to their laws: the conversation is made once, inside
// the item's team under the one `factory` team, with the item in front of it;
// the stages change only through factory.Adapt, which refuses under `fixed`.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func talkLab(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("HOME", root)
	st, err := store.Open(filepath.Join(root, "factory"))
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(t.TempDir(), "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	return st, web, filepath.Join(root, "profile")
}

func TestFactoryTalkMakesOneConversationInTheItemsTeamUnderFactory(t *testing.T) {
	st, web, profile := talkLab(t)
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, web)),
		factory.WithTalk(talkMaker(st, web, profile)))
	first, err := seam.New("web", "fix the ledger double count")
	if err != nil {
		t.Fatal(err)
	}
	second, err := seam.New("web", "make the meter honest")
	if err != nil {
		t.Fatal(err)
	}
	chat, err := seam.Talk(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := seam.Talk(context.Background(), first)
	if err != nil || again != chat {
		t.Fatalf("the second T answered %q, %v; want %q", again, err, chat)
	}
	other, err := seam.Talk(context.Background(), second)
	if err != nil || other == chat {
		t.Fatalf("a second item shares the first's conversation: %q, %v", other, err)
	}

	data, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#" + strconv.Itoa(first) + " · fix the ledger double count", "repo web", talkClosing, "factory_stages", "stages:"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the opening brief lacks %q:\n%s", want, data)
		}
	}
	meta, err := session.LoadMeta(filepath.Dir(chat))
	if err != nil || meta.Workspace != web {
		t.Fatalf("the conversation's folder is %q (%v), want the checkout %q", meta.Workspace, err, web)
	}

	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	var parents, items int
	parent := ""
	for _, team := range f.Teams {
		if team.Name == "factory" && team.Parent == "" {
			parents++
			parent = team.ID
		}
	}
	for _, team := range f.Teams {
		if team.Parent == parent && parent != "" {
			items++
		}
	}
	if parents != 1 || items != 2 {
		t.Fatalf("teams: %d factory teams and %d item teams under it, want 1 and 2: %+v", parents, items, f.Teams)
	}
	key, _ := filepath.EvalSymlinks(chat)
	found := false
	for _, team := range f.Teams {
		if team.Name == "#"+strconv.Itoa(first)+" · fix the ledger double count" && team.Holds(filepath.Clean(key)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the item's team does not hold its conversation: %+v", f.Teams)
	}

	// AND IT IS PUT AWAY WITH THE ITEM: hiding it archives the conversation
	// and closes the item's team.
	away := talkPutAway(seam, st, profile)
	if err := away.Dismiss(first); err != nil {
		t.Fatal(err)
	}
	if meta, _ := session.LoadMeta(filepath.Dir(chat)); !meta.Archived {
		t.Error("hiding the item left its conversation on the lists")
	}
	f, _ = teams.Load(profile)
	for _, team := range f.Teams {
		if team.Holds(filepath.Clean(key)) && !team.Closed() {
			t.Errorf("hiding the item left its team open: %+v", team)
		}
	}
}

func TestFactoryStagesDoorAppliesAdaptAndRefusesUnderFixed(t *testing.T) {
	st, web, _ := talkLab(t)
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "fix the ledger double count", Kind: factory.KindIssue,
		Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	door := stagesDoor(st, web).(storeStagesDoor)
	if ref, err := door.Ref(context.Background(), id); err != nil || ref != "#"+strconv.Itoa(id) {
		t.Fatalf("ref = %q, %v", ref, err)
	}
	if _, err := door.Ref(context.Background(), 999); err == nil {
		t.Fatal("an item that is not there was named")
	}
	now, err := door.Apply(context.Background(), id, factory.PlanEdit{Skip: []string{"review"}, Why: "one-line fix"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range now {
		if name == "review" {
			t.Fatalf("review is still on: %v", now)
		}
	}
	it, _ := st.Get(id)
	if i := factory.StageIndex(it.Stages, "review"); i < 0 || it.Stages[i].On {
		t.Fatalf("the saved item still runs review: %+v", it.Stages)
	}
	if _, err := door.Apply(context.Background(), id, factory.PlanEdit{Skip: []string{"proof"}}); err == nil || !strings.Contains(err.Error(), "may not skip proof") {
		t.Fatalf("skipping proof = %v, want Adapt's refusal", err)
	}

	fixed := factory.DefaultRecipe()
	fixed.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	if err := factory.Save(web, fixed); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	_, err = door.Apply(context.Background(), id, factory.PlanEdit{On: []string{"review"}})
	if err == nil || err.Error() != "the recipe for issue is fixed; plan may not change the stages" {
		t.Fatalf("under fixed = %v, want Adapt's sentence", err)
	}
	after, _ := st.Get(id)
	if strings.Join(factory.StageLines(after.Stages), "\n") != strings.Join(factory.StageLines(before.Stages), "\n") {
		t.Fatal("a refused change still changed the item")
	}
}

func TestFactoryStagesDoorOfANilStoreIsNil(t *testing.T) {
	if door := stagesDoor(nil, t.TempDir()); door != nil {
		t.Fatalf("a nil store became a non-nil stages door: %#v", door)
	}
}
