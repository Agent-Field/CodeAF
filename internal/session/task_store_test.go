package session

// The durable frontier, as tests: the checkpoint after every transition, the
// interrupt consumed exactly once, the queued node that resumes, and the four
// ways a checkpoint is refused.
//
// The recovery tests are deliberately split the way the executor's are
// (task_test.go): [TestTaskRecovery…Interrupt…] drives the whole agent, because
// "the process died and the branch is still there" is not a claim a stub can
// make, and the frontier half is driven with a scripted runner, because
// "recovery is load, reconcile, continue" is a scheduling law and should be
// readable without a provider on the other end.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── harness ─────────────────────────────────────────────────────────────────

// journalIn is a session file path in a directory of its own, and the checkpoint
// that will sit beside it.
func journalIn(t *testing.T) (string, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	return journal, taskCheckpointPath(journal)
}

// writeCheckpoint puts one document on disk, as this store writes it.
func writeCheckpoint(t *testing.T, path string, document taskDocument) {
	t.Helper()
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode the checkpoint: %v", err)
	}
	writeFile(t, path, string(encoded)+"\n")
}

// readCheckpoint is what is on disk right now, validated exactly as a resume
// would validate it.
func readCheckpoint(t *testing.T, path string) taskDocument {
	t.Helper()
	document, ok := loadTaskCheckpoint(path)
	if !ok {
		t.Fatalf("no valid checkpoint at %s", path)
	}
	return document
}

// awaitRecord polls the checkpoint until one node satisfies a condition, which
// is what a test needs for the writes that happen AFTER a node's done channel
// closes — the completion note is handed over on the far side of it.
func awaitRecord(t *testing.T, path string, id uint64, want func(taskRecord) bool, why string) taskRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if document, ok := loadTaskCheckpoint(path); ok {
			for _, record := range document.Nodes {
				if record.ID == id && want(record) {
					return record
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the checkpoint never showed node %d %s", id, why)
			return taskRecord{}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recordOf is one node out of a document.
func recordOf(t *testing.T, document taskDocument, id uint64) taskRecord {
	t.Helper()
	for _, record := range document.Nodes {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("the checkpoint has no node %d: %+v", id, document.Nodes)
	return taskRecord{}
}

// steeringNotes is what the session has queued for the model to read.
func steeringNotes(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	notes := make([]string, 0, len(agent.steering))
	for _, message := range agent.steering {
		notes = append(notes, message.text())
	}
	return notes
}

// ── the checkpoint ──────────────────────────────────────────────────────────

// EVERY TRANSITION IS ON DISK, starting with admission: a process killed a
// microsecond after the person approved a task still resumes with that task.
func TestCheckpointIsWrittenAfterEveryTransition(t *testing.T) {
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})

	running := make(chan *TaskNode, 1)
	release := make(chan struct{})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		running <- node
		<-release
		node.finish("wrote the file", []string{"hello.txt"}, "task/greet-ab12cd", mergeMerged)
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()

	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "Add the greeting", summary: "two lines", brief: "write hello.txt",
		acceptance: "the file is there", maxSteps: 12, noProgress: 4,
	})

	// ADMITTED — and the node is on disk with its frozen spec, before anything it
	// does could have been observed.
	node := recordOf(t, readCheckpoint(t, checkpoint), id)
	if node.Title != "Add the greeting" || node.Brief != "write hello.txt" || node.Acceptance != "the file is there" {
		t.Fatalf("the admitted node's spec did not survive: %+v", node)
	}
	if node.MaxSteps != 12 || node.NoProgress != 4 {
		t.Fatalf("the node's thresholds did not survive: %+v", node)
	}
	if node.State != TaskRunning && node.State != TaskQueued {
		t.Fatalf("state = %q at admission", node.State)
	}

	// RUNNING — the checkpoint says so before the run is allowed to finish.
	started := <-running
	if node := recordOf(t, readCheckpoint(t, checkpoint), id); node.State != TaskRunning {
		t.Fatalf("a running node is %q on disk", node.State)
	}
	close(release)
	waitDoneNode(t, started)

	// DONE — with the leavings a person and a dependent both read, and the
	// receipt that its completion has been announced. The receipt is written on
	// the far side of the done channel, so it is waited for rather than assumed.
	node = awaitRecord(t, checkpoint, id, func(record taskRecord) bool {
		return record.State == TaskDone && record.Noted
	}, "done and announced")
	if node.Report != "wrote the file" || node.Branch != "task/greet-ab12cd" || node.Merge != mergeMerged {
		t.Fatalf("the node's leavings did not survive: %+v", node)
	}
	if len(node.Changed) != 1 || node.Changed[0] != "hello.txt" {
		t.Fatalf("changed = %v", node.Changed)
	}
	document := readCheckpoint(t, checkpoint)
	if document.Seq < id {
		t.Fatalf("the id counter (%d) is behind the last node (%d): a resume would reuse an id", document.Seq, id)
	}
}

