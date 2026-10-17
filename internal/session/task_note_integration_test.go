package session

// A NOTE'S OUTCOME IS THE PRODUCER'S OWN ANSWER, TYPED, ON EVERY ROAD.
//
// These laws send through the app's door ([Agent.AppTaskActionReceipt]) into
// the real producers — a node's admission and its checkpoint, a run's SQLite
// store, a plan task's SQLite store — and read the outcome, the key on the disk
// and the key on the page back. Nothing here is matched by position or words.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// Delivered, held and refused are each the node's own answer, and the key that
// reached the disk is the one the receipt names.
func TestANoteReceiptIsTheNodesOwnAnswer(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	task := strconv.FormatUint(id, 10)

	// Nobody is in the room and the work is running: HELD, with its key on disk.
	held := agent.AppTaskActionReceipt(AppTaskIntent{ID: task, Action: "note", Text: "same words", ClientID: "k-held"})
	if held.Outcome != AppTaskHeld || held.ClientID != "k-held" || held.Reason != "" {
		t.Fatalf("held receipt = %+v", held)
	}
	// A reader in the room: DELIVERED, into the reader's queue.
	worker := workerIn(t, node)
	delivered := agent.AppTaskActionReceipt(AppTaskIntent{ID: task, Action: "note", Text: "same words", ClientID: "k-live"})
	if delivered.Outcome != AppTaskDelivered || delivered.ClientID != "k-live" {
		t.Fatalf("delivered receipt = %+v", delivered)
	}
	if got := workerQueue(worker); len(got) == 0 || !strings.Contains(got[0], "same words") {
		t.Fatalf("the reader never got the note: %q", got)
	}
	if keys := diskKeys(t, path, id); strings.Join(keys, ",") != "k-held,k-live" {
		t.Fatalf("disk keys %q", keys)
	}
	// A disk that will not take it: REFUSED before effect, never unknown.
	queued := len(workerQueue(worker))
	blockStore(t, node)
	refused := agent.AppTaskActionReceipt(AppTaskIntent{ID: task, Action: "note", Text: "same words", ClientID: "k-refused"})
	if refused.Outcome != AppTaskRefused || refused.ClientID != "k-refused" || !strings.HasPrefix(refused.Reason, errNotWrittenDown.Error()) {
		t.Fatalf("refused receipt = %+v", refused)
	}
	if got := len(workerQueue(worker)); got != queued {
		t.Fatalf("a refused note reached the reader (%d → %d)", queued, got)
	}
	if said := saidTo(t, agent, id); len(said) != 2 {
		t.Fatalf("the record holds %d directions, want the two that were taken", len(said))
	}
	steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))

	// A verb that carries no words has no receipt and is not taken.
	paused := agent.AppTaskActionReceipt(AppTaskIntent{ID: task, Action: "pause"})
	node.graph.mu.Lock()
	wasPaused := node.app.Paused
	node.graph.mu.Unlock()
	if paused.Outcome != AppTaskRefused || wasPaused {
		t.Fatalf("pause through the receipt door = %+v, paused=%v", paused, wasPaused)
	}
}

// The two outcomes that are NOT refusals are read off the engine's own
// sentinels as the producers wrap them, never off their sentences.
func TestUnknownAndBriefNotKeptAreNeverRefusals(t *testing.T) {
	uncertain := appTaskReceiptOf(SteerReceipt{}, fmt.Errorf("%w: %v", errDirectionUncertain, errors.New("disk full")), "k")
	if uncertain.Outcome != AppTaskUnknown || uncertain.ClientID != "k" || uncertain.Reason == "" {
		t.Fatalf("an unknown rollback = %+v", uncertain)
	}
	brief := appTaskReceiptOf(SteerReceipt{Held: true}, fmt.Errorf("%w: %v", ErrBriefNotKept, errors.New("disk full")), "k")
	if brief.Outcome != AppTaskHeld || !brief.BriefNotKept {
		t.Fatalf("a held brief whose page brief was not kept = %+v", brief)
	}
	// A refusal whose WORDS say "unknown" is still a refusal: the sentence is
	// never read.
	worded := appTaskReceiptOf(SteerReceipt{}, errors.New("no answer came back, so it is not known whether these words arrived"), "k")
	if worded.Outcome != AppTaskRefused {
		t.Fatalf("a sentence was classified: %+v", worded)
	}
}

