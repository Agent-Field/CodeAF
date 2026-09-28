//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderMergesProjectAndPermissionEnvironment(t *testing.T) {
	workspace := t.TempDir()
	global := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "senior-dev.json"), []byte(`{
  "instructions": ["PROJECT.md"],
  "permission": {"edit": "allow"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"SENIOR_DEV_PERMISSION": `{"edit":"deny","read":"ask"}`,
	}
	env := NewEnv(func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})

	// The once-per-run loader layers env over project config.
	loaded, err := (Loader{GlobalDir: global, Env: env}).Load(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	permission, _ := loaded["permission"].(*OrderedObject)
	edit, _ := permission.Get("edit")
	read, _ := permission.Get("read")
	if edit != "deny" || read != "ask" {
		t.Fatalf("permission merge = %#v", permission)
	}
	instructions, _ := loaded["instructions"].([]any)
	if len(instructions) != 1 || instructions[0] != "PROJECT.md" {
		t.Fatalf("instructions = %#v", instructions)
	}
}
