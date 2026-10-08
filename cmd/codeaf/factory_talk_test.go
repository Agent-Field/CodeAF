package main

// The item's own conversation (`T`) and the item door behind `factory_item`,
// held to their laws: the conversation is made once, inside the item's team
// under the one `factory` team, with the item in front of it and its marker
// first; the door previews without writing, applies each field, changes the
// stages only through factory.Adapt, and refuses a stage change under `fixed`.

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
	for _, want := range []string{"#" + strconv.Itoa(first) + " · fix the ledger double count", "repo web", talkClosing, "factory_item", "stages:",
		"[factory item #" + strconv.Itoa(first) + "]", "hub", "leave notes its stages will read", "its stages will report into this conversation"} {
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

func TestFactoryItemDoorAppliesEachFieldAndRefusesUnderFixed(t *testing.T) {
	st, web, _ := talkLab(t)
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "fix the ledger double count", Kind: factory.KindIssue,
		Gate: factory.GateShip, Cap: 5, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	door := itemDoor(st, web).(storeItemDoor)
	ctx := context.Background()
	if _, _, err := door.Preview(ctx, 999, session.ItemChange{Cap: 8}); err == nil || !strings.Contains(err.Error(), "there is no item 999") {
		t.Fatalf("an item that is not there = %v", err)
	}

	// THE PREVIEW WRITES NOTHING.
	change := session.ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}, Why: "one-line fix"},
		Gate: factory.GatePlan, Cap: 8, Effort: "strong", Note: "the fixture in testdata is flaky"}
	before, after, err := door.Preview(ctx, id, change)
	if err != nil {
		t.Fatal(err)
	}
	if before.Gate != factory.GateShip || after.Gate != factory.GatePlan || after.Cap != 8 || len(after.Notes) != 1 {
		t.Fatalf("preview = %+v → %+v", before, after)
	}
	if held, _ := st.Get(id); held.Gate != factory.GateShip || held.Cap != 5 || len(held.Notes) != 0 {
		t.Fatalf("a preview wrote to the store: %+v", held)
	}

	// AND THE APPLY WRITES EVERY FIELD, ONCE.
	now, err := door.Apply(ctx, id, change)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	if i := factory.StageIndex(it.Stages, "review"); i < 0 || it.Stages[i].On {
		t.Fatalf("the saved item still runs review: %+v", it.Stages)
	}
	if it.Gate != factory.GatePlan || it.Cap != 8 || session.ItemEffort(it) != "strong" ||
		strings.Join(it.Notes, "|") != "the fixture in testdata is flaky" {
		t.Fatalf("the saved item = gate %s cap %v effort %q notes %v", it.Gate, it.Cap, session.ItemEffort(it), it.Notes)
	}
	if now.Gate != it.Gate || now.Cap != it.Cap {
		t.Fatalf("Apply answered %+v, the store holds %+v", now, it)
	}
	if _, err := door.Apply(ctx, id, session.ItemChange{Edit: factory.PlanEdit{Skip: []string{"proof"}}}); err == nil || !strings.Contains(err.Error(), "may not skip proof") {
		t.Fatalf("skipping proof = %v, want Adapt's refusal", err)
	}

	fixed := factory.DefaultRecipe()
	fixed.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	if err := factory.Save(web, fixed); err != nil {
		t.Fatal(err)
	}
	held, _ := st.Get(id)
	if _, _, err := door.Preview(ctx, id, session.ItemChange{Edit: factory.PlanEdit{On: []string{"review"}}, Cap: 12}); err == nil ||
		err.Error() != "the recipe for issue is fixed; plan may not change the stages" {
		t.Fatalf("preview under fixed = %v, want Adapt's sentence", err)
	}
	_, err = door.Apply(ctx, id, session.ItemChange{Edit: factory.PlanEdit{On: []string{"review"}}, Cap: 12})
	if err == nil || err.Error() != "the recipe for issue is fixed; plan may not change the stages" {
		t.Fatalf("under fixed = %v, want Adapt's sentence", err)
	}
	later, _ := st.Get(id)
	if strings.Join(factory.StageLines(later.Stages), "\n") != strings.Join(factory.StageLines(held.Stages), "\n") || later.Cap != held.Cap {
		t.Fatal("a refused change still changed the item")
	}
}

func TestFactoryItemDoorOfANilStoreIsNil(t *testing.T) {
	if door := itemDoor(nil, t.TempDir()); door != nil {
		t.Fatalf("a nil store became a non-nil item door: %#v", door)
	}
}

// THE RECIPE DOOR SAYS THE STAGES A KIND RUNS, so the recipe card can show
// them before and after its line, and refuses an unknown checkout in the
// bank's own words.
func TestFactoryRecipeDoorSaysTheStagesAKindRuns(t *testing.T) {
	st, web, _ := talkLab(t)
	door := recipeDoor(st, web).(fileRecipeDoor)
	names, err := door.Stages(context.Background(), "web", factory.KindIssue)
	if err != nil || strings.Join(names, " ") != "plan write test review security proof" {
		t.Fatalf("stages = %v, %v", names, err)
	}
	if _, err := door.Stages(context.Background(), "nowhere", factory.KindIssue); err == nil || !strings.Contains(err.Error(), "does not know where nowhere is checked out") {
		t.Fatalf("an unknown checkout = %v", err)
	}
}

// THE BRIEF'S MARKER names the item by its ref, and a forge-numbered item by
// its floor id beside it.
func TestFactoryTalkMarker(t *testing.T) {
	if got := talkMarker(factory.Item{ID: 1}); got != "[factory item #1]" {
		t.Errorf("a floor-numbered item = %q", got)
	}
	if got := talkMarker(factory.Item{ID: 7, Num: 1540}); got != "[factory item #1540 · 7]" {
		t.Errorf("a forge-numbered item = %q", got)
	}
	brief := talkBrief(factory.Item{ID: 3, Repo: "web", Title: "t", Notes: []string{"the fixture is flaky"}}, factory.DefaultRecipe())
	if !strings.HasPrefix(brief, "[factory item #3]\n#3 · t\nrepo web") || !strings.Contains(brief, "note for the stages: the fixture is flaky") {
		t.Errorf("brief = %q", brief)
	}
}
