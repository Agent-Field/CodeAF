package run_test

import (
	"context"
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

func TestDelegatedWorkerKeepsWorkspaceRoutesAndScopedRulesWithoutPrototypeFlag(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	chatDir := t.TempDir()
	parent := filepath.Join(chatDir, "trees", "1")
	app, library := filepath.Join(parent, "app"), filepath.Join(parent, "library")
	for _, dir := range []string{app, library} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(app, "AGENTS.md"), []byte("APP_SCOPE_RULE: use app conventions."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "AGENTS.md"), []byte("LIBRARY_SCOPE_RULE: use library conventions."), 0600); err != nil {
		t.Fatal(err)
	}
	store := runOpenStore(t)
	tasks, err := store.AddMany([]plandb.TaskSpec{{ID: "child", ParentID: store.RootID(), Title: "small delegated step", Description: "inspect the dependency"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Claim(tasks[0].ID, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	set := &session.WorkspaceSet{Version: 1, Primary: "app", Roots: []session.WorkspaceRootUse{
		{Slot: "app", Access: session.WorkspaceWrite, Copy: &session.TaskCopyRecord{Dir: app}},
		{Slot: "library", Access: session.WorkspaceRead, Copy: &session.TaskCopyRecord{Dir: library}},
	}}
	registry, err := workspaceidentity.Open(filepath.Join(profile, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, chatDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range set.Roots {
		root := &set.Roots[i]
		project, err := registry.Register(workspaceidentity.Project, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		record, err := registry.Register(workspaceidentity.Run, root.Copy.Dir, workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: project.URI})
		if err != nil {
			t.Fatal(err)
		}
		root.Copy.URI = record.URI
		root.Copy.ChatURI = chat.URI
		root.Copy.ProjectURI = project.URI
	}
	var requested string
	provider := &seat{script: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		var content strings.Builder
		for _, message := range messages {
			content.WriteString(messageContent(message))
		}
		requested = content.String()
		if _, err := store.Done(task.ID, task.ID, "inspected", nil, nil); err != nil {
			return nil, err
		}
		return toolReply(`{"command":"true"}`), nil
	}}}
	worker := run.NewBashWorker(store, app, "test/model", provider, run.WithWorkspaceSet(set))
	// A caller cannot change a seated worker's admitted routes afterward.
	set.Roots[1].Copy.Dir = "/fixture/unadmitted-mutation"
	if _, err := worker.Run(runContext(t), *task); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{library, "APP_SCOPE_RULE", "LIBRARY_SCOPE_RULE", "not an operating-system sandbox"} {
		if !strings.Contains(requested, want) {
			t.Errorf("delegated request omitted %q", want)
		}
	}
	if strings.Contains(requested, "/fixture/unadmitted-mutation") {
		t.Fatal("worker routes alias caller-owned metadata")
	}
	if !strings.Contains(requested, "UNDER "+library+" AND NOWHERE ELSE") {
		t.Fatal("secondary rules lost their local scope")
	}
}