// A running quick task takes a keyed note through the receipt door; a settled
// one refuses it there, and nothing starts.
func TestAQuickTaskNoteReceiptIsDeliveredWhileRunningAndRefusedAfter(t *testing.T) {
	agent, node, _ := appNodeFixture(t, true)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	running := agent.AppTaskActionReceipt(AppTaskIntent{ID: "1", Action: "note", Text: "use tabs", ClientID: "q-1"})
	if running.Outcome == AppTaskRefused || running.Outcome == AppTaskUnknown || running.ClientID != "q-1" {
		t.Fatalf("running quick receipt = %+v", running)
	}
	if keys := diskKeys(t, path, 1); len(keys) != 1 || keys[0] != "q-1" {
		t.Fatalf("disk keys %q", keys)
	}
	node.graph.mu.Lock()
	node.state = TaskDone
	before := len(node.graph.nodes)
	node.graph.mu.Unlock()
	late := agent.AppTaskActionReceipt(AppTaskIntent{ID: "1", Action: "note", Text: "too late", ClientID: "q-2"})
	if late.Outcome != AppTaskRefused || !strings.Contains(late.Reason, "not running") {
		t.Fatalf("settled quick receipt = %+v", late)
	}
	node.graph.mu.Lock()
	after := len(node.graph.nodes)
	node.graph.mu.Unlock()
	if after != before || len(node.directionsNow()) != 1 || node.stateNow() != TaskDone {
		t.Fatal("a refused quick note left something behind")
	}
}

// THE NODE DOOR CARRIES THE KEY INTO A RUN'S STORE. A run row is not a node,
// so [Agent.sayToTask] hands it to the run; the key must ride that hand-off and
// be committed in the note's own transaction.
func TestTheNodeDoorCommitsARunRowNotesKeyInTheStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBatchRunDouble())
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 61, "the run", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	if _, err := agent.sayToTask(61, "same words", fromPerson, spokenSource{clientID: "via-node-door"}); err != nil {
		t.Fatalf("node door: %v", err)
	}
	receipt := agent.AppTaskActionReceipt(AppTaskIntent{ID: "61", Action: "note", Text: "same words", ClientID: "via-app"})
	if receipt.Outcome != AppTaskDelivered || receipt.ClientID != "via-app" {
		t.Fatalf("run row receipt = %+v", receipt)
	}
	store := beltRunStoreAt(t, dir)
	notes := store.Notes("61", 0)
	_ = store.Close()
	if len(notes) != 2 || notes[0].ClientID != "via-node-door" || notes[1].ClientID != "via-app" || notes[0].ID == notes[1].ID {
		t.Fatalf("store notes = %#v", notes)
	}
}

// A PLAN TASK'S NOTE THROUGH THE APP DOOR is committed with its key, and the
// app's own page carries the key the store kept. Equal words under two keys
// are two notes; an unkeyed one is unknown; no read step is invented.
func TestAPlanTaskNoteThroughTheAppDoorKeepsItsKeyOnThePage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	armPlanStore(t, agent, path, "ask-chat")
	rows, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, row := range rows {
		if row.Title == "Write handler" {
			id = row.ID
		}
	}
	if id == "" {
		t.Fatalf("the plan task is not on the list: %+v", rows)
	}
	for _, key := range []string{"p-1", "p-2", ""} {
		receipt := agent.AppTaskActionReceipt(AppTaskIntent{ID: id, Action: "note", Text: "same words", ClientID: key})
		if receipt.Outcome != AppTaskDelivered || receipt.ClientID != key {
			t.Fatalf("plan note %q receipt = %+v", key, receipt)
		}
	}
	view, found, err := agent.AppTask(id)
	if err != nil || !found {
		t.Fatalf("AppTask = %v %v", found, err)
	}
	var keys []string
	for _, note := range view.Notes {
		if note.From != AppNoteFromPerson {
			continue
		}
		if note.ReadAtStep != nil {
			t.Fatalf("a plan note was given a read step: %+v", note)
		}
		keys = append(keys, note.ClientID)
	}
	if strings.Join(keys, "|") != "p-1|p-2|" {
		t.Fatalf("page keys = %q", keys)
	}
}

