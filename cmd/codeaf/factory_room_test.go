package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
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

// bringBackFixture is one machine's saved conversations and teams for a
// drop's delete: a places root, a profile, and a maker of saved conversations
// under it.
func bringBackFixture(t *testing.T) (places, profile string, conversation func(id string) string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	places, profile = filepath.Join(root, "projects"), filepath.Join(root, "profile")
	conversation = func(id string) string {
		t.Helper()
		dir := filepath.Join(places, "-work", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: root}); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "transcript.jsonl")
		if err := os.WriteFile(file, []byte("{\"type\":\"header\",\"version\":1,\"id\":\""+id+"\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	return places, profile, conversation
}

func conversationKept(t *testing.T, file string) bool {
	t.Helper()
	_, err := os.Stat(file)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// A DROP DELETES THE JOB'S OWN CREW AND CONVERSATIONS AND NOTHING ELSE: the
// job's agent, the step chat it recorded, an earlier round's chat it recorded
// on the step, an earlier round's step the factory stamped in its crew, and the
// crew itself go; a conversation a person put in that crew loses the crew and
// is kept; another job's crew and conversations, a person's own crew and the
// one `factory` team are untouched. A FRESH RUN THEN MAKES A ROOM OF ITS OWN,
// even though it would have found the old one by its name.
func TestADropDeletesOnlyTheJobsOwnCrewAndConversations(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	talk, stage, earlier, mine := conversation("job-talk"), conversation("job-stage"), conversation("job-stage-round-1"), conversation("mine")
	round := conversation("job-write-round-1")
	otherTalk, otherStage, lead := conversation("other-talk"), conversation("other-stage"), conversation("my-lead")
	job := factory.Item{ID: 41, Num: 41, Repo: "acme/api", Title: "Total double-counts", Talk: talk,
		Stream: &factory.Stream{Room: teams.NewID(), Phases: []factory.Phase{{Name: "write", Chat: stage, Rounds: []factory.RoundLine{{Round: 1, Chat: round}}}}}}
	other := factory.Item{ID: 42, Num: 42, Repo: "acme/api", Title: "Another job", Talk: otherTalk,
		Stream: &factory.Stream{Room: teams.NewID(), Phases: []factory.Phase{{Name: "write", Chat: otherStage}}}}
	parent, crew := teams.NewID(), teams.NewID()
	if err := teams.Save(profile, []teams.Team{
		{ID: parent, Name: factoryTeamName, FactoryManaged: true},
		{ID: job.Stream.Room, Name: talkTeamName(job), Parent: parent, FactoryManaged: true, Manager: talk, Members: []teams.Member{
			{Key: talk, File: talk, Handle: "item41"},
			{Key: earlier, File: earlier, Handle: "plan", FactoryStage: true},
			{Key: stage, File: stage, Handle: "write", FactoryStage: true},
			{Key: mine, File: mine, Handle: "mine"},
		}},
		{ID: other.Stream.Room, Name: talkTeamName(other), Parent: parent, FactoryManaged: true, Manager: otherTalk, Members: []teams.Member{
			{Key: otherTalk, File: otherTalk, Handle: "item42"},
			{Key: otherStage, File: otherStage, Handle: "write", FactoryStage: true},
		}},
		{ID: crew, Name: "my crew", Manager: lead, Members: []teams.Member{{Key: lead, File: lead, Handle: "lead"}, {Key: mine, File: mine, Handle: "mine"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := deleteJobChats(profile, places, job, nil); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{talk, stage, earlier, round} {
		if conversationKept(t, gone) {
			t.Fatalf("the job's conversation %s was kept", gone)
		}
	}
	for _, kept := range []string{mine, otherTalk, otherStage, lead} {
		if !conversationKept(t, kept) {
			t.Fatalf("a conversation that is not the job's was deleted: %s", kept)
		}
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Team(job.Stream.Room); ok {
		t.Fatal("the job's crew was kept")
	}
	if _, err := os.Stat(teams.TeamDir(profile, job.Stream.Room)); !os.IsNotExist(err) {
		t.Fatalf("the job's crew's history was kept: %v", err)
	}
	if t2, ok := f.Team(other.Stream.Room); !ok || t2.Closed() || !t2.Holds(otherTalk) || !t2.Holds(otherStage) {
		t.Fatalf("another job's crew was touched: %+v", t2)
	}
	if c, ok := f.Team(crew); !ok || c.Closed() || !c.Holds(mine) || !c.Holds(lead) {
		t.Fatalf("a person's own crew was touched: %+v", c)
	}
	if p, ok := f.Team(parent); !ok || p.Closed() {
		t.Fatal("the factory team was touched")
	}
	// The job as a drop leaves it: no room, no conversation, a new run.
	fresh := job
	fresh.Talk, fresh.Stream = "", &factory.Stream{}
	room, err := factoryItemTeam(profile, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if room == "" || room == job.Stream.Room || room == other.Stream.Room {
		t.Fatalf("the fresh run's room %q is not its own", room)
	}
}

// A JOB THAT NEVER RAN still has its agent's conversation, in the crew the
// factory made for it by its name: a drop finds that crew by the conversation
// the job recorded, and deletes both.
func TestADropFindsTheCrewOfAJobThatNeverRan(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	talk, lead := conversation("job-talk"), conversation("my-lead")
	job := factory.Item{ID: 7, Num: 7, Repo: "acme/api", Title: "Never ran", Talk: talk}
	parent, room, crew := teams.NewID(), teams.NewID(), teams.NewID()
	if err := teams.Save(profile, []teams.Team{
		{ID: parent, Name: factoryTeamName, FactoryManaged: true},
		{ID: room, Name: talkTeamName(job), Parent: parent, FactoryManaged: true, Manager: talk, Members: []teams.Member{{Key: talk, File: talk, Handle: "item7"}}},
		{ID: crew, Name: talkTeamName(job), Manager: lead, Members: []teams.Member{{Key: lead, File: lead, Handle: "lead"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := deleteJobChats(profile, places, job, nil); err != nil {
		t.Fatal(err)
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Team(room); ok || conversationKept(t, talk) {
		t.Fatal("the job's crew or conversation was kept")
	}
	if _, ok := f.Team(crew); !ok || !conversationKept(t, lead) {
		t.Fatal("a person's crew of the same name was deleted")
	}
}

// A CREW THAT CANNOT BE DELETED IS CLOSED, and the delete says so: the job's
// conversations still go, and a fresh run never joins the closed crew by its
// name, so the job still starts in a room of its own.
func TestADropClosesACrewItCannotDelete(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	talk, helper := conversation("job-talk"), conversation("helper")
	job := factory.Item{ID: 41, Num: 41, Repo: "acme/api", Title: "Total double-counts", Talk: talk, Stream: &factory.Stream{Room: teams.NewID()}}
	parent, nested := teams.NewID(), teams.NewID()
	if err := teams.Save(profile, []teams.Team{
		{ID: parent, Name: factoryTeamName, FactoryManaged: true},
		{ID: job.Stream.Room, Name: talkTeamName(job), Parent: parent, FactoryManaged: true, Manager: talk, Members: []teams.Member{{Key: talk, File: talk, Handle: "item41"}}},
		{ID: nested, Name: "helpers", Parent: job.Stream.Room, Manager: helper, Members: []teams.Member{{Key: helper, File: helper, Handle: "helper"}}},
	}); err != nil {
		t.Fatal(err)
	}
	err := deleteJobChats(profile, places, job, nil)
	if err == nil || !strings.Contains(err.Error(), "the job's crew was closed, not deleted") {
		t.Fatalf("a crew that was kept was not reported: %v", err)
	}
	if conversationKept(t, talk) || !conversationKept(t, helper) {
		t.Fatal("the job's conversation was kept, or one that is not the job's was deleted")
	}
	f, err := teams.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := f.Team(job.Stream.Room); !ok || !r.Closed() {
		t.Fatalf("the crew is neither deleted nor closed: %+v", r)
	}
	if n, ok := f.Team(nested); !ok || !n.Holds(helper) {
		t.Fatal("the crew inside it was deleted")
	}
	fresh := job
	fresh.Talk, fresh.Stream = "", &factory.Stream{}
	room, err := factoryItemTeam(profile, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if room == "" || room == job.Stream.Room {
		t.Fatalf("the fresh run joined the closed crew: %q", room)
	}
}
