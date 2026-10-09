package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// writeWorkerTranscript writes one worker journal beside a task's trajectory, the
// way the belt does, with the given assistant messages in order.
func writeWorkerTranscript(t *testing.T, dir, id, name string, said ...string) {
	t.Helper()
	var lines []string
	for _, text := range said {
		lines = append(lines, `{"type":"message","role":"assistant","content":"`+text+`"}`)
	}
	path := filepath.Join(plandb.TaskDir(dir, id), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write the worker transcript: %v", err)
	}
}

// A page says how its worker ended, what the run wrote and what the newest worker
// last said; a task that has none of the three draws none of them.
func TestPlanTaskPageCarriesEndingChangedAndLastWords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha", Description: "work"},
		plandb.TaskSpec{ID: "beta", Title: "Beta", Description: "idle"},
	)
	writePlanTrajectory(t, dir, "alpha",
		`{"kind":"step","step":1,"command":"$ echo one","observation":"one"}`,
		`{"kind":"end","steps":1,"result":"first try","reason":"step cap"}`,
		`{"kind":"end","steps":2,"result":"all done","reason":"turn ended"}`,
	)
	long := strings.Repeat("w", planLastWordsCap+50)
	writeWorkerTranscript(t, dir, "alpha", "20260101-000000.000000_worker.jsonl", "an older worker")
	writeWorkerTranscript(t, dir, "alpha", "20260102-000000.000000_worker.jsonl", "thinking aloud", long)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	g := agent.graph()
	agent.publishRunRow(g, TaskNotice{
		ID: 3, Title: "Alpha", State: TaskDone, PlanTask: "t-alpha",
		Changed: []string{"internal/x/a.go", "docs/b.md"},
	})

	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("no page")
	}
	if page.Ended == nil || page.Ended.Reason != "turn ended" || page.Ended.Result != "all done" {
		t.Fatalf("Ended = %+v, want the LAST ending line", page.Ended)
	}
	if page.Ended.At.IsZero() {
		t.Fatal("an ending with no clock of its own must fall back to the file's, not zero")
	}
	if want := []string{"internal/x/a.go", "docs/b.md"}; !reflect.DeepEqual(page.Changed, want) {
		t.Fatalf("Changed = %v, want %v", page.Changed, want)
	}
	if got := []rune(page.LastWords); len(got) != planLastWordsCap || !strings.HasSuffix(page.LastWords, "…") {
		t.Fatalf("LastWords has %d characters, want %d ending in an ellipsis", len(got), planLastWordsCap)
	}

	idle, ok := agent.PlanTaskPage("t-beta")
	if !ok {
		t.Fatal("no page for the idle task")
	}
	if idle.Ended != nil || idle.Changed != nil || idle.LastWords != "" {
		t.Fatalf("an idle task drew %+v", idle)
	}
}
