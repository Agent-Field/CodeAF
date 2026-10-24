package tui3

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func activityScaleWorld(chats, tasks int) (session.World, time.Time) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	world := session.World{Read: now}
	project := session.Project{Name: "Mock workspace"}
	for chat := 0; chat < chats; chat++ {
		row := session.SessionRow{ID: fmt.Sprintf("chat-%03d", chat), Title: fmt.Sprintf("Mock history chat %03d", chat), Transcript: fmt.Sprintf("/mock/%03d/transcript.jsonl", chat), At: now}
		for task := 1; task <= tasks; task++ {
			parent := ""
			if task > 1 {
				parent = fmt.Sprint((task-2)/4 + 1)
			}
			row.Tasks.Rows = append(row.Tasks.Rows, session.TaskIndexEntry{SessionID: row.ID, ID: fmt.Sprint(task), Parent: parent, Title: fmt.Sprintf("Mock repair %03d", task), Status: string(session.TaskDone), EndedAt: now.Add(-time.Duration(task) * time.Minute)})
		}
		project.Sessions = append(project.Sessions, row)
	}
	world.Projects = []session.Project{project}
	return world, now
}

func TestActivityFilteringSharesUnchangedRowsAndNeverMutatesSnapshot(t *testing.T) {
	a := tasksTableApp(t)
	original := append([]tasksItem(nil), a.taskSheet.reading.items...)
	unchanged := a.taskSheet.filtered(a)
	if &unchanged.items[0] != &a.taskSheet.reading.items[0] {
		t.Fatal("unfiltered read copied every task")
	}
	first := original[0]
	a.deletedSessionRows = map[tasksKey]bool{tasksKeyOf(first.entry): true}
	removed := a.taskSheet.filtered(a)
	if len(removed.items) != len(original)-1 {
		t.Fatalf("removed %d rows, want one", len(original)-len(removed.items))
	}
	if !reflect.DeepEqual(a.taskSheet.reading.items, original) {
		t.Fatal("filter mutated the held snapshot")
	}
	delete(a.deletedSessionRows, tasksKeyOf(first.entry))
	if restored := a.taskSheet.filtered(a); !reflect.DeepEqual(restored.items, original) {
		t.Fatal("clearing deletion did not restore the held reading")
	}
}

func TestActivityPaintScratchExpiresBeforeFilterAndResize(t *testing.T) {
	a := tasksTableApp(t)
	a.taskSheetBody(122, 35)
	if a.taskSheet.paint != nil || a.taskSheet.reading.layouts != nil {
		t.Fatal("paint scratch survived its frame")
	}
	a.taskSheet.query.insert("Thor")
	filtered := a.tasksFiltered()
	if len(filtered.items) >= len(a.taskSheet.reading.items) || len(filtered.items) == 0 {
		t.Fatalf("filter reused the previous frame: %d items", len(filtered.items))
	}
	a.taskSheetBody(70, 20)
	if a.taskSheet.paint != nil {
		t.Fatal("resize retained paint scratch")
	}
}

// Compare memoized ancestry with the old independent walk for malformed graphs,
// including out-of-order parents, cycles, self-links and missing parents.
func TestActivityParentIndexPreservesMalformedGraphPolicy(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 300; trial++ {
		items := make([]tasksItem, 40)
		for i := range items {
			items[i].entry = session.TaskIndexEntry{SessionID: "mock", ID: fmt.Sprint(i), Parent: fmt.Sprint(rng.Intn(45)), Status: string(session.TaskDone)}
		}
		tree := tasksTreeOf(items, time.Now(), tasksSort{})
		for _, item := range items {
			key := tasksKeyOf(item.entry)
			seen := map[tasksKey]bool{key: true}
			at, valid := key, true
			var first tasksKey
			for {
				current := tree.at[at]
				parent := tasksKey{session: "mock", id: current.entry.Parent}
				if _, exists := tree.at[parent]; !exists || parent == at {
					break
				}
				if at == key {
					first = parent
				}
				if seen[parent] {
					valid = false
					break
				}
				seen[parent], at = true, parent
			}
			got, nested := tree.up[key]
			wantNested := valid && first != (tasksKey{})
			if nested != wantNested || (nested && got != first) {
				t.Fatalf("trial %d key %v parent=%v nested=%v; want %v nested=%v", trial, key, got, nested, first, wantNested)
			}
		}
	}
}

// Repainting a large history must spend memory on layout facts, not another
// complete copy of every task and conversation record on the page.
func TestActivityScalePaintAllocationBudget(t *testing.T) {
	a := activityScaleApp()
	a.taskSheetBody(160, 45)
	const budget = 4 << 20
	least := allocatedBytes(func() {
		rows := a.taskSheetBody(160, 45)
		if len(rows) == 0 || !strings.Contains(rows[0].text, "chats") {
			t.Fatal("large history did not paint its conversation count")
		}
	})
	if least > budget {
		t.Fatalf("Activity paint allocated %d bytes for 5,120 tasks; budget is %d", least, budget)
	}
}

func activityScaleApp() *app {
	world, now := activityScaleWorld(40, 128)
	a := &app{pal: newPalette(tokens.NoColor, false), width: 160, height: 55, clock: func() time.Time { return now }}
	a.raisePlace(pageTasks)
	a.taskSheet.world = world
	a.taskSheet.reading = readTasks(world, tasksMine{}, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
	a.taskSheet.awayAt = a.elsewhere().Read
	return a
}

func BenchmarkActivityScaleFrame(b *testing.B) {
	a := activityScaleApp()
	a.taskSheetBody(160, 45)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.taskSheetBody(160, 45)
	}
}
