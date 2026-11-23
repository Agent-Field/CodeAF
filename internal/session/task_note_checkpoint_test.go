package session

// AN AUTHORED NOTE'S KEY IS WRITTEN WITH ITS WORDS, IN THE ONE ADMISSION THAT
// PUTS THEM ON THE DISK, BEFORE ANY WORKER IS HANDED THEM.
//
// The key is how a surface tells which of its notes a page line is. It is not a
// retry identity — two notes under one key are two notes — so these laws read
// the checkpoint on disk and the worker's own queue, never a fixture's echo.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// workerIn stands a real agent in the node's room, so a note is handed to a
// reader and the reader's queue says when it arrived.
func workerIn(t *testing.T, node *TaskNode) *Agent {
	t.Helper()
	worker, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
		InTask: true, tasker: node.graph, taskID: node.id, taskDepth: 1,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent for the node: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	node.openRoom().speaking(worker)
	t.Cleanup(func() { node.openRoom().speaking(nil) })
	return worker
}

func workerQueue(worker *Agent) []string {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	var out []string
	for _, note := range worker.steering {
		out = append(out, note.text())
	}
	return out
}

// diskKeys is each direction's key as a resume would read it off the file.
func diskKeys(t *testing.T, path string, id uint64) []string {
	t.Helper()
	var keys []string
	for _, said := range keptDirections(t, path, id).directions {
		keys = append(keys, said.clientID)
	}
	return keys
}

// blockStore points the graph at a checkpoint no process can write: its
// directory is an ordinary file.
func blockStore(t *testing.T, node *TaskNode) {
	t.Helper()
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	node.graph.mu.Lock()
	node.graph.store = newTaskStore(filepath.Join(blocked, "tasks.json"))
	node.graph.mu.Unlock()
}

