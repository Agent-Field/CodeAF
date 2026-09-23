//go:build !windows

// Per-leaf cwd behaviour: a project instance in context redirects tool paths
// and the shell's working directory into its own directory.
package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
)

func TestConcurrentLeafContextsResolveTheirOwnToolCWD(t *testing.T) {
	root := t.TempDir()
	leafA := filepath.Join(root, ".worktrees", "wt-a")
	leafB := filepath.Join(root, ".worktrees", "wt-b")
	for _, directory := range []string{leafA, leafB} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registry := New(root)
	type leaf struct {
		id        string
		directory string
	}
	leaves := []leaf{{id: "a", directory: leafA}, {id: "b", directory: leafB}}
	var wait sync.WaitGroup
	errors := make(chan error, len(leaves))
	for _, item := range leaves {
		item := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			ctx := project.WithContext(context.Background(), project.InstanceContext{
				Directory: item.directory,
				Worktree:  root,
				Project:   project.Info{ID: "p", Worktree: root, Sandboxes: []string{item.directory}},
			})
			raw, _ := json.Marshal(map[string]any{
				// An absolute root path must be transparently redirected into
				// this leaf, not written into the shared checkout.
				"filePath": filepath.Join(root, "owned.txt"),
				"content":  item.id,
			})
			if _, err := registry.Execute(ctx, steploop.ToolCall{Name: "write", Input: raw}); err != nil {
				errors <- err
				return
			}
			bashRaw, _ := json.Marshal(map[string]any{"command": "pwd"})
			result, err := registry.Execute(ctx, steploop.ToolCall{Name: "bash", Input: bashRaw})
			if err != nil {
				errors <- err
				return
			}
			// The shell prints the folder the kernel resolved, and a temporary
			// folder on macOS is reached through a symlink, so the same folder
			// can come back spelled /private/var/…; it is compared resolved.
			if !sameFolder(strings.TrimSpace(result.Output), item.directory) {
				errors <- &cwdError{got: strings.TrimSpace(result.Output), want: item.directory}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(root, "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("shared root was modified: %v", err)
	}
	for _, item := range leaves {
		data, err := os.ReadFile(filepath.Join(item.directory, "owned.txt"))
		if err != nil {
			t.Fatalf("read %s leaf: %v", item.id, err)
		}
		if string(data) != item.id {
			t.Fatalf("%s leaf content = %q", item.id, data)
		}
	}
}

func TestRelativeTraversalIntoMainWorktreeRedirectsToLeafContract(t *testing.T) {
	// A sandboxed instance resolves relative paths before redirecting paths
	// that land in the main checkout into its own directory.
	mainWorktree := t.TempDir()
	leafWorktree := filepath.Join(mainWorktree, ".worktrees", "wt-task")
	if err := os.MkdirAll(leafWorktree, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := New(mainWorktree)
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: leafWorktree,
		Worktree:  mainWorktree,
		Project:   project.Info{ID: "p", Worktree: mainWorktree, Sandboxes: []string{leafWorktree}},
	})
	target := filepath.Join(mainWorktree, "relative-owned.txt")
	relative, err := filepath.Rel(leafWorktree, target)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"filePath": relative, "content": "leaf"})
	if _, err := registry.Execute(ctx, steploop.ToolCall{Name: "write", Input: raw}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("relative traversal modified main checkout: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(leafWorktree, "relative-owned.txt"))
	if err != nil || string(data) != "leaf" {
		t.Fatalf("redirected leaf file = %q, %v", data, err)
	}
}

type cwdError struct {
	got  string
	want string
}

func (e *cwdError) Error() string { return "tool cwd = " + e.got + ", want " + e.want }
