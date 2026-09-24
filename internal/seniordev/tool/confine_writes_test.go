//go:build !windows

package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/permission"
)

// WRITES STAY IN THE WORKSPACE, READS DO NOT HAVE TO. codeaf lands only the
// copy a run is handed, so under ConfineWrites every writer refuses a path
// outside the workspace — named absolutely, climbed to with `..`, or reached
// through a link inside the workspace that points out — and the file outside
// is untouched. A read outside still works, because a task's statement can
// live outside its copy. This is the road a run took when its brief told it
// to make a checkout in the person's projects folder and it edited there.
func TestConfinedWritesRefuseEveryPathOutsideTheWorkspace(t *testing.T) {
	workspace, external := t.TempDir(), t.TempDir()
	target := filepath.Join(external, "outside.txt")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(workspace, "out")); err != nil {
		t.Fatal(err)
	}
	registry := NewWithOptions(workspace, RegistryOptions{
		AllowExternalDirectories: true,
		ConfineWrites:            true,
		Permission:               permissionEvaluatorFunc(func(permission.AskInput) error { return nil }),
	})
	escape, err := filepath.Rel(workspace, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, escape, filepath.Join("out", "outside.txt")} {
		for _, call := range []struct {
			tool  string
			input map[string]any
		}{
			{"write", map[string]any{"filePath": path, "content": "changed\n"}},
			{"edit", map[string]any{"filePath": path, "oldString": "keep", "newString": "changed"}},
			{"apply_patch", map[string]any{"patchText": "*** Begin Patch\n*** Update File: " + path + "\n@@\n-keep\n+changed\n*** End Patch"}},
			{"apply_patch", map[string]any{"patchText": "*** Begin Patch\n*** Add File: " + path + ".new\n+new\n*** End Patch"}},
		} {
			_, err := execute(t, registry, call.tool, call.input)
			if err == nil || !strings.Contains(err.Error(), "outside this run's workspace") {
				t.Fatalf("%s of %q: err = %v, want the write refused", call.tool, path, err)
			}
		}
	}
	if data, _ := os.ReadFile(target); string(data) != "keep\n" {
		t.Fatalf("the file outside the workspace changed: %q", data)
	}
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Fatal("a file was added outside the workspace")
	}
	if result, err := execute(t, registry, "read", map[string]any{"filePath": target}); err != nil || !strings.Contains(result.Output, "keep") {
		t.Fatalf("a read outside the workspace was refused: %v", err)
	}
	if _, err := execute(t, registry, "write", map[string]any{"filePath": "inside.txt", "content": "fine\n"}); err != nil {
		t.Fatalf("a write inside the workspace was refused: %v", err)
	}
}

// AND WITHOUT THE OPTION NOTHING CHANGES: an embedder that never asked for the
// fence keeps the permission flow it had.
func TestUnconfinedWritesStillReachOutsideThroughPermission(t *testing.T) {
	workspace, external := t.TempDir(), t.TempDir()
	target := filepath.Join(external, "outside.txt")
	registry := NewWithOptions(workspace, RegistryOptions{
		AllowExternalDirectories: true,
		Permission:               permissionEvaluatorFunc(func(permission.AskInput) error { return nil }),
	})
	if _, err := execute(t, registry, "write", map[string]any{"filePath": target, "content": "x\n"}); err != nil {
		t.Fatalf("an unconfined registry refused an allowed write: %v", err)
	}
}
