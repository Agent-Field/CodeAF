package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func placementRowFixture(t *testing.T, state session.TaskState, computer string) (*app, session.TaskIndexEntry) {
	t.Helper()
	a := completionFoldApp(t)
	entry := session.TaskIndexEntry{ID: "7", SessionID: a.taskSheetSelfID(), Title: "Create hello.txt", Label: "Create hello.txt", Status: string(state), Computer: computer}
	notice := session.TaskNotice{Computer: computer}
	if computer != "" {
		notice.Remote = &session.TaskRemoteRecord{Executor: session.TaskExecutor{Name: computer}}
	}
	if state == session.TaskDone || state == session.TaskFailed {
		entry.EndedAt = a.now()
		entry.RanElsewhere, notice.RanElsewhere = computer != "", computer != ""
		if notice.Remote != nil {
			notice.Remote.Finished, notice.Remote.Settled = true, true
		}
	}
	a.taskUpdate(update(7, entry.Title, state, notice))
	return a, entry
}

func placementRowReadings(a *app, entry session.TaskIndexEntry) map[string]string {
	item := &tasksItem{entry: entry, runs: true}
	reading := tasksReading{now: a.now()}
	rows := []tasksLine{{kind: tasksLineTask, item: item}, {kind: tasksLineTail, item: item}}
	readings := map[string]string{
		"Background work":    a.completionRows([]workCompletion{{task: 7, title: entry.Title, state: entry.Status}}, 200)[0].text,
		"conversation rail":  strings.Join(a.railUnder(a.tasks[7], 180), "\n"),
		"conversation phase": strings.Join(a.railDoing(a.tasks[7], 180), "\n"),
		"task room":          a.roomStateWord(a.tasks[7]),
		"Tasks":              reading.paint(rows, 0, 200, a.pal, false),
		"compact Tasks":      reading.paint(rows, 1, 70, a.pal, false),
		"task page":          strings.Join(a.taskCardBody(entry, 180), "\n"),
	}
	var preview []string
	for _, row := range a.taskPaneRecord(*item, 100) {
		preview = append(preview, row.text)
	}
	readings["task preview"] = strings.Join(preview, "\n")
	if entry.Status == string(session.TaskRunning) {
		r, watched := watchedPlacementReading(a, entry)
		lines := []tasksLine{{kind: tasksLineTask, item: &watched}, {kind: tasksLineTail, item: &watched}}
		readings["Home watched Tasks"] = r.paint(lines, 0, 180, a.pal, false)
		readings["Home watched compact Tasks"] = r.paint(lines, 1, 70, a.pal, false)
	}
	approved := &taskCard{id: 7, title: entry.Title, verdict: "approved"}
	readings["accepted conversation card"] = strings.Join(a.taskCardRows(approved, 200, false), "\n")
	if at := a.doneEntryFor(7); at >= 0 {
		card := *a.entries[at].done
		card.quiet = false
		readings["conversation landing"] = strings.Join(a.doneRows(&card, 200, false), "\n")
		card.open = true
		readings["open conversation landing"] = strings.Join(a.doneRows(&card, 200, false), "\n")
	}
	return readings
}

// R9.1 requires placement on the actual result door, including failed work and
// restored receipts, even when this window cannot choose task computers.
func TestTheBackgroundWorkRowSaysWhereARemoteTaskRan(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskDone, session.TaskFailed} {
		for _, saved := range []bool{false, true} {
			a, entry := placementRowFixture(t, state, "beta")
			if saved {
				delete(a.tasks, 7)
				a.comp.tasks = []session.TaskIndexEntry{entry}
			}
			a.openWorkfold(1)
			a.touch()
			found := false
			for _, row := range a.visible(a.bodyWidth()) {
				if row.hit == hitWorkCompletion && row.completion.task == 7 {
					found = true
					if text := plain(row.text); strings.Count(text, "ran on beta") != 1 || !strings.Contains(text, "open result") {
						t.Errorf("state %s, saved %v: %s", state, saved, text)
					}
					a.openWorkCompletion(row.completion)
					if a.taskSheet.detail.Computer != "beta" || !a.taskSheet.detail.RanElsewhere {
						t.Errorf("open result dropped the executor: %+v", a.taskSheet.detail)
					}
				}
			}
			if !found {
				t.Fatalf("state %s, saved %v: no Background work result row", state, saved)
			}
		}
	}
}

// R9.1 makes the saved executor authoritative on each list, independently of
// whether this particular window has acquired the pairing capability.
func TestEveryTaskSurfaceKeepsTheRecordedComputerWithoutTheCapability(t *testing.T) {
	for _, state := range []session.TaskState{session.TaskRunning, session.TaskDone} {
		a, entry := placementRowFixture(t, state, "beta")
		want := "running on beta"
		if entry.RanElsewhere {
			want = "ran on beta"
		}
		for surface, text := range placementRowReadings(a, entry) {
			t.Run(string(state)+"/"+surface, func(t *testing.T) {
				if strings.Count(plain(text), want) != 1 {
					t.Fatalf("recorded placement missing or repeated: %s", plain(text))
				}
			})
		}
		t.Run(string(state)+"/saved task page", func(t *testing.T) {
			delete(a.tasks, 7)
			if text := plain(strings.Join(a.taskCardBody(entry, 180), "\n")); !strings.Contains(text, want) {
				t.Fatal(text)
			}
		})
	}
}

