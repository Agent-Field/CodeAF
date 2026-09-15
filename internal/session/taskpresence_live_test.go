package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

// ── what the work is doing, carried to another window ───────────────────────

// A RUNNING NODE SAYS WHAT ITS WORKER IS DOING, and only while the worker is
// the life it is in. The line is the recorder's, the same one this window's own
// index rows carry; through a check it would be the worker's last call sitting
// there finished, so the file leaves it off and the phase says what is true.
func TestPresenceCarriesWhatARunningNodeIsDoing(t *testing.T) {
	bucket := t.TempDir()
	agent, _ := newPresenceSession(t, bucket, "abab1212abab1212")

	room := newTaskRoom()
	room.live.call = "bash go test ./internal/session/"
	room.live.began = time.Now().Add(-40 * time.Second)
	graph := agent.graph()
	graph.mu.Lock()
	for _, node := range []*TaskNode{
		{graph: graph, id: 4, spec: taskSpec{title: "Port the parser"}, state: TaskRunning, started: time.Now(), room: room},
		{graph: graph, id: 5, spec: taskSpec{title: "Write the change entry"}, state: TaskQueued},
	} {
		graph.nodes[node.id] = node
		graph.order = append(graph.order, node.id)
	}
	graph.mu.Unlock()

	tasks := agent.presenceSnapshot(time.Now()).RunningTasks
	if len(tasks) != 2 {
		t.Fatalf("presence carries %d tasks, want 2: %+v", len(tasks), tasks)
	}
	if !strings.HasPrefix(tasks[0].Activity, "bash go test ./internal/session/") {
		t.Fatalf("the running node's activity reads %q", tasks[0].Activity)
	}
	if tasks[1].Activity != "" {
		t.Fatalf("a queued node, which has no worker yet, says %q", tasks[1].Activity)
	}

	graph.mu.Lock()
	graph.nodes[4].life = TaskPhaseChecking
	graph.mu.Unlock()
	checking := agent.presenceSnapshot(time.Now()).RunningTasks[0]
	if checking.Activity != "" || checking.Phase != TaskPhaseChecking {
		t.Fatalf("a node under its check reads activity %q, phase %q", checking.Activity, checking.Phase)
	}
}

// AN ADAPTIVE RUN IS WORK OUT, UNDER ITS INDEX ROW'S OWN ID. Its root row says
// `running` in the project index from its first breath, and the join every
// other window makes on (SessionID, ID) has to find it named here — or home
// counts a run in full flight as incomplete.
func TestPresenceCountsAnAdaptiveRunUnderItsIndexRow(t *testing.T) {
	bucket := t.TempDir()
	agent, dir := newPresenceSession(t, bucket, "cdcd3434cdcd3434")

	family := agent.newOrchestrateFamily("migrate every adapter", "", "2")
	run := orchestrate.New("migrate every adapter", nil, nil, orchestrate.Options{Cap: 1})
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"2": {run: run, cancel: func() {}, family: family, born: time.Now()}}
	agent.mu.Unlock()

	presence := agent.presenceSnapshot(time.Now())
	root := strconv.FormatUint(family.root, 10)
	if len(presence.RunningTasks) != 1 || presence.RunningTasks[0].ID != root {
		t.Fatalf("presence carries %+v, want the run under its root id %s", presence.RunningTasks, root)
	}
	row := presence.RunningTasks[0]
	if row.Title != "migrate every adapter" || row.State != string(TaskRunning) || row.StartedAt.IsZero() {
		t.Fatalf("the run's row reads %+v", row)
	}
	// A RUN WITH NOTHING LAID OUT DOES NOT COUNT YET, and says no `0 of 0`.
	if row.Done != 0 || row.Total != 0 {
		t.Fatalf("a run with no planned nodes counts %d of %d", row.Done, row.Total)
	}

	var entry TaskIndexEntry
	for _, landed := range ReadTaskIndex(filepath.Join(bucket, taskIndexName)) {
		if landed.ID == root {
			entry = landed
		}
	}
	if !entry.Live() {
		t.Fatalf("the run's index row is %+v, want the running row it writes at its start", entry)
	}
	held := SessionRow{ID: filepath.Base(dir), Live: true, Presence: presence}
	if !held.Runs(entry) {
		t.Fatal("home's join does not find the run's index row among what the session has out")
	}
}

// THE COUNT IS THE TREE'S OWN TEST OF WHICH WORKERS ARE HOME, so the number on
// a row and the rows beneath it cannot disagree.
func TestAnAdaptiveRunCountsItsSettledNodesOfEveryPlannedOne(t *testing.T) {
	done, total := orchestrateProgress([]orchestrate.NodeStatus{
		{State: orchestrate.Done},
		{State: orchestrate.Failed},
		{State: orchestrate.Running},
		{State: orchestrate.Queued},
		{State: orchestrate.Ready},
	})
	if done != 2 || total != 5 {
		t.Fatalf("progress reads %d of %d, want 2 of 5", done, total)
	}
}

// A LIVE JOB IS ON THE FILE WHILE IT RUNS AND OFF IT ONCE IT IS NOT. It is the
// list the conversation's own column draws, spelled for another window: the
// handle, the row's name, the folder it was started in, and when it forked.
func TestPresenceCarriesTheJobsThisSessionHasRunning(t *testing.T) {
	bucket := t.TempDir()
	agent, _ := newPresenceSession(t, bucket, "efef5656efef5656")

	started, err := agent.jobs.start("sleep 30")
	if err != nil {
		t.Fatalf("could not start the job: %v", err)
	}
	jobs := agent.presenceSnapshot(time.Now()).Jobs
	if len(jobs) != 1 {
		t.Fatalf("presence carries %d jobs, want the one running: %+v", len(jobs), jobs)
	}
	want := PresenceJob{ID: strconv.Itoa(started.id), Title: "sleep 30", Dir: agent.config.Workspace}
	if jobs[0].ID != want.ID || jobs[0].Title != want.Title || jobs[0].Dir != want.Dir || jobs[0].StartedAt.IsZero() {
		t.Fatalf("the job reads %+v, want %+v with a start", jobs[0], want)
	}

	if _, isError := agent.jobs.kill(context.Background(), started.id); isError {
		t.Fatal("could not end the job")
	}
	waitFor(t, "the ended job to leave presence", func() bool {
		return len(agent.presenceSnapshot(time.Now()).Jobs) == 0
	})
}

// A FILE FROM A BUILD THAT NEVER HEARD OF THESE FIELDS READS AS IT ALWAYS DID:
// no activity, no count, no jobs — nothing to draw, never a refusal.
func TestAnOlderPresenceFileReadsWithoutActivityCountOrJobs(t *testing.T) {
	dir := t.TempDir()
	older := `{"schema":` + itoa(presenceSchema) + `,"sessionId":"0a0a7878a0a07878","workspace":"/work",` +
		`"pid":4242,"updatedAt":"` + time.Now().Format(time.RFC3339Nano) + `","state":"working",` +
		`"runningTasks":[{"id":"3","title":"Port the parser","state":"running"}]}`
	if err := os.WriteFile(filepath.Join(dir, presenceName), []byte(older+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	presence, ok := ReadSessionPresence(dir, time.Now())
	if !ok {
		t.Fatal("a file from an older build was refused outright")
	}
	task := presence.RunningTasks[0]
	if task.Activity != "" || task.Done != 0 || task.Total != 0 || presence.Jobs != nil {
		t.Fatalf("an older file grew fields it never wrote: %+v, jobs %+v", task, presence.Jobs)
	}
}
