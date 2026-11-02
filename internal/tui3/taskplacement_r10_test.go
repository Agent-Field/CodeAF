package tui3

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func r10RailFixture(t *testing.T, computer string) (*app, *taskNode) {
	t.Helper()
	a, _, _ := taskApp(t)
	a.taskUpdate(update(7, "replace the whole content of notes.txt with the requested words", session.TaskRunning, session.TaskNotice{Computer: computer}))
	node := a.tasks[7]
	node.started, node.began = a.now().Add(-4*time.Minute), a.now().Add(-4*time.Minute)
	return a, node
}

// R10(b2) names the executor on the actual rail headline, giving the title's
// cells to placement first. A narrow rail may drop the whole phrase only.
func TestTheRailSaysWhichComputerATaskRunsOn(t *testing.T) {
	a, node := r10RailFixture(t, "beta")
	for width := 8; width <= 120; width++ {
		line := plain(a.railEntryRow(railEntry{node: node, group: railRunning}, width))
		if ansi.StringWidth(line) > width {
			t.Errorf("width %d spilled: %q", width, line)
		}
		if width >= 24 && !strings.Contains(line, "running on beta") {
			t.Errorf("width %d spent placement cells on the title: %q", width, line)
		}
		if !strings.Contains(line, "running on beta") && strings.Contains(line, "running on") {
			t.Errorf("width %d cut a placement word: %q", width, line)
		}
	}
}

func r10HistoryFixture(t *testing.T, computer string, historical bool) (*app, tasksReading, tasksItem) {
	t.Helper()
	a, _, _ := taskApp(t)
	owner := session.SessionRow{ID: "r10-conversation", Title: "export work"}
	entry := session.TaskIndexEntry{ID: "7", SessionID: owner.ID, Title: "create hello.txt containing the word KIWI", Status: string(session.TaskDone), Computer: computer, RanElsewhere: computer != ""}
	plan := session.PlanTaskRow{ID: "p-r10", Title: entry.Title, Status: "done", Steps: 2, Archived: true}
	row := tasksMineRow{entry: entry, planTask: plan.ID}
	if historical {
		plan.HistoricalRun, plan.HistoricalSession = 7, owner.ID
		row.planTask = ""
	}
	mine := tasksMine{row: owner, rows: []tasksMineRow{row}, plan: []session.PlanTaskRow{plan}}
	reading := readTasks(session.World{}, mine, session.LastDays(a.now(), 1), tasksSort{}, a.now(), a.now())
	if len(reading.items) != 1 || reading.items[0].plan == nil {
		t.Fatalf("not the actual history plan row: %+v", reading.items)
	}
	return a, reading, reading.items[0]
}

// R10(b3) follows the same plan-over-record replacement /history uses, both
// while the store is held and after it becomes a retained run receipt.
func TestAFinishedHistoryRowSaysWhichComputerItRanOn(t *testing.T) {
	for _, historical := range []bool{false, true} {
		a, r, item := r10HistoryFixture(t, "beta", historical)
		lines := []tasksLine{{kind: tasksLineTask, item: &item}, {kind: tasksLineTail, item: &item}}
		for i := range lines {
			text := plain(r.paint(lines, i, 160, a.pal, false))
			if !strings.Contains(text, "ran on beta") || !strings.Contains(text, "2 steps") {
				t.Errorf("historical %v, line %d: %s", historical, i, text)
			}
		}
	}
}

func r10LocalRows(t *testing.T) map[string]string {
	t.Helper()
	a, node := r10RailFixture(t, "")
	rows := map[string]string{}
	for _, width := range []int{20, 40, 80, 160} {
		rows[fmt.Sprintf("rail/%d", width)] = a.railEntryRow(railEntry{node: node, group: railRunning}, width)
	}
	for _, historical := range []bool{false, true} {
		a, r, item := r10HistoryFixture(t, "", historical)
		lines := []tasksLine{{kind: tasksLineTask, item: &item}, {kind: tasksLineTail, item: &item}}
		for i := range lines {
			rows[fmt.Sprintf("history/%v/%d", historical, i)] = r.paint(lines, i, 160, a.pal, false)
		}
	}
	return rows
}

// R10's stability rule pins the actual local rail and plan/history bytes from
// 4443a1750, before either remote-only renderer change.
func TestLocalRailAndHistoryRowsKeepTheirOriginalBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/task_local_r10_4443a1750.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]string
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	got := r10LocalRows(t)
	for surface, original := range want {
		if got[surface] != original {
			t.Errorf("%s changed: got %q, want %q", surface, got[surface], original)
		}
	}
}
