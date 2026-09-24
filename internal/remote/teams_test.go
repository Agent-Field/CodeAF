package remote

import (
	"encoding/json"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// teamsLoop is an in-process engine whose profile is the test's own.
func teamsLoop(t *testing.T) (*Loop, string) {
	t.Helper()
	dir := t.TempDir()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, ProfileDir: dir}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop, dir
}

// THE TEAMS DOORS CROSS, AND THEY ANSWER FROM THE ENGINE'S PROFILE. The welcome
// says the engine has them; a write at the right base lands in the engine's
// teams.json; a read at the stamp the window holds answers "same" in a frame of
// a few bytes; a line the far session appends is read after the cursor, and a
// quiet log after it answers no entries.
func TestTheTeamsDoorsCrossFromTheEnginesProfile(t *testing.T) {
	loop, dir := teamsLoop(t)
	if !loop.Client.Welcome().Teams {
		t.Fatal("an engine of this build does not say it has the teams doors")
	}
	first, err := loop.Client.TeamsRead("", nil)
	if err != nil || first.Same || len(first.Teams) != 0 || first.Stamp != teamstore.MissingStamp {
		t.Fatalf("the first read of no file: %+v, %v", first, err)
	}
	wrote, err := loop.Client.TeamsUpdate(first.Stamp, []teamstore.Team{{ID: "0a0a0a0a0a0a", Name: "harbor",
		Members: []teamstore.Member{{Key: "/srv/a.jsonl", Word: "the parser"}}}})
	if err != nil || wrote.Stale || len(wrote.Teams) != 1 || wrote.Teams[0].Members[0].Handle == "" {
		t.Fatalf("the write: %+v, %v", wrote, err)
	}
	on, err := teamstore.Load(dir)
	if err != nil || len(on.Teams) != 1 || on.Teams[0].Name != "harbor" {
		t.Fatalf("the engine's file holds %+v, %v", on, err)
	}
	same, err := loop.Client.TeamsRead(wrote.Stamp, nil)
	if err != nil || !same.Same || same.Teams != nil {
		t.Fatalf("a read at the held stamp: %+v, %v", same, err)
	}
	if raw, _ := json.Marshal(same); len(raw) > 64 {
		t.Fatalf("an unchanged answer is %d bytes: %s", len(raw), raw)
	}

	if err := teamstore.AppendTraffic(dir, "0a0a0a0a0a0a", teamstore.Entry{Kind: teamstore.KindNote,
		From: teamstore.FromManager, To: "parser", Text: "take the lexer"}); err != nil {
		t.Fatal(err)
	}
	got, err := loop.Client.TeamsTraffic("0a0a0a0a0a0a", "", 50)
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Text != "take the lexer" {
		t.Fatalf("the tail: %+v, %v", got, err)
	}
	quiet, err := loop.Client.TeamsTraffic("0a0a0a0a0a0a", got.Entries[0].ID, 50)
	if err != nil || len(quiet.Entries) != 0 {
		t.Fatalf("a quiet log after the cursor: %+v, %v", quiet, err)
	}
	if raw, _ := json.Marshal(quiet); len(raw) > 64 {
		t.Fatalf("a quiet Traffic answer is %d bytes: %s", len(raw), raw)
	}
	if _, err := loop.Client.TeamsTraffic("../escape", "", 1); err == nil {
		t.Fatal("a team id that is a path was read")
	}
}

// A WRITE ON A FILE THAT MOVED IS REFUSED, NOT MERGED BY ACCIDENT. The far
// session sets a manager after the window read the file; the window's write at
// the old base answers Stale, writes nothing, and the manager is still there.
func TestATeamsWriteAtAnOldBaseIsStale(t *testing.T) {
	loop, dir := teamsLoop(t)
	if err := teamstore.Save(dir, []teamstore.Team{{ID: "0a0a0a0a0a0a", Name: "harbor",
		Members: []teamstore.Member{{Key: "k1", Word: "one"}}}}); err != nil {
		t.Fatal(err)
	}
	read, err := loop.Client.TeamsRead("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamstore.Update(dir, func(f *teamstore.File) error { return f.SetManager("0a0a0a0a0a0a", "k1") }); err != nil {
		t.Fatal(err)
	}
	mine := read.Teams
	mine[0].Name = "renamed"
	stale, err := loop.Client.TeamsUpdate(read.Stamp, mine)
	if err != nil || !stale.Stale || stale.Stamp == read.Stamp || stale.Teams != nil {
		t.Fatalf("a write at an old base: %+v, %v", stale, err)
	}
	on, _ := teamstore.Load(dir)
	if on.Teams[0].Name != "harbor" || on.Teams[0].Manager != "k1" {
		t.Fatalf("the refused write touched the file: %+v", on.Teams[0])
	}
}
