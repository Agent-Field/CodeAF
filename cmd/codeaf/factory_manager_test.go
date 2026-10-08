package main

// The item's conversation is the manager of its run: a launch makes it the way
// `T` does when the item has none, names it the lead of the item's team (the
// team the stages' conversations join), reports every stage into its journal,
// and reads what the person typed there as the run's brief.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// itemTeamLead is the item's team under `factory` and its lead's key.
func itemTeamLead(t *testing.T, profile string, it factory.Item) (teams.Team, int) {
	t.Helper()
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	var found teams.Team
	n := 0
	for _, team := range f.Teams {
		if team.Name == talkTeamName(it) && !team.Closed() {
			found = team
			n++
		}
	}
	return found, n
}

func realKey(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

func TestTheManagerIsMadeWhenMissingAndLeadsTheItemsTeam(t *testing.T) {
	st, web, profile := talkLab(t)
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "fix the ledger double count"})
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	// A RUN BEFORE ANY `T` already made the item's team for its stages.
	stageTeam, err := factoryItemTeam(profile, it)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := managerMaker(st, web, profile)(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(chat)
	if !strings.Contains(string(data), talkManager) {
		t.Fatalf("the manager's brief does not tell it it is the manager:\n%s", data)
	}
	team, n := itemTeamLead(t, profile, it)
	if n != 1 || team.ID != stageTeam {
		t.Fatalf("%d teams named for the item, the lead's is %q, the stages' %q", n, team.ID, stageTeam)
	}
	if team.Manager != realKey(chat) {
		t.Fatalf("the item's team is led by %q, want its conversation %q", team.Manager, realKey(chat))
	}
	if again, _ := factoryItemTeam(profile, it); again != stageTeam {
		t.Fatalf("the stages now join %q, not the manager's team %q", again, stageTeam)
	}
}

func TestTheTalkDoorAndTheRunMakeOneManager(t *testing.T) {
	st, web, profile := talkLab(t)
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, web)),
		factory.WithTalk(talkMaker(st, web, profile)))
	id, err := seam.New("web", "make the meter honest")
	if err != nil {
		t.Fatal(err)
	}
	chat, err := seam.Talk(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	team, n := itemTeamLead(t, profile, it)
	if n != 1 || team.Manager != realKey(chat) {
		t.Fatalf("after T: %d item teams, led by %q; want one led by %q", n, team.Manager, realKey(chat))
	}
	got, err := managerMaker(st, web, profile)(context.Background(), it)
	if err != nil || got != chat {
		t.Fatalf("the run's maker answered %q, %v; want T's conversation %q", got, err, chat)
	}
	if again, n := itemTeamLead(t, profile, it); n != 1 || again.ID != team.ID || again.Manager != team.Manager || len(again.Members) != len(team.Members) {
		t.Fatalf("the run's maker changed the team: %+v → %+v", team, again)
	}
}

func TestARunReportsIntoTheManagersJournalAndReadsItsBrief(t *testing.T) {
	st, web, profile := talkLab(t)
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "fix the ledger double count",
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, Ask: "plan it", Until: factory.UntilDone, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	chat, err := talkMaker(st, web, profile)(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error { it.Talk = chat; return nil }); err != nil {
		t.Fatal(err)
	}
	// The person told the manager something before the run.
	f, err := os.OpenFile(chat, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"message","role":"user","content":"keep the old API","timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}` + "\n")
	_ = f.Close()

	var mu sync.Mutex
	var notes []string
	r := factoryrun.New(factoryrun.Options{
		Store: st,
		Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageChat: factoryrun.ExecutorFunc(func(_ context.Context, job factoryrun.Job) (factory.StageResult, error) {
				mu.Lock()
				notes = append([]string(nil), job.Notes...)
				mu.Unlock()
				return factory.StageResult{Done: true, Notes: []string{"Read the ledger first."}}, nil
			}),
		},
		Manager:  managerMaker(st, web, profile),
		Talk:     sessionTalk{},
		TalkPoll: time.Hour,
	})
	if err := r.Launch(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := session.LastSaid(chat); got == "landed · proof sheet ready · your approval" {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(chat)
			t.Fatalf("the manager's journal never said the landing:\n%s", data)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if strings.Join(notes, "|") != "keep the old API" {
		t.Errorf("plan's brief read %q, want what the person told the manager", notes)
	}
	mu.Unlock()
	data, _ := os.ReadFile(chat)
	for _, want := range []string{`"content":"plan started"`, `"content":"plan done · Read the ledger first."`,
		`"presentation":{"audience":"human","kind":"factory-progress"}`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the manager's journal lacks %s:\n%s", want, data)
		}
	}
	if team, _ := itemTeamLead(t, profile, it); team.Manager != realKey(chat) {
		t.Errorf("the item's team is led by %q, want its conversation", team.Manager)
	}
}
