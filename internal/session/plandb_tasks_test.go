package session

// The chat's reading of its plan: the rows PlanTasks draws and the page
// PlanTaskPage opens. Every fixture seeds the store through its own API — the
// same door the wire uses — and writes trajectories the way the run engine
// records them; no model is called.

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
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

// A TASK WHOSE ENDING CARRIES A PERSON'S STOP SAYS SO ON ITS ROW, and so does
// everything that stop took down with it. The store ends what is under a
// cancelled task in the same write, at the same instant and under its own
// reason; a row reads as stopped when its own reason is the stop's, or when it
// was cancelled in the very instant an ancestor was stopped. A task cancelled
// for any other reason, at any other time, is not.
func TestAStopMarksTheTaskAndWhatItTookDownAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha"},
		plandb.TaskSpec{ID: "alpha-part", Title: "Alpha's part", ParentID: "alpha"},
		plandb.TaskSpec{ID: "beta", Title: "Beta"},
		plandb.TaskSpec{ID: "beta-part", Title: "Beta's part", ParentID: "beta"},
		plandb.TaskSpec{ID: "gamma", Title: "Gamma"},
	)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel("alpha", stopBecause(taskStoppedWord, "wrong folder")); err != nil {
		t.Fatalf("stop alpha: %v", err)
	}
	if _, err := store.Cancel("beta", "the plan no longer needs it"); err != nil {
		t.Fatalf("cancel beta: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	want := map[string]bool{"t-alpha": true, "t-alpha-part": true, "t-beta": false, "t-beta-part": false, "t-gamma": false}
	seen := 0
	for _, row := range agent.PlanTasks() {
		stopped, named := want[row.ID]
		if !named {
			continue
		}
		seen++
		if row.Stopped != stopped {
			t.Errorf("%s: Stopped = %v, want %v (status %s)", row.ID, row.Stopped, stopped, row.Status)
		}
	}
	if seen != len(want) {
		t.Fatalf("read %d of the %d rows the test names", seen, len(want))
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
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{
		ID: "alpha", Title: "Alpha", Description: "the work order",
		Checks: []string{"go test ./internal/tui3", "go vet ./internal/session"},
	})

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
	if !reflect.DeepEqual(page.Checks, []string{"go test ./internal/tui3", "go vet ./internal/session"}) {
		t.Fatalf("page checks = %#v", page.Checks)
	}
	if page.Folder != agent.config.Workspace {
		t.Fatalf("page folder = %q, want workspace %q", page.Folder, agent.config.Workspace)
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

// A run root carries progress over every task below it, not its own runtime
// status. Ready and dependency-held work are both queued; claimed and running
// work are both running. The figures come from one deterministic store read.
func TestPlanTaskRootCarriesSubtreeProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "done", Title: "Done"},
		plandb.TaskSpec{ID: "running", Title: "Running"},
		plandb.TaskSpec{ID: "ready", Title: "Ready"},
		plandb.TaskSpec{ID: "failed", Title: "Failed"},
		plandb.TaskSpec{ID: "held", Title: "Held", ParentID: "ready", Dependencies: []plandb.Dependency{{TaskID: "running", Kind: plandb.DepBlocks}}},
	)

	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to move tasks: %v", err)
	}
	if _, err := store.Claim("done", "worker-done"); err != nil {
		t.Fatalf("claim done task: %v", err)
	}
	if _, err := store.Done("done", "worker-done", "done", nil, nil); err != nil {
		t.Fatalf("finish done task: %v", err)
	}
	if _, err := store.Claim("running", "worker-running"); err != nil {
		t.Fatalf("claim running task: %v", err)
	}
	if _, err := store.Claim("failed", "worker-failed"); err != nil {
		t.Fatalf("claim failed task: %v", err)
	}
	if _, err := store.Fail("failed", "worker-failed", "boom"); err != nil {
		t.Fatalf("fail task: %v", err)
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	root := planRowByID(t, agent.PlanTasks(), "t-"+planRootID)
	if root.Done != 1 || root.Running != 1 || root.Queued != 2 || root.Failed != 1 || root.Total != 5 {
		t.Fatalf("root progress = done %d, running %d, queued %d, failed %d, total %d; want 1, 1, 2, 1, 5", root.Done, root.Running, root.Queued, root.Failed, root.Total)
	}
}

// A REOPENED CONVERSATION FINDS THE RUN IT LEFT. The plan was armed only by the
// `/task` that seeded it, in the process that seeded it, so closing the terminal
// and reopening the conversation drew a rail with no run on it while the store
// sat in the session folder with every row. The store is the memory: a
// conversation under the belt whose folder holds a plan store reads it, with no
// `/task` typed in this process.
func TestPlanTasksFindsTheStoreAReopenedConversationLeft(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make the session folder: %v", err)
	}
	seedPlanStore(t, filepath.Join(dir, planStoreFilename), filepath.Base(dir),
		plandb.TaskSpec{ID: "alpha", Title: "Alpha"},
	)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	rows := agent.PlanTasks()
	if len(rows) != 2 || rows[1].ID != "t-alpha" {
		t.Fatalf("PlanTasks in a reopened conversation = %v, want the root and t-alpha", rowIDs(rows))
	}
}

