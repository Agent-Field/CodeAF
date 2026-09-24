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

// THE DELEGATION DOORS CROSS --host, AND AN ENGINE WITHOUT THEM GETS A SEAM
// WITHOUT THEM. This build's engine says Delegation: the seam carries every
// door, and a packet raised and a decision made through it land in the
// ENGINE's profile, not this machine's. An engine that has the teams doors
// and not these (Teams true, Delegation false) gets the teams seam with the
// delegation doors nil, which the surface reads as "not over this
// connection"; it is never handed doors onto this laptop's files.
func TestTheDelegationDoorsCrossHostOnlyWhenTheEngineSaysSo(t *testing.T) {
	far := t.TempDir()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: &quietAgent{}, ProfileDir: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if err := teamstore.Save(far, []teamstore.Team{{ID: "0a0a0a0a0a0a", Name: "harbor", Manager: "hm",
		Members: []teamstore.Member{{Key: "hm", Handle: "boss"}, {Key: "k1", Handle: "web"}}}}); err != nil {
		t.Fatal(err)
	}
	welcome := loop.Client.Welcome()
	seam := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if seam.Defaults == nil || seam.Packets == nil || seam.Raise == nil || seam.Decide == nil ||
		seam.Escalate == nil || seam.Spend == nil || seam.Delete == nil {
		t.Fatal("an engine with the delegation doors got a seam without them")
	}
	p, err := seam.Raise(teamstore.Packet{Team: "0a0a0a0a0a0a", Kind: teamstore.PacketQuestion,
		RaisedBy: "web", Question: "which port?"})
	if err != nil {
		t.Fatal(err)
	}
	open, stamp, same, err := seam.Packets("0a0a0a0a0a0a", "")
	if err != nil || same || len(open) != 1 || open[0].ID != p.ID {
		t.Fatalf("packets over the seam: %+v %v %v", open, same, err)
	}
	if _, _, same, _ := seam.Packets("0a0a0a0a0a0a", stamp); !same {
		t.Fatal("a quiet packet read over the seam was not same")
	}
	if _, err := seam.Decide(p.ID, "boss", "8080", "the default"); err != nil {
		t.Fatal(err)
	}
	if got, err := teamstore.PacketByID(far, p.ID); err != nil || got.Decision != "8080" {
		t.Fatalf("the decision is not in the engine's profile: %+v %v", got, err)
	}

	welcome.Delegation = false
	older := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if older.Load == nil || older.Update == nil {
		t.Fatal("an engine with the teams doors and not the delegation doors lost its teams seam")
	}
	if older.Defaults != nil || older.Packets != nil || older.Raise != nil || older.Decide != nil ||
		older.Escalate != nil || older.Spend != nil || older.Delete != nil {
		t.Fatal("an engine without the delegation doors was handed them")
	}
}

// THE WRAP-UP'S TWO DOORS CROSS --host WHEN THE ENGINE SAYS SO (DESIGN.md
// 8.8): `Wrap up first` lands the one request line in the ENGINE's Traffic,
// and accepting a closing report closes the team in the engine's profile. An
// engine that does not say WrapUp hands no such doors, and the close card
// offers `Close now` only.
func TestTheWrapUpDoorsCrossHostOnlyWhenTheEngineSaysSo(t *testing.T) {
	far := t.TempDir()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: &quietAgent{}, ProfileDir: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	const harbor = "0a0a0a0a0a0a"
	if err := teamstore.Save(far, []teamstore.Team{{ID: harbor, Name: "harbor", Manager: "hm",
		Members: []teamstore.Member{{Key: "hm", Handle: "boss"}, {Key: "k1", Handle: "web"}}}}); err != nil {
		t.Fatal(err)
	}
	welcome := loop.Client.Welcome()
	seam := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if seam.WrapUp == nil || seam.AcceptClosing == nil {
		t.Fatal("an engine with the wrap-up doors got a seam without them")
	}
	if err := seam.WrapUp(harbor, ""); err != nil {
		t.Fatal(err)
	}
	log, err := teamstore.ReadTraffic(far, harbor, "", 10)
	if err != nil || len(log) != 1 || !teamstore.IsWrapUp(log[0]) {
		t.Fatalf("the engine's Traffic after a wrap-up: %+v %v", log, err)
	}
	p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor, Kind: teamstore.PacketClosing,
		RaisedBy: teamstore.FromManager, Question: "close harbor?",
		Options: []teamstore.Option{{ID: teamstore.OptionClose, Label: "Close", Consequence: "the team closes"},
			{ID: teamstore.OptionKeepGoing, Label: "Keep going", Consequence: "the team goes on"}},
		Report: &teamstore.ClosingReport{Done: "the parser"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seam.Decide(p.ID, teamstore.Person, teamstore.OptionClose, ""); err != nil {
		t.Fatal(err)
	}
	if closed, err := seam.AcceptClosing(p.ID); err != nil || !closed {
		t.Fatalf("accepting the report over --host: %v %v", closed, err)
	}
	f, err := teamstore.Load(far)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := f.Team(harbor); !ok || !got.Closed() {
		t.Fatalf("the engine's harbor is not closed: %+v", got)
	}

	welcome.WrapUp = false
	older := hostTeamsSeam(hostFar{client: loop.Client}, welcome)
	if older.Decide == nil || older.WrapUp != nil || older.AcceptClosing != nil {
		t.Fatal("an engine without the wrap-up doors was handed them, or lost the others")
	}
}
