package session

// The durable frontier: the graph as a checkpoint, and recovery as a pure
// function of it.
//
// Until this file the task graph lived in one place — memory. Kill the app
// while a node is grinding on a build and everything about that work vanished
// with the process: which nodes were admitted, what they were briefed with,
// which of them had already landed and what their reports said, and — worst —
// the fact that a branch called task/fix-the-reconciler-9c1a2f is sitting in the
// repository with somebody's half-finished work on it and nothing left alive
// that knows why. The worktree survived; the knowledge of it did not.
//
// The research names the contract exactly (harness-research-notes.md §7, Resume
// semantics, arXiv:2608.03836): SCHEMA-VALID CHECKPOINTS, CONSUME-ONCE
// INTERRUPTS, RECOVERY AS A PURE FUNCTION OF DURABLE STATE. They machine-checked
// real frameworks against it and found violations, which is a good reason to
// write the three rules down rather than to assume them:
//
//   - THE CHECKPOINT IS WRITTEN AFTER EVERY TRANSITION, not at exit. A process
//     that is killed does not get to run its shutdown path, so a checkpoint that
//     is only correct at exit is a checkpoint that is only correct when nothing
//     went wrong. Admitted, running, the working copy prepared, the verdict in,
//     merged or conflicted or failed: each of those is a `g.checkpoint()` on the
//     way out of the transition, atomically (tmp + rename), so the file on disk
//     is always a whole graph somebody wrote and never half of two.
//
//   - LOADING IS SCHEMA-VALIDATED, AND A VIOLATION DROPS THE FILE WHOLE. This is
//     state.go's law and it is here for state.go's reason: refusing to start a
//     session because a bookkeeping file has a bad byte costs the person their
//     whole conversation, while starting with no graph costs them a summary they
//     can rebuild by working. One log line naming the path, and never fatal.
//
//   - AN INTERRUPT IS CONSUMED BY EXACTLY ONE RECOVERY. A node that was RUNNING
//     when the process died is interrupted work: nobody will ever finish it,
//     nobody will audit it, and its branch is on disk. Recovery turns it into a
//     failed node whose report says so and names the branch — and it RECORDS in
//     the checkpoint that it did (Interrupted), so the next resume reads plain
//     history rather than interrupting the same node a second time. The state
//     rewrite alone already makes it unrepeatable; the flag is the checkpoint
//     admitting to the consumption, which is the half of the contract a reader
//     can check.
//
// ── RECOVERY IS LOAD, RECONCILE, CONTINUE ──
//
// [Agent.recoverTasks] is three lines of work and no cleverness. LOAD the
// checkpoint. RECONCILE it with the disk — a node that was running is failed
// with its branch named, done and failed nodes come back as history with their
// leavings intact, queued nodes come back queued. CONTINUE by turning the same
// frontier every other transition turns: a queued node whose prerequisites are
// (still) done starts now, a queued node whose prerequisite was interrupted
// fails through the cascade that already exists. There is no resume path
// separate from the ordinary scheduler, because a second scheduler is a second
// set of rules to disagree with the first.
//
// ── A COMPLETION IS ANNOUNCED EXACTLY ONCE, ACROSS LIVES ──
//
// A done node must not re-notify on resume: its note is already in the
// transcript the journal replays, and hearing "task 1 finished" again would tell
// the model that work it has already read about has just happened. So the
// checkpoint records whether a node's completion note was ever handed to the
// steering lane (Noted), and recovery delivers a note ONLY for the nodes that
// never got one — the interrupted node, and the rare node that landed in the
// instant between its completion and the process dying. Those arrive inside the
// single recovery note, in the shape [taskNote] would have produced, so the
// model reads one grammar for one kind of news.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// taskFileVersion is the schema of <journal>.tasks.json. A file carrying any
	// other version is IGNORED rather than migrated, for state.go's reason: the
	// graph is a record of work that has already happened, and guessing at an
	// older shape risks rehydrating a report into a field that no longer means
	// what it meant — which here would mean briefing a dependent with it.
	taskFileVersion = 1

	// taskDocumentType is the type tag. A file that is JSON, parses, and is not
	// this is not this store's file.
	taskDocumentType = "tasks"
)

// taskCheckpointPath derives the checkpoint from the journal: the same name with
// a different extension, beside it in the same directory.
//
// PER-JOURNAL, exactly as statePath is (state.go), and for the same reason the
// directory's shape demands: ~/.aforge/v3/<workspace>/ holds every session this
// workspace ever had, so a single tasks.json there would be every window writing
// over each other's graphs. A graph belongs to ONE conversation, and the journal
// is what names one conversation — which is also what makes "same journal" the
// definition of a resumed session.
func taskCheckpointPath(sessionFile string) string {
	sessionFile = strings.TrimSpace(sessionFile)
	if sessionFile == "" {
		return ""
	}
	return strings.TrimSuffix(sessionFile, filepath.Ext(sessionFile)) + ".tasks.json"
}