// THE FIRST LAW. The key is on the disk before the worker's queue holds the
// words, and the note costs exactly one write — there is no second write after
// delivery that could fail and turn a delivered note into an error.
func TestAKeyedNoteIsOnTheDiskBeforeTheWorkerHearsIt(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	worker := workerIn(t, node)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	var writes, queuedAtWrite int
	node.graph.mu.Lock()
	node.graph.store.duringWrite = func() {
		writes++
		queuedAtWrite = len(workerQueue(worker))
	}
	node.graph.mu.Unlock()

	if err := agent.AppTaskAction(AppTaskIntent{ID: strconv.FormatUint(id, 10), Action: "note", Text: "make it CSV", ClientID: "c-1"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if writes != 1 || queuedAtWrite != 0 {
		t.Fatalf("writes=%d, queued at the write=%d; want one write, before the hand-off", writes, queuedAtWrite)
	}
	if got := workerQueue(worker); len(got) == 0 || !strings.Contains(got[0], "make it CSV") {
		t.Fatalf("the worker never got the note: %q", got)
	}
	if keys := diskKeys(t, path, id); len(keys) != 1 || keys[0] != "c-1" {
		t.Fatalf("disk keys %q, want [c-1]", keys)
	}
	node.graph.mu.Lock()
	sidecar := len(node.app.Notes)
	node.graph.mu.Unlock()
	if sidecar != 0 {
		t.Fatalf("the note was also written to the app sidecar (%d rows)", sidecar)
	}
}

// Equal words under distinct keys are distinct notes, one key said twice is two
// notes, and a note with no key carries none — on the page, on the disk, and on
// a node restored from that disk.
func TestKeyedNotesSurviveARestartByTheirOwnKeys(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	workerIn(t, node)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	task := strconv.FormatUint(id, 10)
	want := []string{"c-1", "c-2", "", "c-1", ""}
	for _, key := range []string{"c-1", "c-2", "", "c-1", strings.Repeat("x", AppNoteClientIDMax+1)} {
		if err := agent.AppTaskAction(AppTaskIntent{ID: task, Action: "note", Text: "same words", ClientID: key}); err != nil {
			t.Fatalf("note %q: %v", key, err)
		}
	}
	row, found, err := agent.AppTask(task)
	if err != nil || !found || len(row.Notes) != len(want) {
		t.Fatalf("page %+v %v", row.Notes, err)
	}
	seen := map[uint64]bool{}
	for i, key := range want {
		if row.Notes[i].ClientID != key || seen[row.Notes[i].Direction] {
			t.Fatalf("page note %d = %+v, want key %q on its own direction", i, row.Notes[i], key)
		}
		seen[row.Notes[i].Direction] = true
	}
	// An unkeyed note is written at the next checkpoint, as it always was.
	node.graph.checkpoint()
	if keys := diskKeys(t, path, id); strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("disk keys %q, want %q", keys, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document taskDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, record := range document.Nodes {
		if record.ID != id {
			continue
		}
		restored := restoreNode(node.graph, record)
		restored.graph.mu.Lock()
		notes := restored.appNotesLocked()
		restored.graph.mu.Unlock()
		for i, key := range want {
			if i >= len(notes) || notes[i].ClientID != key || notes[i].Direction != row.Notes[i].Direction {
				t.Fatalf("restored notes %+v, want keys %q on the same directions", notes, want)
			}
		}
		return
	}
	t.Fatalf("no node %d on the disk", id)
}

// A DISK THAT WILL NOT TAKE THE NOTE REFUSES IT BEFORE ANY EFFECT: nothing on the
// record, nothing in the worker's queue, nothing on the page.
func TestAKeyedNoteTheDiskRefusesIsNotDelivered(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	worker := workerIn(t, node)
	blockStore(t, node)
	err := agent.AppTaskAction(AppTaskIntent{ID: strconv.FormatUint(id, 10), Action: "note", Text: "make it CSV", ClientID: "c-1"})
	if !errors.Is(err, errNotWrittenDown) {
		t.Fatalf("err = %v, want the refusal before effect", err)
	}
	if errors.Is(err, ErrSendUnanswered) {
		t.Fatal("a refusal before effect reads as unknown")
	}
	if got := workerQueue(worker); len(got) != 0 {
		t.Fatalf("a refused note reached the worker: %q", got)
	}
	if said := saidTo(t, agent, id); len(said) != 0 {
		t.Fatalf("the record kept %+v", said)
	}
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
}

// A HELD NOTE IS A NOTE WITH ITS KEY: nobody is in the room, the work is still
// running, and the words are on the disk with the key and no read step yet.
func TestAHeldKeyedNoteKeepsItsKeyAndIsNotRead(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	task := strconv.FormatUint(id, 10)
	if err := agent.AppTaskAction(AppTaskIntent{ID: task, Action: "note", Text: "hold this", ClientID: "held-1"}); err != nil {
		t.Fatalf("held note: %v", err)
	}
	if keys := diskKeys(t, path, id); len(keys) != 1 || keys[0] != "held-1" {
		t.Fatalf("disk keys %q", keys)
	}
	row, _, _ := agent.AppTask(task)
	if len(row.Notes) != 1 || row.Notes[0].ClientID != "held-1" || row.Notes[0].ReadAtStep != nil {
		t.Fatalf("page %+v", row.Notes)
	}
}

// A KEY-ONLY NOTE THE DISK WILL NOT GIVE BACK IS UNKNOWN, not refused: its
// rollback takes the written road too, so a failed rollback cannot be told as a
// refusal over words a resume might still find.
func TestAKeyedNoteWhoseRollbackFailsIsUnknown(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	source := spokenSource{clientID: "c-9"}
	heard, err := admitDirection(node, "make it CSV", directionFromPerson, source)
	if err != nil || !heard.fresh() {
		t.Fatalf("admit: %+v %v", heard, err)
	}
	if keys := diskKeys(t, path, id); len(keys) != 1 || keys[0] != "c-9" {
		t.Fatalf("disk keys %q", keys)
	}
	blockStore(t, node)
	if err := dropDirection(node, heard, source); err == nil {
		t.Fatal("a rollback the disk refused was reported as done")
	}
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
}

// Notes asked at once under distinct keys are each written with their own key,
// and none is lost or swapped.
func TestConcurrentKeyedNotesKeepTheirOwnKeys(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	workerIn(t, node)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	const notes = 8
	var wait sync.WaitGroup
	errs := make([]error, notes)
	start := make(chan struct{})
	for i := range notes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs[i] = agent.AppTaskAction(AppTaskIntent{ID: strconv.FormatUint(id, 10), Action: "note", Text: "same words", ClientID: "k-" + strconv.Itoa(i)})
		}()
	}
	close(start)
	wait.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("note %d: %v", i, err)
		}
	}
	keys := diskKeys(t, path, id)
	got := map[string]bool{}
	for _, key := range keys {
		got[key] = true
	}
	if len(keys) != notes || len(got) != notes {
		t.Fatalf("disk keys %q, want %d distinct", keys, notes)
	}
}