// The worktree is written down the moment the node has one, because it is the
// only thing that can tell a person where interrupted work went.
func TestCheckpointRecordsTheWorkingCopyBeforeTheWorkStarts(t *testing.T) {
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	graph := agent.graph()
	id := graph.reserve()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	graph.admit(id, taskSpec{title: "t", brief: "b", acceptance: "a"})

	graph.node(id).setTree(taskTree{dir: "/tmp/work/3", root: "/tmp/repo", branch: "task/t-9c1a2f"})

	node := recordOf(t, readCheckpoint(t, checkpoint), id)
	if node.Branch != "task/t-9c1a2f" || node.Worktree != "/tmp/work/3" {
		t.Fatalf("the working copy was not written down: %+v", node)
	}
}

// ── recovery ────────────────────────────────────────────────────────────────

// THE INTERRUPT, CONSUMED ONCE. A node that was running when the session ended
// comes back failed, with its branch named and its worktree pointed at; the
// finished node beside it comes back as history and is NOT announced a second
// time; and a second resume reads plain history rather than interrupting the
// same node again.
func TestRecoveryInterruptsARunningNodeExactlyOnce(t *testing.T) {
	repo := newTestRepo(t)
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	checkpoint := taskCheckpointPath(journal)

	// The branch and the worktree directory a killed node would have left.
	branch := "task/fix-the-reconciler-9c1a2f"
	mustGit(t, repo, "branch", branch)
	worktree := filepath.Join(repo, ".aforge-v3", "tasks", "2")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}

	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{
			{
				ID: 1, Title: "Name the bug", Brief: "find it", Acceptance: "named",
				State: TaskDone, Report: "the nil map is built in reconcile()",
				Merge: mergeMerged, Noted: true, ElapsedMS: 4000,
			},
			{
				ID: 2, Title: "Fix the reconciler", Brief: "fix it", Acceptance: "tests pass",
				DependsOn: []uint64{1}, State: TaskRunning, Branch: branch, Worktree: worktree,
			},
		},
	})

	ran := make(chan uint64, 4)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = journal
	})
	// Anything the recovered frontier decides to run would land here — and
	// nothing should, because one node is history and the other has just been
	// interrupted.
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(node *TaskNode) { ran <- node.id }
	graph.mu.Unlock()

	// THE INTERRUPTED NODE: failed, with the branch named in its own report.
	interrupted := graph.node(2)
	if interrupted == nil {
		t.Fatal("the running node did not come back at all")
	}
	notice := interrupted.notice()
	if notice.State != TaskFailed {
		t.Fatalf("an interrupted node is %q, want failed", notice.State)
	}
	if !strings.Contains(notice.Report, "session ended mid-run") || !strings.Contains(notice.Report, branch) {
		t.Fatalf("report = %q, want it to say the session ended mid-run and name the branch", notice.Report)
	}
	if !strings.Contains(notice.Report, worktree) {
		t.Fatalf("report = %q, want it to point at the worktree that is still on disk", notice.Report)
	}
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want aborted", notice.Merge)
	}
	// And nothing was thrown away to say it.
	if branches := gitOut(t, repo, "branch", "--list", branch); !strings.Contains(branches, branch) {
		t.Fatal("recovery deleted the interrupted node's branch: the work is gone")
	}

	// THE FINISHED NODE: history, with its report intact and its age frozen.
	done := graph.node(1).notice()
	if done.State != TaskDone || done.Report != "the nil map is built in reconcile()" {
		t.Fatalf("the finished node did not come back as history: %+v", done)
	}
	if done.Elapsed != 4*time.Second {
		t.Fatalf("elapsed = %s, want the age the checkpoint recorded", done.Elapsed)
	}

	// NOTHING RE-RAN.
	select {
	case id := <-ran:
		t.Fatalf("recovery re-ran node %d", id)
	case <-time.After(200 * time.Millisecond):
	}

	// ONE NOTE FOR THE WHOLE GRAPH, and the finished node is not re-announced in
	// it: its completion is already in the transcript the journal replays.
	notes := steeringNotes(agent)
	if len(notes) != 1 {
		t.Fatalf("recovery queued %d notes, want exactly one: %v", len(notes), notes)
	}
	note := notes[0]
	if !strings.HasPrefix(note, "recovered task graph: 1 done · 1 interrupted (branch "+branch+" kept)") {
		t.Fatalf("the recovery note = %q", note)
	}
	if strings.Contains(note, "task 1 finished") {
		t.Fatalf("the finished node was announced a second time: %q", note)
	}
	// The interrupted node's news, on the other hand, has never been said — so it
	// is said here, in the shape a completion always takes.
	if !strings.Contains(note, "task 2 failed: Fix the reconciler") {
		t.Fatalf("the interrupt was never announced: %q", note)
	}

	// THE RECEIPT IS ON DISK before anything else happens.
	document := readCheckpoint(t, checkpoint)
	record := recordOf(t, document, 2)
	if !record.Interrupted || record.State != TaskFailed || !record.Noted {
		t.Fatalf("the checkpoint does not record the consumed interrupt: %+v", record)
	}

	// CONSUME-ONCE: a second resume reads history. The same node is not
	// interrupted again and its news is not repeated. (The journal is held by one
	// aforge at a time, so the first session closes before the second opens —
	// which is exactly the sequence a person resuming a session performs.)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = journal
	})
	again := second.graph().node(2).notice()
	if again.State != TaskFailed || again.Report != notice.Report {
		t.Fatalf("the second resume rewrote the interrupted node: %+v", again)
	}
	notes = steeringNotes(second)
	if len(notes) != 1 {
		t.Fatalf("the second resume queued %d notes: %v", len(notes), notes)
	}
	if strings.Contains(notes[0], "interrupted") {
		t.Fatalf("the same interrupt was consumed twice: %q", notes[0])
	}
	if !strings.HasPrefix(notes[0], "recovered task graph: 1 done · 1 failed") {
		t.Fatalf("the second resume's note = %q", notes[0])
	}
	if strings.Contains(notes[0], "task 2 failed") {
		t.Fatalf("the interrupt's note was delivered twice: %q", notes[0])
	}
}

