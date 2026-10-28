package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract A2 opens transferred finished work through Tasks without a plan store.
func TestMovedFinishedTaskOpensReadOnlyFromTasks(t *testing.T) {
	root := t.TempDir()
	place := session.Place{Dir: root, Workspace: filepath.Join(root, "work")}
	if err := os.MkdirAll(filepath.Join(place.NodeJournals(), "1"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		place.Tasks(): `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"title":"Create hello-task.txt","state":"done","planTask":"t-1","pendingRun":{"brief":"Write a friendly greeting"}}]}`,
		filepath.Join(place.NodeJournals(), "1", "worker.jsonl"):     "{\"type\":\"session\",\"version\":1,\"id\":\"worker\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"created hello-task.txt\"}\n",
		filepath.Join(place.NodeJournals(), "1", "trajectory.jsonl"): "{\"kind\":\"step\",\"step\":1,\"command\":\"printf hello\",\"observation\":\"hello\"}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	agent, err := session.New(session.Config{Workspace: place.Workspace, Place: place, SessionFile: place.Transcript(), Model: "stub/chat", APIKey: "stub-key", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := newTestApp(agent)
	a.width, a.height = 180, 48
	a.homeRoot = t.TempDir()
	a.file, a.title = place.Transcript(), "moved chat"
	a.planRows, a.planRowsRead, a.planRowsFront = agent.PlanTasks(), true, a.frontGen
	a.planRowsStamp, a.planRowsAt = a.railStamp, a.now()
	if !openTaskPlaceWithRows(a) {
		t.Fatal("Tasks did not open")
	}
	found := false
	for _, item := range a.taskSheet.reading.items {
		if item.plan != nil && item.plan.ID == "t-1" {
			a.taskSheetPointAt(item.entry)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("finished task missing from Tasks: %s", taskSheetText(a))
	}
	spend(t, a, a.taskSheet.enter(a))
	if a.room == nil || a.room.plan == nil || !a.room.done {
		t.Fatal("Tasks did not open a finished plan page")
	}
	text := planRoomText(t, a)
	for _, want := range []string{"done", "Create hello-task.txt", "Write a friendly greeting", "created hello-task.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("page missing %q:\n%s", want, text)
		}
	}
	if got := strings.Join(roomCommands(a), "|"); !strings.Contains(got, "printf hello") {
		t.Errorf("recorded steps missing: %s", got)
	}
	if words := a.tasksPlanKeyWords(a.room.plan.page.Row); len(words) != 0 {
		t.Errorf("finished task actions: %v", words)
	}
	drive(t, a, key(stopRaiseKey), key("p"))
	spend(t, a, a.planRoomSteer("resume this task"))
	if !strings.Contains(planRoomText(t, a), "this task has finished") {
		t.Error("finished task did not explain its refusal")
	}
	if !a.room.done {
		t.Fatal("finished task restarted")
	}
}
