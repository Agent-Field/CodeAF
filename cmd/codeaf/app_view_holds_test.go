package main

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// AN OPEN VIEW KEEPS ITS CONVERSATION ONLY AMONG THE MOST RECENTLY USED. A
// window opens every chat it shows and never says when it stops showing one,
// so each chat shown in a session used to stay held, with its journal and
// presence watch, until the app quit (#2471). Past the idle rule, only the
// [appViewHolds] most recently used views keep their hold, however many chats
// were shown.
func TestAppIdleViewsBeyondTheRecentHoldsAreLetGo(t *testing.T) {
	var mu sync.Mutex
	var opens, releases int
	open := func(ask appOpenAsk) (*appConn, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		return &appConn{
			Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript,
			Release: func() { mu.Lock(); releases++; mu.Unlock() },
		}, nil
	}
	backend := newFakeBackend(newAppRecorder(), open)
	defer backend.Close()
	dir := t.TempDir()
	minted := 0
	backend.mint = func(string) (string, error) {
		minted++
		return filepath.Join(dir, fmt.Sprintf("chat%02d", minted), "transcript.jsonl"), nil
	}
	clock := time.Now()
	backend.now = func() time.Time { return clock }

	var ids []string
	view := func() {
		t.Helper()
		clock = clock.Add(time.Minute)
		agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.OpenAgent(agent.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, agent.ID)
	}
	held := func() map[string]bool {
		out := map[string]bool{}
		backend.locked(func() {
			for id, a := range backend.agents {
				if a.live != nil {
					out[id] = true
				}
			}
		})
		return out
	}

	const chats = appViewHolds + 4
	for range chats {
		view()
	}
	backend.sweepOnce()
	if got := len(held()); got != chats {
		t.Fatalf("within the idle rule %d of %d views are held", got, chats)
	}

	clock = clock.Add(appAgentIdle + time.Second)
	backend.sweepOnce()
	kept := held()
	if len(kept) != appViewHolds {
		t.Fatalf("after the idle rule %d views are held, want %d", len(kept), appViewHolds)
	}
	for i, id := range ids {
		if want := i >= len(ids)-appViewHolds; kept[id] != want {
			t.Fatalf("view %d held = %v, want only the %d most recently used", i, kept[id], appViewHolds)
		}
	}

	// Opening more chats and leaving them idle never grows the holds.
	for round := 0; round < 3; round++ {
		view()
		clock = clock.Add(appAgentIdle + time.Second)
		backend.sweepOnce()
		if got := len(held()); got != appViewHolds {
			t.Fatalf("round %d: %d views held, want %d", round, got, appViewHolds)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if releases != opens-appViewHolds {
		t.Fatalf("opened %d, released %d; want all but %d released", opens, releases, appViewHolds)
	}
}
