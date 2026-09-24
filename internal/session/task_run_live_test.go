package session

// A run on the worker harness as the rest of the machine sees it: while it runs,
// when its chat closes under it, and once it is over (task_run_files.go,
// taskpresence.go, task_run_belt.go).
//
// Three holes, each measured on the real binary before these tests existed: a
// live run showed its window as idle everywhere outside that window, a run cut
// short by its chat closing left no row at all, and a finished run was listed
// twice in its own chat's `tasks` answer.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveRunChat is a conversation in a real session folder under a real project
// bucket, working in a committed repository, with the engine double seated.
func liveRunChat(t *testing.T, double *beltRunDouble) (*Agent, Place, string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := beltRunCommittedRepo(t)
	bucket := filepath.Join(t.TempDir(), "projects", "-work-repo")
	place := Place{Dir: filepath.Join(bucket, "0123456789abcdef"), Workspace: repo}
	if err := os.MkdirAll(place.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
		config.SessionFile = place.Transcript()
	})
	return agent, place, repo
}

// indexRowFor is the project record's last word on one run, or nil. The record
// reads newest first, so the first match is the latest row.
func indexRowFor(agent *Agent, id string) *TaskIndexEntry {
	for _, entry := range ReadTaskIndex(agent.config.taskIndexFile()) {
		if entry.ID == id {
			entry := entry
			return &entry
		}
	}
	return nil
}

// A LIVE RUN IS WORK OUT, SEEN FROM EVERY OTHER WINDOW. The window running it
// says so in its presence file and the project's record carries a running row
// from the run's first breath, so home's and the sessions page's rollup count it
// as running and another window's `<elsewhere>` lists it — rather than drawing a
// window with a run in flight as idle.
func TestALiveBeltRunInAnotherWindowCountsAsRunning(t *testing.T) {
	double := newBeltRunDouble("the change is made")
	agent, place, repo := liveRunChat(t, double)
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: repo, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	t.Cleanup(func() { endBeltRun(t, agent, double) })

	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(place.Transcript()); err != nil {
		if err := os.WriteFile(place.Transcript(), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(agent.presenceSnapshot(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(place.Dir, presenceName), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	bucket := filepath.Dir(place.Dir)
	world := readWorld(filepath.Dir(bucket), time.Now())
	var found *SessionRow
	for _, project := range world.Projects {
		for i := range project.Sessions {
			if project.Sessions[i].ID == place.ID() {
				found = &project.Sessions[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("the window running the run is not on home at all: %+v", world.Projects)
	}
	if found.Tasks.Running != 1 {
		t.Fatalf("home counts %d running in the window with a live run, want 1 (rollup %+v)", found.Tasks.Running, found.Tasks)
	}
	away := ReadElsewhere(bucket, time.Now(), "another-window").Tasks()
	seen := false
	for _, task := range away {
		if task.Task.Title == "make the change" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("another window does not see the live run: %+v", away)
	}
}

// ONE RUN, ONE ROW, in its own chat's `tasks` answer. The run's own plan rows
// lead that answer, and the project's record now has a row for the same run;
// listing both named one piece of work twice.
func TestAFinishedRunIsListedOnceInItsOwnTasksAnswer(t *testing.T) {
	double := newBeltRunDouble("the change is made")
	agent, _, repo := liveRunChat(t, double)
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: repo, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	endBeltRun(t, agent, double)

	answer, failed, err := agent.tasksTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil || failed {
		t.Fatalf("the tasks tool failed: %v %q", err, answer)
	}
	if got := strings.Count(answer, "make the change"); got != 1 {
		t.Fatalf("the finished run is listed %d times, want once:\n%s", got, answer)
	}
}

// A RUN WHOSE CHAT CLOSED UNDER IT LEAVES AN INTERRUPTED ROW, naming the files
// its workers had touched by then — and when it is carried on and finishes, the
// row that closes it supersedes that one.
func TestARunCutShortByItsChatClosingLeavesAnInterruptedRow(t *testing.T) {
	first := newBeltRunDouble("never finishes")
	first.honoursStop = true
	first.early = func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "seed.txt"), []byte("changed by the run\n"), 0o644); err != nil {
			t.Errorf("worker write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "early.txt"), []byte("a new file\n"), 0o644); err != nil {
			t.Errorf("worker write: %v", err)
		}
	}
	agent, place, repo := liveRunChat(t, first)
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: repo, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-first.entered
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	beltRunWaitFor(t, "the run to let go", func() bool {
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		return agent.beltRun == nil
	})

	row := indexRowFor(agent, "71")
	if row == nil {
		t.Fatal("a run whose chat closed under it left no row in the project's record")
	}
	if row.Status != string(TaskInterrupted) {
		t.Fatalf("the cut-short run's row says %q, want %q", row.Status, TaskInterrupted)
	}
	got := strings.Join(row.Files, ",")
	if !strings.Contains(got, "seed.txt") || !strings.Contains(got, "early.txt") {
		t.Fatalf("the interrupted row names %q, want the files touched so far", got)
	}

	// CARRIED ON AND FINISHED, the closing row supersedes the interrupted one.
	// The next life holds the run's row the way a conversation read back from
	// disk holds it: interrupted, with the copy it was written down with.
	kept, found := runRowOf(agent.graph(), 71)
	if !found || kept.Copy == nil {
		t.Fatalf("the closed conversation kept no copy for its run: %+v", kept)
	}
	second := newBeltRunDouble("carried on and done")
	second.real = true
	registerBeltRunEngine(t, second)
	again, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
		config.SessionFile = place.Transcript()
	})
	keepInterruptedRun(t, again, again.graph(), 71, "make the change", kept.Copy)
	if _, err := again.ContinueRun(context.Background(), 71); err != nil {
		t.Fatalf("carrying the run on: %v", err)
	}
	<-second.entered
	endBeltRun(t, again, second)
	row = indexRowFor(again, "71")
	if row == nil || row.Status != string(TaskDone) {
		t.Fatalf("the carried-on run's last row is %+v, want done", row)
	}
	got = strings.Join(row.Files, ",")
	if !strings.Contains(got, "seed.txt") || !strings.Contains(got, "early.txt") {
		t.Fatalf("the carried-on run's row names %q, want every file the run touched", got)
	}
}