// A branch that is gone is SAID to be gone. A report promising work on a branch
// the person has since deleted would be the harness telling them their work is
// safe when it is not.
func TestRecoveryTellsTheTruthAboutAMissingBranch(t *testing.T) {
	workspace := t.TempDir()
	graph := newTaskGraph()
	graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Grind", Brief: "b", Acceptance: "a",
			State: TaskRunning, Branch: "task/grind-000000",
		}},
	}, workspace)

	report := graph.node(1).notice().Report
	if !strings.Contains(report, "no longer in the repository") {
		t.Fatalf("report = %q, want it to say the branch is gone", report)
	}
}

// RECOVERY IS LOAD, RECONCILE, CONTINUE. A node that was waiting on work that
// had already landed starts the moment the graph is back — with its
// prerequisite's report in its brief, exactly as it would have been had nothing
// died — and the finished node is not run again.
func TestRecoveryResumesAQueuedNodeAndNeverReRunsAFinishedOne(t *testing.T) {
	graph := newTaskGraph()
	var (
		mu     sync.Mutex
		briefs = map[uint64]string{}
		ran    = make(chan uint64, 4)
	)
	graph.run = func(node *TaskNode) {
		mu.Lock()
		briefs[node.id] = node.assembledBrief()
		mu.Unlock()
		ran <- node.id
		node.finish("the sweep is done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}

	recovery := graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{
			{
				ID: 1, Title: "Name the bug", Brief: "find it", Acceptance: "named",
				State: TaskDone, Report: "the nil map is built in reconcile()", Noted: true,
			},
			{
				ID: 2, Title: "Fix it", Brief: "fix the reconciler", Acceptance: "tests pass",
				DependsOn: []uint64{1}, State: TaskQueued,
			},
		},
	}, t.TempDir())

	if recovery.done != 1 || recovery.waiting != 1 || recovery.interrupted != 0 {
		t.Fatalf("recovery counted %+v", recovery)
	}
	graph.runFrontier()

	if id := waitStarted(t, ran); id != 2 {
		t.Fatalf("node %d ran, want the one that was waiting", id)
	}
	waitDoneNode(t, graph.node(2))
	if state := graph.node(2).stateNow(); state != TaskDone {
		t.Fatalf("the resumed node is %q, want done", state)
	}

	mu.Lock()
	brief := briefs[2]
	mu.Unlock()
	if !strings.Contains(brief, "fix the reconciler") {
		t.Fatalf("the resumed node lost its own brief: %q", brief)
	}
	if !strings.Contains(brief, "the nil map is built in reconcile()") {
		t.Fatalf("the prerequisite's report did not survive the resume: %q", brief)
	}

	// The finished node was never handed to the runner.
	select {
	case id := <-ran:
		t.Fatalf("node %d ran a second time", id)
	case <-time.After(200 * time.Millisecond):
	}
}

