package tui3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"

	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTeamMissingFileIsNoTeamsAndNoError(t *testing.T) {
	got, err := loadTeams(t.TempDir(), nil)
	if err != nil || got != nil {
		t.Fatalf("missing file: %v, %v", got, err)
	}
}

// AN EMPTY PROFILE DIRECTORY IS THE ORDINARY LAUNCH, and the sets go to this
// process's own profile in the state root rather than nowhere. The first build
// read "" as "keep them in memory", so on a plain launch no team outlived the
// window it was made in.
func TestTeamFileOnTheOrdinaryLaunchIsTheProfilesOwn(t *testing.T) {
	got := teamsPath("")
	if got == "" || got == teamsFile || !filepath.IsAbs(got) {
		t.Fatalf("an empty profile directory put the sets at %q", got)
	}
	if want := config.ProfilePath("", teamsFile); got != want {
		t.Fatalf("sets at %q, the profile keeps its files at %q", got, want)
	}
}

func TestTeamCorruptFileErrorsAndIsNotClobbered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, teamsFile)
	bad := []byte("{not json")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTeams(dir, nil); err == nil {
		t.Fatal("corrupt file loaded without error")
	}
	if raw, _ := os.ReadFile(path); string(raw) != string(bad) {
		t.Fatalf("load changed the file to %q", raw)
	}

	// The app's first load moves it aside, so a later save cannot overwrite it.
	a := &app{profileDir: dir}
	a.teamsEnsure()
	if !a.wall.loaded || a.wall.activeID != "" || len(a.wall.teams) != 0 {
		t.Fatalf("ensure on corrupt file: %+v", a.wall)
	}
	if _, err := a.teamMake("new", []chatTab{{key: "k", word: "w"}}); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(path + ".unreadable-*")
	if len(aside) != 1 {
		t.Fatalf("corrupt file not kept aside: %v", aside)
	}
	if raw, _ := os.ReadFile(aside[0]); string(raw) != string(bad) {
		t.Fatalf("kept file holds %q", raw)
	}
}

func TestTeamFromTabsSkipsPagesAndKeyless(t *testing.T) {
	now := time.Now()
	sp := teamFromTabs("s", []chatTab{
		{key: "start", word: "Home", start: true},
		{key: "work", word: "Work", work: true},
		{key: "", word: "nameless"},
		{key: "a", file: "fa", where: "/w", word: "alpha"},
		{key: "a", file: "fa", where: "/w", word: "alpha again"},
		{key: "b", file: "fb", where: "/v", word: "beta"},
	}, now)
	want := []teamMember{{Key: "a", File: "fa", Where: "/w", Word: "alpha"}, {Key: "b", File: "fb", Where: "/v", Word: "beta"}}
	if !reflect.DeepEqual(sp.Members, want) || sp.Name != "s" || !sp.Made.Equal(now) {
		t.Fatalf("got %+v", sp)
	}
}

