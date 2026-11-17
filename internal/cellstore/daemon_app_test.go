package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
)

// An idle desktop app must leave its saving helper alone so the helper reaches
// its own idle exit and stops waking. Saving work in flight keeps it warm.
func TestAppTouchesHelpersOnlyForSavingWork(t *testing.T) {
	isolateSavingWork(t)
	socket := testSocket(t)
	healths := healthRequests(t, socket)
	ownerExited := make(chan struct{})
	owner := &spawnedDaemon{socket: socket, exited: ownerExited}
	daemonOwners.Store(socket, owner)
	t.Cleanup(func() { daemonOwners.CompareAndDelete(socket, owner) })

	// One tick's work runs here, synchronously: the helper counts a health
	// before answering it, so a touch that returned has already been counted.
	tick := func() int {
		touchBusyAppDaemons(context.Background())
		n := len(healths)
		for len(healths) > 0 {
			<-healths
		}
		return n
	}
	quiet := func(when string) {
		t.Helper()
		if n := tick(); n != 0 {
			t.Fatalf("%s: an idle app touched its saving helper %d times", when, n)
		}
	}
	touched := func(when string) {
		t.Helper()
		if n := tick(); n != 1 {
			t.Fatalf("%s: saving work touched the helper %d times, want 1", when, n)
		}
	}

	quiet("before any chat")
	var chat SealWatch
	chat.Waiting()
	quiet("a chat waiting for input")
	chat.TurnStarted()
	chat.TurnStarted()
	touched("a running turn")
	chat.TurnEnded()
	quiet("after the turn ended")
	chat.TurnEnded()
	if n := liveTurns.Load(); n != 0 {
		t.Fatalf("repeated turn words left %d live turns", n)
	}

	root := t.TempDir()
	captures.Lock()
	captures.held[root] = &Recorder{}
	captures.Unlock()
	touched("held calls")
	captures.Lock()
	delete(captures.held, root)
	running := &captureState{done: make(chan struct{})}
	captures.cells[root] = running
	captures.Unlock()
	touched("a running capture")
	captures.Lock()
	running.err = errors.New("seal failed")
	close(running.done)
	captures.Unlock()
	quiet("a finished capture kept only for its error")

	engineCommits.Lock()
	engineCommits.cells[root] = engineDebt{root: root, generation: 1}
	engineCommits.Unlock()
	touched("a seal owing its engine sync")
	engineCommits.Lock()
	engineCommits.cells[root] = engineDebt{root: root}
	engineCommits.Unlock()
	quiet("a synced seal")

	close(ownerExited)
	chat.TurnStarted()
	defer chat.TurnEnded()
	quiet("a helper that already exited")
}

// isolateSavingWork gives the test empty registries of saving work and returns
// what earlier tests left when it ends, so only this test's work is read.
func isolateSavingWork(t *testing.T) {
	t.Helper()
	turns := liveTurns.Swap(0)
	captures.Lock()
	held, cells := captures.held, captures.cells
	captures.held, captures.cells = map[string]*Recorder{}, map[string]*captureState{}
	captures.Unlock()
	engineCommits.Lock()
	debts := engineCommits.cells
	engineCommits.cells = map[string]engineDebt{}
	engineCommits.Unlock()
	t.Cleanup(func() {
		liveTurns.Add(turns)
		captures.Lock()
		captures.held, captures.cells = held, cells
		captures.Unlock()
		engineCommits.Lock()
		engineCommits.cells = debts
		engineCommits.Unlock()
	})
}

// healthRequests serves socket as a helper and signals each health request
// before answering it, so a finished touch has always been counted.
func healthRequests(t *testing.T, socket string) <-chan struct{} {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan struct{}, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req wireRequest
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			_ = json.Unmarshal(line, &req)
			if req.Verb == "health" {
				got <- struct{}{}
			}
			reply, _ := json.Marshal(wireResponse{ID: req.ID, Ok: json.RawMessage(`{}`)})
			_, _ = conn.Write(append(reply, '\n'))
			conn.Close()
		}
	}()
	return got
}
