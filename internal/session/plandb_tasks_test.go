package session

// The chat's reading of its plan: the rows PlanTasks draws and the page
// PlanTaskPage opens. Every fixture seeds the store through its own API — the
// same door the wire uses — and writes trajectories the way the run engine
// records them; no model is called.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// armPlanStore points a test agent's graph at a store path and chat tag the way
// [planSeed] leaves it armed: the plan is set under its own gate, and the
// experiment's switch is on because the switch is what makes a plan reachable
// at all.
func armPlanStore(t *testing.T, agent *Agent, path, chat string) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	g := agent.graph()
	g.planMu.Lock()
	g.plan = &planState{path: path, chat: chat}
	g.planMu.Unlock()
}

// seedPlanStore opens a fresh plan store with one chat tag and seeds it with
// the tasks named, closing the seeding handle so the run's own reads open the
// file afresh.
func seedPlanStore(t *testing.T, path, chat string, specs ...plandb.TaskSpec) {
	t.Helper()
	store, err := plandb.Open(path, "the run", planRootID, "The run", "drive the plan", chat)
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	if len(specs) > 0 {
		if _, err := store.AddMany(specs); err != nil {
			t.Fatalf("seed the store: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the seeding handle: %v", err)
	}
}

// writePlanTrajectory writes one task's record beside the store, one line per
// entry, the way the run engine appends it.
func writePlanTrajectory(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	taskDir := plandb.TaskDir(dir, id)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatalf("make the task's record folder: %v", err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(taskDir, planTrajectoryFile), []byte(body), 0o644); err != nil {
		t.Fatalf("write the trajectory: %v", err)
	}
}

// The plan a chat reads is its own. PlanTasks answers the rows the
// conversation's chat tag carries, in the store's admission order, and nothing
// from another chat; a store that holds no rows for this chat answers an empty
// read, not nil. The store keeps one chat per file — a child inherits its
// root's tag — so the two chats here are one store read under two tags, which
// is exactly the narrowing these verbs exist for.
func TestPlanTasksAnswersOnlyThisChatsRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha"},
		plandb.TaskSpec{ID: "beta", Title: "Beta"},
	)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	rows := agent.PlanTasks()
	// The root is the run itself and the two children are its work, all three
	// of this chat; admission order is the order they were made in.
	want := []string{"t-" + planRootID, "t-alpha", "t-beta"}
	if len(rows) != len(want) {
		t.Fatalf("PlanTasks under chat-a = %d rows, want %d (%v)", len(rows), len(want), rowIDs(rows))
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Fatalf("row %d = %q, want %q (%v)", i, rows[i].ID, id, rowIDs(rows))
		}
	}

	// The same store under another chat: the store is there, this chat's part
	// of it is not, and the empty read is a slice rather than nil.
	other, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, other, path, "chat-b")
	if rows := other.PlanTasks(); len(rows) != 0 {
		t.Fatalf("PlanTasks under chat-b = %v, want none", rowIDs(rows))
	} else if rows == nil {
		t.Fatal("a store with no rows for this chat answered nil, want an empty read")
	}
}

// A conversation with no plan answers nil rather than an empty read: the
// emptiness is "there is no store", which a surface must tell apart from "the
// store holds nothing of mine".
func TestPlanTasksIsNilWithNoStore(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	if rows := agent.PlanTasks(); rows != nil {
		t.Fatalf("PlanTasks with no store = %#v, want nil", rows)
	}
}

// A row counts its task's own steps and money. The trajectory's step lines are
// counted and the run's ending line is not; the dollars are the task's spend
// rows summed, which is the figure no store rollup carries.
func TestPlanTaskRowCountsStepsAndSumsSpend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	writePlanTrajectory(t, dir, "alpha",
		`{"kind":"step","step":1,"command":"$ echo one","observation":"one"}`,
		`{"kind":"step","step":2,"command":"$ echo two","observation":"two"}`,
		`{"kind":"step","step":3,"command":"$ echo three","observation":"three"}`,
		`{"kind":"end","steps":3,"result":"done","reason":"turn ended"}`,
	)

	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to charge it: %v", err)
	}
	if err := store.AddSpend("alpha", "test/model", "work", 0.10, 10, 20); err != nil {
		t.Fatalf("charge the task: %v", err)
	}
	if err := store.AddSpend("alpha", "test/model", "work", 0.25, 10, 20); err != nil {
		t.Fatalf("charge the task: %v", err)
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	row := planRowByID(t, agent.PlanTasks(), "t-alpha")
	if row.Steps != 3 {
		t.Fatalf("row steps = %d, want 3", row.Steps)
	}
	if math.Abs(row.USD-0.35) > 1e-9 {
		t.Fatalf("row spend = %v, want 0.35", row.USD)
	}
	if want := filepath.Join(plandb.TaskDir(dir, "alpha"), planTrajectoryFile); row.TrajectoryPath != want {
		t.Fatalf("trajectory path = %q, want %q", row.TrajectoryPath, want)
	}
}

