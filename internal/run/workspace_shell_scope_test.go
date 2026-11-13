package run_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestWorkspaceWorkerShellHonorsEachAdmittedWriteRoot(t *testing.T) {
	testWorkspaceWorkerShellScope(t, false)
}
func TestWorkspaceWorkerShellProtectsReadOnlyPrimary(t *testing.T) {
	testWorkspaceWorkerShellScope(t, true)
}
func testWorkspaceWorkerShellScope(t *testing.T, primaryRead bool) {
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	registry, err := workspaceidentity.Open(filepath.Join(profile, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chatDir := t.TempDir()
	chat, err := registry.Register(workspaceidentity.Chat, chatDir)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(chatDir, "trees", "1")
	set := &session.WorkspaceSet{Version: 1, Primary: "app"}
	dirs := map[string]string{}
	originals := map[string]string{}
	for _, slot := range []string{"app", "library", "reference"} {
		dir := filepath.Join(base, slot)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		dirs[slot] = dir
		original := t.TempDir()
		originals[slot] = original
		project, err := registry.Register(workspaceidentity.Project, original)
		if err != nil {
			t.Fatal(err)
		}
		record, err := registry.Register(workspaceidentity.Run, dir, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI})
		if err != nil {
			t.Fatal(err)
		}
		access := session.WorkspaceWrite
		if slot == "reference" || primaryRead && slot == "app" {
			access = session.WorkspaceRead
		}
		set.Roots = append(set.Roots, session.WorkspaceRootUse{Slot: slot, Access: access, Copy: &session.TaskCopyRecord{URI: record.URI, ChatURI: chat.URI, ProjectURI: project.URI, Dir: dir, Ground: original}})
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dirs["library"], "escape")); err != nil {
		t.Fatal(err)
	}
	store := runOpenStore(t)
	tasks, err := store.AddMany([]plandb.TaskSpec{{ID: "scope", ParentID: store.RootID(), Title: "scope", Description: "write admitted roots only"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Claim(tasks[0].ID, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{"printf moved > ../library/move-fd.txt 3>&2-", "printf denied >& ../reference/both.txt", "printf denied >| ../reference/clobber.txt", "printf denied &> ../reference/all.txt", "printf denied &>> ../reference/append-all.txt", "printf denied <> ../reference/read-write.txt", "printf fd > ../library/fd.txt 2>&1", "> ../reference/pure.txt", "X=y > ../reference/assignment.txt", "printf denied > " + filepath.Join(originals["app"], "original.txt"), "printf app > app.txt", "printf library > ../library/library.txt", "cd ../library && printf relative > relative.txt", "printf denied > ../reference/denied.txt", "printf denied > " + filepath.Join(outside, "denied.txt"), "printf denied > ../library/escape/denied.txt", "printf denied > mixed.txt > " + filepath.Join(outside, "mixed.txt"), "touch ../library/atomic.txt " + filepath.Join(outside, "atomic.txt")}
	script := make([]step, 0, len(commands)+1)
	for _, command := range commands {
		command := command
		script = append(script, func(_ context.Context, messages []ai.Message) (*ai.Response, error) {

			return toolReply(`{"command":` + quoteShellJSON(command) + `}`), nil
		})
	}
	script = append(script, func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		var observed strings.Builder
		for _, message := range messages {
			observed.WriteString(messageContent(message))
		}
		text := observed.String()
		if !strings.Contains(text, "admitted writable copies: "+dirs["library"]) && !strings.Contains(text, "admitted writable copies: "+dirs["app"]+", "+dirs["library"]) {
			t.Error("refusal omitted the complete writable-copy scope")
		}
		_, err := store.Done(task.ID, task.ID, "checked", nil, nil)
		return toolReply(`{"command":"true"}`), err
	})
	worker := run.NewBashWorker(store, dirs["app"], "test/model", &seat{script: script}, run.WithWorkspaceSet(set))
	if _, err := worker.Run(runContext(t), *task); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{filepath.Join(dirs["library"], "move-fd.txt"): "moved", filepath.Join(dirs["library"], "fd.txt"): "fd", filepath.Join(dirs["app"], "app.txt"): "app", filepath.Join(dirs["library"], "library.txt"): "library", filepath.Join(dirs["library"], "relative.txt"): "relative"}
	if primaryRead {
		delete(expected, filepath.Join(dirs["app"], "app.txt"))
	}
	for path, want := range expected {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Errorf("declared write target %s: %q %v", filepath.Base(path), raw, err)
		}
	}
	if primaryRead {
		if _, err := os.Lstat(filepath.Join(dirs["app"], "app.txt")); !os.IsNotExist(err) {
			t.Errorf("readonly primary was written: %v", err)
		}
	}
	for _, path := range []string{filepath.Join(dirs["reference"], "both.txt"), filepath.Join(dirs["reference"], "clobber.txt"), filepath.Join(dirs["reference"], "all.txt"), filepath.Join(dirs["reference"], "append-all.txt"), filepath.Join(dirs["reference"], "read-write.txt"), filepath.Join(dirs["reference"], "pure.txt"), filepath.Join(dirs["reference"], "assignment.txt"), filepath.Join(originals["app"], "original.txt"), filepath.Join(dirs["reference"], "denied.txt"), filepath.Join(outside, "denied.txt"), filepath.Join(outside, "mixed.txt"), filepath.Join(dirs["app"], "mixed.txt"), filepath.Join(dirs["library"], "atomic.txt"), filepath.Join(outside, "atomic.txt")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("unapproved or mixed command wrote %s: %v", filepath.Base(path), err)
		}
	}
}

func quoteShellJSON(value string) string { raw, _ := json.Marshal(value); return string(raw) }
