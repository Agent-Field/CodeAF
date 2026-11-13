package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestRemoteTalkDoesNotReuseIdenticalLocalJob(t *testing.T) {
	floor, web, profile := talkLab(t)
	local, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "web", Num: 9, Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Kind: factory.KindIssue, State: factory.StateNew, Title: "same forge issue"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := talkMaker(floor, web, profile)(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := store.Open(filepath.Join(floor.Root(), "workers", strconv.Itoa(os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	remote := api.ToItem(api.FromItem(local))
	if err := mirror.Save(remote); err != nil {
		t.Fatal(err)
	}
	base := "https://factory.example"
	factoryLocalLease(mirror)(base, api.WorkerLease{ID: "remote-lease", Job: remote.ID, Worker: "here", ExpiresAt: time.Now().Add(time.Hour)})
	doors := factoryLocalJobs(floor, base, web, profile, factory.Snapshot{Items: []factory.Item{remote}}, nil)
	door := doors[remote.ID].seam.Talk
	if door == nil {
		t.Fatal("remote job has no Talk door")
	}
	second, err := door(context.Background(), remote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("remote factory job got local factory manager %q (same ID, Repo, Num, Created)", second)
	}
	held, err := managerMaker(floor, web, profile)(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	if held != first {
		t.Fatalf("owned runner lost held manager: got %q, want %q", held, first)
	}
}

// A worker's mirror goes when the worker ends; the conversation's folder stays
// with the floor.
func TestRemoteTalkWorksInTheFloorsFolder(t *testing.T) {
	floor, web, profile := talkLab(t)
	mirror, err := store.Open(filepath.Join(floor.Root(), "workers", strconv.Itoa(os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	remote := factory.Item{ID: 41, Repo: "elsewhere", Title: "remote", Kind: factory.KindIssue, State: factory.StateNew}
	if err := mirror.Save(remote); err != nil {
		t.Fatal(err)
	}
	base := "https://factory.example"
	factoryLocalLease(mirror)(base, api.WorkerLease{ID: "remote-lease", Job: remote.ID, Worker: "here", ExpiresAt: time.Now().Add(time.Hour)})
	doors := factoryLocalJobs(floor, base, web, profile, factory.Snapshot{Items: []factory.Item{remote}}, nil)
	chat, err := doors[remote.ID].seam.Talk(context.Background(), remote.ID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := v3ProjectDir(filepath.Join(floor.Root(), "chats", strconv.Itoa(remote.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(chat)) != want {
		t.Fatalf("the conversation is in %q, want the floor's %q", chat, want)
	}
}
