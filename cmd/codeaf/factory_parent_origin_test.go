package main

// The `factory` parent is found by stamped origin, never by its name: an
// ordinary crew a person called `factory` is left alone and the factory makes
// its own dedicated team beside it.

import (
	"testing"
	"time"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func teamByID(f teamstore.File, id string) teamstore.Team {
	for _, t := range f.Teams {
		if t.ID == id {
			return t
		}
	}
	return teamstore.Team{}
}

func TestOrdinarySameNameCrewIsNeverAdoptedAsTheFactoryParent(t *testing.T) {
	cases := map[string]teamstore.Team{
		"empty":   {ID: "crew", Name: factoryTeamName},
		"members": {ID: "crew", Name: factoryTeamName, Manager: "m", Members: []teamstore.Member{{Key: "m", Handle: "lead"}}},
	}
	for name, crew := range cases {
		t.Run(name, func(t *testing.T) {
			f := teamstore.File{Teams: []teamstore.Team{crew}}
			parent := factoryParentTeam(&f, time.Now())
			item := itemTeamIn(&f, "#1 · review", time.Now())
			if parent == "crew" {
				t.Fatal("an ordinary crew was adopted as the factory parent")
			}
			if got := teamByID(f, "crew"); got.FactoryManaged || len(got.Members) != len(crew.Members) || got.Manager != crew.Manager {
				t.Fatalf("the ordinary crew changed: %+v", got)
			}
			if !teamByID(f, parent).FactoryManaged || teamByID(f, item).Parent != parent {
				t.Fatalf("factory teams wrong: %+v", f.Teams)
			}
			if again := factoryParentTeam(&f, time.Now()); again != parent || len(f.Teams) != 3 {
				t.Fatalf("second find gave %q among %d teams", again, len(f.Teams))
			}
		})
	}
}

func TestExplicitlyStampedFactoryParentIsReusedBesideAnOrdinaryOne(t *testing.T) {
	f := teamstore.File{Teams: []teamstore.Team{
		{ID: "crew", Name: factoryTeamName},
		{ID: "stamped", Name: factoryTeamName, FactoryManaged: true},
	}}
	if got := factoryParentTeam(&f, time.Now()); got != "stamped" || len(f.Teams) != 2 {
		t.Fatalf("parent = %q among %d teams", got, len(f.Teams))
	}
	if teamByID(f, "crew").FactoryManaged {
		t.Fatal("the ordinary crew was stamped")
	}
}

func TestReferencedLegacyParentIsReusedAfterRoomReconciliation(t *testing.T) {
	profile := t.TempDir()
	if err := teamstore.Update(profile, func(f *teamstore.File) error {
		f.Teams = []teamstore.Team{
			{ID: "old", Name: factoryTeamName},
			{ID: "room", Name: "#3 · old", Parent: "old"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := factoryStampRoom(profile, "room"); err != nil {
		t.Fatal(err)
	}
	f, err := teamstore.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := factoryParentTeam(f, time.Now()); got != "old" || len(f.Teams) != 2 {
		t.Fatalf("parent = %q among %d teams", got, len(f.Teams))
	}
	// An unstamped parent that holds a stamped child is the same evidence.
	g := teamstore.File{Teams: []teamstore.Team{
		{ID: "old", Name: factoryTeamName},
		{ID: "kid", Name: "#3 · old", Parent: "old", FactoryManaged: true},
	}}
	if got := factoryParentTeam(&g, time.Now()); got != "old" || !teamByID(g, "old").FactoryManaged {
		t.Fatalf("parent = %q: %+v", got, g.Teams)
	}
}
