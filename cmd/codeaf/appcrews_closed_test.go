package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// closedCrewRig is a profile with an open crew "dock" managed by "boss" and a
// closed crew "harbor", and one conversation, "shared", in both: its home is
// dock, where its manager is.
func closedCrewRig(t *testing.T) (*appBackend, *appRecorder, string, string) {
	t.Helper()
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	conv := func(name string) teamstore.Member {
		key := filepath.Join(profile, "places", "p", name, "transcript.jsonl")
		return teamstore.Member{Key: key, File: key, Handle: name, HandleBy: teamstore.HandleByTyped}
	}
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := teamstore.Save(profile, []teamstore.Team{
		{ID: "0d0d0d0d0d0d", Name: "dock", Made: made, Members: []teamstore.Member{conv("boss"), conv("shared")}, Manager: conv("boss").Key},
		{ID: "0a0a0a0a0a0a", Name: "harbor", Made: made, Members: []teamstore.Member{conv("shared")}, State: teamstore.TeamClosed, ClosedAt: made},
	}); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, nil)
	backend.profileDir = profile
	t.Cleanup(func() { backend.Close() })
	return backend, rec, profile, appMemberAgent(conv("shared"))
}

// A CLOSED CREW'S MEMBER ROWS SAY WHERE ELSE THE MEMBER IS AND WHOM IT
// REPORTS TO, as an open crew's do: crews.get and the closed list read its
// members' other crews too.
func TestAClosedCrewsMembersKeepAlsoInAndReportsTo(t *testing.T) {
	backend, _, _, shared := closedCrewRig(t)
	check := func(where string, crew appwire.Crew) {
		t.Helper()
		for _, m := range crew.Members {
			if m.Agent != shared {
				continue
			}
			if !slices.Contains(m.AlsoIn, "0d0d0d0d0d0d") || m.ReportsTo == nil || m.ReportsTo.Crew != "0d0d0d0d0d0d" {
				t.Fatalf("%s: also_in %v, reports_to %+v", where, m.AlsoIn, m.ReportsTo)
			}
			return
		}
		t.Fatalf("%s: no shared member in %+v", where, crew.Members)
	}
	got, err := backend.CrewGet("0a0a0a0a0a0a")
	if err != nil {
		t.Fatal(err)
	}
	check("crews.get", got)
	closed, err := backend.CrewList(true)
	if err != nil || len(closed) != 1 {
		t.Fatalf("closed list %+v %v", closed, err)
	}
	check("the closed list", closed[0])
}

// THE WATCH KEEPS A CREW THAT CLOSED WHILE IT WATCHED, BUT NEVER READS IT
// AGAIN UNTIL ITS ROW MOVES: archiving it, from any door, is said; a look
// that finds it unchanged costs its revision only.
func TestTheWatchReadsAClosedCrewOnlyWhenItMoves(t *testing.T) {
	backend, rec, profile, _ := closedCrewRig(t)
	run := &appCrewRun{
		ready: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
		prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{},
		acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{},
		held: map[string][]string{}, opening: map[string]bool{},
	}
	backend.factoryRooms.Do(func() {})
	backend.crewTick(run, true)
	if err := teamstore.Update(profile, func(f *teamstore.File) error { return f.Close("0d0d0d0d0d0d", time.Now(), "") }); err != nil {
		t.Fatal(err)
	}
	backend.crewTick(run, false)
	rec.waitFor(t, time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.CrewChanged)
		return ok && c.Crew.ID == "0d0d0d0d0d0d" && c.Crew.State == appwire.CrewClosed
	})
	before := crewClosedReads.Load()
	for range 5 {
		backend.crewTick(run, false)
	}
	if got := crewClosedReads.Load() - before; got != 0 {
		t.Fatalf("an unchanged closed crew was read %d times", got)
	}
	if err := teamstore.Update(profile, func(f *teamstore.File) error { return f.Archive("0d0d0d0d0d0d", time.Now()) }); err != nil {
		t.Fatal(err)
	}
	backend.crewTick(run, false)
	rec.waitFor(t, time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.CrewChanged)
		return ok && c.Crew.ID == "0d0d0d0d0d0d" && c.Crew.ArchivedAt != nil
	})
	if got := crewClosedReads.Load() - before; got != 1 {
		t.Fatalf("the archived crew was read %d times, want once", got)
	}
}
