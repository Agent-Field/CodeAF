package main

// The headless factory engine reconciles an old teams file BEFORE its first
// team-creating door, so one job is one parent, one room; and Dismiss closes
// only the team the factory recorded, never a person's crew.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func routingTeams(t *testing.T, profile string, set []teams.Team) {
	t.Helper()
	if err := teams.Update(profile, func(f *teams.File) error { f.Teams = set; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestHeadlessOldStoreNewItemThenOldItemTalkStaysOneParentSameRoom(t *testing.T) {
	st, web, profile := talkLab(t)
	routingTeams(t, profile, []teams.Team{
		{ID: "p", Name: factoryTeamName},
		{ID: "room", Name: "#1 · old job", Parent: "p", FactoryManaged: true},
	})
	old, err := st.Create(factory.Item{Repo: "web", Kind: factory.KindIssue, Title: "old job", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(old.ID, func(it *factory.Item) error { it.Stream = &factory.Stream{Room: "room"}; return nil }); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.Create(factory.Item{Repo: "web", Kind: factory.KindIssue, Title: "new job", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}

	// The first team-creating door is the new item's stage round.
	var seen factory.Item
	exec := factoryChatStage(st, profile, factoryrun.ExecutorFunc(func(_ context.Context, job factoryrun.Job) (factory.StageResult, error) {
		seen = job.Item
		return factory.StageResult{}, nil
	}))
	if err := st.Update(fresh.ID, func(it *factory.Item) error { it.Stream = &factory.Stream{}; return nil }); err != nil {
		t.Fatal(err)
	}
	freshItem, _ := st.Get(fresh.ID)
	if _, err := exec.Run(context.Background(), factoryrun.Job{Item: freshItem}); err != nil {
		t.Fatal(err)
	}
	if seen.Stream == nil || seen.Stream.Room == "" {
		t.Fatal("the new item got no room")
	}

	// Then the old item's talk.
	oldItem, _ := st.Get(old.ID)
	chat := filepath.Join(t.TempDir(), "talk.jsonl")
	if err := talkJoinTeam(profile, oldItem, chat, web, talkTeamName(oldItem)); err != nil {
		t.Fatal(err)
	}
	f, _ := teams.Load(profile)
	parents := 0
	for _, tm := range f.Teams {
		if tm.Name == factoryTeamName && tm.Parent == "" {
			parents++
		}
	}
	if parents != 1 || len(f.Teams) != 3 {
		t.Fatalf("want one parent and three teams, got %d parents: %+v", parents, f.Teams)
	}
	if room := teamByID(*f, "room"); len(room.Members) != 1 {
		t.Fatalf("the old item's talk did not join its recorded room: %+v", f.Teams)
	}
	if teamByID(*f, seen.Stream.Room).Parent != "p" {
		t.Fatalf("the new item's team is not under the existing parent: %+v", f.Teams)
	}
}

func TestDismissClosesTheRecordedRoomNotAnOrdinaryCrewHoldingTheTalk(t *testing.T) {
	st, web, profile := talkLab(t)
	it, err := st.Create(factory.Item{Repo: "web", Kind: factory.KindIssue, Title: "job", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(t.TempDir(), "t", "talk.jsonl")
	key := chat
	if real, err := filepath.EvalSymlinks(filepath.Dir(chat)); err == nil {
		key = filepath.Join(real, "talk.jsonl")
	}
	member := teams.Member{Key: key, File: chat, Handle: "item1", JoinedAt: time.Now()}
	routingTeams(t, profile, []teams.Team{
		{ID: "crew", Name: "mine", Members: []teams.Member{member}},
		{ID: "p", Name: factoryTeamName, FactoryManaged: true},
		{ID: "room", Name: "#1 · job", Parent: "p", FactoryManaged: true, Members: []teams.Member{member}},
	})
	if err := st.Update(it.ID, func(i *factory.Item) error {
		i.Talk = chat
		i.Stream = &factory.Stream{Room: "room"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = web
	seam := factory.Seam{Dismiss: func(int) error { return nil }}
	if err := talkPutAway(seam, st, profile).Dismiss(it.ID); err != nil {
		t.Fatal(err)
	}
	f, _ := teams.Load(profile)
	if teamByID(*f, "crew").Closed() || teamByID(*f, "p").Closed() {
		t.Fatalf("an ordinary crew or the factory parent was closed: %+v", f.Teams)
	}
	if !teamByID(*f, "room").Closed() {
		t.Fatalf("the recorded room stayed open: %+v", f.Teams)
	}

	// Before any room is recorded, an ordinary crew first in the file is still left open.
	routingTeams(t, profile, []teams.Team{
		{ID: "crew", Name: "mine", Members: []teams.Member{member}},
		{ID: "p", Name: factoryTeamName, FactoryManaged: true},
		{ID: "made", Name: "#1 · job", Parent: "p", FactoryManaged: true, Members: []teams.Member{member}},
	})
	_ = st.Update(it.ID, func(i *factory.Item) error { i.Stream = nil; return nil })
	if err := talkPutAway(seam, st, profile).Dismiss(it.ID); err != nil {
		t.Fatal(err)
	}
	f, _ = teams.Load(profile)
	if teamByID(*f, "crew").Closed() || !teamByID(*f, "made").Closed() || teamByID(*f, "p").Closed() {
		t.Fatalf("wrong teams closed: %+v", f.Teams)
	}
}

func TestForemanFirstReconcilesTheLegacyParent(t *testing.T) {
	st, web, profile := talkLab(t)
	routingTeams(t, profile, []teams.Team{{ID: "p", Name: factoryTeamName}, {ID: "room", Name: "old", Parent: "p", FactoryManaged: true}})
	it, err := st.Create(factory.Item{Repo: "web", Kind: factory.KindIssue, Title: "old", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Update(it.ID, func(i *factory.Item) error { i.Stream = &factory.Stream{Room: "room"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = foremanMaker(st, web, profile, nil)(context.Background()); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Teams) != 2 || len(teamByID(*f, "p").Members) != 1 {
		t.Fatal("foreman split the legacy parent")
	}
}
func TestAnEmptyFactoryParentCannotBeAdoptedOrClosedAsAnItemRoom(t *testing.T) {
	st, _, profile := talkLab(t)
	routingTeams(t, profile, []teams.Team{{ID: "p", Name: factoryTeamName, FactoryManaged: true}})
	it, err := st.Create(factory.Item{Repo: "web", Kind: factory.KindIssue, Title: "bad room", State: factory.StateNew, Talk: filepath.Join(t.TempDir(), "talk.jsonl"), Stream: &factory.Stream{Room: "p"}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got := recordedRoomIn(f, it); got != "" {
		t.Fatalf("parent adopted as room: %s", got)
	}
	if err = talkPutAway(factory.Seam{Dismiss: func(int) error { return nil }}, st, profile).Dismiss(it.ID); err != nil {
		t.Fatal(err)
	}
	f, err = teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if teamByID(*f, "p").Closed() {
		t.Fatal("dismiss closed the empty factory parent")
	}
}
