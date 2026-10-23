package main

// The door a job's own conversation spends from the job's budget through:
// it finds the job by its Talk, books the chat's dollars where the job's
// budget is checked (a live worker's mirror while one runs it, the floor
// otherwise), and refuses a turn once the budget is spent.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func chatSpendFloor(t *testing.T) (*store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	talk := filepath.Join(t.TempDir(), "manager", "transcript.jsonl")
	return st, talk
}

// thisWorkersMirror is a mirror of this (live) process's worker, laid out as
// appFactoryHost.runWorker lays it out.
func thisWorkersMirror(t *testing.T, floor *store.Store) *store.Store {
	t.Helper()
	m, err := store.Open(filepath.Join(floor.Root(), "workers", strconv.Itoa(os.Getpid()), "0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Before Start the chat's dollars are the job's ChatSpent on the floor, the
// job's cost reads them, and at the budget the chat is refused in the job's
// words. Another conversation is no job's.
func TestAJobsChatSpendsOnTheFloorBeforeItsRun(t *testing.T) {
	st, talk := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "fix it", State: factory.StateNew, Cap: 0.5, Talk: talk})
	if err != nil {
		t.Fatal(err)
	}
	door := jobChatDoorOf(st)
	if door.Chat(filepath.Join(filepath.Dir(talk), "other.jsonl")) != nil {
		t.Fatal("a conversation that is no job's was answered as one")
	}
	chat := door.Chat(talk)
	if chat == nil {
		t.Fatal("the job's own conversation was not found by its Talk")
	}
	chat.Spend(0.2)
	chat.Spend(0.3)
	got, _ := st.Get(it.ID)
	if got.ChatSpent != 0.5 || got.Stream != nil || factory.JobSpent(got) != 0.5 {
		t.Fatalf("chat $%v, stream %+v; want $0.50 on the job with no run made", got.ChatSpent, got.Stream)
	}
	if want := fmt.Sprintf(factoryrun.ChatBudgetReached, "$0.50", "$0.50"); chat.Refusal() != want {
		t.Fatalf("refusal %q, want %q", chat.Refusal(), want)
	}
	if jobChatDoorOf(nil) != nil {
		t.Fatal("a door with no floor is a door")
	}
}

// While this machine's worker runs the job, the chat's dollars go on the
// worker's mirror, where the runner checks the budget after every round and
// from where its report carries them to the factory once; the floor's figure
// is not touched. A mirror of another factory's job with the same number is
// not this job.
func TestAJobsChatSpendsOnTheMirrorOfTheWorkerRunningIt(t *testing.T) {
	st, talk := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "fix it", State: factory.StateQueued, Cap: 5, Talk: talk, Stream: &factory.Stream{Spent: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mirror := thisWorkersMirror(t, st)
	held := it
	held.State, held.Stream = factory.StateRunning, &factory.Stream{Spent: 1}
	if err := mirror.Save(held); err != nil {
		t.Fatal(err)
	}
	chat := jobChatDoorOf(st).Chat(talk)
	chat.Spend(0.25)
	onMirror, _ := mirror.Get(it.ID)
	onFloor, _ := st.Get(it.ID)
	if onMirror.Stream.Spent != 1.25 || onFloor.Stream.Spent != 1 {
		t.Fatalf("mirror $%v, floor $%v; want the worker's run to take it and the floor to wait for its report", onMirror.Stream.Spent, onFloor.Stream.Spent)
	}

	// The mirror now holds another factory's job #1 (its run at $1), whose
	// chat is someone else's: this chat's money goes on the floor.
	other := held
	other.Talk = filepath.Join(filepath.Dir(talk), "someone-elses.jsonl")
	if err := mirror.Save(other); err != nil {
		t.Fatal(err)
	}
	chat.Spend(0.5)
	onMirror, _ = mirror.Get(it.ID)
	onFloor, _ = st.Get(it.ID)
	if onMirror.Stream.Spent != 1 || onFloor.Stream.Spent != 1.5 {
		t.Fatalf("mirror $%v, floor $%v; another job of the same number took this chat's money", onMirror.Stream.Spent, onFloor.Stream.Spent)
	}
}

// A job of another machine's factory has no record on this floor: a worker
// here running it is where its chat is found, and spends.
func TestAJobOfAnotherFactoryIsFoundOnTheMirrorRunningIt(t *testing.T) {
	st, talk := chatSpendFloor(t)
	mirror := thisWorkersMirror(t, st)
	if err := mirror.Save(factory.Item{ID: 41, Repo: "acme/api", Title: "theirs", State: factory.StateRunning, Talk: talk, Stream: &factory.Stream{}}); err != nil {
		t.Fatal(err)
	}
	chat := jobChatDoorOf(st).Chat(talk)
	if chat == nil {
		t.Fatal("the job a worker here runs was not found")
	}
	chat.Spend(0.1)
	if got, _ := mirror.Get(41); got.Stream.Spent != 0.1 {
		t.Fatalf("mirror $%v, want $0.10", got.Stream.Spent)
	}
}

func TestEveryOwnedStageChatSharesBudgetWithoutManagerAuthority(t *testing.T) {
	st, talk := chatSpendFloor(t)
	stage, old := filepath.Join(filepath.Dir(talk), "stage.jsonl"), filepath.Join(filepath.Dir(talk), "old-stage.jsonl")
	it, err := st.Create(factory.Item{Repo: "acme/api", Cap: 5, Talk: talk, Stream: &factory.Stream{Spent: 4, Phases: []factory.Phase{{Name: "write", Chat: stage, Rounds: []factory.RoundLine{{Round: 1, Chat: old}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	door := jobChatDoorOf(st)
	for _, transcript := range []string{stage, old} {
		budget := door.Chat(transcript)
		if budget == nil {
			t.Fatal("owned historical stage has no budget")
		}
		if _, manager := budget.(interface {
			MaintainPR(context.Context, string) (string, error)
		}); manager {
			t.Fatal("stage gained manager authority")
		}
		budget.Spend(0.5)
	}
	got, _ := st.Get(it.ID)
	if factory.JobSpent(got) != 5 || door.Chat(stage).Refusal() == "" {
		t.Fatal("stage views bypassed job budget")
	}
}
