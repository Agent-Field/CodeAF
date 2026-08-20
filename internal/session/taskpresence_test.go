package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newPresenceSession builds one agent that lives in a real-shaped session
// folder: a bucket standing in for ~/.aforge/v3/projects/<workspace>/ with one
// session folder under it, exactly the layout [ReadProjectPresence] reads.
func newPresenceSession(t *testing.T, bucket, id string) (*Agent, string) {
	t.Helper()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: dir, Workspace: workspace}
		config.SessionFile = filepath.Join(dir, placeTranscript)
	})
	return agent, dir
}

// waitForPresence polls until the folder holds a presence file a reader
// believes, because the heartbeat writes it on a goroutine of its own.
func waitForPresence(t *testing.T, dir string) SessionPresence {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if presence, ok := ReadSessionPresence(dir, time.Now()); ok {
			return presence
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no presence file appeared in %s", dir)
	return SessionPresence{}
}

func TestPresenceWritesAWholeFileAnotherWindowCanRead(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "aaaa1111aaaa1111")

	presence := waitForPresence(t, dir)
	if presence.Schema != presenceSchema {
		t.Fatalf("schema is %d, want %d", presence.Schema, presenceSchema)
	}
	if presence.SessionID != "aaaa1111aaaa1111" {
		t.Fatalf("session id is %q", presence.SessionID)
	}
	if presence.Workspace != agent.config.Workspace {
		t.Fatalf("workspace is %q, want %q", presence.Workspace, agent.config.Workspace)
	}
	if presence.PID != os.Getpid() {
		t.Fatalf("pid is %d, want %d", presence.PID, os.Getpid())
	}
	if presence.UpdatedAt.IsZero() {
		t.Fatal("presence carries no stamp, so nothing can ever call it stale")
	}
	if presence.State != PresenceIdle {
		t.Fatalf("a session with no turn says %q, want %q", presence.State, PresenceIdle)
	}
	if presence.Dir != dir {
		t.Fatalf("reader filled in dir %q, want %q", presence.Dir, dir)
	}
	// The whole file parses, which is the atomicity claim: a reader never sees
	// half a refresh.
	raw, err := os.ReadFile(filepath.Join(dir, presenceName))
	if err != nil {
		t.Fatalf("read presence: %v", err)
	}
	var again SessionPresence
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatalf("presence file is not whole JSON: %v\n%s", err, raw)
	}
}

