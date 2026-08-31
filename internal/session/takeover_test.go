package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A REQUEST LEFT BESIDE THE PRESENCE FILE IS ANSWERED ON THE TICK: taken off
// disk, announced once on the task lane, and remembered.
func TestATakeoverRequestIsTakenAnnouncedOnceAndRemembered(t *testing.T) {
	agent, dir := questionSession(t, "takeover", nil)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	drainRoster(lane)

	if err := AskTakeover(dir); err != nil {
		t.Fatal(err)
	}
	agent.drainTakeover()
	select {
	case ev := <-lane:
		if ev.Kind != EventTakeover || ev.Text != TakeoverWord {
			t.Fatalf("lane carried %v %q, want the take-over", ev.Kind, ev.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no take-over reached the task lane")
	}
	if _, err := os.Stat(TakeoverPath(dir)); !os.IsNotExist(err) {
		t.Fatal("the request was announced and left on disk")
	}
	if !agent.TakeoverAsked() {
		t.Fatal("the agent forgot it was asked")
	}
	// A SECOND TICK SAYS NOTHING MORE.
	agent.drainTakeover()
	select {
	case ev := <-lane:
		t.Fatalf("a second tick announced %v again", ev.Kind)
	case <-time.After(200 * time.Millisecond):
	}
}

// A TURN IN FLIGHT LEAVES THE REQUEST WHERE IT IS, and the tick after the turn
// ends answers it. A reply is never cut by a window that asked.
func TestATakeoverWaitsForTheTurnToEnd(t *testing.T) {
	agent, dir := questionSession(t, "takeover", nil)
	if err := AskTakeover(dir); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.running = true
	agent.mu.Unlock()
	agent.drainTakeover()
	if _, err := os.Stat(TakeoverPath(dir)); err != nil {
		t.Fatal("the request was taken while a turn was running")
	}
	if agent.TakeoverAsked() {
		t.Fatal("the agent announced a take-over mid-turn")
	}
	agent.mu.Lock()
	agent.running = false
	agent.mu.Unlock()
	agent.drainTakeover()
	if !agent.TakeoverAsked() {
		t.Fatal("the tick after the turn did not answer the request")
	}
}

// A REQUEST NOBODY IS WAITING ON ANY MORE IS NOT ANSWERED: one withdrawn, and
// one left by a window that died asking.
func TestAWithdrawnOrStaleTakeoverIsIgnored(t *testing.T) {
	agent, dir := questionSession(t, "takeover", nil)
	if err := AskTakeover(dir); err != nil {
		t.Fatal(err)
	}
	CancelTakeover(dir)
	agent.drainTakeover()
	if agent.TakeoverAsked() {
		t.Fatal("a withdrawn request was answered")
	}
	stale := `{"at":"` + time.Now().Add(-2*takeoverStale).Format(time.RFC3339Nano) + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, takeoverName), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	agent.drainTakeover()
	if agent.TakeoverAsked() {
		t.Fatal("a stale request was answered")
	}
	if _, err := os.Stat(TakeoverPath(dir)); !os.IsNotExist(err) {
		t.Fatal("a stale request was left on disk")
	}
	if err := AskTakeover(""); err == nil {
		t.Fatal("a conversation with no folder took a request")
	}
}

// drainRoster empties the roster replay a fresh task lane opens with.
func drainRoster(lane <-chan Event) {
	for {
		select {
		case <-lane:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}
