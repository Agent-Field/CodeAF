package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestOwnedRunIdentityReopensItsReboundCopy(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "example")}
	original := filepath.Join(place.Trees(), "7")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	owned, err := registerOwnedWorkspace(taskTree{dir: original, ground: project, mode: TaskModeMirror}, place)
	if err != nil {
		t.Fatal(err)
	}
	recorded := runCopyOf(owned)
	if recorded.URI == "" || recorded.ChatURI == "" || recorded.ProjectURI == "" {
		t.Fatalf("ownership missing: %#v", recorded)
	}
	moved := filepath.Join(place.Trees(), "relocated")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Bind(recorded.URI, moved); err != nil {
		t.Fatal(err)
	}
	reopened, err := runCopyTree(recorded, place)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.dir != moved || runCopyOf(reopened).URI != recorded.URI {
		t.Fatalf("reopen used stale binding: %#v", reopened)
	}
}

func TestUnknownWorkspaceIdentityCannotFallBackToExistingPath(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	place := Place{Dir: t.TempDir()}
	record := &TaskCopyRecord{Dir: t.TempDir(), URI: "codeaf-workspace://foreign/unknown", Mode: TaskModeMirror}
	if _, err := runCopyTree(record, place); err == nil {
		t.Fatal("unknown identity silently used its path fallback")
	}
}

func TestOwnedIdentityRefusesAnotherConversationAndKeepsLegacyCopies(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "one")}
	path := filepath.Join(place.Trees(), "7")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	owned, err := registerOwnedWorkspace(taskTree{dir: path, ground: t.TempDir(), mode: TaskModeMirror}, place)
	if err != nil {
		t.Fatal(err)
	}
	notice := TaskNotice{ID: 7, State: TaskRunning, Copy: runCopyOf(owned)}
	restored := runRowNotice(runRowRecord(notice)).Copy
	if restored.URI != owned.uri || restored.ChatURI != owned.chatURI || restored.ProjectURI != owned.projectURI {
		t.Fatalf("row lost ownership: %#v", restored)
	}
	if _, err := runCopyTree(restored, Place{Dir: t.TempDir()}); err == nil {
		t.Fatal("another conversation acquired run")
	}
	legacy, err := runCopyTree(&TaskCopyRecord{Dir: path, Mode: TaskModeMirror}, place)
	if err != nil || legacy.dir != path {
		t.Fatalf("legacy copy no longer opens: %#v %v", legacy, err)
	}
}

func TestInPlaceProjectNeverBecomesOwnedRun(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	project := t.TempDir()
	tree, err := registerOwnedWorkspace(taskTree{dir: project, ground: project, mode: TaskModeInPlace}, Place{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if tree.uri != "" || tree.chatURI == "" || tree.projectURI == "" {
		t.Fatalf("project was mislabeled as owned run: %#v", tree)
	}
}

func TestOneConversationCanOwnRunsForDifferentProjects(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "multi")}
	var first taskTree
	for _, id := range []string{"one", "two"} {
		path := filepath.Join(place.Trees(), id)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		tree, err := registerOwnedWorkspace(taskTree{dir: path, ground: t.TempDir(), mode: TaskModeMirror}, place)
		if err != nil {
			t.Fatal(err)
		}
		if first.uri == "" {
			first = tree
			continue
		}
		if tree.chatURI != first.chatURI || tree.projectURI == first.projectURI {
			t.Fatalf("conversation ownership changed between projects: %#v %#v", first, tree)
		}
	}
}