func TestPresenceHeartbeatMovesTheStamp(t *testing.T) {
	// A desk of the test's own, on a fast cadence — the agent's real one beats
	// every five seconds, which is right for a machine and wrong here.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	dir := t.TempDir()
	desk := &presenceDesk{
		agent: agent,
		path:  filepath.Join(dir, presenceName),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		nudge: make(chan struct{}, 1),
		every: 10 * time.Millisecond,
	}
	go desk.beat()

	first := waitForPresence(t, dir)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if later, ok := ReadSessionPresence(dir, time.Now()); ok && later.UpdatedAt.After(first.UpdatedAt) {
			close(desk.stop)
			<-desk.done
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(desk.stop)
	<-desk.done
	t.Fatal("the heartbeat never moved the stamp, so age cannot be a liveness clock")
}

func TestAStalePresenceFileIsNotALiveSession(t *testing.T) {
	bucket := t.TempDir()
	dir := filepath.Join(bucket, "bbbb2222bbbb2222")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	write := func(at time.Time) {
		raw, err := json.Marshal(SessionPresence{
			Schema:    presenceSchema,
			SessionID: "bbbb2222bbbb2222",
			Workspace: "/somewhere",
			PID:       424242,
			UpdatedAt: at,
			State:     PresenceWorking,
			RunningTasks: []PresenceTask{
				{ID: "3", Title: "a task nobody is running any more", State: "running", StartedAt: at},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, presenceName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(now.Add(-presenceWindow + time.Second))
	if _, ok := ReadSessionPresence(dir, now); !ok {
		t.Fatal("a file written inside the window was refused")
	}
	if rows := ReadProjectPresence(bucket, now, ""); len(rows) != 1 {
		t.Fatalf("the bucket answered %d live sessions, want 1", len(rows))
	}

	// One second past the window the session is gone, and its "running" claim
	// goes with it — that is the whole reason the stamp is written.
	write(now.Add(-presenceWindow - time.Second))
	if presence, ok := ReadSessionPresence(dir, now); ok {
		t.Fatalf("a stale file was believed: %+v", presence)
	}
	if rows := ReadProjectPresence(bucket, now, ""); len(rows) != 0 {
		t.Fatalf("the bucket answered %d live sessions from a stale file, want 0", len(rows))
	}
}

func TestPresenceRefusesASchemaItDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(SessionPresence{
		Schema:    presenceSchema + 1,
		SessionID: "cccc3333cccc3333",
		UpdatedAt: time.Now(),
		State:     PresenceWorking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, presenceName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if presence, ok := ReadSessionPresence(dir, time.Now()); ok {
		t.Fatalf("a newer build's file was read anyway: %+v", presence)
	}
}

func TestPresenceSaysWhenTheSessionNeedsItsPerson(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "dddd4444dddd4444")
	waitForPresence(t, dir)

	// A pending question, exactly as [Agent.askAnswer] leaves one, with the
	// line the gate banks beside it.
	agent.mu.Lock()
	agent.consent = map[uint64]chan consentAnswer{1: make(chan consentAnswer, 1)}
	agent.mu.Unlock()
	release := agent.presenceWaiting("needs your ok to run bash")

	snapshot := agent.presenceSnapshot(time.Now())
	if snapshot.State != PresenceWaiting {
		t.Fatalf("a session with a question out says %q, want %q", snapshot.State, PresenceWaiting)
	}
	if !snapshot.NeedsPerson() {
		t.Fatal("NeedsPerson disagreed with the state beside it")
	}
	if snapshot.Reason != "needs your ok to run bash" {
		t.Fatalf("reason is %q", snapshot.Reason)
	}

	// And the nudge the gate sent put it on disk without waiting a heartbeat.
	deadline := time.Now().Add(5 * time.Second)
	for {
		presence, ok := ReadSessionPresence(dir, time.Now())
		if ok && presence.NeedsPerson() && presence.Reason == "needs your ok to run bash" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the waiting session never reached disk: %+v", presence)
		}
		time.Sleep(5 * time.Millisecond)
	}

	release()
	agent.mu.Lock()
	agent.consent = nil
	agent.mu.Unlock()
	answered := agent.presenceSnapshot(time.Now())
	if answered.State != PresenceIdle || answered.Reason != "" {
		t.Fatalf("an answered session still says %q / %q", answered.State, answered.Reason)
	}
}

func TestPresenceWaitingOutranksWorking(t *testing.T) {
	bucket := t.TempDir()
	agent, _ := newPresenceSession(t, bucket, "eeee5555eeee5555")

	agent.mu.Lock()
	agent.running = true
	agent.mu.Unlock()
	if state := agent.presenceSnapshot(time.Now()).State; state != PresenceWorking {
		t.Fatalf("a running turn says %q, want %q", state, PresenceWorking)
	}

	agent.mu.Lock()
	agent.taskAnswers = map[uint64]chan TaskAnswer{9: make(chan TaskAnswer, 1)}
	agent.mu.Unlock()
	if state := agent.presenceSnapshot(time.Now()).State; state != PresenceWaiting {
		t.Fatalf("a turn blocked on a proposal says %q, want %q", state, PresenceWaiting)
	}

	agent.mu.Lock()
	agent.running = false
	agent.taskAnswers = nil
	agent.mu.Unlock()
	if state := agent.presenceSnapshot(time.Now()).State; state != PresenceIdle {
		t.Fatalf("a quiet session says %q, want %q", state, PresenceIdle)
	}
}

func TestPresenceCarriesTheRunningTasksAndClearsThem(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "ffff6666ffff6666")
	waitForPresence(t, dir)

	started := time.Now().Add(-2 * time.Minute)
	graph := agent.graph()
	graph.mu.Lock()
	for _, node := range []*TaskNode{
		{graph: graph, id: 7, spec: taskSpec{title: "fix the flaky auth test"}, state: TaskRunning, started: started},
		{graph: graph, id: 8, spec: taskSpec{title: "clean up the imports"}, state: TaskQueued},
		{graph: graph, id: 9, spec: taskSpec{title: "work that already landed"}, state: TaskDone, started: started},
	} {
		graph.nodes[node.id] = node
		graph.order = append(graph.order, node.id)
	}
	graph.mu.Unlock()

	tasks := agent.presenceSnapshot(time.Now()).RunningTasks
	if len(tasks) != 2 {
		t.Fatalf("presence carries %d tasks, want the two that are not settled: %+v", len(tasks), tasks)
	}
	if tasks[0].ID != "7" || tasks[0].Title != "fix the flaky auth test" || tasks[0].State != string(TaskRunning) {
		t.Fatalf("the running node reads %+v", tasks[0])
	}
	if !tasks[0].StartedAt.Equal(started) {
		t.Fatalf("started-at is %v, want %v", tasks[0].StartedAt, started)
	}
	if tasks[1].ID != "8" || tasks[1].State != string(TaskQueued) {
		t.Fatalf("the queued node reads %+v", tasks[1])
	}
	if !tasks[1].StartedAt.IsZero() {
		t.Fatal("a queued node claimed a start time it never had")
	}

	// And when the work lands, presence stops claiming it. What happened to it
	// is the project index's answer, not this file's.
	graph.mu.Lock()
	graph.nodes[7].state = TaskDone
	graph.nodes[8].state = TaskFailed
	graph.mu.Unlock()
	if tasks := agent.presenceSnapshot(time.Now()).RunningTasks; len(tasks) != 0 {
		t.Fatalf("landed work is still in presence: %+v", tasks)
	}
}

func TestACleanCloseTakesThePresenceFileWithIt(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "1111aaaa1111aaaa")
	waitForPresence(t, dir)

	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, presenceName)); !os.IsNotExist(err) {
		t.Fatalf("the presence file outlived a clean close (stat err %v)", err)
	}
	if rows := ReadProjectPresence(bucket, time.Now(), ""); len(rows) != 0 {
		t.Fatalf("a closed session is still on the bucket: %+v", rows)
	}
}

