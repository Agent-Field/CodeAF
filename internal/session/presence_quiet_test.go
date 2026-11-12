package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// AN IDLE CHAT WRITES NOTHING BETWEEN ITS HEARTBEATS. The sixth live A+B run's
// engine idled at 7–9% of a core with 24 idle job and step chats open, and
// every one of them rewrote its presence file every seven seconds and looked
// at its doorstep every five. This drives one real desk on a fake clock — the
// shared maintenance and refresh clocks are both the test's — through a whole
// idle refresh period and fails if the file is replaced before the heartbeat
// is due, then checks that the heartbeat lands, that readers still believe
// the claim between heartbeats, and that a change is written at once.
func TestAnIdleChatWritesNothingBetweenHeartbeats(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, presenceName)
	beats, refreshes := make(chan time.Time), make(chan time.Time)
	desk := &presenceDesk{
		agent: agent, path: path, ticks: beats, refreshTicks: refreshes,
		stop: make(chan struct{}), done: make(chan struct{}), nudge: make(chan struct{}, 1),
	}
	go desk.beat()
	defer func() { close(desk.stop); <-desk.done }()
	first := waitForPresence(t, dir)
	if first.LeaseSeconds != int(presenceIdleWindow/time.Second) {
		t.Fatalf("an idle claim was written with lease %ds, want %s", first.LeaseSeconds, presenceIdleWindow)
	}
	firstFile, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t0 := first.UpdatedAt
	// One simulated second at a time, both shared clocks at their own
	// cadences, for everything short of one idle refresh.
	for elapsed := time.Second; elapsed < presenceIdleRefresh; elapsed += time.Second {
		at := t0.Add(elapsed)
		if elapsed%presenceHeartbeat == 0 {
			beats <- at
		}
		if elapsed%presenceRefresh == 0 {
			refreshes <- at
		}
	}
	// The loop is sequential, so a further receive means the last tick was
	// handled.
	beats <- t0.Add(presenceIdleRefresh - time.Second)
	now, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(firstFile, now) || !now.ModTime().Equal(firstFile.ModTime()) {
		t.Fatal("an idle chat rewrote its presence file between heartbeats")
	}
	// Readers believe the unchanged claim all the while: past the busy window,
	// inside the lease.
	if _, ok := ReadSessionPresence(dir, t0.Add(presenceIdleRefresh)); !ok {
		t.Fatal("a reader dropped a live idle chat between its heartbeats")
	}
	// The heartbeat lands when it is due.
	beat := t0.Add(presenceIdleRefresh)
	refreshes <- beat
	beats <- beat
	refreshed, ok := ReadSessionPresence(dir, beat)
	if !ok || !refreshed.UpdatedAt.Equal(beat) {
		t.Fatalf("the idle heartbeat did not land: %+v", refreshed)
	}
	if _, ok := ReadSessionPresence(dir, beat.Add(presenceIdleWindow+time.Second)); ok {
		t.Fatal("an idle claim nobody refreshed never expired")
	}
	// A change is written on the next refresh tick without waiting out the
	// idle period, even with no nudge, and a busy claim goes back to the short
	// window every older reader uses.
	agent.mu.Lock()
	agent.running = true
	agent.mu.Unlock()
	defer func() { agent.mu.Lock(); agent.running = false; agent.mu.Unlock() }()
	changed := beat.Add(presenceRefresh)
	refreshes <- changed
	beats <- changed
	busy, ok := ReadSessionPresence(dir, changed)
	if !ok || busy.State != PresenceWorking || busy.LeaseSeconds != 0 || !busy.UpdatedAt.Equal(changed) {
		t.Fatalf("a change waited for the idle heartbeat: %+v", busy)
	}
	if busy.Window() != presenceWindow {
		t.Fatalf("a busy claim is believed for %s, want %s", busy.Window(), presenceWindow)
	}
}

// A QUIET SESSION IS KNOCKED ON ONLY WHERE THE WATCH CANNOT SEE. Its doorstep
// safety pass runs rarely because an answer or a request wakes it at once; a
// folder the kernel is not watching gets no such wake, so the shared beat
// knocks on it every time, exactly as the pass ran before.
func TestQuietDoorstepIsWokenByTheWatchOrKnockedWhereItIsBlind(t *testing.T) {
	var watch presenceDoorWatch
	watched := t.TempDir()
	blind := filepath.Join(t.TempDir(), "not-made-yet")
	seen, _, _, cancelSeen := watch.subscribe(watched)
	defer cancelSeen()
	unseen, _, _, cancelUnseen := watch.subscribe(blind)
	defer cancelUnseen()
	watch.mu.Lock()
	run := watch.run
	watch.mu.Unlock()
	watch.rescan(run, time.Now())
	select {
	case <-seen:
		t.Fatal("a watched folder was knocked on by the safety beat")
	default:
	}
	select {
	case <-unseen:
	default:
		t.Fatal("a folder the watch cannot see was not knocked on")
	}
	if err := deliverAnswer(watched, Answer{At: time.Now(), Kind: QuestionConsent, ID: 1, Key: "allow"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("an answer from another window waited for the safety pass")
	}
}