// A node waiting on work that was interrupted can never have its brief
// assembled, so the existing cascade fails it — recovery adds no second rule.
func TestRecoveryFailsANodeWaitingOnAnInterruptedOne(t *testing.T) {
	graph := newTaskGraph()
	graph.run = func(node *TaskNode) { t.Errorf("node %d ran", node.id) }

	graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{
			{ID: 1, Title: "Grind", Brief: "b", Acceptance: "a", State: TaskRunning},
			{ID: 2, Title: "Build on it", Brief: "b", Acceptance: "a", DependsOn: []uint64{1}, State: TaskQueued},
		},
	}, t.TempDir())
	graph.runFrontier()

	waitDoneNode(t, graph.node(2))
	if state := graph.node(2).stateNow(); state != TaskFailed {
		t.Fatalf("the dependent of an interrupted node is %q, want failed", state)
	}
	if report := graph.node(2).notice().Report; !strings.Contains(report, "did not finish") {
		t.Fatalf("report = %q, want it to name what it waited on", report)
	}
}

// ── the file that is refused ────────────────────────────────────────────────

// A CHECKPOINT THAT IS NOT WHOLE IS DROPPED WHOLE. Every rule below is one this
// store enforces on the way out, so a file that breaks one was written by
// something else — and half a graph is a graph nobody scheduled.
func TestCorruptCheckpointsAreIgnoredWholeAndAreNeverFatal(t *testing.T) {
	valid := taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{
			{ID: 1, Title: "t", Brief: "b", Acceptance: "a", State: TaskDone, Noted: true},
			{ID: 2, Title: "t", Brief: "b", Acceptance: "a", State: TaskQueued, DependsOn: []uint64{1}},
		},
	}
	encoded, err := json.MarshalIndent(valid, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTasks(encoded); err != nil {
		t.Fatalf("the store refuses its own file: %v", err)
	}

	for _, refused := range []struct {
		name    string
		content string
	}{
		{"not json at all", "{\"type\":\"tasks\", this is not json"},
		{"another version", `{"type":"tasks","version":99,"seq":1,"nodes":[]}`},
		{"another file's type", `{"type":"state","version":1,"seq":1,"nodes":[]}`},
		{"a node with no id", `{"type":"tasks","version":1,"seq":1,"nodes":[{"title":"t","brief":"b","acceptance":"a","state":"done"}]}`},
		{"the same id twice", `{"type":"tasks","version":1,"seq":2,"nodes":[
			{"id":1,"title":"t","brief":"b","acceptance":"a","state":"done"},
			{"id":1,"title":"t","brief":"b","acceptance":"a","state":"done"}]}`},
		{"a node with no brief", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"","acceptance":"a","state":"done"}]}`},
		{"a node with no acceptance", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"","state":"done"}]}`},
		{"a state nobody defined", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"a","state":"halfway"}]}`},
		{"a merge outcome nobody defined", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"a","state":"done","merge":"sort-of"}]}`},
		{"an edge to a node that is not there", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"a","state":"queued","depends_on":[7]}]}`},
		{"an edge that points forwards", `{"type":"tasks","version":1,"seq":2,"nodes":[
			{"id":1,"title":"t","brief":"b","acceptance":"a","state":"queued","depends_on":[2]},
			{"id":2,"title":"t","brief":"b","acceptance":"a","state":"done"}]}`},
		{"an id counter behind the graph", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":4,"title":"t","brief":"b","acceptance":"a","state":"done"}]}`},
		{"a negative threshold", `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"a","state":"done","max_steps":-3}]}`},
	} {
		t.Run(refused.name, func(t *testing.T) {
			journal, checkpoint := journalIn(t)
			writeFile(t, checkpoint, refused.content)
			if document, ok := loadTaskCheckpoint(checkpoint); ok {
				t.Fatalf("the checkpoint was accepted: %+v", document)
			}

			// AND IT IS NEVER FATAL: the session opens, with no graph rather than
			// with half of one, and says nothing about work it cannot vouch for.
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.SessionFile = journal
			})
			if nodes := admitted(agent.graph()); nodes != 0 {
				t.Fatalf("a refused checkpoint restored %d nodes", nodes)
			}
			if notes := steeringNotes(agent); len(notes) != 0 {
				t.Fatalf("a refused checkpoint spoke: %v", notes)
			}
		})
	}
}