// R9.1 treats the index's executor as a fact even when a window still has an
// older live node from before the placement update arrived.
func TestASavedTaskComputerSurvivesAnOlderLiveNode(t *testing.T) {
	a, entry := placementRowFixture(t, session.TaskDone, "")
	a.file = filepath.Join(a.homeRoot, "conversation", "session.jsonl")
	entry.SessionID = a.taskSheetSelfID()
	entry.Computer, entry.RanElsewhere = "beta", true
	a.comp.tasks = []session.TaskIndexEntry{entry}
	found := false
	for _, row := range a.taskSheetOwnRows() {
		if row.ID == entry.ID {
			found = true
			entry = row
		}
	}
	if !found {
		t.Fatal("the saved task disappeared from the actual Tasks reading")
	}
	if text := plain(strings.Join(a.taskCardBody(entry, 180), "\n")); !strings.Contains(text, "ran on beta") {
		t.Fatal(text)
	}
}

// R9.1 includes Home's merge with another window's live presence: presence
// does not carry placement and must preserve it from the saved record.
func TestHomeKeepsARemoteExecutorWhenAnotherWindowOwnsTheTask(t *testing.T) {
	a, entry := placementRowFixture(t, session.TaskRunning, "beta")
	r, item := watchedPlacementReading(a, entry)
	if !item.away {
		t.Fatalf("expected the watched task, got %+v", item)
	}
	lines := []tasksLine{{kind: tasksLineTask, item: &item}, {kind: tasksLineTail, item: &item}}
	for _, i := range []int{0, 1} {
		if text := plain(r.paint(lines, i, 180, a.pal, false)); !strings.Contains(text, "running on beta") {
			t.Errorf("watched row %d: %s", i, text)
		}
	}
	var preview []string
	for _, row := range a.taskPaneRecord(item, 100) {
		preview = append(preview, plain(row.text))
	}
	if text := strings.Join(preview, "\n"); !strings.Contains(text, "running on beta") {
		t.Errorf("watched preview: %s", text)
	}
	if text := a.roomStateWord(taskGuestNode(item)); !strings.Contains(text, "running on beta") {
		t.Errorf("watched task room: %s", text)
	}
}

// R9.1 also applies after a watcher receives the owner's finished notice;
// phase text alone cannot retain the executor on a settled task page.
func TestAWatchingTaskPageKeepsTheOwnersExecutorAfterFinishing(t *testing.T) {
	a, door := guestLab(t)
	door.watching()
	enterAway(t, a)
	for _, state := range []session.TaskState{session.TaskRunning, session.TaskDone} {
		finished := state == session.TaskDone
		ownerSays(t, a, session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, State: state, Computer: "beta", RanElsewhere: finished,
			Remote: &session.TaskRemoteRecord{Executor: session.TaskExecutor{Name: "beta"}, Finished: finished}}})
		want := "running on beta"
		if finished {
			want = "ran on beta"
		}
		if text := a.roomStateWord(a.roomNode()); !strings.Contains(text, want) {
			t.Errorf("%s: %s", state, text)
		}
		if a.tasks[7].computer != "" {
			t.Fatal("the owner's executor changed this conversation's local task")
		}
	}
}

func watchedPlacementReading(a *app, entry session.TaskIndexEntry) (tasksReading, tasksItem) {
	entry.SessionID = "watched conversation"
	owner := session.SessionRow{ID: entry.SessionID}
	owner.Tasks.Rows = []session.TaskIndexEntry{entry}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{owner}}}}
	mine := tasksMine{away: []session.ElsewhereTask{{SessionID: entry.SessionID, Session: "another window", Task: session.PresenceTask{ID: entry.ID, Title: entry.Title, State: entry.Status}}}}
	r := readTasks(world, mine, session.LastDays(a.now(), 1), tasksSort{}, a.now(), a.now())
	return r, r.items[0]
}

// R9's stability rule compares the same local rows with dev 9be6706b3's
// renderers, captured in the fixture with their original bytes intact.
func TestALocalTaskRowIsUnchangedInEveryList(t *testing.T) {
	raw, err := os.ReadFile("testdata/task_local_dev_9be6706b3.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for state, surfaces := range want {
		a, entry := placementRowFixture(t, session.TaskState(state), "")
		got := placementRowReadings(a, entry)
		if len(got) != len(surfaces) {
			t.Fatalf("%s: dev fixture covers %d surfaces, current row has %d", state, len(surfaces), len(got))
		}
		for surface, original := range surfaces {
			if got[surface] != original {
				t.Errorf("%s/%s changed dev's local row:\n got %q\nwant %q", state, surface, got[surface], original)
			}
		}
	}
}
