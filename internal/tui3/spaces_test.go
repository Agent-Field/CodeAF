package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"

	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSpaceSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	made := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	want := []space{
		{Name: "port", Made: made, Members: []spaceMember{{Key: "k1", File: "f1", Where: "/w/a", Word: "one"}}},
		{Name: "docs", Made: made, Members: []spaceMember{{Key: "k2", File: "f2", Where: "/w/b", Word: "two"}}},
	}
	if err := saveSpaces(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSpaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %#v\nwant %#v", got, want)
	}
	// The write is a rename, so nothing temporary is left beside the file.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != spacesFile {
		t.Fatalf("profile holds %v, want only %s", entries, spacesFile)
	}
	// And it is never config.json.
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		t.Fatal("spaces wrote config.json")
	}
}

func TestSpaceMissingFileIsNoSpacesAndNoError(t *testing.T) {
	got, err := loadSpaces(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("missing file: %v, %v", got, err)
	}
}

// AN EMPTY PROFILE DIRECTORY IS THE ORDINARY LAUNCH, and the sets go to this
// process's own profile in the state root rather than nowhere. The first build
// read "" as "keep them in memory", so on a plain launch no space outlived the
// window it was made in.
func TestSpaceFileOnTheOrdinaryLaunchIsTheProfilesOwn(t *testing.T) {
	got := spacesPath("")
	if got == "" || got == spacesFile || !filepath.IsAbs(got) {
		t.Fatalf("an empty profile directory put the sets at %q", got)
	}
	if want := config.ProfilePath("", spacesFile); got != want {
		t.Fatalf("sets at %q, the profile keeps its files at %q", got, want)
	}
}

func TestSpaceCorruptFileErrorsAndIsNotClobbered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, spacesFile)
	bad := []byte("{not json")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSpaces(dir); err == nil {
		t.Fatal("corrupt file loaded without error")
	}
	if raw, _ := os.ReadFile(path); string(raw) != string(bad) {
		t.Fatalf("load changed the file to %q", raw)
	}

	// The app's first load moves it aside, so a later save cannot overwrite it.
	a := &app{profileDir: dir}
	a.spacesEnsure()
	if !a.wall.loaded || a.wall.active != -1 || len(a.wall.spaces) != 0 {
		t.Fatalf("ensure on corrupt file: %+v", a.wall)
	}
	if _, err := a.spaceMake("new", []chatTab{{key: "k", word: "w"}}); err != nil {
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

func TestSpaceFromTabsSkipsPagesAndKeyless(t *testing.T) {
	now := time.Now()
	sp := spaceFromTabs("s", []chatTab{
		{key: "start", word: "Home", start: true},
		{key: "work", word: "Work", work: true},
		{key: "", word: "nameless"},
		{key: "a", file: "fa", where: "/w", word: "alpha"},
		{key: "a", file: "fa", where: "/w", word: "alpha again"},
		{key: "b", file: "fb", where: "/v", word: "beta"},
	}, now)
	want := []spaceMember{{Key: "a", File: "fa", Where: "/w", Word: "alpha"}, {Key: "b", File: "fb", Where: "/v", Word: "beta"}}
	if !reflect.DeepEqual(sp.Members, want) || sp.Name != "s" || !sp.Made.Equal(now) {
		t.Fatalf("got %+v", sp)
	}
}

func TestSpaceSuggestName(t *testing.T) {
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
		if got := spaceSuggestName(c.tabs); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestSpaceTabsMergeLiveAndKeepStoredOrder(t *testing.T) {
	sp := space{Name: "s", Members: []spaceMember{
		{Key: "c", File: "fc", Where: "/w", Word: "gamma"},
		{Key: "a", File: "fa", Where: "/w", Word: "alpha"},
	}}
	live := []chatTab{
		{key: "a", file: "fa", word: "alpha now", here: true, held: true, signal: tabSignal(1)},
		{key: "b", file: "fb", word: "beta"},
	}
	got := spaceTabs(sp, live)
	if len(got) != 2 || got[0].key != "c" || got[1].key != "a" {
		t.Fatalf("order: %+v", got)
	}
	if want := (chatTab{key: "c", file: "fc", where: "/w", word: "gamma", full: "gamma"}); got[0] != want {
		t.Fatalf("closed member rebuilt as %+v", got[0])
	}
	if got[1] != live[0] {
		t.Fatalf("live member not the live tab: %+v", got[1])
	}
}

func TestSpaceStripTabsKeepsTheFrontTab(t *testing.T) {
	a := &app{}
	a.spacesEnsure()
	tabs := []chatTab{{key: "a", word: "alpha"}, {key: "b", word: "beta", here: true}, {key: "c", word: "gamma"}}
	if got := a.spaceStripTabs(tabs); !reflect.DeepEqual(got, tabs) {
		t.Fatalf("no space active changed the strip: %+v", got)
	}
	if _, err := a.spaceMake("s", []chatTab{{key: "c", word: "gamma"}, {key: "a", word: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	a.wall.active = 0
	got := a.spaceStripTabs(tabs)
	var keys []string
	for _, tab := range got {
		keys = append(keys, tab.key)
	}
	if strings.Join(keys, ",") != "c,a,b" {
		t.Fatalf("strip keys %v, want c,a,b", keys)
	}
	// A front tab that is a member is not drawn twice.
	tabs[1].here, tabs[0].here = false, true
	if got := a.spaceStripTabs(tabs); len(got) != 2 {
		t.Fatalf("member front tab doubled: %+v", got)
	}
}

func TestSpaceNotActiveBeforeLoad(t *testing.T) {
	a := &app{}
	a.wall.spaces = []space{{Name: "s", Members: []spaceMember{{Key: "a"}}}}
	if _, ok := a.spaceActive(); ok {
		t.Fatal("zero-value active read as a space before any load")
	}
}

func TestSpaceMakeReplacesByNameAndDeleteFollowsActive(t *testing.T) {
	dir := t.TempDir()
	a := &app{profileDir: dir}
	i0, err := a.spaceMake("Port", []chatTab{{key: "a", word: "alpha"}})
	if err != nil || i0 != 0 {
		t.Fatalf("make: %d %v", i0, err)
	}
	i1, _ := a.spaceMake("docs", []chatTab{{key: "b", word: "beta"}})
	again, _ := a.spaceMake("port", []chatTab{{key: "c", word: "gamma"}})
	if again != 0 || len(a.wall.spaces) != 2 || a.wall.spaces[0].Members[0].Key != "c" {
		t.Fatalf("same name did not replace: %+v", a.wall.spaces)
	}
	if _, err := a.spaceMake("  ", []chatTab{{key: "a"}}); err == nil {
		t.Fatal("blank name accepted")
	}
	a.wall.active = i1
	if err := a.spaceDelete(0); err != nil {
		t.Fatal(err)
	}
	if sp, ok := a.spaceActive(); !ok || sp.Name != "docs" {
		t.Fatalf("active did not follow: %+v %v", sp, ok)
	}
	b := &app{profileDir: dir}
	b.spacesEnsure()
	if names := b.spaceNames(); !reflect.DeepEqual(names, []string{"docs"}) {
		t.Fatalf("reloaded names %v", names)
	}
	if err := a.spaceDelete(0); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.spaceActive(); ok {
		t.Fatal("deleted space still active")
	}
}