// taskRecord is one node as it survives the process.
//
// It carries the node's whole life in three parts: the FROZEN SPEC it was
// admitted with (title, summary, brief, acceptance, depends_on, thresholds),
// where it had got to (state, elapsed), and its LEAVINGS (report, changed,
// branch, worktree, merge). The assembled brief is deliberately absent: it is
// JIT by contract (task_run.go), so a queued node that resumes assembles it from
// the reports its prerequisites left, which is the same thing it would have done
// had nothing died.
type taskRecord struct {
	ID         uint64   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	Brief      string   `json:"brief"`
	Acceptance string   `json:"acceptance"`
	DependsOn  []uint64 `json:"depends_on,omitempty"`

	State  TaskState `json:"state"`
	Report string    `json:"report,omitempty"`

	Changed  []string `json:"changed,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	Merge    string   `json:"merge,omitempty"`

	// MaxSteps and NoProgress are the node's own thresholds, 0 when it named
	// none and the defaults apply.
	MaxSteps   int `json:"max_steps,omitempty"`
	NoProgress int `json:"no_progress,omitempty"`

	// ElapsedMS is the node's age: how long it has been running, frozen at the
	// moment it landed. Milliseconds because a duration in JSON should be a
	// number a person can read, not a Go-shaped string.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`

	// Noted says this node's completion note has been handed to the steering
	// lane. It is what stops a resumed session re-announcing work the transcript
	// already carries.
	//
	// It is HANDED OVER, not read: a note enqueued in the instant before the
	// process died never reached the transcript and is lost. That window is one
	// step boundary wide and closing it would mean the graph reaching into the
	// turn loop to ask whether a message had drained yet, which is a coupling
	// worth more than the case it buys.
	Noted bool `json:"noted,omitempty"`

	// Interrupted says this node was RUNNING when a session ended and that a
	// recovery has consumed that fact. It is the consume-once receipt.
	Interrupted bool `json:"interrupted,omitempty"`
}

// taskDocument is the file: a type tag, a version, the id counter, and the
// nodes in admission order.
//
// Seq is on it because ids must not be reused across a resume: a second session
// that started counting from one would admit a node with the id of a node whose
// report is still in the transcript, and every sentence either of them appears
// in would be about the wrong work.
type taskDocument struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Seq     uint64       `json:"seq"`
	Nodes   []taskRecord `json:"nodes"`
}

// taskStore is the file and the lock that serializes writes to it.
//
// The lock is the STORE's rather than the graph's, and it is taken OUTSIDE the
// graph's for the whole snapshot-and-write: two nodes landing at once would
// otherwise be free to serialize their snapshots in one order and their writes
// in the other, leaving the older graph on disk as the last word. The ordering
// is store.mu → graph.mu, always, and nothing in this file ever takes them the
// other way round.
type taskStore struct {
	mu   sync.Mutex
	path string
}

func newTaskStore(path string) *taskStore {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return &taskStore{path: path}
}

// save snapshots the graph and writes it, atomically.
//
// A FAILED WRITE IS A LOG LINE, NOT AN ERROR RETURNED UPWARDS. Its caller is a
// state transition — a node starting, a node landing — and there is nothing
// useful a transition could do with the news that a bookkeeping file could not
// be written: the work is real either way, and refusing to run a task because a
// checkpoint could not be saved would trade the whole feature for the resume of
// it.
func (s *taskStore) save(graph *TaskGraph) {
	if s == nil || graph == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	document := graph.document()
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		log.Printf("session: could not encode the task checkpoint %s: %v", s.path, err)
		return
	}
	if directory := filepath.Dir(s.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
			return
		}
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
		return
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		log.Printf("session: could not write the task checkpoint %s: %v", s.path, err)
	}
}

// ── the graph, written down ─────────────────────────────────────────────────

// checkpoint persists the graph after a transition. It is a no-op for a graph
// with no file behind it — a session with no journal, and every scripted graph
// in the tests — which is why every transition may call it unconditionally.
//
// It must be called with the graph's lock RELEASED: the write takes the store's
// lock and then the graph's, and a caller holding the graph's would close the
// cycle.
func (g *TaskGraph) checkpoint() {
	if g == nil {
		return
	}
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	store.save(g)
}

