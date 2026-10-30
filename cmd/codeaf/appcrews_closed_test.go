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

// A FACTORY ROOM A PERSON CLOSED OR ARCHIVED IS LISTED; only a room its job's
// end closed is left to be read where its job names it.
func TestTheClosedListsHideOnlyRoomsTheirJobsEnded(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	made := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := teamstore.Save(profile, []teamstore.Team{
		{ID: "0f0f0f0f0f0f", Name: "factory", Made: made, FactoryManaged: true},
		{ID: "0b0b0b0b0b0b", Name: "#1 closed by hand", Parent: "0f0f0f0f0f0f", Made: made, FactoryManaged: true, State: teamstore.TeamClosed, ClosedAt: made},
		{ID: "0c0c0c0c0c0c", Name: "#2 archived by hand", Parent: "0f0f0f0f0f0f", Made: made, FactoryManaged: true, State: teamstore.TeamClosed, ClosedAt: made, ArchivedAt: made},
		{ID: "0e0e0e0e0e0e", Name: "#3 its job ended", Parent: "0f0f0f0f0f0f", Made: made, FactoryManaged: true, State: teamstore.TeamClosed, ClosedAt: made, JobEnded: true},
	}); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), nil)
	backend.profileDir = profile
	t.Cleanup(func() { backend.Close() })
	ids := func(list []appwire.Crew, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range list {
			out = append(out, c.ID)
		}
		return out
	}
	if got := ids(backend.CrewList(true)); !slices.Equal(got, []string{"0b0b0b0b0b0b"}) {
		t.Fatalf("closed list %v", got)
	}
	if got := ids(backend.CrewListArchived()); !slices.Equal(got, []string{"0c0c0c0c0c0c"}) {
		t.Fatalf("archived list %v", got)
	}
}

// A ROOM ITS JOB'S END CLOSED LEAVES THE WATCH once its close is said: there
// is one for every job the factory finishes, so the watch's looks never grow
// with them. A crew a person closes stays, so archiving or deleting it from
// another window is said too.
func TestARoomItsJobClosedLeavesTheWatch(t *testing.T) {
	backend, rec, profile, _ := closedCrewRig(t)
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: "0e0e0e0e0e0e", Name: "#4", Made: time.Now(), FactoryManaged: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	run := &appCrewRun{
		ready: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
		prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{},
		acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{},
		held: map[string][]string{}, opening: map[string]bool{},
	}
	backend.factoryRooms.Do(func() {})
	backend.crewTick(run, true)
	if err := teamstore.UpdateOpen(profile, func(f *teamstore.File) error {
		if err := f.CloseForJob("0e0e0e0e0e0e", time.Now()); err != nil {
			return err
		}
		return f.Close("0d0d0d0d0d0d", time.Now(), "")
	}); err != nil {
		t.Fatal(err)
	}
	backend.crewTick(run, false)
	rec.waitFor(t, time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.CrewChanged)
		return ok && c.Crew.ID == "0e0e0e0e0e0e" && c.Crew.State == appwire.CrewClosed
	})
	var job, person bool
	run.locked(func() { _, job = run.prints["0e0e0e0e0e0e"]; _, person = run.prints["0d0d0d0d0d0d"] })
	if job || !person {
		t.Fatalf("watched after the close: the job's room %v, the person's crew %v", job, person)
	}
}

// A CREW ALREADY CLOSED WHEN THE WATCH STARTS IS FOLLOWED TOO. The closed and
// archived lists hold it, so another door deleting or archiving it is said, as
// for a crew that closed while the watch held it, and an unchanged one costs
// its revision only.
func TestTheWatchFollowsCrewsClosedBeforeItStarted(t *testing.T) {
	for _, door := range []string{"archive", "delete"} {
		t.Run(door, func(t *testing.T) {
			backend, rec, profile, _ := closedCrewRig(t)
			run := &appCrewRun{
				ready: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
				prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{},
				acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{},
				held: map[string][]string{}, opening: map[string]bool{},
			}
			backend.factoryRooms.Do(func() {})
			backend.crewTick(run, true)
			before := crewClosedReads.Load()
			backend.crewTick(run, false)
			if got := crewClosedReads.Load() - before; got != 0 {
				t.Fatalf("an unchanged closed crew was read %d times", got)
			}
			if err := teamstore.Update(profile, func(f *teamstore.File) error {
				if door == "archive" {
					return f.Archive("0a0a0a0a0a0a", time.Now())
				}
				f.Teams = slices.DeleteFunc(f.Teams, func(t teamstore.Team) bool { return t.ID == "0a0a0a0a0a0a" })
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			backend.crewTick(run, false)
			rec.waitFor(t, time.Second, func(_ string, data any) bool {
				if door == "archive" {
					c, ok := data.(appwire.CrewChanged)
					return ok && c.Crew.ID == "0a0a0a0a0a0a" && c.Crew.ArchivedAt != nil
				}
				c, ok := data.(appwire.CrewRemoved)
				return ok && c.ID == "0a0a0a0a0a0a"
			})
		})
	}
}