// A page is the task's row, its description, its notes in order and its steps;
// a task the chat did not spawn — another chat's, or none at all — answers
// false.
func TestPlanTaskPageDrawsTheTaskAndRefusesAnotherChat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha", Description: "the work order"})

	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to leave notes: %v", err)
	}
	if _, err := store.AddNote("alpha", "worker-1", "a handoff"); err != nil {
		t.Fatalf("leave a worker note: %v", err)
	}
	if _, err := store.AddPersonNote("alpha", "a question"); err != nil {
		t.Fatalf("leave a person note: %v", err)
	}
	_ = store.Close()
	writePlanTrajectory(t, dir, "alpha", `{"kind":"step","step":1,"command":"$ echo one","observation":"one"}`)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the chat's own task answered no page")
	}
	if page.Row.ID != "t-alpha" || page.Description != "the work order" {
		t.Fatalf("page row = %q, description = %q", page.Row.ID, page.Description)
	}
	if len(page.Steps) != 1 || page.Steps[0].Command != "$ echo one" {
		t.Fatalf("page steps = %#v", page.Steps)
	}
	if len(page.Notes) != 2 {
		t.Fatalf("page notes = %d, want 2", len(page.Notes))
	}
	if page.Notes[0].Author != "worker-1" || page.Notes[0].Person || page.Notes[0].Body != "a handoff" || page.Notes[0].At.IsZero() {
		t.Fatalf("worker note = %#v", page.Notes[0])
	}
	if !page.Notes[1].Person || page.Notes[1].Author != "" || page.Notes[1].Body != "a question" {
		t.Fatalf("person note = %#v", page.Notes[1])
	}

	// Another chat reads the same store: the task is there, the page is not.
	other, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, other, path, "chat-b")
	if _, ok := other.PlanTaskPage("t-alpha"); ok {
		t.Fatal("another chat's task answered a page")
	}
	if _, ok := other.PlanTaskPage("t-nowhere"); ok {
		t.Fatal("a task that does not exist answered a page")
	}
}

// A row carries the live step the store holds for its task — the number, the
// command and the moment — and the zero value for a task that is running
// nothing; the page carries it through its row. This is the reading the
// engine publishes on EventToolBegin and clears at every ending.
func TestPlanTaskRowCarriesTheLiveStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha"},
		plandb.TaskSpec{ID: "beta", Title: "Beta"},
	)

	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to publish a live step: %v", err)
	}
	if err := store.SetLive("alpha", 7, "$ git grep -n RateLimit internal/api"); err != nil {
		t.Fatalf("publish the live step: %v", err)
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	rows := agent.PlanTasks()
	alpha := planRowByID(t, rows, "t-alpha")
	if alpha.Live.Step != 7 || alpha.Live.Command != "$ git grep -n RateLimit internal/api" {
		t.Fatalf("live step = %#v, want step 7 with its command", alpha.Live)
	}
	if alpha.Live.Since.IsZero() {
		t.Fatal("the live step's moment is the zero time, want when the command began")
	}
	if alpha.Live.Empty() {
		t.Fatal("a running task's row reads empty")
	}

	// A task the store holds no live row for answers the zero value — the
	// emptiness the surface draws as no line at all.
	if beta := planRowByID(t, rows, "t-beta"); !beta.Live.Empty() {
		t.Fatalf("a task running nothing answers %#v, want the zero value", beta.Live)
	}

	// The page carries the same reading through its own row.
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the chat's own task answered no page")
	}
	if page.Row.Live.Step != 7 || page.Row.Live.Command != alpha.Live.Command {
		t.Fatalf("the page's live step = %#v, want the row's", page.Row.Live)
	}
}

// A task page carries the two run seat models and associates a held question
// with the plan row whose live node raised it. The question keeps the exact
// options the engine already accepts.
func TestPlanTaskPageCarriesSeatModelsAndHeldQuestionFacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Alpha"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.config.RolesSource = tierSettings(map[string]string{
		roles.TierKey(roles.TierWorker):     "test/work",
		roles.TierKey(roles.TierMastermind): "test/plan",
	})
	armPlanStore(t, agent, path, "chat-a")

	const nodeID = uint64(91)
	g := agent.graph()
	g.mu.Lock()
	g.nodes[nodeID] = &TaskNode{id: nodeID, graph: g, spec: taskSpec{title: "Alpha", planID: "alpha"}}
	g.mu.Unlock()
	agent.mu.Lock()
	agent.taskAnswers = map[uint64]*taskQuestion{nodeID: {notice: TaskNotice{ID: nodeID, Title: "Alpha", Summary: "choose now"}}}
	agent.mu.Unlock()

	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the task page was not answered")
	}
	if page.WorkModel != "test/work" || page.PlanModel != "test/plan" {
		t.Fatalf("seat models = work %q, plan %q; want test/work and test/plan", page.WorkModel, page.PlanModel)
	}
	if len(page.Questions) != 1 || page.Questions[0].TaskID != "t-alpha" {
		t.Fatalf("held questions = %#v, want alpha's question", page.Questions)
	}
	if page.Questions[0].Head == "" || len(page.Questions[0].Options) == 0 {
		t.Fatalf("held question lost its words or answer keys: %#v", page.Questions[0])
	}
}

// rowIDs names the rows' ids for a failure message.
func rowIDs(rows []PlanTaskRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// planRowByID answers the row with the id, failing when it is not there.
func planRowByID(t *testing.T, rows []PlanTaskRow, id string) PlanTaskRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("no row %q among %v", id, rowIDs(rows))
	return PlanTaskRow{}
}