// document is the graph as the file sees it, in admission order — the order that
// makes the frontier deterministic, and the order a person reads the file in.
func (g *TaskGraph) document() taskDocument {
	g.mu.Lock()
	defer g.mu.Unlock()
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: g.seq}
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil {
			continue
		}
		document.Nodes = append(document.Nodes, node.recordLocked())
	}
	return document
}

// recordLocked copies one node out, with the graph held.
func (n *TaskNode) recordLocked() taskRecord {
	elapsed := n.elapsed
	if elapsed == 0 && !n.started.IsZero() {
		elapsed = time.Since(n.started)
	}
	changed := make([]string, len(n.changed))
	copy(changed, n.changed)
	dependsOn := make([]uint64, len(n.dependsOn))
	copy(dependsOn, n.dependsOn)
	return taskRecord{
		ID:          n.id,
		Title:       n.spec.title,
		Summary:     n.spec.summary,
		Brief:       n.spec.brief,
		Acceptance:  n.spec.acceptance,
		DependsOn:   dependsOn,
		State:       n.state,
		Report:      n.report,
		Changed:     changed,
		Branch:      n.branch,
		Worktree:    n.worktree,
		Merge:       n.merge,
		MaxSteps:    n.spec.maxSteps,
		NoProgress:  n.spec.noProgress,
		ElapsedMS:   elapsed.Milliseconds(),
		Noted:       n.noted,
		Interrupted: n.interrupted,
	}
}

// ── the file, read back ─────────────────────────────────────────────────────

// loadTaskCheckpoint reads one checkpoint and reports whether there is a graph
// in it. Everything that is not a whole valid document is a false and at most
// one log line: a missing file is the ordinary case (a fresh session) and says
// nothing at all.
func loadTaskCheckpoint(path string) (taskDocument, bool) {
	if strings.TrimSpace(path) == "" {
		return taskDocument{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("session: ignoring unreadable task checkpoint %s: %v", path, err)
		}
		return taskDocument{}, false
	}
	document, err := decodeTasks(content)
	if err != nil {
		log.Printf("session: ignoring corrupt task checkpoint %s: %v", path, err)
		return taskDocument{}, false
	}
	return document, true
}

// decodeTasks parses and VALIDATES one checkpoint. Every rule below is a rule
// this store enforces on the way out, so a file that breaks one was not written
// by this code — and a half-loaded graph is a graph nobody scheduled, which is
// the one thing worse than no graph at all.
//
// The edge rules are where the validation earns its place. A dependency naming a
// node the file does not contain is a node that can never be assembled a brief;
// a dependency on a LATER id is a cycle the frontier would wait on forever,
// because ids are minted in admission order and an edge can only ever point
// backwards.
func decodeTasks(content []byte) (taskDocument, error) {
	var document taskDocument
	if err := json.Unmarshal(content, &document); err != nil {
		return taskDocument{}, err
	}
	if document.Type != taskDocumentType {
		return taskDocument{}, fmt.Errorf("type is %q, want %q", document.Type, taskDocumentType)
	}
	if document.Version != taskFileVersion {
		return taskDocument{}, fmt.Errorf("version %d, want %d", document.Version, taskFileVersion)
	}
	seen := make(map[uint64]bool, len(document.Nodes))
	var highest uint64
	for _, record := range document.Nodes {
		switch {
		case record.ID == 0:
			return taskDocument{}, fmt.Errorf("a node has no id")
		case seen[record.ID]:
			return taskDocument{}, fmt.Errorf("node %d appears twice", record.ID)
		case strings.TrimSpace(record.Title) == "":
			return taskDocument{}, fmt.Errorf("node %d has no title", record.ID)
		case strings.TrimSpace(record.Brief) == "":
			return taskDocument{}, fmt.Errorf("node %d has no brief", record.ID)
		case strings.TrimSpace(record.Acceptance) == "":
			return taskDocument{}, fmt.Errorf("node %d has no acceptance", record.ID)
		case !validTaskState(record.State):
			return taskDocument{}, fmt.Errorf("node %d is in state %q", record.ID, record.State)
		case !validMergeOutcome(record.Merge):
			return taskDocument{}, fmt.Errorf("node %d has merge outcome %q", record.ID, record.Merge)
		case record.MaxSteps < 0 || record.NoProgress < 0:
			return taskDocument{}, fmt.Errorf("node %d has a negative threshold", record.ID)
		case record.ElapsedMS < 0:
			return taskDocument{}, fmt.Errorf("node %d has a negative elapsed", record.ID)
		}
		for _, dependency := range record.DependsOn {
			if !seen[dependency] {
				return taskDocument{}, fmt.Errorf("node %d waits on %d, which is not in this graph", record.ID, dependency)
			}
		}
		seen[record.ID] = true
		if record.ID > highest {
			highest = record.ID
		}
	}
	if document.Seq < highest {
		return taskDocument{}, fmt.Errorf("the id counter is %d behind node %d", document.Seq, highest)
	}
	return document, nil
}