// A session with no journal has nowhere to write a checkpoint, and that is a
// working graph with no disk behind it rather than a session that refuses tasks.
func TestGraphWithoutAJournalStillRuns(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := agent.graph()
	if graph.store != nil {
		t.Fatal("a session with no journal has a checkpoint file")
	}
	ran := make(chan uint64, 1)
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		ran <- node.id
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "t", brief: "b", acceptance: "a"})
	if got := waitStarted(t, ran); got != id {
		t.Fatalf("node %d ran, want %d", got, id)
	}
}

// The ids a resumed session mints carry on from the checkpoint. Reusing one
// would put two different pieces of work behind the same sentence.
func TestResumedGraphDoesNotReuseIds(t *testing.T) {
	journal, checkpoint := journalIn(t)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 7,
		Nodes: []taskRecord{{
			ID: 7, Title: "t", Brief: "b", Acceptance: "a", State: TaskDone, Noted: true,
		}},
	})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	if next := agent.graph().reserve(); next != 8 {
		t.Fatalf("the next id is %d, want 8", next)
	}
}

// The note is the whole of what a person is told about a resumed graph, and it
// is one line: what survived, what was interrupted, what is still waiting.
func TestRecoveryNoteReadsAsOneLine(t *testing.T) {
	recovery := taskRecovery{done: 2, interrupted: 1, waiting: 1, branches: []string{"task/fix-it-9c1a2f"}}
	want := "recovered task graph: 2 done · 1 interrupted (branch task/fix-it-9c1a2f kept) · 1 waiting"
	if got := recovery.note(); got != want {
		t.Fatalf("note = %q, want %q", got, want)
	}
	if note := (taskRecovery{}).note(); note != "" {
		t.Fatalf("an empty graph said %q", note)
	}
	if note := (taskRecovery{interrupted: 1}).note(); !strings.Contains(note, "no branch kept") {
		t.Fatalf("an interrupt with nothing kept said %q", note)
	}
	many := taskRecovery{interrupted: 2, branches: []string{"task/a-1", "task/b-2"}}
	if note := many.note(); !strings.Contains(note, "branches task/a-1, task/b-2 kept") {
		t.Fatalf("two interrupts said %q", note)
	}
}

// The checkpoint sits beside the journal, per conversation — never one file per
// session directory, which every window in a workspace would write over.
func TestCheckpointPathIsPerJournal(t *testing.T) {
	if got := taskCheckpointPath("/home/p/.aforge/v3/work/20260815-101112.jsonl"); got != "/home/p/.aforge/v3/work/20260815-101112.tasks.json" {
		t.Fatalf("path = %q", got)
	}
	if got := taskCheckpointPath("  "); got != "" {
		t.Fatalf("a session with no journal got the path %q", got)
	}
	if got := fmt.Sprint(taskCheckpointPath("session")); got != "session.tasks.json" {
		t.Fatalf("path = %q", got)
	}
}
