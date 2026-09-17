package session

// The person's hard steering of a run's plan, read back through the same door
// the surfaces read: every fixture seeds the store through its own API and
// writes nothing but the store, so a verb's effect is proved by the read that
// follows it. Two chat tags are armed over one store, which is the case the
// boundary exists for — a task is reachable only from the conversation that
// spawned it.

import (
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
		if row := planRowByID(t, rows, id); row.Status != "cancelled" {
			t.Fatalf("a cancelled task reads %q, want %q", row.Status, "cancelled")
		}
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

// A store refusal travels back as the store wrote it: the root is the
// harness's own, and a task that already ended cannot be cancelled.
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

	if err := agent.PlanCancel("t-root"); err == nil || err.Error() != "the harness owns the root task" {
		t.Fatalf("PlanCancel on the root = %v, want the store's own sentence", err)
	}
	if err := agent.PlanCancel("t-done-one"); err == nil || err.Error() != `task "done-one" is already terminal` {
		t.Fatalf("PlanCancel on a done task = %v, want the store's terminal refusal", err)
	}
}
