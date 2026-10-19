package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestARecordedRoomIsMadeOnAMachineThatNeverSawIt(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	st, err := store.Open(filepath.Join(root, "mirror"))
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "work", Title: "new machine", Stream: &factory.Stream{Room: "0a1b2c3d4e5f"}})
	if err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(root, "chat.jsonl")
	exec := factoryChatStage(st, profile, factoryrun.ExecutorFunc(func(_ context.Context, j factoryrun.Job) (factory.StageResult, error) {
		if j.Item.Stream.Room != it.Stream.Room {
			t.Fatalf("logical room changed: %q", j.Item.Stream.Room)
		}
		return factory.StageResult{}, stageJoinTeam(profile, j.Item.Stream.Room, chat, root, "review", "review", "Actual opening instructions.")
	}))
	for round := 0; round < 2; round++ {
		if _, err = exec.Run(context.Background(), factoryrun.Job{Item: it}); err != nil {
			t.Fatal(err)
		}
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	room, ok := f.Team(it.Stream.Room)
	if !ok || !room.FactoryManaged || room.Name != talkTeamName(it) || len(room.Members) != 1 {
		t.Fatalf("room: %+v", room)
	}
	parent, ok := f.Team(room.Parent)
	if !ok || !parent.FactoryManaged || len(f.Teams) != 2 {
		t.Fatal("missing dedicated factory parent")
	}
	if !room.Members[0].FactoryStage || room.Members[0].FactoryBrief != "Actual opening instructions." {
		t.Fatal("stage brief not recorded")
	}
	kept, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Stream.Room != it.Stream.Room {
		t.Fatal("mirror room changed")
	}
}

func TestARecordedRoomNeverAdoptsACollidingLocalCrew(t *testing.T) {
	for _, kind := range []string{"ordinary", "under-an-ordinary-factory", "root", "factory-parent", "closed"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			profile := filepath.Join(root, "profile")
			st, err := store.Open(filepath.Join(root, "mirror"))
			if err != nil {
				t.Fatal(err)
			}
			const id = "0a1b2c3d4e5f"
			if err = teams.Update(profile, func(f *teams.File) error {
				c := teams.Team{ID: id, Name: "Customer crew", Manager: "lead", Members: []teams.Member{{Key: "lead", File: "/ordinary/chat", Handle: "lead"}}}
				switch kind {
				case "under-an-ordinary-factory":
					f.Teams = append(f.Teams, teams.Team{ID: "parent", Name: factoryTeamName})
					c.Parent = "parent"
				case "root":
					c.Root = true
				case "factory-parent":
					c.Name = factoryTeamName
					c.FactoryManaged = true
				case "closed":
					c.FactoryManaged = true
					c.State = teams.TeamClosed
				}
				f.Teams = append(f.Teams, c)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(teams.Path(profile))
			if err != nil {
				t.Fatal(err)
			}
			it, err := st.Create(factory.Item{Repo: "work", Title: "collision", Stream: &factory.Stream{Room: id}})
			if err != nil {
				t.Fatal(err)
			}
			called := false
			exec := factoryChatStage(st, profile, factoryrun.ExecutorFunc(func(_ context.Context, j factoryrun.Job) (factory.StageResult, error) {
				called = true
				if j.Item.Stream.Room != "" {
					t.Fatal("colliding crew was adopted")
				}
				return factory.StageResult{}, nil
			}))
			if _, err = exec.Run(context.Background(), factoryrun.Job{Item: it}); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("collision blocked approved work")
			}
			after, err := os.ReadFile(teams.Path(profile))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("colliding local crew changed")
			}
			kept, err := st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if kept.Stream.Room != id {
				t.Fatal("logical room was cleared")
			}
		})
	}
}