// A brief is a keyed note plus the page's brief, and the key rides the
// direction rather than the sidecar.
func TestAKeyedBriefKeepsItsKeyOnTheDirection(t *testing.T) {
	agent, node, _ := appNodeFixture(t, false)
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "brief", Text: "a new brief", ClientID: "b-1"}); err != nil {
		t.Fatal(err)
	}
	row, _, _ := agent.AppTask("1")
	if row.Brief != "a new brief" || len(row.Notes) != 1 || row.Notes[0].ClientID != "b-1" {
		t.Fatalf("row %+v", row)
	}
	node.graph.mu.Lock()
	sidecar := len(node.app.Notes)
	node.graph.mu.Unlock()
	if sidecar != 0 {
		t.Fatalf("the brief's note was written to the sidecar too")
	}
}

// A record written while the key lived in the app's sidecar still reads back
// with it; a direction's own key wins wherever it has one; and a direction with
// neither is unknown, never stamped from its neighbours.
func TestALegacySidecarKeyReadsBackOnlyWhereTheDirectionHasNone(t *testing.T) {
	agent, node, _ := appNodeFixture(t, false)
	node.graph.mu.Lock()
	legacy, _, _ := node.assignment.hear("old words", directionFromPerson, time.Now(), spokenSource{})
	keyed, _, _ := node.assignment.hear("new words", directionFromPerson, time.Now(), spokenSource{clientID: "own"})
	node.assignment.hear("no key anywhere", directionFromPerson, time.Now(), spokenSource{})
	// The sidecar rows are deliberately NOT in direction order, so a reading
	// that paired them by position would put each key on the wrong note.
	node.app.Notes = []AppTaskNote{{Text: "new words", Direction: keyed, ClientID: "stale"}, {Text: "old words", Direction: legacy, ClientID: "legacy-1"}}
	node.graph.mu.Unlock()
	row, _, _ := agent.AppTask("1")
	want := []string{"own", "legacy-1", ""}
	if len(row.Notes) == len(want) && (row.Notes[0].Direction != keyed || row.Notes[1].Direction != legacy) {
		t.Fatalf("page %+v, want the sidecar's own direction ids", row.Notes)
	}
	if len(row.Notes) != len(want) {
		t.Fatalf("page %+v", row.Notes)
	}
	for i, key := range want {
		if row.Notes[i].ClientID != key {
			t.Fatalf("note %d key %q, want %q (%+v)", i, row.Notes[i].ClientID, key, row.Notes)
		}
	}
}

// A RUNNING QUICK TASK TAKES A KEYED NOTE through the node's own door; a settled
// one refuses it in the engine's words; neither starts anything.
func TestAQuickTaskTakesAKeyedNoteOnlyWhileRunning(t *testing.T) {
	agent, node, _ := appNodeFixture(t, true)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	count := func() int {
		node.graph.mu.Lock()
		defer node.graph.mu.Unlock()
		return len(node.graph.nodes)
	}
	before := count()
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "note", Text: "use tabs", ClientID: "q-1"}); err != nil {
		t.Fatalf("running quick note: %v", err)
	}
	if keys := diskKeys(t, path, 1); len(keys) != 1 || keys[0] != "q-1" {
		t.Fatalf("disk keys %q", keys)
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "pause"}); err == nil || err.Error() != "no such task" {
		t.Fatalf("a non-note action on a quick task = %v, want the old refusal", err)
	}
	node.graph.mu.Lock()
	node.state = TaskDone
	node.graph.mu.Unlock()
	err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "note", Text: "too late", ClientID: "q-2"})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("settled quick note = %v, want refused", err)
	}
	if said := node.directionsNow(); len(said) != 1 {
		t.Fatalf("a refused note reached the record: %+v", said)
	}
	if count() != before || node.stateNow() != TaskDone {
		t.Fatalf("a note started work: %d nodes (was %d), state %s", count(), before, node.stateNow())
	}
}
