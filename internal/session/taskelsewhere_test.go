package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── THE OTHER WINDOWS ───────────────────────────────────────────────────────
//
// A project is a directory and a directory may have three aforge windows open on
// it. These are the whole of what one window is allowed to say about the others:
// what they have out right now, and whether a row of the shared index that says
// `running` is a claim anybody is still standing behind.

// writeWindow puts one window's presence on disk, exactly as a live session's
// heartbeat writes it, and names it in meta.json the way a session that settled
// on a title does. age is how long ago the window last said anything.
func writeWindow(t *testing.T, bucket, id, title string, age time.Duration, tasks ...PresenceTask) string {
	t.Helper()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("window folder: %v", err)
	}
	raw, err := json.Marshal(SessionPresence{
		Schema:       presenceSchema,
		SessionID:    id,
		Workspace:    "/work/aforge",
		PID:          4242,
		UpdatedAt:    time.Now().Add(-age),
		State:        PresenceWorking,
		RunningTasks: tasks,
	})
	if err != nil {
		t.Fatalf("marshal presence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, presenceName), raw, 0o600); err != nil {
		t.Fatalf("write presence: %v", err)
	}
	if title != "" {
		meta, err := json.Marshal(Meta{ID: id, Title: title})
		if err != nil {
			t.Fatalf("marshal meta: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, placeMeta), meta, 0o600); err != nil {
			t.Fatalf("write meta: %v", err)
		}
	}
	return dir
}

// indexRow is one row of the project's index as another window wrote it.
func indexRow(id, session, title, status string) TaskIndexEntry {
	return TaskIndexEntry{
		ID:        id,
		Name:      TaskSlug(title),
		Label:     title,
		Title:     title,
		Status:    status,
		SessionID: session,
	}
}

// A WINDOW'S OWN LIST OF WHAT IT HAS OUT IS THE ONLY PLACE ORDINARY CROSS-WINDOW
// WORK CAN BE READ FROM. The project's index gets nothing until the work lands,
// so a reader that only had the file would show a busy directory as an idle one.
func TestElsewhereCarriesTheOtherWindowsRunningWork(t *testing.T) {
	bucket := t.TempDir()
	writeWindow(t, bucket, "window-one", "Port the parser", time.Second,
		PresenceTask{ID: "3", Title: "Port the parser", State: string(TaskRunning)})
	writeWindow(t, bucket, "window-two", "", time.Second,
		PresenceTask{ID: "1", Title: "Sweep the call sites", State: string(TaskQueued)})

	away := ReadElsewhere(bucket, time.Now(), "")
	if !away.Any() {
		t.Fatal("two open windows and the reading says the project is quiet")
	}
	tasks := away.Tasks()
	if len(tasks) != 2 {
		t.Fatalf("the reading carries %d pieces of work, want 2: %+v", len(tasks), tasks)
	}
	byTitle := map[string]ElsewhereTask{}
	for _, task := range tasks {
		byTitle[task.Task.Title] = task
	}
	// A WINDOW THAT SETTLED ON A TITLE IS CALLED BY IT, and one that never did has
	// no name at all rather than a string of hex standing in for one.
	if got := byTitle["Port the parser"].Session; got != "Port the parser" {
		t.Fatalf("the first window is called %q", got)
	}
	if got := byTitle["Sweep the call sites"].Session; got != "" {
		t.Fatalf("an unnamed window was given the name %q", got)
	}
	if got := byTitle["Sweep the call sites"].SessionID; got != "window-two" {
		t.Fatalf("the row cites window %q", got)
	}
}

// THE CALLER'S OWN WINDOW IS NEVER ONE OF THE OTHERS. A surface drawing "what
// else is running" that drew itself would be reporting its own work twice.
func TestElsewhereLeavesTheAskingWindowOut(t *testing.T) {
	bucket := t.TempDir()
	writeWindow(t, bucket, "mine", "", time.Second,
		PresenceTask{ID: "1", Title: "My own task", State: string(TaskRunning)})
	writeWindow(t, bucket, "theirs", "", time.Second,
		PresenceTask{ID: "1", Title: "Their task", State: string(TaskRunning)})

	away := ReadElsewhere(bucket, time.Now(), "mine")
	tasks := away.Tasks()
	if len(tasks) != 1 || tasks[0].Task.Title != "Their task" {
		t.Fatalf("the reading is %+v, want only the other window's work", tasks)
	}
}

// A ROW THAT SAYS `running` IS BELIEVED ONLY WHILE THE WINDOW THAT WROTE IT IS
// STILL SAYING SO. This is world.go's law asked from inside a session rather
// than from the home page, and the two ask it through the same join.
func TestElsewhereJudgesARowThatClaimsToBeRunning(t *testing.T) {
	bucket := t.TempDir()
	writeWindow(t, bucket, "theirs", "", time.Second,
		PresenceTask{ID: "3", Title: "Port the parser", State: string(TaskRunning)})
	away := ReadElsewhere(bucket, time.Now(), "")

	held := indexRow("3", "theirs", "Port the parser", string(TaskRunning))
	if !away.Runs(held) {
		t.Fatal("the window says it has node 3 out and the reading calls the row a record")
	}
	// A row the window does NOT name is work that stopped — finished, abandoned,
	// or never resumed — whatever the file's oldest word for it was.
	dropped := indexRow("9", "theirs", "Sweep the call sites", string(TaskRunning))
	if away.Runs(dropped) {
		t.Fatal("a row no window names was called running")
	}
	// A row from a window that is not open at all is the whole reason the rule
	// exists: nothing refreshed it, so nothing is behind it.
	gone := indexRow("3", "a-window-that-closed", "Port the parser", string(TaskRunning))
	if away.Runs(gone) {
		t.Fatal("a closed window's row was called running")
	}
	// And a row that already landed is a record however the reading answers.
	landed := indexRow("3", "theirs", "Port the parser", string(TaskDone))
	if away.Runs(landed) {
		t.Fatal("a landed row was called running")
	}
}

// A WINDOW THAT STOPPED SAYING ANYTHING IS A WINDOW THAT IS GONE, and every
// `running` it left behind goes with it. Staleness is the whole cleanup — there
// is no daemon and no liveness check on a pid.
func TestElsewhereDropsAWindowThatWentQuiet(t *testing.T) {
	bucket := t.TempDir()
	writeWindow(t, bucket, "theirs", "", presenceWindow+time.Second,
		PresenceTask{ID: "3", Title: "Port the parser", State: string(TaskRunning)})

	away := ReadElsewhere(bucket, time.Now(), "")
	if away.Any() {
		t.Fatal("a window that has not spoken in a minute is still on the reading")
	}
	if len(away.Tasks()) != 0 {
		t.Fatalf("a gone window's work is still drawn: %+v", away.Tasks())
	}
	if away.Runs(indexRow("3", "theirs", "Port the parser", string(TaskRunning))) {
		t.Fatal("a gone window's row is still called running")
	}
}

// THE HAND-BUILT READING KEEPS THE SAME RULE. [NewElsewhere] exists so a caller
// that already has the rows — or a surface's test that never started a second
// process — can build one, and it must not be a way round the freshness window.
func TestNewElsewhereRefusesAClaimNobodyRefreshed(t *testing.T) {
	now := time.Now()
	fresh := SessionPresence{
		Schema: presenceSchema, SessionID: "theirs", UpdatedAt: now, State: PresenceWorking,
		RunningTasks: []PresenceTask{{ID: "3", Title: "Port the parser", State: string(TaskRunning)}},
	}
	stale := SessionPresence{
		Schema: presenceSchema, SessionID: "long-gone", UpdatedAt: now.Add(-presenceWindow - time.Minute),
		State:        PresenceWorking,
		RunningTasks: []PresenceTask{{ID: "1", Title: "Something older", State: string(TaskRunning)}},
	}
	away := NewElsewhere(now, map[string]string{"theirs": "the other window"}, fresh, stale)
	tasks := away.Tasks()
	if len(tasks) != 1 || tasks[0].Task.Title != "Port the parser" {
		t.Fatalf("the reading is %+v, want only the window that is still speaking", tasks)
	}
	if tasks[0].Session != "the other window" {
		t.Fatalf("the window is called %q", tasks[0].Session)
	}
	if away.Runs(indexRow("1", "long-gone", "Something older", string(TaskRunning))) {
		t.Fatal("a stale row smuggled in through the hand-built door was called running")
	}
}

// A SESSION WITH NO FOLDER HAS NO OTHER WINDOWS, because it has no bucket to
// look in and no id to leave out of the answer.
func TestElsewhereIsEmptyForASessionWithNoFolder(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if away := agent.Elsewhere(); away.Any() {
		t.Fatal("a memory-only conversation found other windows on a project it is not in")
	}
}
