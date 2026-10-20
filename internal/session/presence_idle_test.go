package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdlePresenceSkipsUnchangedStateAndRefreshesJSON(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(t.TempDir(), presenceName)
	desk := &presenceDesk{agent: agent, path: path}
	desk.write()
	raw, _ := os.ReadFile(path)
	before, _ := os.Stat(path)
	first := desk.refreshed
	for i := 0; i < 100; i++ {
		desk.write()
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an unchanged idle snapshot replaced or touched its file")
	}
	desk.refresh(first.Add(presenceRefresh))
	after, _ = os.Stat(path)
	later, _ := os.ReadFile(path)
	if os.SameFile(before, after) || bytes.Equal(raw, later) {
		t.Fatal("heartbeat did not atomically replace the JSON claim")
	}
	var oldReader struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal(later, &oldReader); err != nil {
		t.Fatal(err)
	}
	// A skipped refresh must not expire the claim before the next one lands.
	next := first.Add(3 * presenceRefresh)
	if !oldReader.UpdatedAt.After(next.Add(-presenceWindow)) {
		t.Fatal("an older reader would expire the claim after one missed refresh")
	}
	desk.refresh(next)
	if _, ok := ReadSessionPresence(filepath.Dir(path), next); !ok {
		t.Fatal("claim expired after a missed refresh")
	}
	if desk.active {
		t.Fatal("idle presence needs active maintenance")
	}
	desk.agent.mu.Lock()
	desk.agent.running = true
	desk.agent.mu.Unlock()
	desk.write()
	presence, ok := ReadSessionPresence(filepath.Dir(path), time.Now())
	if !ok || presence.State != PresenceWorking {
		t.Fatalf("changed state did not land: %+v", presence)
	}
	desk.agent.mu.Lock()
	desk.agent.running = false
	desk.agent.mu.Unlock()
}

func TestPresenceDoorstepSharesWatchAndClock(t *testing.T) {
	var watch presenceDoorWatch
	one, two := t.TempDir(), t.TempDir()
	requests, beats, refreshes, cancel := watch.subscribe(one)
	defer cancel()
	_, otherBeats, otherRefreshes, otherCancel := watch.subscribe(two)
	defer otherCancel()
	watch.mu.Lock()
	run := watch.run
	if len(run.clients) != 2 || len(run.watched) != 2 {
		t.Fatal("sessions did not share one watcher")
	}
	watch.mu.Unlock()
	select {
	case <-beats:
		t.Fatal("idle subscription woke without an input or safety pass")
	default:
	}
	if err := AskTakeover(one); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("takeover waited for the safety clock")
	}
	now := time.Now()
	watch.refresh(run, now)
	for _, ticks := range []<-chan time.Time{refreshes, otherRefreshes} {
		select {
		case at := <-ticks:
			if !at.Equal(now) {
				t.Fatal("unexpected refresh clock")
			}
		default:
			t.Fatal("shared refresh missed a session")
		}
	}
	watch.rescan(run, now)
	for _, ticks := range []<-chan time.Time{beats, otherBeats} {
		select {
		case at := <-ticks:
			if !at.Equal(now) {
				t.Fatal("unexpected safety clock")
			}
		default:
			t.Fatal("shared safety pass missed a session")
		}
	}
}

func TestPresenceWatcherDeliversTakeoverWithoutHeartbeat(t *testing.T) {
	agent, dir := newPresenceSession(t, t.TempDir(), "eeee1111eeee1111")
	waitForPresence(t, dir)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	drainRoster(lane)
	if err := AskTakeover(dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-lane:
			if event.Kind == EventTakeover {
				return
			}
		case <-deadline.C:
			t.Fatal("request did not land before the five-second safety clock")
		}
	}
}

func TestClosedSignalWorksBeforeAndAfterClose(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	signal := agent.ClosedSignal()
	select {
	case <-signal:
		t.Fatal("open agent signalled closed")
	default:
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-signal:
	default:
		t.Fatal("close did not signal subscribers")
	}
	if signal != agent.ClosedSignal() {
		t.Fatal("late subscriber received a different signal")
	}
	late, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	_ = late.Close()
	select {
	case <-late.ClosedSignal():
	default:
		t.Fatal("subscription after close missed the signal")
	}
}

func TestPresenceWatcherDeliversDeletionWithoutHeartbeat(t *testing.T) {
	agent, dir := newPresenceSession(t, t.TempDir(), "dddd1111dddd1111")
	waitForPresence(t, dir)
	// A task that does not exist still owes an error receipt. Receiving it
	// proves the owner consumed the request rather than the requester timing out.
	taskResult := make(chan error, 1)
	go func() { taskResult <- requestTaskDeletion(filepath.Join(dir, placeTranscript), "missing") }()
	select {
	case err := <-taskResult:
		if err == nil {
			t.Fatal("missing task unexpectedly deleted")
		}
	case <-time.After(time.Second):
		t.Fatal("task request waited for safety scan")
	}
	closed := agent.ClosedSignal()
	if err := writePresenceRequest(filepath.Join(dir, conversationDeleteRequest), []byte(time.Now().UTC().Format(time.RFC3339Nano))); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("conversation deletion waited for safety scan")
	}
}

func TestPresenceJSONClockExpiresAndFeedsEveryReader(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	root := t.TempDir()
	dir := filepath.Join(root, "bucket", "ffffffff11111111")
	desk := &presenceDesk{agent: agent, path: filepath.Join(dir, presenceName)}
	desk.write()
	first := desk.refreshed
	now := first.Add(presenceWindow + time.Second)
	desk.refresh(now)
	presence, ok := ReadSessionPresence(dir, now)
	if !ok || !presence.UpdatedAt.Equal(now) {
		t.Fatalf("live refreshed claim missing: %+v", presence)
	}
	anyAge, ok := ReadSessionPresenceAnyAge(dir)
	if !ok || !anyAge.Fresh(now) {
		t.Fatal("holder reader ignored the JSON clock")
	}
	if rows := SweepStaleBuilds(root, "different-build", now); len(rows) != 1 {
		t.Fatalf("build sweep ignored refreshed claim: %+v", rows)
	}
	expired := now.Add(presenceWindow + time.Second)
	if _, ok := ReadSessionPresence(dir, expired); ok {
		t.Fatal("an unrefreshed claim never expired")
	}
	if rows := SweepStaleBuilds(root, "different-build", expired); len(rows) != 0 {
		t.Fatal("build sweep kept an expired claim")
	}
}

func TestPresenceFileMtimeCannotReviveAnExpiredClaim(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, presenceName)
	desk := &presenceDesk{agent: agent, path: path}
	desk.write()
	now := desk.refreshed.Add(presenceWindow + time.Second)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadSessionPresence(dir, now); ok {
		t.Fatal("file timestamp revived an expired JSON claim")
	}
	presence, ok := ReadSessionPresenceAnyAge(dir)
	if !ok || !presence.UpdatedAt.Equal(desk.refreshed) {
		t.Fatalf("holder reader changed the JSON clock: %+v", presence)
	}
}