func TestTeamSuggestName(t *testing.T) {
	cases := []struct {
		name string
		tabs []chatTab
		want string
	}{
		{"shared workspace", []chatTab{{key: "a", where: "/src/CodeAF", word: "Fix it"}, {key: "b", where: "/src/CodeAF/", word: "Other"}}, "codeaf"},
		{"mixed workspaces", []chatTab{{key: "a", where: "/src/one", word: "Ship The Port"}, {key: "b", where: "/src/two", word: "x"}}, "ship"},
		{"no workspace", []chatTab{{key: "a", word: "Hello world"}}, "hello"},
		{"pages skipped", []chatTab{{key: "s", start: true, where: "/elsewhere", word: "Home"}, {key: "a", where: "/src/lab", word: "x"}}, "lab"},
		{"cut to sixteen", []chatTab{{key: "a", where: "/src/a-very-long-project-name", word: "x"}}, "a-very-long-proj"},
		{"nothing", nil, ""},
	}
	for _, c := range cases {
		if got := teamSuggestName(c.tabs); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestTeamTabsMergeLiveAndKeepStoredOrder(t *testing.T) {
	sp := team{Name: "s", Members: []teamMember{
		{Key: "c", File: "fc", Where: "/w", Word: "gamma"},
		{Key: "a", File: "fa", Where: "/w", Word: "alpha"},
		{Key: "d", File: "fd", Where: "/w", Handle: "lexer"},
	}}
	live := []chatTab{
		{key: "a", file: "fa", word: "alpha now", here: true, held: true, signal: tabSignal(1)},
		{key: "b", file: "fb", word: "beta"},
	}
	// Held: c is held behind with a title, d is held with none yet (a member
	// the manager just started).
	held := func(key string) bool { return key == "c" || key == "d" }
	got := teamTabs(sp, live, held)
	if len(got) != 3 || got[0].key != "c" || got[1].key != "a" || got[2].key != "d" {
		t.Fatalf("order: %+v", got)
	}
	if want := (chatTab{key: "c", file: "fc", where: "/w", word: "gamma", full: "gamma"}); got[0] != want {
		t.Fatalf("held member rebuilt as %+v", got[0])
	}
	if got[1] != live[0] {
		t.Fatalf("live member not the live tab: %+v", got[1])
	}
	if got[2].word != "@lexer" {
		t.Fatalf("a held member with no title is drawn by its handle: %+v", got[2])
	}
}

// A MEMBER THIS WINDOW DOES NOT HAVE OPEN GETS NO TAB. The strip is what is
// open here, narrowed to the team; the owner saw three tabs over a wall of one.
func TestTeamTabsDrawNoTabForAMemberNotOpenHere(t *testing.T) {
	sp := team{Name: "test", Members: []teamMember{
		{Key: "a", File: "fa", Word: "alpha"},
		{Key: "b", File: "fb", Word: "beta"},
		{Key: "c", File: "fc", Word: "gamma"},
	}}
	live := []chatTab{{key: "a", file: "fa", word: "alpha", here: true}}
	got := teamTabs(sp, live, func(string) bool { return false })
	if len(got) != 1 || got[0].key != "a" {
		t.Fatalf("only the open member is a tab: %+v", got)
	}
}

func TestTeamStripTabsKeepsTheFrontTab(t *testing.T) {
	a := &app{}
	a.teamsEnsure()
	tabs := []chatTab{{key: "a", word: "alpha"}, {key: "b", word: "beta", here: true}, {key: "c", word: "gamma"}}
	if got := a.teamStripTabs(tabs); !reflect.DeepEqual(got, tabs) {
		t.Fatalf("no team active changed the strip: %+v", got)
	}
	if _, err := a.teamMake("s", []chatTab{{key: "c", word: "gamma"}, {key: "a", word: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	a.wall.activeID = a.wall.teams[0].ID
	got := a.teamStripTabs(tabs)
	// The team has no manager, so its first place is the manager's empty one
	// (teammanager.go), and the members follow it.
	if len(got) == 0 || !got[0].slot {
		t.Fatalf("the manager's place is not first: %+v", got)
	}
	var keys []string
	for _, tab := range got[1:] {
		keys = append(keys, tab.key)
	}
	if strings.Join(keys, ",") != "c,a,b" {
		t.Fatalf("strip keys %v, want c,a,b", keys)
	}
	// A front tab that is a member is not drawn twice.
	tabs[1].here, tabs[0].here = false, true
	if got := a.teamStripTabs(tabs); len(got) != 3 {
		t.Fatalf("member front tab doubled: %+v", got)
	}
}

func TestTeamNotActiveBeforeLoad(t *testing.T) {
	a := &app{}
	a.wall.teams = []team{{ID: "s", Name: "s", Members: []teamMember{{Key: "a"}}}}
	a.wall.activeID = "s"
	if _, ok := a.teamActive(); ok {
		t.Fatal("zero-value active read as a team before any load")
	}
}

func TestTeamMakeReplacesByNameAndDeleteFollowsActive(t *testing.T) {
	dir := t.TempDir()
	a := &app{profileDir: dir}
	i0, err := a.teamMake("Port", []chatTab{{key: "a", word: "alpha"}})
	if err != nil || len(i0) != 12 {
		t.Fatalf("make: %q %v", i0, err)
	}
	i1, _ := a.teamMake("docs", []chatTab{{key: "b", word: "beta"}})
	again, _ := a.teamMake("port", []chatTab{{key: "c", word: "gamma"}})
	if again != i0 || len(a.wall.teams) != 2 || a.wall.teams[0].Members[0].Key != "c" {
		t.Fatalf("same name did not replace: %+v", a.wall.teams)
	}
	if _, err := a.teamMake("  ", []chatTab{{key: "a"}}); err == nil {
		t.Fatal("blank name accepted")
	}
	a.wall.activeID = i1
	if err := a.teamDelete(i0); err != nil {
		t.Fatal(err)
	}
	if sp, ok := a.teamActive(); !ok || sp.Name != "docs" {
		t.Fatalf("active did not follow: %+v %v", sp, ok)
	}
	teamsFlush(t, a)
	b := &app{profileDir: dir}
	b.teamsEnsure()
	if names := b.teamNames(); !reflect.DeepEqual(names, []string{"docs"}) {
		t.Fatalf("reloaded names %v", names)
	}
	if err := a.teamDelete(i1); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.teamActive(); ok {
		t.Fatal("deleted team still active")
	}
}

// WHAT A LATER BUILD WROTE SURVIVES THIS ONE. A field it does not know, on a
// team or beside the list, and the reserved Manager, come back out of a load
// and a save exactly as they went in.
func TestTeamRoundTripKeepsUnknownFieldsAndTheManager(t *testing.T) {
	dir := t.TempDir()
	in := `{"version":2,"teams":[{"id":"abcdefabcdef","name":"harbor","parent":"","members":[{"key":"k1","file":"","where":"","word":""}],` +
		`"manager":"k1","hue":120,"tier":1,"made":"2026-09-20T10:00:00Z","pinned":true,"rules":{"quiet":["k2"]}}]}`
	if err := os.WriteFile(filepath.Join(dir, teamsFile), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadTeams(dir, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("loaded %+v %v", got, err)
	}
	if got[0].Manager != "k1" {
		t.Fatalf("the manager was lost on load: %q", got[0].Manager)
	}
	if err := saveTeams(dir, got); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, teamsFile))
	var disk struct {
		Teams []map[string]json.RawMessage `json:"teams"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil || len(disk.Teams) != 1 {
		t.Fatalf("saved %s", raw)
	}
	saved := disk.Teams[0]
	for key, want := range map[string]string{"pinned": `true`, "rules": `{"quiet":["k2"]}`, "manager": `"k1"`, "id": `"abcdefabcdef"`} {
		var flat bytes.Buffer
		if err := json.Compact(&flat, saved[key]); err != nil || flat.String() != want {
			t.Fatalf("%s saved as %s, want %s\n%s", key, saved[key], want, raw)
		}
	}
	// And an edit through the app keeps them too.
	a := newTestAppWithProfile(dir, nil)
	if err := a.teamRename("abcdefabcdef", "dock"); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	raw, _ = os.ReadFile(filepath.Join(dir, teamsFile))
	if !strings.Contains(string(raw), `"pinned": true`) || !strings.Contains(string(raw), `"manager": "k1"`) || !strings.Contains(string(raw), `"dock"`) {
		t.Fatalf("an edit dropped what it did not know:\n%s", raw)
	}
}

// THE TREE: one parent, which exists, and no loops; a team deleted hands its
// children to its own parent and closes no conversation.
func TestTeamTreeRefusesLoopsAndDeleteReparents(t *testing.T) {
	a := newTestAppWithProfile(t.TempDir(), nil)
	mk := func(name, key string) string {
		t.Helper()
		id, err := a.teamMake(name, []chatTab{{key: key, word: name}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	top, mid, low, other := mk("top", "k1"), mk("mid", "k2"), mk("low", "k3"), mk("other", "k4")
	if err := a.teamSetParent(mid, top); err != nil {
		t.Fatal(err)
	}
	if err := a.teamSetParent(low, mid); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, parent, why string }{
		{top, top, "a team under itself"},
		{top, low, "a team under its own grandchild"},
		{mid, low, "a team under its own child"},
		{mid, "nobody", "a parent that does not exist"},
		{"nobody", top, "a team that does not exist"},
	} {
		if err := a.teamSetParent(c.id, c.parent); err == nil {
			t.Fatalf("%s was allowed", c.why)
		}
	}
	names := func(ts []team) string {
		var out []string
		for _, tm := range ts {
			out = append(out, tm.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(a.teamAncestors(low)); got != "mid,top" {
		t.Fatalf("low's ancestors are %q", got)
	}
	if got := names(a.teamChildren("")); got != "top,other" {
		t.Fatalf("the top level is %q", got)
	}
	if got := names(a.teamChildren(top)); got != "mid" {
		t.Fatalf("top's children are %q", got)
	}
	if err := a.teamSetParent(other, low); err != nil {
		t.Fatal(err)
	}
	if err := a.teamDelete(mid); err != nil {
		t.Fatal(err)
	}
	if lowT, _ := a.teamByID(low); lowT.Parent != top {
		t.Fatalf("low's parent after mid went is %q, want top", lowT.Parent)
	}
	if otherT, _ := a.teamByID(other); otherT.Parent != low {
		t.Fatalf("a team under a survivor moved: %q", otherT.Parent)
	}
	if got := names(a.teamAncestors(other)); got != "low,top" {
		t.Fatalf("other's ancestors are %q", got)
	}
	// The tree is on disk, as every edit is.
	teamsFlush(t, a)
	b := newTestAppWithProfile(a.profileDir, nil)
	b.teamsEnsure()
	if lowT, _ := b.teamByID(low); lowT.Parent != top {
		t.Fatalf("reloaded, low sits under %q", lowT.Parent)
	}
}

// NOTHING NAMES A TEAM BY ITS PLACE, so deleting one or reordering the list
// never hands another team's state to a neighbour: the active team, its
// remembered place, an open popover and the strip's chip all stay on theirs.
func TestTeamDeleteOrReorderNeverRetargetsAnother(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	if len(tiles) < 3 {
		t.Fatalf("the fixture has %d tiles", len(tiles))
	}
	first, err := a.teamMake("first", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := a.teamMake("second", []chatTab{tiles[1].tab, tiles[2].tab})
	third, _ := a.teamMake("third", []chatTab{tiles[2].tab})

	a.wallSetTeam(second)
	a.wallMove(1, 2)
	a.wallSetTeam("")
	a.wallSetTeam(third)
	a.wallSetTeam(second)
	a.wallOpenSettings(third, wallPop{})
	_ = a.wallFrame(a.width, a.height)
	chip := plain(a.tabsRow(a.width))

	if err := a.teamDelete(first); err != nil {
		t.Fatal(err)
	}
	if a.wall.activeID != second || a.wall.pop.team != third {
		t.Fatalf("after the delete the wall shows %q and the settings are %q", a.wall.activeID, a.wall.pop.team)
	}
	if got, ok := a.teamActive(); !ok || got.Name != "second" {
		t.Fatalf("the strip is narrowed to %+v", got)
	}
	if place, ok := a.wall.places[second]; !ok || place.key != tiles[2].tab.key {
		t.Fatalf("second's place is %+v %v", place, ok)
	}
	a.touch()
	if got := plain(a.tabsRow(a.width)); !strings.Contains(got, "● second ▾") || got == "" {
		t.Fatalf("the chip went from %q to %q", chip, got)
	}

	// The list reordered under the same state.
	a.wall.teams[0], a.wall.teams[1] = a.wall.teams[1], a.wall.teams[0]
	if got, _ := a.teamActive(); got.Name != "second" {
		t.Fatalf("a reorder moved the strip to %q", got.Name)
	}
	a.wall.pop = wallPop{}
	_ = a.wallFrame(a.width, a.height)
	a.wall.hover = wallHitForTeam(t, a, wallHitChip, third).ref()
	a.wall.teams[0], a.wall.teams[1] = a.wall.teams[1], a.wall.teams[0]
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if !strings.Contains(frame, "third · 1 open here · 1 member") {
		t.Fatalf("the hover followed the place, not the team:\n%s", frame)
	}
	wallKeyPress(a, "D")
	if _, ok := a.teamByID(second); ok || a.wall.activeID != "" {
		t.Fatalf("D deleted the wrong team: %v active %q", a.teamNames(), a.wall.activeID)
	}
	if _, ok := a.teamByID(third); !ok {
		t.Fatal("D took a neighbour with it")
	}
}

// A CONVERSATION STARTED WHILE A TEAM IS SHOWN IS ONE OF IT. /new (the strip's
// + and the start page take the same road) and a folder typed on home both
// mint one, and each lands in the team the strip is narrowed to, saved. Going
// back to a conversation that already exists changes no team, and with no
// team shown a new conversation joins nothing.
func TestTeamNewConversationJoinsTheShownTeam(t *testing.T) {
	a, _, _ := tabApp(t)
	a.profileDir = t.TempDir()
	n := 0
	a.start = func(workspace string) (Conversation, error) {
		n++
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: fmt.Sprintf("/tmp/lab/new-%d.jsonl", n), Workspace: "/tmp/lab"}, nil
	}
	before := a.frontTabKey()
	tabs := a.tabList()
	id, err := a.teamMake("harbor", tabs[:1])
	if err != nil {
		t.Fatal(err)
	}
	a.teamActivate(id)

	if _, ok := a.renew(); !ok {
		t.Fatal("/new refused")
	}
	fresh := a.frontTabKey()
	if fresh == before || fresh == "" {
		t.Fatalf("/new left %q in front", fresh)
	}
	if got := a.teamsOf(fresh); len(got) != 1 || got[0] != id {
		t.Fatalf("the new conversation is in %v, want harbor", got)
	}
	if _, refusal := a.startBeside("/tmp/elsewhere"); refusal != "" {
		t.Fatalf("home's new conversation refused: %s", refusal)
	}
	beside := a.frontTabKey()
	if got := a.teamsOf(beside); len(got) != 1 || got[0] != id {
		t.Fatalf("the conversation started from home is in %v", got)
	}
	teamsFlush(t, a)
	disk, _ := loadTeams(a.profileDir, nil)
	if len(disk) != 1 || !teamHolds(disk[0], fresh) || !teamHolds(disk[0], beside) {
		t.Fatalf("the joins were not saved: %+v", disk)
	}

	// Switching to one that exists is not a join.
	outside := ""
	for _, tab := range a.tabList() {
		if !teamHolds(disk[0], tab.key) {
			outside = tab.key
			_ = a.tabGo(tab)
			break
		}
	}
	if outside == "" {
		t.Fatal("every conversation is already in the team")
	}
	if got := a.teamsOf(outside); len(got) != 0 {
		t.Fatalf("switching to %q put it in %v", outside, got)
	}

	// With no team shown, nothing joins.
	a.teamActivate("")
	if _, ok := a.renew(); !ok {
		t.Fatal("/new refused")
	}
	if got := a.teamsOf(a.frontTabKey()); len(got) != 0 {
		t.Fatalf("a new conversation with no team shown joined %v", got)
	}
}