func validTaskState(state TaskState) bool {
	switch state {
	case TaskQueued, TaskRunning, TaskDone, TaskFailed:
		return true
	}
	return false
}

func validMergeOutcome(merge string) bool {
	switch merge {
	case "", mergeMerged, mergeConflicted, mergeInPlace, mergeAborted:
		return true
	}
	return false
}

// ── recovery ────────────────────────────────────────────────────────────────

// taskRecovery is what one recovery found, in the four categories a person and a
// model both need kept apart, plus the notes nobody ever got.
type taskRecovery struct {
	done        int
	failed      int
	interrupted int
	waiting     int
	// branches are the interrupted nodes' branches that are still on disk. They
	// are the whole reason the summary is worth reading: a kept branch is work
	// the person still has.
	branches []string
	// notes are the completion notes that were never handed over, in the shape
	// [taskNote] would have produced for them.
	notes []string
}

// any reports whether the recovery restored anything at all. A checkpoint that
// held an empty graph — a session that proposed nothing — is not news.
func (r taskRecovery) any() bool {
	return r.done+r.failed+r.interrupted+r.waiting > 0
}

// note is the ONE line the person and the model read about a resumed graph,
// with whatever was never announced attached under it.
//
//	recovered task graph: 2 done · 1 interrupted (branch task/fix-it-9c1a2f kept) · 1 waiting
//
//	task 3 failed: Fix the reconciler
//	session ended mid-run; branch task/fix-it-9c1a2f kept
//	it was stopped; its branch task/fix-it-9c1a2f was kept
func (r taskRecovery) note() string {
	if !r.any() {
		return ""
	}
	parts := make([]string, 0, 4)
	if r.done > 0 {
		parts = append(parts, strconv.Itoa(r.done)+" done")
	}
	if r.failed > 0 {
		parts = append(parts, strconv.Itoa(r.failed)+" failed")
	}
	if r.interrupted > 0 {
		parts = append(parts, strconv.Itoa(r.interrupted)+" interrupted ("+keptBranches(r.branches)+")")
	}
	if r.waiting > 0 {
		parts = append(parts, strconv.Itoa(r.waiting)+" waiting")
	}
	note := "recovered task graph: " + strings.Join(parts, " · ")
	if len(r.notes) > 0 {
		note += "\n\n" + strings.Join(r.notes, "\n\n")
	}
	return note
}

// keptBranches names what an interrupt left behind, or says plainly that it left
// nothing — "1 interrupted" with no clause would leave the person wondering
// whether there is a branch to go and look at.
func keptBranches(branches []string) string {
	switch len(branches) {
	case 0:
		return "no branch kept"
	case 1:
		return "branch " + branches[0] + " kept"
	default:
		return "branches " + strings.Join(branches, ", ") + " kept"
	}
}

// recoverTasks is the whole resume: load, reconcile, continue.
//
// It runs at Agent construction, which is the only moment at which "this journal
// has been opened again" is a fact rather than a guess, and it runs for a
// CONVERSATION only: a node's own agent has a journal of its own with no graph
// under it, and a node that rehydrated a graph would be a second scheduler
// running inside a worktree.
func (a *Agent) recoverTasks() {
	if a.config.InTask {
		return
	}
	document, found := loadTaskCheckpoint(taskCheckpointPath(a.config.SessionFile))
	if !found || len(document.Nodes) == 0 {
		return
	}

	graph := a.graph()
	recovery := graph.rehydrate(document, a.config.Workspace)
	// The consume-once receipt reaches the disk BEFORE anything else happens: a
	// second crash between here and the first turn must not hand the same
	// interrupt to a second recovery.
	graph.checkpoint()

	if note := recovery.note(); note != "" {
		a.enqueueSteering(note)
	}
	// CONTINUE — the same frontier every other transition turns. A queued node
	// whose prerequisites are done starts now; one whose prerequisite was
	// interrupted fails through the cascade that already exists.
	graph.runFrontier()
}

