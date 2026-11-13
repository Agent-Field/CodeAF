package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Contract G3/J16 opens a moved finished worker whose checkpoint has no plan identity.
func TestMovedFinishedTaskWithoutPlanIdentityShowsItsWorker(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	place := session.Place{Dir: filepath.Join(t.TempDir(), "copied-chat"), Owned: true}
	place.Workspace = place.Work()
	if err := os.MkdirAll(filepath.Join(place.NodeJournals(), "1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(place.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	checkpoint := `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"title":"Create hello task file","state":"done","model":"fixture/history","report":"file created","result":"file created"}]}`
	if err := os.WriteFile(place.Tasks(), []byte(checkpoint), 0600); err != nil {
		t.Fatal(err)
	}
	worker := "{\"type\":\"session\",\"version\":1,\"id\":\"worker\",\"model\":\"fixture/history\"}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"Create hello task file\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"The retained worker inspected the files.\"}\n"
	if err := os.WriteFile(filepath.Join(place.NodeJournals(), "1", "20261027-011448.092944_worker.jsonl"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	step := `{"kind":"step","step":1,"command":"printf hello-task","observation":"hello-task"}` + "\n"
	if err := os.WriteFile(filepath.Join(place.NodeJournals(), "1", "trajectory.jsonl"), []byte(step), 0600); err != nil {
		t.Fatal(err)
	}
	agent, err := session.New(session.Config{Place: place, Workspace: place.Workspace, SessionFile: place.Transcript(), Model: "fixture/history", APIKey: "fixture", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.Close() })
	a := newTestApp(agent)
	a.width, a.height = 120, 40
	a.file, a.workspace = place.Transcript(), place.Workspace
	rows := agent.PlanTasks()
	if len(rows) != 1 || rows[0].Title != "Create hello task file" {
		t.Fatalf("finished task is absent from its task list: %+v", rows)
	}
	openPlanRoomNow(t, a, rows[0].ID)
	if a.room == nil || a.room.plan == nil || !a.room.plan.page.Historical {
		t.Fatal("moved finished task did not open its historical page")
	}
	if text := roomText(a); !strings.Contains(text, "The retained worker inspected the files.") || !strings.Contains(text, "file created") {
		t.Fatalf("moved task has no saved worker conversation: %s", text)
	}
	if commands := strings.Join(roomCommands(a), "\n"); !strings.Contains(commands, "printf hello-task") {
		t.Fatalf("moved task has no retained step: %q", commands)
	}
	drive(t, a, key("r"), key("R"))
	if !a.room.done || a.input.String() != "rR" {
		t.Fatal("finished task accepted a restart action")
	}
}