func TestOwnedBeltWorkerUsesRealScratchWithoutChangingHome(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "worker")}
	workspace := filepath.Join(place.Trees(), "7")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	tree, err := registerOwnedWorkspace(taskTree{dir: workspace, ground: t.TempDir(), mode: TaskModeMirror}, place)
	if err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(filepath.Join(place.Dir, planStoreFilename), "scratch proof", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("scratch", "bash", `{"command":"printf '%s\n' \"$HOME\" \"$TMPDIR\" \"$XDG_CACHE_HOME\" \"$CODEAF_WORKSPACE_URI\" > runtime.txt; mktemp >> runtime.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, err := NewBeltWorker(Config{Workspace: workspace, WorkspaceURI: tree.uri, Model: "test/model"}, script, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	drainTurn(t, agent, "Check the temporary directory")
	raw, err := os.ReadFile(filepath.Join(workspace, "runtime.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 {
		t.Fatalf("worker environment missing: %q", raw)
	}
	scratch, cache := lines[1], lines[2]
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	scratchRecord, err := registry.Lookup(workspaceidentity.Temp, scratch)
	if err != nil || scratchRecord.OwnerURI != tree.uri {
		t.Fatalf("temporary directory has wrong owner: %#v %v", scratchRecord, err)
	}
	cacheRecord, err := registry.Lookup(workspaceidentity.Cache, cache)
	if err != nil || cacheRecord.OwnerURI != tree.projectURI {
		t.Fatalf("shared cache has wrong owner: %#v %v", cacheRecord, err)
	}
	if lines[0] != os.Getenv("HOME") || lines[3] != tree.uri || filepath.Dir(lines[4]) != scratch {
		t.Fatalf("worker runtime escaped assigned scratch: %q", raw)
	}
	if _, err := os.Stat(lines[4]); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptedTakeAdoptsOnlyRestoredRunIdentities(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	sourceCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	source := Place{Dir: sourceCell.Root, Workspace: project}
	if err := SaveMeta(source.Dir, Meta{Workspace: project}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source.Trees(), "7")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	owned, err := registerOwnedWorkspace(taskTree{dir: path, ground: project, root: project, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	omittedPath := filepath.Join(source.Trees(), "8")
	if err := os.MkdirAll(omittedPath, 0700); err != nil {
		t.Fatal(err)
	}
	omitted, err := registerOwnedWorkspace(taskTree{dir: omittedPath, ground: project, root: project, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 8, Runs: []runRecord{{ID: 7, Title: "carried task", State: TaskInterrupted, Copy: runCopyOf(owned)}, {ID: 8, Title: "not restored", State: TaskInterrupted, Copy: runCopyOf(omitted)}}}
	if err := newTaskStore(source.Tasks()).writeLocked(document); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	receiverCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	receiverProject := t.TempDir()
	receiver := Place{Dir: receiverCell.Root, Workspace: receiverProject}
	if err := SaveMeta(receiver.Dir, Meta{Workspace: receiverProject}); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(receiver.Trees(), "7")
	if err := os.MkdirAll(restored, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiver.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	incoming := readCheckpoint(t, receiver.Tasks()).Runs[0].Copy
	if _, err := runCopyTree(incoming, receiver); err == nil {
		t.Fatal("receiver resolved foreign copy before accepted take")
	}
	if err := AdoptTaskWorkspaceIdentities(receiver, receiverProject, []string{"7"}); err != nil {
		t.Fatal(err)
	}
	received := readCheckpoint(t, receiver.Tasks())
	if _, err := runCopyTree(received.Runs[1].Copy, receiver); err == nil {
		t.Fatal("omitted task copy gained a local identity")
	}
	adopted := received.Runs[0].Copy
	reopened, err := runCopyTree(adopted, receiver)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.uri != owned.uri || reopened.dir != restored || reopened.root != receiverProject || reopened.ground != receiverProject {
		t.Fatalf("receiver used source bindings: %#v", reopened)
	}
}

func TestRunsShareProjectCacheButKeepSeparateTemporaryFiles(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "shared-cache")}
	firstProject, secondProject := t.TempDir(), t.TempDir()
	var tempPaths, cachePaths []string
	for index, project := range []string{firstProject, firstProject, secondProject} {
		id := fmt.Sprint(index + 1)
		workspace := filepath.Join(place.Trees(), id)
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		tree, err := registerOwnedWorkspace(taskTree{dir: workspace, ground: project, mode: TaskModeMirror}, place)
		if err != nil {
			t.Fatal(err)
		}
		store, err := plandb.Open(filepath.Join(place.Dir, "runs", id, planStoreFilename), "cache proof", id, "", "")
		if err != nil {
			t.Fatal(err)
		}
		script := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				arguments, _ := json.Marshal(map[string]string{"command": `printf '%s\n' "$TMPDIR" "$XDG_CACHE_HOME" > paths.txt`})
				return toolResponse("paths", "bash", string(arguments)), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
		}}
		agent, err := NewBeltWorker(Config{Workspace: workspace, WorkspaceURI: tree.uri, Model: "test/model"}, script, store.Task(id), store.Path(), store.RootID())
		if err != nil {
			store.Close()
			t.Fatal(err)
		}
		drainTurn(t, agent, "Check the assigned cache")
		agent.Close()
		store.Close()
		raw, err := os.ReadFile(filepath.Join(workspace, "paths.txt"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) != 2 {
			t.Fatalf("worker paths: %q", raw)
		}
		tempPaths = append(tempPaths, lines[0])
		cachePaths = append(cachePaths, lines[1])
	}
	if tempPaths[0] == tempPaths[1] || tempPaths[1] == tempPaths[2] {
		t.Fatalf("runs shared temporary files: %#v", tempPaths)
	}
	if cachePaths[0] != cachePaths[1] || cachePaths[0] == cachePaths[2] {
		t.Fatalf("cache scope does not follow project: %#v", cachePaths)
	}
	if rel, err := filepath.Rel(cachedir.Root(), cachePaths[0]); err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("actual worker cache is outside existing cache owner: %q", cachePaths[0])
	}
	marker := filepath.Join(cachePaths[0], "shared-proof.txt")
	if err := os.WriteFile(marker, []byte("reused cache bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := cachedir.Size(); got != int64(len("reused cache bytes")) {
		t.Fatalf("actual worker cache is invisible to cache accounting: %d", got)
	}
	raw, err := os.ReadFile(filepath.Join(cachePaths[1], "shared-proof.txt"))
	if err != nil || string(raw) != "reused cache bytes" {
		t.Fatalf("project cache was not actually shared: %q %v", raw, err)
	}
}

func TestAcceptedTakeKeepsNestedTaskLandingInItsParentCopy(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	sourceCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	source := Place{Dir: sourceCell.Root, Workspace: project}
	if err := SaveMeta(source.Dir, Meta{Workspace: project}); err != nil {
		t.Fatal(err)
	}
	parentPath, childPath := filepath.Join(source.Trees(), "7"), filepath.Join(source.Trees(), "8")
	for _, path := range []string{parentPath, childPath} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := registerOwnedWorkspace(taskTree{dir: parentPath, ground: project, root: project, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	child, err := registerOwnedWorkspace(taskTree{dir: childPath, ground: parentPath, root: parentPath, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	if child.projectURI != parent.projectURI || child.groundURI != parent.uri || child.rootURI != parent.uri {
		t.Fatalf("nested task lost its parent: %#v", child)
	}
	// Reversed rows exercise dependency order rather than depending on checkpoint ordering.
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 8, Runs: []runRecord{{ID: 8, Title: "child", State: TaskInterrupted, Copy: runCopyOf(child)}, {ID: 7, Title: "parent", State: TaskInterrupted, Copy: runCopyOf(parent)}}}
	if err := newTaskStore(source.Tasks()).writeLocked(document); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	receiverCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	receiverProject := t.TempDir()
	receiver := Place{Dir: receiverCell.Root, Workspace: receiverProject}
	if err := SaveMeta(receiver.Dir, Meta{Workspace: receiverProject}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"7", "8"} {
		if err := os.MkdirAll(filepath.Join(receiver.Trees(), id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(receiver.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := AdoptTaskWorkspaceIdentities(receiver, receiverProject, []string{"7", "8"}); err != nil {
		t.Fatal(err)
	}
	record := readCheckpoint(t, receiver.Tasks()).Runs[0].Copy
	reopened, err := runCopyTree(record, receiver)
	if err != nil {
		t.Fatal(err)
	}
	parentOnReceiver := filepath.Join(receiver.Trees(), "7")
	if reopened.ground != parentOnReceiver || reopened.root != parentOnReceiver || reopened.projectURI != parent.projectURI {
		t.Fatalf("nested task would land in original project: %#v", reopened)
	}
}

func TestOwnedCopyCannotLandInAnotherConversationsKnownRun(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	project := t.TempDir()
	places := []Place{{Dir: filepath.Join(home.Dir(), "chats", "one")}, {Dir: filepath.Join(home.Dir(), "chats", "two")}}
	var trees []taskTree
	for _, place := range places {
		path := filepath.Join(place.Trees(), "7")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		tree, err := registerOwnedWorkspace(taskTree{dir: path, ground: project, root: project, mode: TaskModeWorktree}, place)
		if err != nil {
			t.Fatal(err)
		}
		trees = append(trees, tree)
	}
	record := runCopyOf(trees[0])
	record.GroundURI, record.RootURI = trees[1].uri, trees[1].uri
	if _, err := runCopyTree(record, places[0]); err == nil {
		t.Fatal("task adopted another conversation's known landing target")
	}
}

func TestExplicitInPlaceWorkerRoutesScratchWithoutOwningTheProject(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	project := t.TempDir()
	place := Place{Dir: t.TempDir()}
	tree, err := registerOwnedWorkspace(taskTree{dir: project, ground: project, mode: TaskModeInPlace}, place)
	if err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(filepath.Join(place.Dir, planStoreFilename), "in-place runtime", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			arguments, _ := json.Marshal(map[string]string{"command": `pwd > placement.txt; printf '%s\n' "$TMPDIR" "$XDG_CACHE_HOME" "$CODEAF_WORKSPACE_URI" >> placement.txt; mktemp >> placement.txt`})
			return toolResponse("runtime", "bash", string(arguments)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, err := NewBeltWorker(Config{Workspace: project, WorkspaceURI: tree.projectURI, Model: "test/model"}, script, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	drainTurn(t, agent, "Check explicit in-place runtime")
	raw, err := os.ReadFile(filepath.Join(project, "placement.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 || canonicalPath(lines[0]) != project || lines[3] != tree.projectURI || filepath.Dir(lines[4]) != lines[1] {
		t.Fatalf("in-place worker changed placement: %q", raw)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Lookup(workspaceidentity.Run, project); err == nil {
		t.Fatal("explicit project became an owned run")
	}
	for _, binding := range []struct {
		role workspaceidentity.Role
		path string
	}{{workspaceidentity.Temp, lines[1]}, {workspaceidentity.Cache, lines[2]}} {
		record, err := registry.Lookup(binding.role, binding.path)
		if err != nil || record.OwnerURI != tree.projectURI {
			t.Fatalf("project runtime has wrong owner: %#v %v", record, err)
		}
	}
}

func TestAcceptedTakeAdoptsLegacyNodeIdentitiesWithoutRunRows(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(home.EnvVar, t.TempDir())
	sourceCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	source := Place{Dir: sourceCell.Root, Workspace: project}
	if err := SaveMeta(source.Dir, Meta{Workspace: project}); err != nil {
		t.Fatal(err)
	}
	parentPath, childPath := filepath.Join(source.Trees(), "7"), filepath.Join(source.Trees(), "9")
	for _, path := range []string{parentPath, childPath} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := registerOwnedWorkspace(taskTree{dir: parentPath, ground: project, root: project, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	child, err := registerOwnedWorkspace(taskTree{dir: childPath, ground: parentPath, root: parentPath, mode: TaskModeWorktree}, source)
	if err != nil {
		t.Fatal(err)
	}
	recordOf := func(id uint64, tree taskTree) taskRecord {
		return taskRecord{ID: id, Title: "legacy task", Brief: "Preserve this exact brief: /source/reference stays quoted", Acceptance: "files preserved", State: TaskFailed, Mode: tree.mode, Ground: tree.ground, Worktree: tree.dir, WorkspaceURI: tree.uri, ChatURI: tree.chatURI, ProjectURI: tree.projectURI, GroundURI: tree.groundURI, RootURI: tree.rootURI}
	}
	childRecord := recordOf(9, child)
	childRecord.Parent = 7
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 9, Nodes: []taskRecord{recordOf(7, parent), childRecord}}
	if err := newTaskStore(source.Tasks()).writeLocked(document); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	receiverCell, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	receiverProject := t.TempDir()
	receiver := Place{Dir: receiverCell.Root, Workspace: receiverProject}
	if err := SaveMeta(receiver.Dir, Meta{Workspace: receiverProject}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"7", "9"} {
		if err := os.MkdirAll(filepath.Join(receiver.Trees(), id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(receiver.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := AdoptTaskWorkspaceIdentities(receiver, receiverProject, []string{"7", "9"}); err != nil {
		t.Fatal(err)
	}
	adopted := readCheckpoint(t, receiver.Tasks())
	if len(adopted.Runs) != 0 {
		t.Fatal("legacy task received run-copy semantics")
	}
	node := adopted.Nodes[1]
	if node.WorkspaceURI != child.uri || node.Worktree != filepath.Join(receiver.Trees(), "9") || node.Ground != filepath.Join(receiver.Trees(), "7") || node.Brief != childRecord.Brief {
		t.Fatalf("legacy transfer lost structured placement: %#v", node)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if binding, err := registry.Resolve(node.WorkspaceURI); err != nil || binding.Path != node.Worktree {
		t.Fatalf("legacy identity was not admitted: %#v %v", binding, err)
	}
}
