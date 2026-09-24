package main

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// A WRITE THAT MEETS ANOTHER WRITER IS MADE AGAIN ON TOP OF IT. The window made
// its change to the list it held; the far session set a manager in between, so
// the engine answers Stale. The seam reads the file again, makes the change
// again on the fresh list, and the second write carries both: the rename and
// the manager nobody here saw.
func TestHostTeamsRetriesAStaleWriteOnTopOfTheOtherWriter(t *testing.T) {
	far := []teamstore.Team{{ID: "0a0a0a0a0a0a", Name: "harbor", Members: []teamstore.Member{{Key: "k1"}}}}
	stamp, reads := "1.1", 0
	var wrote [][]teamstore.Team
	h := &hostTeams{
		read: func(since string, _ []float64) (remote.TeamsReading, error) {
			reads++
			if since == stamp {
				return remote.TeamsReading{Stamp: stamp, Same: true}, nil
			}
			return remote.TeamsReading{Stamp: stamp, Teams: cloneTeams(far)}, nil
		},
		write: func(base string, teams []teamstore.Team) (remote.TeamsReading, error) {
			wrote = append(wrote, cloneTeams(teams))
			if base != stamp {
				return remote.TeamsReading{Stamp: stamp, Stale: true}, nil
			}
			far, stamp = cloneTeams(teams), "2.2"
			return remote.TeamsReading{Stamp: stamp, Teams: cloneTeams(far)}, nil
		},
	}
	if _, _, known := h.load(nil); known {
		t.Fatal("a seam that has read nothing says it holds something")
	}
	if _, _, _, err := h.readSince("", nil); err != nil {
		t.Fatal(err)
	}
	// The far session writes after the window read.
	far[0].Manager, stamp = "k1", "1.5"

	calls := 0
	teams, at, err := h.update(func(f *teamstore.File) error {
		calls++
		f.Teams[0].Name = "dock"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(wrote) != 2 || reads != 2 {
		t.Fatalf("the change was made %d times, written %d times, read %d times", calls, len(wrote), reads)
	}
	if at != "2.2" || teams[0].Name != "dock" || teams[0].Manager != "k1" {
		t.Fatalf("the second write lost something: %+v at %q", teams, at)
	}
	if held, heldAt, known := h.load(nil); !known || heldAt != "2.2" || held[0].Manager != "k1" {
		t.Fatalf("the seam holds %+v at %q", held, heldAt)
	}
}

// A WRITER THAT NEVER STOPS WINS, AND THE WINDOW IS TOLD. Three stale answers
// in a row are an error the surface says, not a write made over the top.
func TestHostTeamsGivesUpAfterThreeStaleWrites(t *testing.T) {
	writes := 0
	h := &hostTeams{
		read: func(string, []float64) (remote.TeamsReading, error) {
			return remote.TeamsReading{Stamp: "1.1"}, nil
		},
		write: func(string, []teamstore.Team) (remote.TeamsReading, error) {
			writes++
			return remote.TeamsReading{Stamp: "9.9", Stale: true}, nil
		},
	}
	if _, _, err := h.update(func(*teamstore.File) error { return nil }); !errors.Is(err, errHostTeamsBusy) || writes != hostTeamsTries {
		t.Fatalf("after %d stale writes: %v", writes, err)
	}
}

// AN ENGINE WITHOUT THE TEAMS DOORS GETS NO SEAM. The welcome of an older
// engine carries no Teams, and the door hands the surface the zero seam, which
// over --host is teams off; it never hands one onto this laptop's profile.
func TestAnOlderEngineGetsNoTeamsSeam(t *testing.T) {
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: &quietAgent{}, ProfileDir: t.TempDir()}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	far := hostFar{client: loop.Client}
	welcome := loop.Client.Welcome()
	if seam := hostTeamsSeam(far, welcome); seam.Load == nil || seam.Update == nil || seam.Traffic == nil || seam.ReadSince == nil {
		t.Fatal("an engine with the teams doors got no seam")
	}
	welcome.Teams = false
	if seam := hostTeamsSeam(far, welcome); seam.Load != nil || seam.Update != nil {
		t.Fatal("an older engine got a teams seam")
	}
	if seam := hostTeamsSeam(hostFar{}, remote.Welcome{Teams: true}); seam.Load != nil {
		t.Fatal("no connection got a teams seam")
	}
}