// A COMMIT THAT FAILS MAY FOLLOW THE EFFECT, so the app is told UNKNOWN and the
// words are never sent again. The refusals before the commit stay refusals.
// SQLite itself refuses the COMMIT here ([commitGate]); the production store is
// untouched and commits with its own tx.Commit.
func TestAKeyedRunRowNoteWhoseCommitFailsIsUnknown(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBatchRunDouble())
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	gate := gateCommitsOf(t, agent.graph().planPath(), "k-commit")
	stand := taskStand{dir: agent.config.Workspace, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 62, "the run", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	agent.beltMu.Lock()
	live := agent.beltRun.store
	agent.beltMu.Unlock()

	// A refusal BEFORE the commit is still a refusal, and never reaches one.
	missing := agent.AppTaskActionReceipt(AppTaskIntent{ID: "63", Action: "note", Text: "same words", ClientID: "k-commit"})
	if missing.Outcome != AppTaskRefused || gate.refused.Load() != 0 {
		t.Fatalf("a note to a task that is not there = %+v (commits refused %d), want refused before any commit", missing, gate.refused.Load())
	}

	receipt := agent.AppTaskActionReceipt(AppTaskIntent{ID: "62", Action: "note", Text: "same words", ClientID: "k-commit"})
	if receipt.Outcome != AppTaskUnknown || receipt.ClientID != "k-commit" || receipt.Reason == "" {
		t.Fatalf("a run row note whose commit failed = %+v, want unknown", receipt)
	}
	if got := gate.refused.Load(); got != 1 {
		t.Fatalf("SQLite refused %d commits, want exactly the note's one", got)
	}
	// The refused COMMIT rolled back: nothing of the note is on the disk. The
	// answer stays unknown anyway, because a real failed sync can leave it there.
	if keys := personNoteKeys(live.Notes("62", 0)); keys != "" {
		t.Fatalf("a note whose commit was refused is on the disk: %q", keys)
	}

	// Disarmed, the same store's next commit works, and the unknown note was not
	// sent again behind the person's back.
	gate.disarm()
	again := agent.AppTaskActionReceipt(AppTaskIntent{ID: "62", Action: "note", Text: "same words", ClientID: "k-after"})
	if again.Outcome != AppTaskDelivered {
		t.Fatalf("the store after a failed commit = %+v", again)
	}
	if keys := personNoteKeys(live.Notes("62", 0)); keys != "k-after" {
		t.Fatalf("person notes after the healthy commit = %q, want only k-after", keys)
	}
	if got := gate.refused.Load(); got != 1 {
		t.Fatalf("SQLite refused %d commits after the gate was disarmed", got)
	}
}

func TestAPlanTaskNoteWhoseCommitFailsIsUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	gate := gateCommitsOf(t, path, "p-commit")
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	armPlanStore(t, agent, path, "ask-chat")
	rows, err := agent.AppTasks()
	if err != nil || len(rows) == 0 {
		t.Fatalf("AppTasks = %v, %v", rows, err)
	}
	id := ""
	for _, row := range rows {
		if row.Title == "Write handler" {
			id = row.ID
		}
	}

	// The same door refuses a task that is not there, before any commit.
	gone := agent.AppTaskActionReceipt(AppTaskIntent{ID: "999", Action: "note", Text: "same words", ClientID: "p-commit"})
	if gone.Outcome != AppTaskRefused || gate.refused.Load() != 0 {
		t.Fatalf("a plan note to nothing = %+v (commits refused %d), want refused before any commit", gone, gate.refused.Load())
	}

	receipt := agent.AppTaskActionReceipt(AppTaskIntent{ID: id, Action: "note", Text: "same words", ClientID: "p-commit"})
	if receipt.Outcome != AppTaskUnknown || receipt.ClientID != "p-commit" || receipt.Reason == "" {
		t.Fatalf("a plan note whose commit failed = %+v, want unknown", receipt)
	}
	if got := gate.refused.Load(); got != 1 {
		t.Fatalf("SQLite refused %d commits, want exactly the note's one", got)
	}
	if keys := planPersonNoteKeys(t, path); keys != "" {
		t.Fatalf("a plan note whose commit was refused is on the disk: %q", keys)
	}

	gate.disarm()
	again := agent.AppTaskActionReceipt(AppTaskIntent{ID: id, Action: "note", Text: "same words", ClientID: "p-after"})
	if again.Outcome != AppTaskDelivered {
		t.Fatalf("the plan store after a failed commit = %+v", again)
	}
	if keys := planPersonNoteKeys(t, path); keys != "p-after" {
		t.Fatalf("plan person notes after the healthy commit = %q, want only p-after", keys)
	}
}

