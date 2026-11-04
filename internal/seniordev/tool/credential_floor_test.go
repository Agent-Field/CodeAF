package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This program's tool loop does not pass the session's gate, so it asks the floor.
func TestSeniorDevCannotReadACredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(home, "project")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)
	for name, input := range map[string]map[string]any{
		"bash": {"command": "gh auth token", "description": "x"},
		"read": {"filePath": filepath.Join(home, ".netrc")},
	} {
		_, err := execute(t, registry, name, input)
		if err == nil || !strings.Contains(err.Error(), "Blocked") {
			t.Fatalf("%s answered %v, want the refusal", name, err)
		}
	}
}
