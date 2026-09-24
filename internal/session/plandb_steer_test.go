package session

// The person's hard steering of a run's plan, read back through the same door
// the surfaces read: every fixture seeds the store through its own API and
// writes nothing but the store, so a verb's effect is proved by the read that
// follows it. Two chat tags are armed over one store, which is the case the
// boundary exists for — a task is reachable only from the conversation that
// spawned it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// Each steering verb changes the store, and the change is read back through
// PlanTaskPage and PlanTasks: the note carries the person as its author, a held
// task wears the hold word the surface draws `your call` from, an amendment
// leads the description, a set priority lands on the task, and a cancelled task
// wears the store's own word.
func TestPlanSteerWritesReachTheStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha", Description: "the work order"},
		plandb.TaskSpec{ID: "beta", Title: "Beta"},
		plandb.TaskSpec{ID: "gamma", Title: "Gamma"},
		plandb.TaskSpec{ID: "gamma-kid", Title: "Gamma kid", ParentID: "gamma"},
	)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	// A note in the person's own voice: it appears on the page with the person
	// as its author and no worker name.
	if err := agent.PlanNote("t-alpha", "please also handle the empty case"); err != nil {
		t.Fatalf("PlanNote: %v", err)
	}
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the page for the noted task was not answered")
	}
	last := page.Notes[len(page.Notes)-1]
	if !last.Person || last.Author != "" || last.Body != "please also handle the empty case" {
		t.Fatalf("the person note = %#v, want the person as author", last)
	}

	// A hold: the task reads the hold word, and resume puts it back on the rung
	// it held.
	if err := agent.PlanPause("t-beta"); err != nil {
		t.Fatalf("PlanPause: %v", err)
	}
	if row := planRowByID(t, agent.PlanTasks(), "t-beta"); row.Status != "paused" {
		t.Fatalf("a held task reads %q, want %q", row.Status, "paused")
	}
	if err := agent.PlanResume("t-beta"); err != nil {
		t.Fatalf("PlanResume: %v", err)
	}
	if row := planRowByID(t, agent.PlanTasks(), "t-beta"); row.Status == "paused" {
		t.Fatalf("a resumed task still reads %q", row.Status)
	}

	// An amendment lands at the front of the work order.
	if err := agent.PlanAmend("t-gamma", "NOTE: read the spec first."); err != nil {
		t.Fatalf("PlanAmend: %v", err)
	}
	if page, _ = agent.PlanTaskPage("t-gamma"); !strings.HasPrefix(page.Description, "NOTE: read the spec first.") {
		t.Fatalf("the amendment did not lead the description: %q", page.Description)
	}

	// A priority set goes through the store's revision verb. The plan row
	// carries no priority, so it is read back off the store itself.
	if err := agent.PlanPriority("t-gamma", 7); err != nil {
		t.Fatalf("PlanPriority: %v", err)
	}
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to read the priority back: %v", err)
	}
	if task := store.Task("gamma"); task == nil || task.Priority != 7 {
		t.Fatalf("the priority was not set on the task: %#v", task)
	}
	_ = store.Close()

	// A cancel ends the task and cascades to its subtree, and every row wears
	// the store's own word for it.
	if err := agent.PlanCancel("t-gamma"); err != nil {
		t.Fatalf("PlanCancel: %v", err)
	}
	rows := agent.PlanTasks()
	for _, id := range []string{"t-gamma", "t-gamma-kid"} {
		if row := planRowByID(t, rows, id); row.Status != "cancelled" || !row.Stopped {
			t.Fatalf("a stopped task reads %#v, want cancelled with Stopped true", row)
		}
	}
	if err := agent.PlanCancel("t-gamma"); err != nil {
		t.Fatalf("already stopped: %v", err)
	}
}

// Every steering verb answers the same two refusals: a task another
// conversation spawned is not reachable from here, and an id this
// conversation's plan does not hold is not either.
func TestPlanSteerRefusesAnotherChatAndAnUnknownID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})

	// This conversation seeded nothing: chat-a's task is another conversation's
	// to it, and its own part of the plan holds no task at all.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-b")

	verbs := []struct {
		name string
		call func(id string) error
	}{
		{"PlanNote", func(id string) error { return agent.PlanNote(id, "hello") }},
		{"PlanPause", func(id string) error { return agent.PlanPause(id) }},
		{"PlanResume", func(id string) error { return agent.PlanResume(id) }},
		{"PlanCancel", func(id string) error { return agent.PlanCancel(id) }},
		{"PlanAmend", func(id string) error { return agent.PlanAmend(id, "more") }},
		{"PlanPriority", func(id string) error { return agent.PlanPriority(id, 3) }},
	}
	for _, verb := range verbs {
		if err := verb.call("t-alpha"); err == nil || err.Error() != "that task belongs to another conversation" {
			t.Fatalf("%s on another chat's task = %v, want the other-conversation refusal", verb.name, err)
		}
		if err := verb.call("t-nope"); err == nil || err.Error() != "no task t-nope in this conversation" {
			t.Fatalf("%s on an unknown id = %v, want the unknown-id refusal naming the id", verb.name, err)
		}
	}
}

