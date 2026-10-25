package main

import (
	"github.com/Agent-Field/codeaf/internal/home"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// crewHeldIdleMember is an agent whose conversation was let go while an open
// crew still lists it, which is what every idle member is after a launch.
func crewHeldIdleMember(t *testing.T) (*appBackend, *appAgent, *int) {
	t.Helper()
	// The remembered lists go under the cache root, which is the test's own.
	t.Setenv(home.EnvVar, t.TempDir())
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	open := fakeOpener(conv, &opens, &releases, &mu)
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		conn, err := open(ask)
		if conn != nil && ask.Transcript != "" {
			conn.Transcript = ask.Transcript
		}
		return conn, err
	})
	backend.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() {
		backend.locked(func() { backend.crewRun = nil })
		backend.Close()
	})
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a, err := backend.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.gate.Lock()
	backend.release(a)
	a.gate.Unlock()
	backend.locked(func() {
		backend.crewRun = &appCrewRun{held: map[string][]string{agent.ID: {"crew"}}}
	})
	return backend, a, &opens
}

// READING AN IDLE CREW MEMBER DOES NOT KEEP ITS CONVERSATION. Listing tasks
// and asks answers for a member at rest without leaving its journal replayed in
// memory, which for a crew of hundreds of members was the bulk of the engine's
// resident set. The task list is the owner-backed one, built once and
// remembered until a file it was built from changes.
func TestAppReadingAnIdleCrewMemberDoesNotKeepItsConversation(t *testing.T) {
	backend, a, opens := crewHeldIdleMember(t)
	dir := t.TempDir()
	var id string
	// Every open keeps the requested transcript, as a real conversation does.
	place := func() {
		backend.locked(func() { a.transcript, id = filepath.Join(dir, "transcript.jsonl"), a.view.ID })
	}
	place()
	before := *opens
	for i := 0; i < 3; i++ {
		place()
		if _, err := backend.ListTasks(id); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.ListAsks(id); err != nil {
			t.Fatal(err)
		}
		if backend.liveOf(a) != nil {
			t.Fatal("the read left a conversation held")
		}
	}
	if got := *opens - before; got != 1 {
		t.Fatalf("three reads opened the conversation %d times, want the one that built the list", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	place()
	if _, err := backend.ListTasks(id); err != nil {
		t.Fatal(err)
	}
	if got := *opens - before; got != 2 {
		t.Fatalf("a changed task file was read with %d opens in all, want the list rebuilt once more", got)
	}
}

// A MEMBER WITH WORK IN FLIGHT IS STILL OPENED BY A READ: its presence says a
// turn is running, so the held conversation is the only complete answer.
func TestAppReadingABusyCrewMemberOpensItsConversation(t *testing.T) {
	backend, a, opens := crewHeldIdleMember(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), []byte(`{"schema":1,"state":"working"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var id string
	backend.locked(func() { a.transcript, id = filepath.Join(dir, "transcript.jsonl"), a.view.ID })
	before := *opens
	if !backend.crewKeeps(id) || backend.memberRests(a) {
		t.Fatal("a member with a working presence was taken for resting")
	}
	_, _ = backend.ListTasks(id)
	if *opens-before != 1 {
		t.Fatalf("a busy member was opened %d times by a read, want once", *opens-before)
	}
}

// THE LIST A MEMBER AT REST WAS BUILT WITH SURVIVES A RELAUNCH. Building it
// replays the member's whole journal, which for a long conversation is the
// larger part of the engine's memory for the moment it takes; the answer is
// kept beside the other caches under the same stamp, so a launch that finds the
// files unchanged reads a small file instead.
func TestAppReadsARestingMembersTasksFromTheCacheAfterARelaunch(t *testing.T) {
	backend, a, opens := crewHeldIdleMember(t)
	dir := t.TempDir()
	var id string
	place := func() {
		backend.locked(func() { a.transcript, id = filepath.Join(dir, "transcript.jsonl"), a.view.ID })
	}
	place()
	before := *opens
	if _, err := backend.ListTasks(id); err != nil {
		t.Fatal(err)
	}
	if got := *opens - before; got != 1 {
		t.Fatalf("the first list opened %d times, want 1", got)
	}

	// A relaunch keeps the files and loses the engine's memory.
	backend.restMemo = appRestMemo{}
	place()
	if _, err := backend.ListTasks(id); err != nil {
		t.Fatal(err)
	}
	if got := *opens - before; got != 1 {
		t.Fatalf("after a relaunch the list opened the conversation again (%d opens in all)", got)
	}

	// A file the list was built from changing makes it stale on disk too.
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	backend.restMemo = appRestMemo{}
	place()
	if _, err := backend.ListTasks(id); err != nil {
		t.Fatal(err)
	}
	if got := *opens - before; got != 2 {
		t.Fatalf("a changed task file was answered from the cache (%d opens in all)", got)
	}
}

// A CREW MEMBER WITH NOTHING TO DO IS LET GO LIKE ANY OTHER once it has idled.
// It used to stay held for as long as its crew was open, so a person who had
// looked at a hundred members in a day kept a hundred journals in memory until
// the app quit. The crew opens a member again when traffic comes for it.
func TestAppLetsAnIdleCrewMemberGoAfterTheIdleTime(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	conv := &fakeConversation{}
	var mu sync.Mutex
	var opens, releases int
	backend := newFakeBackend(newAppRecorder(), fakeOpener(conv, &opens, &releases, &mu))
	t.Cleanup(func() {
		backend.locked(func() { backend.crewRun = nil })
		backend.Close()
	})
	clock := time.Now()
	backend.now = func() time.Time { return clock }
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	backend.locked(func() {
		backend.crewRun = &appCrewRun{held: map[string][]string{agent.ID: {"crew"}}}
		backend.agents[agent.ID].crewHeld = true
	})
	released := func() int { mu.Lock(); defer mu.Unlock(); return releases }

	clock = clock.Add(appAgentIdle - time.Second)
	backend.sweepOnce()
	if released() != 0 {
		t.Fatal("a member was let go before the idle time")
	}
	clock = clock.Add(2 * time.Second)
	backend.sweepOnce()
	if released() != 1 {
		t.Fatalf("an idle crew member was released %d times, want once", released())
	}
	var crewHeld bool
	backend.locked(func() { crewHeld = backend.agents[agent.ID].crewHeld })
	if crewHeld {
		t.Fatal("the crew still counted the member as held, so it would be opened again at once")
	}
}
