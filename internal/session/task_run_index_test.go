package session

// A hand-off's run reaches the project's index and its conversation's presence
// (task_run_index.go), so work a senior-dev run is doing is visible to the `@`
// list, to other windows and to another conversation's tasks tool.

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A SECOND CONVERSATION ON THE PROJECT FINDS A senior-dev RUN. The run's rows
// never reached the project's index, so every other conversation's `tasks` tool,
// the `@` list and the hop's count were blind to it for its whole life and after
// it; and its conversation's presence never named it, so a row that did reach
// the index would have read as nothing behind it. The run's own conversation
// reads it once, from its store, and not a second time from the index.
func TestAnotherConversationOnTheProjectFindsAProgramsRun(t *testing.T) {
	double := newBeltRunDouble("submitted and verified")
	registerBeltRunEngine(t, double)
	bucket := t.TempDir()
	workspace := newTestRepo(t)
	conversation := func(name string) *Agent {
		dir := filepath.Join(bucket, name)
		agent, _ := newTestAgent(t, beltRunCompleter{text: "submitted and verified"}, func(config *Config) {
			config.Workspace = workspace
			config.Place = Place{Dir: dir}
			config.SessionFile = filepath.Join(dir, placeTranscript)
			config.AskConsent = false
			config.Delegates = testPrograms("fake")
		})
		return agent
	}
	runner, other := conversation("runner"), conversation("other")
	handoff := time.Date(2026, time.September, 24, 1, 14, 7, 0, time.UTC)
	clock := &fakeClock{at: handoff}
	runner.taskNow = clock.now

	id, title, _, err := runner.StartDelegate(context.Background(), "fake", "add two files to the project")
	if err != nil {
		t.Fatalf("StartDelegate: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	spec := double.spec
	double.mu.Unlock()
	key := strconv.FormatUint(id, 10)
	slug := TaskSlug(title)

	// WHILE IT RUNS: the index holds it running from the hand-off, the runner's
	// presence names it, and the other conversation finds it by its words.
	row := indexRowFor(t, runner, key)
	if row.Status != string(TaskRunning) || !row.StartedAt.Equal(handoff) {
		t.Fatalf("the index's row for the run = %+v, want it running from the hand-off", row)
	}
	var named bool
	for _, task := range runner.presenceSnapshot(time.Now()).RunningTasks {
		named = named || (task.ID == key && task.StartedAt.Equal(handoff))
	}
	if !named {
		t.Fatalf("the runner's presence does not name the run: %+v", runner.presenceSnapshot(time.Now()).RunningTasks)
	}
	found, failed := runTool(t, other, "tasks", `{"query":"two files"}`)
	if failed || !strings.Contains(found, key+" · "+slug+" · working") {
		t.Fatalf("the other conversation's search answered %q (failed %v), want the run working", found, failed)
	}
	// The runner's own listing names it once, from its store.
	own, failed := runTool(t, runner, "tasks", `{}`)
	if failed || !strings.Contains(own, "#"+key+" · "+title+" · running") || strings.Contains(own, key+" · "+slug) {
		t.Fatalf("the runner's own listing = %q (failed %v), want the run once, by the number its rail shows", own, failed)
	}

	exited := handoff.Add(22*time.Minute + 51*time.Second)
	if err := delegate.WriteProgram(plandb.TaskDir(filepath.Dir(spec.Store.Path()), spec.Store.RootID()), delegate.ProgramRecord{Name: "fake", StartedAt: handoff, EndedAt: exited}); err != nil {
		t.Fatal(err)
	}
	clock.advance(30 * time.Minute)
	endBeltRun(t, runner, double)

	// ONCE IT HAS ENDED: the index closes it on the run's one pair, the
	// runner's presence lets it go, and the other conversation reads the ending.
	row = indexRowFor(t, runner, key)
	if row.Status != string(TaskDone) || !row.EndedAt.Equal(exited) || row.Duration() != 22*time.Minute+51*time.Second {
		t.Fatalf("the index's closing row = %+v, want it done at the program's exit, 22m 51s in", row)
	}
	for _, task := range runner.presenceSnapshot(time.Now()).RunningTasks {
		if task.ID == key {
			t.Fatalf("the runner's presence still names the ended run: %+v", task)
		}
	}
	found, failed = runTool(t, other, "tasks", `{"query":"two files"}`)
	if failed || !strings.Contains(found, key+" · "+slug+" · done") || !strings.Contains(found, "22m 51s") {
		t.Fatalf("the other conversation's search answered %q (failed %v), want the run done with its time", found, failed)
	}
	var mentioned bool
	for _, entry := range other.TaskIndex() {
		mentioned = mentioned || (entry.ID == key && entry.Title == title)
	}
	if !mentioned {
		t.Fatal("the `@` list of the other conversation does not carry the run")
	}
}

// indexRowFor is the project index's last word on one of this conversation's
// rows, read the way every reader of the index reads it.
func indexRowFor(t *testing.T, agent *Agent, id string) TaskIndexEntry {
	t.Helper()
	agent.mu.Lock()
	session := agent.sessionID()
	agent.mu.Unlock()
	for _, entry := range ReadTaskIndex(agent.config.taskIndexFile()) {
		if entry.ID == id && entry.SessionID == session {
			return entry
		}
	}
	t.Fatalf("the project's index holds no row %s for this conversation", id)
	return TaskIndexEntry{}
}
