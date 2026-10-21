package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// The automation tool look inherited the retired probe's execution boundary:
// its read is recorded by the configured seat without a model turn.
func TestAutomationToolLookUsesItsConfiguredSeat(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "status.txt"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	seat := &countingSeat{}
	runner := &automationRunner{base: func(string) (Config, error) {
		return Config{Workspace: workspace, Model: "test/model", Seat: seat}, nil
	}}
	_, err := runner.lookTool(context.Background(), automation.Automation{
		Workspace: workspace,
		Look:      &automation.Look{Tool: "read", Args: []byte(`{"path":"status.txt"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	seat.mu.Lock()
	defer seat.mu.Unlock()
	if len(seat.tools) != 1 || seat.tools[0] != "read" {
		t.Fatalf("automation calls on the seat = %v, want read", seat.tools)
	}
}