// A store refusal travels back as the store wrote it: a task that already ended
// cannot be cancelled. THE RUN'S OWN TASK IS NOT A REFUSAL ANY MORE: a person's
// stop on it ends the run (stoprun.go), and a hold on it answers in a person's
// words, because nothing holds a whole run and the store's own sentence for
// that is about who owns what.
func TestPlanSteerAnswersTheStoresOwnRefusal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha"},
		plandb.TaskSpec{ID: "done-one", Title: "Done one"},
	)
	// A finished task, completed through the store's own verbs: the person's
	// cancel of it is the terminal refusal.
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to finish a task: %v", err)
	}
	if _, err := store.Claim("done-one", "done-one"); err != nil {
		t.Fatalf("claim the task: %v", err)
	}
	if _, err := store.Done("done-one", "done-one", "the work holds", nil, nil); err != nil {
		t.Fatalf("complete the task: %v", err)
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")

	if err := agent.PlanCancel("t-done-one"); err == nil || err.Error() != `task "done-one" is already terminal` {
		t.Fatalf("PlanCancel on a done task = %v, want the store's terminal refusal", err)
	}
	for name, hold := range map[string]func(string) error{"PlanPause": agent.PlanPause, "PlanResume": agent.PlanResume} {
		err := hold("t-root")
		if err == nil || err != errPlanRunNotHeld {
			t.Fatalf("%s on the run's own task = %v, want the sentence that a whole run is not held", name, err)
		}
		if word := carriesMachinery(err.Error()); word != "" {
			t.Fatalf("%s on the run's own task says %q to a person: %v", name, word, err)
		}
	}
	if err := agent.PlanCancel("t-root"); err != nil {
		t.Fatalf("PlanCancel on the run's own task = %v, want the run ended", err)
	}
	after, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if root := after.Task(after.RootID()); root == nil || root.Status != plandb.StatusCancelled {
		t.Fatalf("the run's own task after a person's stop = %+v, want cancelled", root)
	}
	if alpha := after.Task("alpha"); alpha == nil || alpha.Status != plandb.StatusCancelled {
		t.Fatalf("open work under a stopped run = %+v, want cancelled", alpha)
	}
}

// Every steering verb recognizes a task kept from an earlier run, answers one
// sentence that the run has ended, and leaves the ended run exactly as it was.
func TestPlanSteerRefusesEveryVerbOnAnEndedRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "earlier", Title: "Earlier"})
	ended, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("open earlier run: %v", err)
	}
	if _, err := ended.Claim("earlier", "worker"); err != nil {
		t.Fatalf("claim earlier task: %v", err)
	}
	if _, err := ended.Done("earlier", "worker", "earlier work", nil, nil); err != nil {
		t.Fatalf("finish earlier task: %v", err)
	}
	if err := ended.CompleteRoot("earlier result"); err != nil {
		t.Fatalf("end earlier run: %v", err)
	}
	_ = ended.Close()
	archive := path + ".1"
	if err := os.Rename(path, archive); err != nil {
		t.Fatalf("archive earlier run: %v", err)
	}
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "live", Title: "Live"})

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	before, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive before steering: %v", err)
	}
	verbs := []struct {
		name string
		call func() error
	}{
		{"PlanNote", func() error { return agent.PlanNote("t-earlier", "hello") }},
		{"PlanPause", func() error { return agent.PlanPause("t-earlier") }},
		{"PlanResume", func() error { return agent.PlanResume("t-earlier") }},
		{"PlanCancel", func() error { return agent.PlanCancel("t-earlier") }},
		{"PlanAmend", func() error { return agent.PlanAmend("t-earlier", "more") }},
		{"PlanPriority", func() error { return agent.PlanPriority("t-earlier", 3) }},
	}
	for _, verb := range verbs {
		if err := verb.call(); err == nil || err.Error() != "that task's run has ended" {
			t.Fatalf("%s on an ended run = %v, want the ended-run sentence", verb.name, err)
		}
		after, readErr := os.ReadFile(archive)
		if readErr != nil {
			t.Fatalf("read archive after %s: %v", verb.name, readErr)
		}
		if string(after) != string(before) {
			t.Fatalf("%s changed the ended run", verb.name)
		}
	}

	if err := agent.PlanNote("t-live", "still writable"); err != nil {
		t.Fatalf("note on live run: %v", err)
	}
}

// A READ THAT FINDS NO STORE GIVES THE PLAN'S LOCK BACK. A plan is armed before
// its store exists, and a side list's beat can read in that moment. The read
// that opens every run's store took the plan's lock and handed back the closer
// that releases it, and both readers returned on "no stores" without calling
// it: the lock was never released, and every later read, the run's own driver
// and the conversation's close then waited on it for good. Seen on the real
// binary: an engine an hour old that would not end.
func TestAPlanReadThatFindsNoStoreReleasesThePlansLock(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, filepath.Join(t.TempDir(), "no-such-folder", planStoreFilename), "chat-a")
	plan := agent.graph().planIfArmed()
	if plan == nil {
		t.Fatal("the fixture did not arm a plan")
	}
	for _, reading := range []struct {
		name string
		read func()
	}{
		{"the listing", func() { _ = agent.PlanTasks() }},
		{"one task", func() { _, _ = agent.PlanTaskPage("1") }},
		{"a note", func() { _ = agent.PlanNote("1", "hello") }},
		{"every handle", func() { _, _, done := agent.openPlanReadHandles(); done() }},
	} {
		reading.read()
		if !plan.mu.TryLock() {
			// Given back here so the fixture's own close can end: a leaked lock
			// hangs that close exactly as it hung the engine's.
			plan.mu.Unlock()
			t.Fatalf("%s found no store and kept the plan's lock", reading.name)
		}
		plan.mu.Unlock()
	}
}
