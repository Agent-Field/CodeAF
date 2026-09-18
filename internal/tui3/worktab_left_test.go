package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestWorkTabNamePreservesTasksDoor(t *testing.T) {
	if got := pageTasks.word(); got != "work" { t.Fatalf("pageTasks.word() = %q, want work", got) }
	door, ok := parsePageWord("tasks")
	if !ok || door != pageTasks { t.Fatalf("/tasks door = %v, %v; want pageTasks", door, ok) }
	got := strings.Fields(placeWordList())[:placeBarPlaces]
	if strings.Join(got, " ") != "home work spend settings" { t.Fatalf("top bar = %q", got) }
}
func TestWorkCountsDropZero(t *testing.T) {
	got := workCounts(map[tasksSection]int{tasksRunning: 4, tasksParked: 3, tasksNeeds: 1, tasksToday: 2})
	if got != "4 running · 3 queued · 1 your call · 2 done today" { t.Fatalf("counts = %q", got) }
	if got := workCounts(map[tasksSection]int{tasksRunning: 1}); got != "1 running" { t.Fatalf("zero counts survived: %q", got) }
}
func TestWorkGroupsOrderNewestAndOmitEmpty(t *testing.T) {
	now := time.Date(2026, 9, 18, 13, 0, 0, 0, time.Local)
	item := func(id string, section tasksSection, ago time.Duration) tasksItem { return tasksItem{entry: session.TaskIndexEntry{ID: id, SessionID: id, Title: id, Label: id, EndedAt: now.Add(-ago)}, section: section} }
	items := []tasksItem{item("queued-old", tasksParked, 4*time.Minute), item("running", tasksRunning, 2*time.Minute), item("queued-new", tasksParked, time.Minute), item("call", tasksNeeds, 3*time.Minute)}
	got := workGrouped(items, now); want := []string{"your call:call", "running:running", "queued:queued-new", "queued:queued-old"}
	if strings.Join(got, ",") != strings.Join(want, ",") { t.Fatalf("groups = %v, want %v", got, want) }
}
func TestWorkRunTailUsesConversationIndex(t *testing.T) {
	item := tasksItem{entry: session.TaskIndexEntry{SessionID: "chat-1"}, row: session.SessionRow{ID: "chat-1", Title: "poem arc"}}
	if got := workConversationTail(item); got != "· poem arc" { t.Fatalf("tail = %q", got) }
	item.row.Title = ""; if got := workConversationTail(item); got != "" { t.Fatalf("untitled tail = %q", got) }
}
func TestWorkOlderDoneFold(t *testing.T) {
	if got := workOlderFold(41); got != "41 more · type to find one" { t.Fatalf("fold = %q", got) }
	if got := workOlderFold(0); got != "" { t.Fatalf("zero fold = %q", got) }
}