func TestPresenceLeavesOutTheWindowDoingTheAsking(t *testing.T) {
	bucket := t.TempDir()
	mine, mineDir := newPresenceSession(t, bucket, "2222bbbb2222bbbb")
	_, theirsDir := newPresenceSession(t, bucket, "3333cccc3333cccc")
	waitForPresence(t, mineDir)
	waitForPresence(t, theirsDir)

	rows := ReadProjectPresence(bucket, time.Now(), "2222bbbb2222bbbb")
	if len(rows) != 1 || rows[0].SessionID != "3333cccc3333cccc" {
		t.Fatalf("the caller's own session was not left out: %+v", rows)
	}
	// And the agent's own door does the same without being told which it is.
	if rows := mine.ProjectPresence(); len(rows) != 1 || rows[0].SessionID != "3333cccc3333cccc" {
		t.Fatalf("ProjectPresence answered %+v", rows)
	}
}

func TestTwoWindowsOnOneProjectAreBothPresent(t *testing.T) {
	bucket := t.TempDir()
	_, first := newPresenceSession(t, bucket, "4444dddd4444dddd")
	_, second := newPresenceSession(t, bucket, "5555eeee5555eeee")
	waitForPresence(t, first)
	waitForPresence(t, second)

	rows := ReadProjectPresence(bucket, time.Now(), "")
	if len(rows) != 2 {
		t.Fatalf("the bucket answered %d sessions, want 2: %+v", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.SessionID] = true
	}
	if !seen["4444dddd4444dddd"] || !seen["5555eeee5555eeee"] {
		t.Fatalf("both windows are not on the list: %+v", rows)
	}
}

func TestPresenceUnionsEveryProjectOnTheMachine(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "project-one")
	two := filepath.Join(root, "project-two")
	_, first := newPresenceSession(t, one, "6666ffff6666ffff")
	_, second := newPresenceSession(t, two, "7777aaaa7777aaaa")
	waitForPresence(t, first)
	waitForPresence(t, second)

	rows := ReadAllPresence(root, time.Now(), "")
	if len(rows) != 2 {
		t.Fatalf("the machine answered %d live sessions, want 2: %+v", len(rows), rows)
	}
	if rows := ReadAllPresence(root, time.Now(), "6666ffff6666ffff"); len(rows) != 1 || rows[0].SessionID != "7777aaaa7777aaaa" {
		t.Fatalf("the union did not leave out the caller: %+v", rows)
	}
}

func TestASessionWithNoFolderKeepsNoPresence(t *testing.T) {
	// A memory-only conversation and the legacy flat layout have nowhere to put
	// the file and no id anybody could join it on, so they keep none at all.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if agent.presence != nil {
		t.Fatal("a session with no folder started a heartbeat")
	}
	if rows := agent.ProjectPresence(); rows != nil {
		t.Fatalf("a session with no folder answered %+v", rows)
	}
	// And every door on it is safe to call.
	agent.nudgePresence()
	agent.presenceWaiting("needs your ok to run bash")()
	agent.stopPresence()
}

func TestATaskNodesAgentKeepsNoPresence(t *testing.T) {
	// A node is one piece of a conversation's work running somewhere quieter,
	// not a session anybody is sitting in front of. Its parent's presence is
	// what says the work is happening.
	bucket := t.TempDir()
	dir := filepath.Join(bucket, "8888bbbb8888bbbb")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place = Place{Dir: dir, Workspace: config.Workspace}
		config.InTask = true
	})
	if agent.presence != nil {
		t.Fatal("a task node's agent started a heartbeat of its own")
	}
	if _, err := os.Stat(filepath.Join(dir, presenceName)); !os.IsNotExist(err) {
		t.Fatalf("a task node's agent wrote a presence file (stat err %v)", err)
	}
}