// rehydrate rebuilds the graph from a checkpoint and reconciles it with the
// disk. It is the pure half of recovery: no provider, no scheduling, nothing
// that cannot be done with a file and a repository.
//
// THE ONLY RECONCILIATION IS THE INTERRUPT, because it is the only record whose
// truth changed while nobody was watching. A done node is done, a failed node
// failed, a queued node never started — but a RUNNING node names work that
// stopped existing the moment the process did, and what it left is on disk under
// a branch nobody is going to come back for unless somebody says its name.
func (g *TaskGraph) rehydrate(document taskDocument, workspace string) taskRecovery {
	var recovery taskRecovery
	records := make([]taskRecord, 0, len(document.Nodes))
	for _, record := range document.Nodes {
		if record.State == TaskRunning {
			var kept string
			record, kept = interrupt(record, workspace)
			recovery.interrupted++
			if kept != "" {
				recovery.branches = append(recovery.branches, kept)
			}
		} else {
			switch record.State {
			case TaskDone:
				recovery.done++
			case TaskFailed:
				recovery.failed++
			default:
				recovery.waiting++
			}
		}
		records = append(records, record)
	}

	g.mu.Lock()
	if g.nodes == nil {
		g.nodes = make(map[uint64]*TaskNode, len(records))
	}
	if document.Seq > g.seq {
		g.seq = document.Seq
	}
	var unannounced []*TaskNode
	for _, record := range records {
		node := restoreNode(g, record)
		g.nodes[node.id] = node
		g.order = append(g.order, node.id)
		if node.state != TaskQueued && !node.noted {
			unannounced = append(unannounced, node)
		}
	}
	g.mu.Unlock()

	// The notes nobody ever got, in the shape a fresh run would have produced —
	// and marked as handed over, so this is the only life of this session in
	// which they are said.
	for _, node := range unannounced {
		recovery.notes = append(recovery.notes, taskNote(node.notice()))
		node.markNoted()
	}
	return recovery
}

// restoreNode is one record as a node again. Its done channel is CLOSED for a
// terminal node — a waiter on finished work must not block on a run that is
// never going to happen — and open for a queued one, which is genuinely still
// waiting.
func restoreNode(graph *TaskGraph, record taskRecord) *TaskNode {
	node := &TaskNode{
		graph:     graph,
		id:        record.ID,
		dependsOn: record.DependsOn,
		done:      make(chan struct{}),
		spec: taskSpec{
			title:      record.Title,
			summary:    record.Summary,
			brief:      record.Brief,
			acceptance: record.Acceptance,
			dependsOn:  record.DependsOn,
			maxSteps:   record.MaxSteps,
			noProgress: record.NoProgress,
		},
		state:       record.State,
		report:      record.Report,
		changed:     record.Changed,
		branch:      record.Branch,
		worktree:    record.Worktree,
		merge:       record.Merge,
		elapsed:     time.Duration(record.ElapsedMS) * time.Millisecond,
		noted:       record.Noted,
		interrupted: record.Interrupted,
	}
	if record.State != TaskQueued {
		close(node.done)
	}
	return node
}

// interrupt turns a node that was running into the failed node it became when
// the process died, and reports which branch — if any — is still on disk for the
// person to go and look at.
//
// It CHECKS rather than assumes. A branch named in a checkpoint may have been
// merged, deleted or pruned by the person in between, and a report promising
// work on a branch that is gone is worse than no report: it is the harness
// telling somebody their work is safe when it is not.
func interrupt(record taskRecord, workspace string) (taskRecord, string) {
	record.State = TaskFailed
	record.Interrupted = true
	// The completion note is owed: nobody ever announced this node, because
	// nothing was alive to announce it.
	record.Noted = false

	if record.Merge == mergeInPlace {
		// There was no repository to branch from, so its edits are already in the
		// person's tree — calling that "aborted" would say work was thrown away
		// that is sitting in front of them.
		record.Report = "session ended mid-run; it worked directly in the workspace, so whatever it wrote is in your tree"
		return record, ""
	}
	record.Merge = mergeAborted

	branch := strings.TrimSpace(record.Branch)
	switch {
	case branch == "":
		record.Report = "session ended mid-run; it had not got as far as a working copy"
		return record, ""
	case !branchOnDisk(workspace, branch):
		record.Report = "session ended mid-run; its branch " + branch + " is no longer in the repository"
		return record, ""
	}
	report := "session ended mid-run; branch " + branch + " kept"
	if worktree := strings.TrimSpace(record.Worktree); worktree != "" {
		if _, err := os.Stat(worktree); err == nil {
			report += ", its worktree is at " + worktree
		}
	}
	record.Report = report
	return record, branch
}

// branchOnDisk reports whether the node's branch is still in the repository.
func branchOnDisk(workspace, branch string) bool {
	root, ok := repositoryRoot(workspace)
	if !ok {
		return false
	}
	_, err := git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