// A PLAN TASK'S BRIEF ENDS IN THE SAME TRANSACTION, so a commit SQLite refuses
// is UNKNOWN there too: the amendment can be on the disk under that error, and a
// person told "refused" who sends again would find it prepended twice.
func TestAPlanTaskBriefWhoseCommitFailsIsUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	const armed = "brief-commit: keep the old lookup as a fallback"
	gate := gateTaskCommitsOf(t, path, armed)
	seedPlanStore(t, path, "ask-chat", plandb.TaskSpec{ID: "child", Title: "Write handler", Description: "replace the lookup"})
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, nil)
	armPlanStore(t, agent, path, "ask-chat")
	rows, err := agent.AppTasks()
	if err != nil || len(rows) == 0 {
		t.Fatalf("AppTasks = %v, %v", rows, err)
	}
	id := ""
	for _, row := range rows {
		if row.Title == "Write handler" {
			id = row.ID
		}
	}

	gone := agent.AppTaskActionReceipt(AppTaskIntent{ID: "999", Action: "brief", Text: armed, ClientID: "b-gone"})
	if gone.Outcome != AppTaskRefused || gate.refused.Load() != 0 {
		t.Fatalf("a plan brief to nothing = %+v (commits refused %d), want refused before any commit", gone, gate.refused.Load())
	}

	receipt := agent.AppTaskActionReceipt(AppTaskIntent{ID: id, Action: "brief", Text: armed, ClientID: "b-commit"})
	if receipt.Outcome != AppTaskUnknown || receipt.ClientID != "b-commit" || receipt.Reason == "" || receipt.BriefNotKept {
		t.Fatalf("a plan brief whose commit failed = %+v, want unknown", receipt)
	}
	if got := gate.refused.Load(); got != 1 {
		t.Fatalf("SQLite refused %d commits, want exactly the brief's one", got)
	}
	if got := planTaskDescription(t, path); got != "replace the lookup" {
		t.Fatalf("the description after the refused commit = %q, want it unchanged", got)
	}

	gate.disarm()
	again := agent.AppTaskActionReceipt(AppTaskIntent{ID: id, Action: "brief", Text: "brief-after: log each fallback", ClientID: "b-after"})
	if again.Outcome != AppTaskDelivered || again.ClientID != "b-after" {
		t.Fatalf("the plan store after a failed commit = %+v", again)
	}
	// The healthy brief landed once, and the unknown one was never sent again.
	if got := planTaskDescription(t, path); got != "brief-after: log each fallback\n\nreplace the lookup" {
		t.Fatalf("the description after the healthy commit = %q", got)
	}
}

// A failed plan-store commit is unknown whatever road raised it, without the
// road's own mapping; a pre-commit refusal is still a refusal.
func TestAFailedPlanCommitIsUnknownOnEveryReceiptRoad(t *testing.T) {
	uncertain := fmt.Errorf("amend: %w: %w", plandb.ErrCommitUncertain, errors.New("disk I/O error"))
	if got := appTaskReceiptOf(SteerReceipt{}, uncertain, "k"); got.Outcome != AppTaskUnknown || got.ClientID != "k" {
		t.Fatalf("a failed commit = %+v, want unknown", got)
	}
	if got := appTaskReceiptOf(SteerReceipt{}, errors.New(`task "x" not found`), "k"); got.Outcome != AppTaskRefused {
		t.Fatalf("a pre-commit refusal = %+v, want refused", got)
	}
}

// planTaskDescription reads the plan task's description from the disk through
// a handle of its own.
func planTaskDescription(t *testing.T, path string) string {
	t.Helper()
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("reopen the plan store: %v", err)
	}
	defer store.Close()
	task := store.Task("child")
	if task == nil {
		t.Fatal("the plan task is gone")
	}
	return task.Description
}

// personNoteKeys is every person note's client id, in order, joined by "|".
func personNoteKeys(notes []plandb.Note) string {
	var keys []string
	for _, note := range notes {
		if note.From == plandb.NoteFromPerson {
			keys = append(keys, note.ClientID)
		}
	}
	return strings.Join(keys, "|")
}

// planPersonNoteKeys reads the plan task's person notes from the disk through a
// handle of its own.
func planPersonNoteKeys(t *testing.T, path string) string {
	t.Helper()
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("reopen the plan store: %v", err)
	}
	defer store.Close()
	return personNoteKeys(store.Notes("child", 0))
}

// Only the commit wears the sentinel: the store's own refusals keep their words.
func TestOnlyACommitFailureIsUncertain(t *testing.T) {
	refused := errors.New("task \"x\" not found")
	if commitUncertainAsUnanswered(refused) != refused {
		t.Fatal("a pre-commit refusal was rewritten")
	}
	err := commitUncertainAsUnanswered(fmt.Errorf("%w: %w", plandb.ErrCommitUncertain, errors.New("fsync")))
	if !errors.Is(err, ErrSendUnanswered) {
		t.Fatalf("a commit failure = %v, want ErrSendUnanswered", err)
	}
}