// THE RUN'S ROOT IS WHAT THE STORE SAYS IT IS. A run the conversation opens is
// rooted at the task's own number, never at the word `root`, and its row must
// carry the run's progress all the same: that row is where the dot row draws.
func TestPlanTaskRootCarriesProgressWhenTheRootIsATaskNumber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	store, err := plandb.Open(path, "p", "7", "the run", "the brief", "chat-a")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}); err != nil {
		t.Fatalf("add the run's tasks: %v", err)
	}
	if _, err := store.Claim("a", "worker-a"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.Done("a", "worker-a", "done", nil, nil); err != nil {
		t.Fatalf("finish: %v", err)
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	root := planRowByID(t, agent.PlanTasks(), "t-7")
	if root.Total != 2 || root.Done != 1 {
		t.Fatalf("a numbered root's progress = %d of %d, want 1 of 2", root.Done, root.Total)
	}
}

func TestPlanStepDisplayFactsKeepRecordedCommand(t *testing.T) {
	const copy = "/home/santosh/src/doe/peer/c319/v3/projects/p/r/trees/1"
	tests := []struct {
		command string
		record  []int
		prefix  []int
	}{
		// A pipeline is one command: the record's listing and the pager it is
		// read through go together.
		{"ls; ls *.go 2>/dev/null; plandb task overview 2>/dev/null | head -30", []int{2, 3}, nil},
		// Work piped into something is never the record's, whatever it is piped to.
		{"go test ./... | plandb note t-1 -", nil, nil},
		// A part inside a substitution or a group is never marked: the line
		// around a hole in one is not a command anybody ran.
		{"echo $(plandb task overview)", nil, nil},
		{"(cd x; make) && plandb done t-1", nil, nil},
		{"(cd " + copy + " && make)", nil, nil},
		// An ended run's copy has been given back; a folder directly under the
		// conversation's folder of copies is still a run's copy.
		{"cd /home/santosh/src/doe/peer/c319/v3/projects/p/r/trees/7 && go vet ./...", nil, []int{0}},
		// Further down is somewhere the work went, and that is the work.
		{"cd /home/santosh/src/doe/peer/c319/v3/projects/p/r/trees/7/internal && go vet ./...", nil, nil},
		{"cat calc.go go.mod notes.txt", nil, nil},
		{"plandb done t-1 --agent 1 --result 'Added Mul and Div in muldiv.go …'", []int{0}, nil},
		{"cd " + copy + " && ls && cat muldiv.go && go vet ./... && go test -count=1 ./...", nil, []int{0}},
		{"cd " + copy + " && plandb done t-1 --agent 1 --result 'Mul and Div in …'", []int{1}, []int{0}},
	}
	for _, test := range tests {
		step := PlanStep{Command: test.command}
		got := planStepDisplayFacts(step, planRunCopies{live: copy, root: filepath.Dir(copy)}, "plandb")
		if got.Command != test.command {
			t.Fatalf("recorded command changed:\n got %q\nwant %q", got.Command, test.command)
		}
		var record, prefix []int
		for i, part := range got.Parts {
			if part.RecordAddressed {
				record = append(record, i)
			}
			if part.RunCopyPrefix {
				prefix = append(prefix, i)
			}
		}
		if !reflect.DeepEqual(record, test.record) || !reflect.DeepEqual(prefix, test.prefix) {
			t.Errorf("facts for %q: record=%v prefix=%v parts=%#v", test.command, record, prefix, got.Parts)
		}
	}
}

// A belt run owns a copy outside the conversation workspace. Display facts come
// from that live run copy, while Folder keeps naming the landed conversation
// workspace: the two properties are deliberately independent.
func TestPlanStepDisplayFactsUseTheBeltRunWorkspaceAndKeepFolder(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
	})
	id, _, _, err := agent.StartTask(context.Background(), "show the run copy", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	double.mu.Lock()
	runCopy := double.spec.Workspace
	double.mu.Unlock()
	rootID := strconv.FormatUint(id, 10)
	other := filepath.Join(t.TempDir(), "trees", "2")
	writePlanTrajectory(t, dir, rootID,
		`{"kind":"step","step":1,"command":"cd `+runCopy+` && echo work"}`,
		`{"kind":"step","step":2,"command":"cd `+other+` && echo elsewhere"}`,
	)

	page, ok := agent.PlanTaskPage(planStoreID(rootID))
	if !ok {
		t.Fatal("the belt run task answered no page")
	}
	if page.Folder != agent.config.Workspace {
		t.Fatalf("page folder = %q, want landed workspace %q", page.Folder, agent.config.Workspace)
	}
	if len(page.Steps) != 2 || len(page.Steps[0].Parts) == 0 || !page.Steps[0].Parts[0].RunCopyPrefix {
		t.Fatalf("belt workspace was not marked as the run-copy prefix: %#v", page.Steps)
	}
	if len(page.Steps[1].Parts) == 0 || page.Steps[1].Parts[0].RunCopyPrefix {
		t.Fatalf("another folder was marked as the run-copy prefix: %#v", page.Steps[1].Parts)
	}
	endBeltRun(t, agent, double)
}

func TestPlanStepDisplayFactsDoNotRewriteTrajectory(t *testing.T) {
	dir := t.TempDir()
	command := "cd /run/trees/1 && plandb done t-1 --agent 1 --result 'done'"
	writePlanTrajectory(t, dir, "alpha", `{"kind":"step","step":5,"command":"cd /run/trees/1 && plandb done t-1 --agent 1 --result 'done'","observation":"done t-1"}`)
	path := planTrajectoryPath(dir, "alpha")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	steps := planStepDisplayFactsForPage(planTrajectory(dir, "alpha"), planRunCopies{live: "/run/trees/1"})
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("display facts rewrote the trajectory")
	}
	if len(steps) != 1 || steps[0].Command != command {
		t.Fatalf("PlanStep.Command = %q, want %q", steps[0].Command, command)
	}
}
