package syncsetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// A labelled API fixture supplies metadata; native capture and the public Take
// door prove parent binding, not model-driven task creation.
func TestNestedPlainProjectCopiesFollowGroundURIThroughPublicTake(t *testing.T) {
	h := newTwoHomes(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, h.homeA)
	plainProtocolInputs(t, h.work)
	copies := seedNestedPlainProtocolFixture(t, h.cell, h.work)
	a := h.openA()
	a.mustSay("completed parent and child fixture")
	t.Setenv(home.EnvVar, h.homeB)
	received := h.takeOnB(a)
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	for id, copy := range copies {
		local := filepath.Join(received.Taken.Cell.Root, cell.TreesDir, id)
		record, err := registry.Resolve(copy.URI)
		if err != nil || record.Path != local {
			t.Fatalf("copy %s not admitted locally: %+v %v notices=%v", id, record, err, h.wall.all())
		}
	}
	parent := filepath.Join(received.Taken.Cell.Root, cell.TreesDir, "1")
	child := filepath.Join(received.Taken.Cell.Root, cell.TreesDir, "2")
	info, err := os.Stat(filepath.Join(child, ".git"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("child is no longer linked to its restored parent: %v", err)
	}
	common := gitOut(t, child, "rev-parse", "--git-common-dir")
	commonPath := common
	if !filepath.IsAbs(commonPath) {
		commonPath = filepath.Join(child, commonPath)
	}
	if relative, err := filepath.Rel(parent, filepath.Clean(commonPath)); err != nil || !filepath.IsLocal(relative) {
		t.Fatalf("child Git history is not in owned parent: %s", common)
	}
	for name, want := range map[string]string{"child-committed.txt": "nested committed output\n", "child-dirty.txt": "nested dirty output\n"} {
		raw, err := os.ReadFile(filepath.Join(child, name))
		if err != nil || string(raw) != want {
			t.Fatalf("child %s=%q %v", name, raw, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(workspaceOf(received.Taken.Cell.Root), ".git")); !os.IsNotExist(err) {
		t.Fatal("plain receiving project was initialized as Git")
	}
	if _, err := os.Lstat(filepath.Join(h.work, ".git")); !os.IsNotExist(err) {
		t.Fatal("plain borrowed source project was changed")
	}
}

func plainProtocolInputs(t *testing.T, project string) {
	t.Helper()
	for name, body := range map[string]string{"keep.txt": "baseline\n", "large-input.dat": strings.Repeat("unchanged baseline\n", 1<<17)} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func seedNestedPlainProtocolFixture(t *testing.T, at cell.Cell, project string) map[string]*session.TaskCopyRecord {
	t.Helper()
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, at.Root)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := registry.Register(workspaceidentity.Project, project)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(at.Root)
	if err != nil {
		t.Fatal(err)
	}
	meta.ChatURI = chat.URI
	if err := session.SaveMeta(at.Root, meta); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(at.Root, cell.TreesDir, "1")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keep.txt", "large-input.dat"} {
		raw, err := os.ReadFile(filepath.Join(project, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, parent, "init", "-q", "-b", "main")
	git(t, parent, "add", ".")
	git(t, parent, "commit", "-qm", "private plain baseline")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(parent, "committed.txt"), "completed committed result\n")
	git(t, parent, "add", "committed.txt")
	git(t, parent, "commit", "-qm", "parent output")
	write(filepath.Join(parent, "dirty.txt"), "completed dirty result\n")
	child := filepath.Join(at.Root, cell.TreesDir, "2")
	git(t, parent, "worktree", "add", "-qb", "task/child", child)
	write(filepath.Join(child, "child-committed.txt"), "nested committed output\n")
	git(t, child, "add", "child-committed.txt")
	git(t, child, "commit", "-qm", "child output")
	write(filepath.Join(child, "child-dirty.txt"), "nested dirty output\n")
	if err := os.Remove(filepath.Join(child, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(at.Root, cell.TreesDir, "3")
	git(t, parent, "worktree", "add", "-qb", "task/active", active)
	write(filepath.Join(active, "active.txt"), "active output must not restart\n")
	copies := map[string]*session.TaskCopyRecord{}
	for _, id := range []string{"1", "2", "3"} {
		dir := filepath.Join(at.Root, cell.TreesDir, id)
		record, err := registry.Register(workspaceidentity.Run, dir, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: proj.URI})
		if err != nil {
			t.Fatal(err)
		}
		copy := &session.TaskCopyRecord{URI: record.URI, ChatURI: chat.URI, ProjectURI: proj.URI, Dir: dir, GroundURI: proj.URI, Ground: project, Mode: session.TaskModeMirror}
		if id != "1" {
			copy.GroundURI = copies["1"].URI
			copy.Ground = parent
			copy.RootURI = copies["1"].URI
			copy.Root = parent
			copy.Mode = session.TaskModeWorktree
			copy.Branch = "task/child"
			if id == "3" {
				copy.Branch = "task/active"
			}
		}
		copies[id] = copy
	}
	// The normal checkpoint writer spells these fields with the path codec.
	// This labelled API fixture supplies the same portable representation.
	for id, copy := range copies {
		copy.Dir = "session:trees/" + id
		copy.Ground = "workspace:."
		if id != "1" {
			copy.Ground = "session:trees/1"
			copy.Root = "session:trees/1"
		}
	}
	rows := []map[string]any{{"id": 1, "title": "finished parent fixture", "state": "done", "report": "completed report", "result": "completed answer", "costUsd": 0.125, "changed": []string{"committed.txt", "dirty.txt"}, "copy": copies["1"]}, {"id": 2, "title": "finished child fixture", "state": "done", "report": "completed child report", "changed": []string{"child-committed.txt", "child-dirty.txt", "keep.txt"}, "copy": copies["2"]}, {"id": 3, "title": "active child fixture", "state": "running", "report": "partial report", "pendingRun": map[string]any{}, "copy": copies["3"]}}
	raw, err := json.Marshal(map[string]any{"type": "tasks", "version": 1, "seq": 3, "runs": rows})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.Place{Dir: at.Root}.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return copies
}
