package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The actual session engine and the actual host adapter must both carry the
// setting, even when the task was completed before this process opened it.
func hostedTaskSetupApp(t *testing.T) (*app, *remote.Client, *session.Agent) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	checkpoint := `{"type":"tasks","version":1,"seq":7,"nodes":[{"id":7,"title":"Callback handling","brief":"Preserve the destination after sign-in.","acceptance":"Cover expired sessions.","model":"z-ai/glm-5.3","state":"done","noted":true,"report":"The completed report."}]}`
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(checkpoint), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := session.New(session.Config{Workspace: dir, SessionFile: file, APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", Model: "z-ai/glm-5.3", System: "Test only", TaskModels: func() []string { return []string{"z-ai/glm-5.3", "anthropic/claude-sonnet-5"} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: engine, SessionFile: file, Workspace: dir}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	a, _ := taskControlApp(t)
	a.agent = loop.Client.Agent()
	a.file = file
	a.roomNode().state = session.TaskDone
	a.room.done = true
	return a, loop.Client, engine
}

func TestCompletedTaskSetupThroughTheRealHostedAgent(t *testing.T) {
	a, client, engine := hostedTaskSetupApp(t)
	y := panelRow(t, a, "model")
	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: y, Button: tea.MouseLeft})
	if !a.pick.open || a.pick.task != 7 {
		t.Fatal("normal hosted model row did not open task picker")
	}
	a.retargetTask(7, "anthropic/claude-sonnet-5")
	a.closeLists()
	// The host must persist the next model without changing the completed attempt.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(a.file), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"next_model": "anthropic/claude-sonnet-5"`) {
		t.Fatalf("host did not save model: %s", raw)
	}
	y = panelRow(t, a, "effort")
	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: y, Button: tea.MouseLeft})
	if engine.TaskEffort(7) != effortNext(a.taskRung(7)).String() {
		t.Fatal("host did not save thinking")
	}
	a.roomNode().thinking = engine.TaskEffort(7)
	before := client.CallsMade()
	for range 10 {
		a.frame()
		drive(t, a, motionTo(a.width-2, y))
	}
	if calls := client.CallsMade() - before; calls != 0 {
		t.Fatalf("frame/hover made %d network calls", calls)
	}
}

func TestTaskThinkingClickReturnsToAutoThroughTheHost(t *testing.T) {
	a, client, engine := hostedTaskSetupApp(t)
	if err := client.Agent().SetTaskEffort(7, "max"); err != nil {
		t.Fatal(err)
	}
	a.roomNode().thinking = "max"
	y := panelRow(t, a, "effort")
	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: y, Button: tea.MouseLeft})
	if got := engine.TaskEffort(7); got != "" {
		t.Fatalf("max should cycle to auto, got %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(a.file), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Nodes []struct {
			ID         uint64
			State      string
			NextEffort *string `json:"next_effort"`
		}
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Nodes) != 1 || saved.Nodes[0].NextEffort == nil || *saved.Nodes[0].NextEffort != "" || saved.Nodes[0].State != "done" {
		t.Fatalf("auto was not saved separately: %s", raw)
	}
	a.roomNode().thinking = ""
	var rows []string
	for _, row := range a.roomControlRows(a.railRoom()) {
		rows = append(rows, plain(row.text))
	}
	if !strings.Contains(strings.Join(rows, "\n"), "Thinking · auto") {
		t.Fatal("auto is not visible")
	}
	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: panelRow(t, a, "effort"), Button: tea.MouseLeft})
	if got := engine.TaskEffort(7); got != "low" {
		t.Fatalf("auto should cycle to low, got %q", got)
	}
}
